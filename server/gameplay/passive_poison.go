package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const (
	poisonRavagerPassiveName = "LFPoisonRavager_Passive"
	poisonRavagerModifierID  = uint32(0x01ad5389)
	poisonRavagerStackLimit  = uint32(4)
	poisonRavagerTickCount   = uint32(3)
)

type heroPassivePoisonRun struct {
	modifier       *campaignNPCModifierRun
	binding        game.GameplayBinding
	creature       game.GameplayCreature
	sourceObjectID uint32
	targetObjectID uint32
	revision       uint64
	stackCount     uint32
	completedTick  uint32
	cancel         raknet.CancelSchedule
}

type heroPassivePoisonStep struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	tickIndex  uint32
	revision   uint64
	run        *heroPassivePoisonRun
}

func poisonRavagerDefinition(stackCount uint32) sim.AbilityDefinition {
	return sim.AbilityDefinition{
		Name: "LFPoisonRavager_PassivePoison", Kind: sim.AbilityKindModifier,
		MinimumDamage:     2 * float32(stackCount),
		MaximumDamage:     2 * float32(stackCount),
		DamageCoefficient: 0.05 * float32(stackCount),
		DescriptorMask:    36, DamageType: 2, DamageSource: 1,
		IsDescriptorFound: true, IsDamageTypeFound: true,
		IsDamageSourceFound: true,
	}
}

func (e heroPassivePoisonStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.heroPassivePoisons[e.run.targetObjectID] == e.run &&
		e.run.revision == e.revision && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.run.targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		peerSession.removeHeroPassivePoison(e.run)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.release()
	}
	definition := poisonRavagerDefinition(e.run.stackCount)
	damage, err := zoneability.ProjectDamage(
		e.run.creature, definition,
		definition.MinimumDamage, definition.MaximumDamage,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("passivePoisonDamage: %w", err)
	}
	damageResult, err := peerSession.zone.NPCs().Hit(zonenpc.HitRequest{
		SourceObjectID: e.run.sourceObjectID,
		TargetObjectID: e.run.targetObjectID,
		Damage:         damage.Minimum,
		IsPeriodic:     true,
		Metadata:       zoneability.NPCDamageMetadata(definition),
	})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("passivePoisonApply: %w", err)
	}
	e.run.completedTick = e.tickIndex
	transition, err := peerSession.applyCampaignDamageTransition(damageResult)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("passivePoisonTransition: %w", err)
	}
	isFinal := e.tickIndex == poisonRavagerTickCount || damageResult.IsDefeated
	if isFinal {
		peerSession.removeHeroPassivePoison(e.run)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets, err := e.runtime.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.run.sourceObjectID,
		e.timestamp, e.run.binding,
		[]zoneability.AreaResult{{
			Snapshot: target, Damage: damageResult, Definition: definition,
		}}, []campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("passivePoisonPublish: %w", err)
	}
	if !isFinal {
		return packets, nil
	}
	releasePackets, err := e.release()
	if err != nil {
		return nil, err
	}
	return append(packets, releasePackets...), nil
}

func (e heroPassivePoisonStep) release() ([][]byte, error) {
	isCreated, err := e.run.modifier.release(e.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("passivePoisonRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	deletePacket, err := effectraknet.ModifierDelete(
		e.run.targetObjectID, e.run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("passivePoisonDelete: %w", err)
	}
	return [][]byte{deletePacket}, nil
}

func (s *gameplayPeerSession) removeHeroPassivePoison(
	run *heroPassivePoisonRun,
) {
	if s == nil || run == nil || s.heroPassivePoisons[run.targetObjectID] != run {
		return
	}
	delete(s.heroPassivePoisons, run.targetObjectID)
	s.untrackCampaignNPCModifier(run.modifier)
}

func (r campaignDamageRuntime) applyPoisonRavagerPassive(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, result zoneability.AreaResult,
) ([][]byte, error) {
	if result.Definition.Name == "" || result.Definition.DescriptorMask&4 != 0 ||
		result.Damage.IsDamageImmune || result.Damage.IsDefeated ||
		result.Damage.Damage <= 0 {
		return nil, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.deployedObjectID == sourceObjectID &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	if creature.PassiveAbility != util.HashID(poisonRavagerPassiveName) {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	_, isTargetFound := peerSession.zone.NPCs().NPC(result.Damage.ObjectID)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.heroPassivePoisons == nil {
		peerSession.heroPassivePoisons = make(map[uint32]*heroPassivePoisonRun)
	}
	previous := peerSession.heroPassivePoisons[result.Damage.ObjectID]
	isNew := previous == nil
	run := previous
	if isNew {
		modifier, err := newCampaignNPCModifierRun(r.npc.modifierPool)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("passivePoisonRun: %w", err)
		}
		run = &heroPassivePoisonRun{
			modifier: modifier, targetObjectID: result.Damage.ObjectID,
		}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			r.registry.mutex.Unlock()
			_, releaseErr := modifier.release(r.npc.modifierPool)
			return nil, fmt.Errorf(
				"passivePoisonTrack: %w", errors.Join(err, releaseErr),
			)
		}
	}
	previousRevision := run.revision
	previousStackCount := run.stackCount
	previousCompletedTick := run.completedTick
	previousCancel := run.cancel
	run.binding = peerSession.binding
	run.creature = creature
	run.sourceObjectID = sourceObjectID
	run.revision++
	run.stackCount = min(poisonRavagerStackLimit, run.stackCount+1)
	run.completedTick = 0
	revision := run.revision
	stackCount := run.stackCount
	peerSession.heroPassivePoisons[run.targetObjectID] = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: sourceObjectID, TargetObjectID: run.targetObjectID,
			ModifierID: poisonRavagerModifierID, InstanceID: run.modifier.instanceID,
			StackCount: stackCount, Duration: 3 * time.Second, Timestamp: timestamp,
		},
	)
	if err != nil {
		r.rollbackPoisonRavager(
			sessionKey, generation, run, isNew, previousRevision, revision,
			previousStackCount, previousCancel,
			previousCompletedTick,
		)
		return nil, fmt.Errorf("passivePoisonCreate: %w", err)
	}
	producer := make([]raknet.ScheduledPacketProducer, 0, poisonRavagerTickCount)
	for tickIndex := uint32(1); tickIndex <= poisonRavagerTickCount; tickIndex++ {
		delay := time.Duration(tickIndex) * time.Second
		step := heroPassivePoisonStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, timestamp: timestamp + uint64(delay/time.Millisecond),
			tickIndex: tickIndex, revision: revision, run: run,
		}
		producer = append(producer, raknet.ScheduledPacketProducer{
			Delay: delay, Produce: step.produce,
		})
	}
	cancel, scheduleErr := packet.ScheduleProducers(producer)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.rollbackPoisonRavager(
			sessionKey, generation, run, isNew, previousRevision, revision,
			previousStackCount, previousCancel,
			previousCompletedTick,
		)
		return nil, fmt.Errorf("passivePoisonSchedule: %w", scheduleErr)
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.heroPassivePoisons[run.targetObjectID] == run &&
		run.revision == revision
	if isLatest {
		run.cancel = cancel
		r.registry.sessions[sessionKey] = latest
	}
	r.registry.mutex.Unlock()
	if !isLatest {
		cancel()
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

func (r campaignDamageRuntime) rollbackPoisonRavager(
	sessionKey string, generation uint64, run *heroPassivePoisonRun,
	isNew bool, previousRevision uint64, revision uint64,
	previousStackCount uint32, previousCancel raknet.CancelSchedule,
	previousCompletedTick uint32,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.heroPassivePoisons[run.targetObjectID] == run &&
		run.revision == revision
	if isCurrent {
		if isNew {
			peerSession.removeHeroPassivePoison(run)
		} else {
			run.revision = previousRevision
			run.stackCount = previousStackCount
			run.cancel = previousCancel
			run.completedTick = previousCompletedTick
		}
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isNew && isCurrent {
		_, _ = run.modifier.release(r.npc.modifierPool)
	}
}
