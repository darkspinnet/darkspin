package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const (
	campaignNPCDiseaseSpreadRadius   = float32(5)
	campaignNPCDiseaseImmunityPeriod = 7 * time.Second
)

type campaignNPCDiseaseRun struct {
	activeObjectIDs map[uint32]struct{}
	immuneObjectIDs map[uint32]struct{}
}

func newCampaignNPCDiseaseRun() *campaignNPCDiseaseRun {
	return &campaignNPCDiseaseRun{
		activeObjectIDs: make(map[uint32]struct{}),
		immuneObjectIDs: make(map[uint32]struct{}),
	}
}

func (e *campaignNPCDiseaseRun) admit(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	if _, isActive := e.activeObjectIDs[objectID]; isActive {
		return false
	}
	if _, isImmune := e.immuneObjectIDs[objectID]; isImmune {
		return false
	}
	e.activeObjectIDs[objectID] = struct{}{}
	return true
}

func (e *campaignNPCDiseaseRun) canAdmit(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	if _, isActive := e.activeObjectIDs[objectID]; isActive {
		return false
	}
	_, isImmune := e.immuneObjectIDs[objectID]
	return !isImmune
}

func (e *campaignNPCDiseaseRun) expire(objectID uint32) {
	if e == nil || objectID == 0 {
		return
	}
	delete(e.activeObjectIDs, objectID)
	e.immuneObjectIDs[objectID] = struct{}{}
}

func (e *campaignNPCDiseaseRun) rollback(objectID uint32) {
	if e == nil || objectID == 0 {
		return
	}
	delete(e.activeObjectIDs, objectID)
}

type campaignNPCPoisonRun struct {
	key        campaignNPCPoisonKey
	modifier   *campaignNPCModifierRun
	packet     raknet.Packet
	plan       zonenpc.AttackPlan
	binding    game.GameplayBinding
	tickDamage game.DamageRange
	revision   uint64
	stackCount uint32
	cancel     raknet.CancelSchedule
	disease    *campaignNPCDiseaseRun
}

type campaignNPCPoisonKey struct {
	targetObjectID uint32
	modifierName   string
}

type campaignNPCPoisonStep struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	timestamp  uint64
	tickIndex  uint32
	revision   uint64
	run        *campaignNPCPoisonRun
}

func (e campaignNPCPoisonStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCPoisons[e.run.key] == e.run &&
		e.run.revision == e.revision
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	profile := e.run.plan.Profile
	if peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("enemy poison random unavailable")
	}
	selectedDamage, err := sim.SelectRankDamage(
		peerSession.zone.NPCRandom(),
		sim.DamageRange{
			Minimum: e.run.tickDamage.Minimum,
			Maximum: e.run.tickDamage.Maximum,
		},
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyPoisonSelect: %w", err)
	}
	result := zonenpc.AttackResult{
		Damage: selectedDamage * float32(e.run.stackCount),
	}
	packets, statDelta, isApplied, err := e.runtime.applyEnemyStatusDamage(
		&peerSession, e.generation, e.run.plan, result, e.timestamp,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyPoisonDamage: %w", err)
	}
	isFinal := time.Duration(e.tickIndex)*profile.ModifierTickDuration >=
		profile.ModifierDuration
	if isFinal {
		delete(peerSession.campaignNPCPoisons, e.run.key)
		peerSession.untrackCampaignNPCModifier(e.run.modifier)
		if e.run.disease != nil {
			e.run.disease.expire(e.run.plan.TargetObjectID)
			if peerSession.campaignNPCDiseaseImmunityEnds == nil {
				peerSession.campaignNPCDiseaseImmunityEnds = make(map[uint32]uint64)
			}
			peerSession.campaignNPCDiseaseImmunityEnds[e.run.plan.TargetObjectID] =
				e.timestamp + uint64(campaignNPCDiseaseImmunityPeriod/time.Millisecond)
		}
	}
	spreadPlan := make([]zonenpc.AttackPlan, 0)
	isDiseaseSpreader := profile.ModifierName == "LifePlagueSpread" ||
		profile.ModifierName == "VerdanthBossPrimaryPlague"
	if isApplied && e.run.disease != nil && isDiseaseSpreader {
		target, isTargetFound := peerSession.campaignNPCTarget(
			e.generation, e.run.plan.TargetObjectID,
		)
		if isTargetFound {
			for _, candidate := range peerSession.zone.LiveNPCTargets() {
				if candidate.ObjectID == target.ObjectID || candidate.HitPoint <= 0 ||
					!e.run.disease.canAdmit(candidate.ObjectID) ||
					zonegeometry.Distance(target.Position, candidate.Position) >
						campaignNPCDiseaseSpreadRadius {
					continue
				}
				plan := e.run.plan
				plan.TargetObjectID = candidate.ObjectID
				plan.TargetPosition = candidate.Position
				if profile.ModifierName == "VerdanthBossPrimaryPlague" {
					plan.Profile.ModifierName = "VerdanthBossSecondaryPlague"
				}
				spreadPlan = append(spreadPlan, plan)
			}
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	for index, plan := range spreadPlan {
		spreadPackets, spreadErr := e.runtime.applyCampaignNPCDisease(
			e.run.disease, e.run.packet, e.sessionKey, e.generation, plan, e.timestamp,
		)
		if spreadErr != nil {
			return nil, fmt.Errorf("enemyPoisonSpread[%d]: %w", index, spreadErr)
		}
		packets = append(packets, spreadPackets...)
	}

	if isFinal {
		isCreated, releaseErr := e.run.modifier.release(e.runtime.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("enemyPoisonRelease: %w", releaseErr)
		}
		if isCreated {
			deletePacket, deleteErr := effectraknet.ModifierDelete(
				e.run.plan.TargetObjectID, e.run.modifier.instanceID,
			)
			if deleteErr != nil {
				return nil, fmt.Errorf("enemyPoisonDelete: %w", deleteErr)
			}
			packets = append(packets, deletePacket)
		}
	}
	err = e.runtime.stats.Record(context.Background(), e.run.binding, statDelta)
	if err != nil {
		return nil, fmt.Errorf("enemyPoisonStats: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) applyCampaignNPCPoison(
	packet raknet.Packet,
	sessionKey string,
	generation uint64,
	plan zonenpc.AttackPlan,
	timestamp uint64,
) ([][]byte, error) {
	return r.applyCampaignNPCDisease(
		nil, packet, sessionKey, generation, plan, timestamp,
	)
}

func (r campaignNPCActionRuntime) applyCampaignNPCDisease(
	disease *campaignNPCDiseaseRun,
	packet raknet.Packet,
	sessionKey string,
	generation uint64,
	plan zonenpc.AttackPlan,
	timestamp uint64,
) ([][]byte, error) {
	profile := plan.Profile
	if sessionKey == "" || generation == 0 ||
		plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		!isCampaignNPCDamageOverTimeProfile(profile) ||
		profile.ModifierDuration%profile.ModifierTickDuration != 0 {
		return nil, errors.New("enemy damage over time request invalid")
	}

	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	_, isTargetFound := peerSession.campaignNPCTarget(
		generation, plan.TargetObjectID,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if isCampaignNPCDiseaseProfile(profile) &&
		timestamp < peerSession.campaignNPCDiseaseImmunityEnds[plan.TargetObjectID] {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if r.isTargetDebuffImmuneLocked(&peerSession, plan.TargetObjectID) {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(plan.SourceObjectID)
	if !isSourceFound {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	tickDamage := game.DamageRange{
		Minimum: profile.ModifierTickDamage,
		Maximum: profile.ModifierTickDamage,
	}
	if profile.ModifierMinimumTickDamage > 0 {
		var err error
		tickDamage, err = zonenpc.ResolveModifierTickDamage(source, profile)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyPoisonTickDamage: %w", err)
		}
	}
	if peerSession.campaignNPCPoisons == nil {
		peerSession.campaignNPCPoisons = make(map[campaignNPCPoisonKey]*campaignNPCPoisonRun)
	}
	key := campaignNPCPoisonKey{
		targetObjectID: plan.TargetObjectID, modifierName: profile.ModifierName,
	}
	previous := peerSession.campaignNPCPoisons[key]
	if previous != nil && isCampaignNPCDiseaseProfile(profile) {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	isNew := previous == nil
	run := previous
	if isNew {
		if isCampaignNPCDiseaseProfile(profile) {
			if disease == nil {
				disease = newCampaignNPCDiseaseRun()
			}
			if !disease.admit(plan.TargetObjectID) {
				r.registry.mutex.Unlock()
				return nil, nil
			}
		}
		modifier, err := newCampaignNPCModifierRun(r.modifierPool)
		if err != nil {
			if disease != nil {
				disease.rollback(plan.TargetObjectID)
			}
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyPoisonRun: %w", err)
		}
		run = &campaignNPCPoisonRun{
			key: key, modifier: modifier, binding: peerSession.binding,
			packet: packet, disease: disease,
		}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			if disease != nil {
				disease.rollback(plan.TargetObjectID)
			}
			r.registry.mutex.Unlock()
			_, releaseErr := modifier.release(r.modifierPool)
			return nil, fmt.Errorf(
				"enemyPoisonTrack: %w", errors.Join(err, releaseErr),
			)
		}
	}
	previousPlan := run.plan
	previousTickDamage := run.tickDamage
	previousRevision := run.revision
	previousStackCount := run.stackCount
	previousCancel := run.cancel
	tickPlan := plan
	if profile.IsModifierDamageProfileKnown {
		tickPlan.Profile.DescriptorMask = profile.ModifierDescriptorMask
		tickPlan.Profile.DamageType = profile.ModifierDamageType
		tickPlan.Profile.DamageSource = profile.ModifierDamageSource
		tickPlan.Profile.IsDamageProfileKnown = true
	}
	run.plan = tickPlan
	run.tickDamage = tickDamage
	run.revision++
	run.stackCount = min(profile.ModifierMaximumStack, run.stackCount+1)
	revision := run.revision
	stackCount := run.stackCount
	peerSession.campaignNPCPoisons[key] = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: plan.SourceObjectID,
			TargetObjectID: plan.TargetObjectID,
			ModifierID:     profile.ModifierGUID(),
			InstanceID:     run.modifier.instanceID,
			StackCount:     stackCount,
			Duration:       profile.ModifierDuration,
			Timestamp:      timestamp,
		},
	)
	if err != nil {
		r.rollbackCampaignNPCPoison(
			sessionKey, generation, run, isNew, previousPlan,
			previousTickDamage, previousRevision, revision,
			previousStackCount, previousCancel,
		)
		return nil, fmt.Errorf("enemyPoisonCreate: %w", err)
	}

	tickCount := uint32(profile.ModifierDuration / profile.ModifierTickDuration)
	producers := make([]raknet.ScheduledPacketProducer, 0, tickCount)
	for tickIndex := uint32(1); tickIndex <= tickCount; tickIndex++ {
		delay := time.Duration(tickIndex) * profile.ModifierTickDuration
		step := campaignNPCPoisonStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			timestamp: timestamp + uint64(delay/time.Millisecond),
			tickIndex: tickIndex, revision: revision, run: run,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: delay, Produce: step.produce,
		})
	}
	cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.rollbackCampaignNPCPoison(
			sessionKey, generation, run, isNew, previousPlan,
			previousTickDamage, previousRevision, revision,
			previousStackCount, previousCancel,
		)
		return nil, fmt.Errorf("enemyPoisonSchedule: %w", scheduleErr)
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.campaignNPCPoisons[key] == run &&
		run.revision == revision
	if isLatest {
		run.cancel = cancel
		r.registry.sessions[sessionKey] = latest
	}
	r.registry.mutex.Unlock()
	if !isLatest {
		cancel()
		if isNew && run.disease != nil {
			r.registry.mutex.Lock()
			run.disease.rollback(plan.TargetObjectID)
			r.registry.mutex.Unlock()
		}
		return nil, nil
	}
	if previousCancel != nil {
		previousCancel()
	}
	if isNew {
		run.modifier.create()
	}
	return [][]byte{createPacket}, nil
}

func isCampaignNPCDamageOverTimeProfile(profile zonenpc.ActionProfile) bool {
	return profile.ModifierName != "" && profile.ModifierDuration > 0 &&
		profile.ModifierTickDuration > 0 && profile.ModifierMaximumStack > 0 &&
		(profile.ModifierTickDamage > 0 || profile.ModifierMinimumTickDamage > 0)
}

func isCampaignNPCDiseaseProfile(profile zonenpc.ActionProfile) bool {
	return profile.ModifierName == "LifePlagueSpread" ||
		profile.ModifierName == "VerdanthBossPrimaryPlague" ||
		profile.ModifierName == "VerdanthBossSecondaryPlague"
}

func (r campaignNPCActionRuntime) rollbackCampaignNPCPoison(
	sessionKey string,
	generation uint64,
	run *campaignNPCPoisonRun,
	isNew bool,
	previousPlan zonenpc.AttackPlan,
	previousTickDamage game.DamageRange,
	previousRevision uint64,
	revision uint64,
	previousStackCount uint32,
	previousCancel raknet.CancelSchedule,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.campaignNPCPoisons[run.key] == run &&
		run.revision == revision
	if isCurrent {
		if isNew {
			delete(peerSession.campaignNPCPoisons, run.key)
			peerSession.untrackCampaignNPCModifier(run.modifier)
			if run.disease != nil {
				run.disease.rollback(run.plan.TargetObjectID)
			}
		} else {
			run.plan = previousPlan
			run.tickDamage = previousTickDamage
			run.revision = previousRevision
			run.stackCount = previousStackCount
			run.cancel = previousCancel
		}
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isNew && isCurrent {
		_, _ = run.modifier.release(r.modifierPool)
	}
}
