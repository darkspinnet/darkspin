package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Stored on the generic modifier owner so every accepted lifecycle path sees
// the same raw namespace, source snapshot and exact instance.
type campaignFixtureBurnState struct {
	blast               campaignFixtureBlastStep
	profile             zonenpc.ActionProfile
	npc                 *zonenpc.Session
	sourceProfile       game.DamageProfile
	expiresAt           time.Time
	startedAt           time.Time
	inventory           *zoneeffect.Inventory
	scheduleGeneration  uint64
	handoffGeneration   uint64
	publication         *campaignFixturePublication
	cursor              campaignFixtureBurnStep
	isCursorReady       bool
	isCreationPublished bool
	isRetiring          bool
	pendingDeletes      [][]byte
}

// The generic modifier release holds its own mutex while calling this hook.
func (e *campaignNPCModifierRun) retireFixtureInfection() {
	if e.fixtureBurn == nil {
		return
	}
	state := e.fixtureBurn
	state.removeInventory(e.instanceID, e.record.TargetObjectID)
	if !e.isCreated || state.profile.ModifierName != "LifePlagueSpread" {
		return
	}
	state.npc.RetireInfection(e.record.TargetObjectID, e.instanceID, false, state.blast.runtime.npc.now())
}

func (e *campaignFixtureBurnState) removeInventory(instanceID, targetID uint32) {
	removed, isRemoved := e.inventory.Remove(instanceID)
	if isRemoved && removed.TargetObjectID != targetID {
		e.blast.runtime.logger.Printf("Fixture inventory target mismatch instance=%d target=%d removed=%d", instanceID, targetID, removed.TargetObjectID)
	}
}

type campaignFixtureBurnStep struct {
	blast              campaignFixtureBlastStep
	run                *campaignNPCModifierRun
	targetID           uint32
	profile            zonenpc.ActionProfile
	remainingTickCount uint32
	isPermanent        bool
	deadline           time.Duration
	handoffGeneration  uint64
}

func (e campaignFixtureBlastStep) startBurn(targetID uint32, profile zonenpc.ActionProfile) ([][]byte, error) {
	packets, err := e.startBurnWithOrigin(targetID, profile, nil)
	if err != nil {
		return nil, fmt.Errorf("fixtureBurnDirect: %w", err)
	}
	return packets, nil
}

func (e campaignFixtureBlastStep) startContagiousBurn(targetID uint32, origin *campaignNPCModifierRun) ([][]byte, error) {
	packets, err := e.startBurnWithOrigin(targetID, origin.fixtureBurn.profile, origin)
	if err != nil {
		return nil, fmt.Errorf("fixtureBurnSpread: %w", err)
	}
	return packets, nil
}

func (e campaignFixtureBlastStep) startBurnWithOrigin(targetID uint32, profile zonenpc.ActionProfile, origin *campaignNPCModifierRun) ([][]byte, error) {
	if e.publication == nil {
		return nil, errors.New("fixture publication unavailable")
	}
	r := e.runtime
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.isZoneTerminal() || r.registry.isFixtureDiscardingLocked(member.binding.GameID) {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if origin != nil && (member.campaignNPCModifiers[origin.instanceID] != origin || !origin.isActive() || origin.fixtureBurn.isRetiring ||
		member.zone.NPCs().IsInfectionSpreadBlocked(targetID, r.npc.now())) {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := member.zone.NPCs().LiveNPC(targetID)
	if !isTargetFound || !target.IsPublished {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	modifierID := util.HashID(profile.ModifierName)
	for _, candidate := range r.registry.sessions {
		if candidate.zone != member.zone {
			continue
		}
		for _, current := range candidate.campaignNPCModifiers {
			if profile.ModifierName != "LifePlagueSpread" && current != nil && current.isActive() &&
				current.record.TargetObjectID == targetID && current.record.GUID == modifierID && current.record.SourceObjectID == e.source.Plan.ObjectID {
				r.registry.mutex.Unlock()
				return nil, nil
			}
		}
	}
	run, err := newCampaignNPCModifierRun(r.npc.modifierPool)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fixtureBurnAllocate: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: modifierID,
		SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: targetID,
		Rank: 1, Duration: profile.ModifierDuration, Kind: zoneeffect.ModifierKindDebuff,
		InitiatorObject: e.source.Plan.ObjectID,
	}
	blast := e
	blast.publication = nil
	startedAt := r.npc.now()
	run.fixtureBurn = &campaignFixtureBurnState{
		blast: blast, profile: profile, npc: member.zone.NPCs(),
		sourceProfile: zonenpc.DamageProfileAt(e.source, startedAt),
		expiresAt:     startedAt.Add(profile.ModifierDuration), startedAt: startedAt,
		inventory: member.zone.Effect(), handoffGeneration: 1, publication: e.publication,
	}
	run.record.StackCount = 1
	run.record.IsIndefinite = profile.ModifierDuration == 0
	if !run.record.IsIndefinite {
		run.record.ExpiresAt = run.fixtureBurn.expiresAt
	}
	if origin != nil {
		run.fixtureBurn.sourceProfile = origin.fixtureBurn.sourceProfile
	}
	err = member.trackCampaignNPCModifier(run)
	if err == nil {
		r.registry.sessions[e.sessionKey] = member
	}
	r.registry.mutex.Unlock()
	if err != nil {
		isCreated, releaseErr := run.release(r.npc.modifierPool)
		if isCreated && r.logger != nil {
			r.logger.Printf("fixture burn unexpectedly created before tracking target=%d", targetID)
		}
		return nil, fmt.Errorf("fixtureBurnTrack: %w", errors.Join(err, releaseErr))
	}
	// CitadelPlasmaBurn explicitly authors duration=0 (until removal). The
	// finite-duration codec rejects zero, so preserve the native wire contract here.
	create, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: targetID, SourceID: e.source.Plan.ObjectID,
		ModifierGUID: modifierID, InstanceID: run.instanceID, StackCount: 1,
		DurationMilliseconds: uint32(profile.ModifierDuration.Milliseconds()), StartMilliseconds: e.timestamp,
	})
	if err != nil {
		rollbackErr := e.rollbackBurn(run)
		return nil, fmt.Errorf("fixtureBurnCreate: %w", errors.Join(err, rollbackErr))
	}
	step := campaignFixtureBurnStep{
		blast: blast, run: run, targetID: targetID, profile: profile,
		remainingTickCount: uint32(profile.ModifierDuration / time.Second),
		isPermanent:        profile.ModifierDuration == 0,
		deadline:           time.Second, handoffGeneration: 1,
	}
	r.registry.mutex.Lock()
	member, isFound = r.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation && member.campaignNPCModifiers[run.instanceID] == run
	if isCurrent {
		live, isLive := member.zone.NPCs().LiveNPC(targetID)
		isCurrent = isLive && live.IsPublished
	}
	isDisease := profile.ModifierName == "LifePlagueSpread"
	var replacedPackets [][]byte
	var cancels []func()
	var acceptErr error
	isCreated := false
	if isCurrent {
		inventoryErr := run.fixtureBurn.inventory.Put(run.record)
		if inventoryErr != nil {
			acceptErr = fmt.Errorf("fixtureInventory: %w", inventoryErr)
			isCurrent = false
		}
	}
	if isCurrent {
		if isDisease && origin == nil {
			replacedPackets, cancels, isCreated, acceptErr = e.acceptSpreadLocked(run)
		} else if isDisease {
			isAdmitted := origin.isActive() && run.fixtureBurn.npc.AdmitInfection(targetID, run.instanceID, false, r.npc.now())
			isCreated = isAdmitted && run.create()
			if !isCreated && isAdmitted {
				run.fixtureBurn.npc.RollbackInfection(targetID, run.instanceID)
			}
		} else {
			isCreated = run.create()
		}
	}
	r.registry.mutex.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	if acceptErr != nil {
		rollbackErr := e.rollbackBurn(run)
		return nil, fmt.Errorf("fixtureBurnAccept: %w", errors.Join(acceptErr, rollbackErr))
	}
	if !isCreated {
		rollbackErr := e.rollbackBurn(run)
		if rollbackErr != nil {
			return nil, fmt.Errorf("fixtureBurnReject: %w", rollbackErr)
		}
		return nil, nil
	}
	e.publication.stage(step)
	return append(replacedPackets, create), nil
}

func (e campaignFixtureBlastStep) rollbackBurn(run *campaignNPCModifierRun) error {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && member.generation == e.generation && member.campaignNPCModifiers[run.instanceID] == run {
		member.untrackCampaignNPCModifier(run)
		e.runtime.registry.sessions[e.sessionKey] = member
	}
	run.mu.Lock()
	cancel := run.cancel
	run.cancel = nil
	run.mu.Unlock()
	e.runtime.registry.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	run.fixtureBurn.removeInventory(run.instanceID, run.record.TargetObjectID)
	isCreated, err := run.release(e.runtime.npc.modifierPool)
	if err != nil {
		return fmt.Errorf("fixtureRollback: %w", err)
	}
	if isCreated {
		e.runtime.logger.Printf("Rejected fixture modifier was already created instance=%d", run.instanceID)
	}
	return nil
}

func (e campaignFixtureBurnStep) schedule() error {
	publication := &campaignFixturePublication{burn: &e, handoffGeneration: e.handoffGeneration, identity: e.blast.runtime.registry.producerGuard.identity(e.blast.sessionKey)}
	producers := []raknet.ScheduledPacketProducer{{Delay: max(time.Duration(0), e.run.fixtureBurn.startedAt.Add(e.deadline).Sub(e.blast.runtime.npc.now())), Produce: publication.produce, AfterCommit: publication.commit}}
	packet := e.blast.packet.Autonomous()
	if packet.ScheduleGroupResult == nil {
		return errors.New("fixture result scheduler unavailable")
	}
	registry := e.blast.runtime.registry
	registry.mutex.Lock()
	member, isFound := registry.sessions[e.blast.sessionKey]
	e.run.mu.Lock()
	isCurrent := isFound && member.generation == e.blast.generation &&
		member.campaignNPCModifiers[e.run.instanceID] == e.run && !e.run.isReleased && !e.run.fixtureBurn.isRetiring && e.run.fixtureBurn.handoffGeneration == e.handoffGeneration &&
		e.run.fixtureBurn.isCursorReady && e.run.fixtureBurn.publication == nil
	if !isCurrent {
		e.run.mu.Unlock()
		registry.mutex.Unlock()
		return nil
	}
	e.run.fixtureBurn.publication = publication
	e.run.fixtureBurn.cursor = e
	e.run.fixtureBurn.isCursorReady = false
	// Reserve ownership before a zero-delay producer can run and admit its
	// successor. An older admission must never replace that successor's handle.
	e.run.fixtureBurn.scheduleGeneration++
	scheduleGeneration := e.run.fixtureBurn.scheduleGeneration
	previousCancel := e.run.cancel
	e.run.cancel = nil
	e.run.mu.Unlock()
	registry.mutex.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	cancel, err := packet.ScheduleGroupResult(producers, publication.fail)
	registry.mutex.Lock()
	member, isFound = registry.sessions[e.blast.sessionKey]
	e.run.mu.Lock()
	isCurrent = isFound && member.generation == e.blast.generation &&
		member.campaignNPCModifiers[e.run.instanceID] == e.run && !e.run.isReleased && !e.run.fixtureBurn.isRetiring && e.run.fixtureBurn.handoffGeneration == e.handoffGeneration &&
		e.run.fixtureBurn.scheduleGeneration == scheduleGeneration
	if isCurrent && err == nil && cancel != nil {
		e.run.cancel = cancel
	}
	e.run.mu.Unlock()
	registry.mutex.Unlock()
	if !isCurrent {
		if cancel != nil {
			cancel()
		}
		return nil
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("fixtureBurnSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("fixture burn cancellation unavailable")
	}
	return nil
}

func (e campaignFixtureBurnStep) produce() ([][]byte, error) {
	if !e.run.isActive() {
		return nil, nil
	}
	profile := e.profile
	profile.MinimumDamage = profile.ModifierMinimumTickDamage
	profile.MaximumDamage = profile.ModifierMaximumTickDamage
	profile.DamageCoefficient = profile.ModifierTickDamageCoefficient
	profile.DamageType = 3
	profile.DamageSource = 1
	profile.DescriptorMask = 1 << 2
	if profile.IsModifierDamageProfileKnown {
		profile.DamageType = profile.ModifierDamageType
		profile.DamageSource = profile.ModifierDamageSource
		profile.DescriptorMask = profile.ModifierDescriptorMask
	}
	e.blast.timestamp += 1000
	packets, isSurviving, err := e.blast.damageOwned(e.targetID, profile, true, e.run)
	if err != nil {
		deletePackets, deleteErr := e.stop()
		return append(packets, deletePackets...), fmt.Errorf("fixtureBurnTick: %w", errors.Join(err, deleteErr))
	}
	if !e.isPermanent && e.remainingTickCount > 0 {
		e.remainingTickCount--
	}
	if !isSurviving || (!e.isPermanent && e.remainingTickCount == 0) {
		deletePackets, deleteErr := e.stop()
		if deleteErr != nil {
			return packets, fmt.Errorf("fixtureBurnFinish: %w", deleteErr)
		}
		return append(packets, deletePackets...), nil
	}
	return packets, nil
}
