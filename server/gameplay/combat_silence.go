package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type campaignNPCSilenceRun struct {
	modifier  *campaignNPCModifierRun
	targetID  uint32
	expiresAt time.Time
	revision  uint64
	cancel    raknet.CancelSchedule
}

type campaignNPCSilenceExpiryStep struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	revision   uint64
	run        *campaignNPCSilenceRun
	zone       *zone.Zone
}

func (e campaignNPCSilenceExpiryStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCSilences[e.run.targetID] == e.run &&
		e.run.revision == e.revision
	if isCurrent {
		delete(peerSession.campaignNPCSilences, e.run.targetID)
		if peerSession.deployedObjectID == e.run.targetID && peerSession.enemySilenceExpiresAt == e.run.expiresAt {
			peerSession.enemySilenceExpiresAt = time.Time{}
		}
		peerSession.untrackCampaignNPCModifier(e.run.modifier)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	isCreated, err := e.run.modifier.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemySilenceRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.run.targetID, e.run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySilenceDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r campaignNPCActionRuntime) applyCampaignNPCSilence(
	packet raknet.Packet,
	sessionKey string,
	generation uint64,
	plan zonenpc.AttackPlan,
	timestamp uint64,
) ([][]byte, error) {
	if r.registry == nil || r.registry.timer == nil || r.now == nil {
		return nil, errors.New("silence runtime unavailable")
	}
	profile := plan.Profile
	isRangedSilence := profile.ModifierName ==
		"NocturnaBasicRanged_SilenceModifier" &&
		profile.ModifierDuration == 2*time.Second
	isSuppressionAura := profile.ModifierName == "SilenceModifier" &&
		profile.ModifierDuration == time.Second
	if sessionKey == "" || generation == 0 || plan.SourceObjectID == 0 ||
		plan.TargetObjectID == 0 || (!isRangedSilence && !isSuppressionAura) {
		return nil, errors.New("enemy silence request invalid")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, plan.TargetObjectID,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if target.IsHero {
		targetSession, targetSessionKey := r.heroTargetSession(&peerSession, target)
		if targetSession == nil {
			r.registry.mutex.Unlock()
			return nil, nil
		}
		peerSession = *targetSession
		if targetSessionKey != "" {
			sessionKey = targetSessionKey
			generation = peerSession.generation
		}
	}
	if peerSession.isHeroDebuffImmune(plan.TargetObjectID) {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCSilences == nil {
		peerSession.campaignNPCSilences = make(map[uint32]*campaignNPCSilenceRun)
	}
	previous := peerSession.campaignNPCSilences[plan.TargetObjectID]
	isNew := previous == nil
	run := previous
	if isNew {
		modifier, err := newCampaignNPCModifierRun(r.modifierPool)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemySilenceRun: %w", err)
		}
		run = &campaignNPCSilenceRun{
			modifier: modifier, targetID: plan.TargetObjectID,
		}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			r.registry.mutex.Unlock()
			_, releaseErr := modifier.release(r.modifierPool)
			return nil, fmt.Errorf(
				"enemySilenceTrack: %w", errors.Join(err, releaseErr),
			)
		}
	}
	previousExpiresAt := run.expiresAt
	previousRevision := run.revision
	previousCancel := run.cancel
	previousHeroExpiresAt := peerSession.enemySilenceExpiresAt
	startedAt := r.now()
	run.expiresAt = startedAt.Add(profile.ModifierDuration)
	if previousExpiresAt.After(run.expiresAt) {
		run.expiresAt = previousExpiresAt
	}
	run.revision++
	revision := run.revision
	expiresAt := run.expiresAt
	peerSession.campaignNPCSilences[plan.TargetObjectID] = run
	if plan.TargetObjectID == peerSession.deployedObjectID {
		peerSession.extendEnemySilence(plan.TargetObjectID, run.expiresAt)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: plan.SourceObjectID,
			TargetObjectID: plan.TargetObjectID,
			ModifierID:     util.HashID(profile.ModifierName),
			InstanceID:     run.modifier.instanceID,
			Duration:       expiresAt.Sub(startedAt),
			Timestamp:      timestamp,
		},
	)
	if err != nil {
		r.rollbackNocturnaBasicRangedSilence(
			sessionKey, generation, run, isNew, previousExpiresAt,
			previousRevision, revision, previousCancel, previousHeroExpiresAt,
		)
		return nil, fmt.Errorf("enemySilenceCreate: %w", err)
	}
	expiryStep := campaignNPCSilenceExpiryStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		revision: revision, run: run, zone: peerSession.zone,
	}
	cancel, scheduleErr := r.registry.timer.Schedule(max(time.Duration(0), expiresAt.Sub(r.now())), expiryStep.expire)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.rollbackNocturnaBasicRangedSilence(
			sessionKey, generation, run, isNew, previousExpiresAt,
			previousRevision, revision, previousCancel, previousHeroExpiresAt,
		)
		return nil, fmt.Errorf("enemySilenceSchedule: %w", scheduleErr)
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.campaignNPCSilences[plan.TargetObjectID] == run &&
		run.revision == revision
	if isLatest && !run.modifier.isActive() {
		isLatest = run.modifier.create()
	}
	if isLatest {
		run.cancel = raknet.CancelSchedule(cancel)
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
	return [][]byte{createPacket}, nil
}

func (r campaignNPCActionRuntime) rollbackNocturnaBasicRangedSilence(
	sessionKey string,
	generation uint64,
	run *campaignNPCSilenceRun,
	isNew bool,
	previousExpiresAt time.Time,
	previousRevision uint64,
	revision uint64,
	previousCancel raknet.CancelSchedule,
	previousHeroExpiresAt time.Time,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.campaignNPCSilences[run.targetID] == run &&
		run.revision == revision
	if isCurrent {
		if peerSession.deployedObjectID == run.targetID && peerSession.enemySilenceExpiresAt == run.expiresAt {
			peerSession.enemySilenceExpiresAt = previousHeroExpiresAt
		}
		if isNew {
			delete(peerSession.campaignNPCSilences, run.targetID)
			peerSession.untrackCampaignNPCModifier(run.modifier)
		} else {
			run.expiresAt = previousExpiresAt
			run.revision = previousRevision
			run.cancel = previousCancel
		}
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isNew && isCurrent {
		isCreated, releaseErr := run.modifier.release(r.modifierPool)
		if releaseErr != nil && r.logger != nil {
			r.logger.Printf("RakNet silence rollback release failed target=%d created=%t: %v", run.targetID, isCreated, releaseErr)
		}
	}
}

// Expiry belongs to the affected hero's session and the shared timer, so an
// unrelated NPC owner's transport cannot cancel it or consume its packets.
func (e campaignNPCSilenceExpiryStep) expire() {
	packets, err := e.produce()
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet silence expiry failed target=%d: %v", e.run.targetID, err)
		}
		return
	}
	if len(packets) == 0 {
		return
	}
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != e.zone || candidate.isZoneTerminal() || !candidate.stage.IsDungeon() {
			continue
		}
		publishErr := candidate.publishPackets(packets)
		e.runtime.registry.sessions[sessionKey] = candidate
		if publishErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet silence expiry queued target=%d user=%d: %v", e.run.targetID, candidate.binding.UserID, publishErr)
		}
	}
}
