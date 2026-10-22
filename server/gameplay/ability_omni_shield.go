package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
)

// Child396 has default activation0: every accepted request owns an independent
// modifier and attachment. The cast is never the singleton heroModifierRun.
type omniShieldRun struct {
	runtime              campaignAbilityCommandRuntime
	world                *zone.Zone
	userID               uint64
	generation           uint64
	objectID             uint32
	creatureIndex        uint32
	instanceID           uint32
	effectSlot           uint8
	definition           sim.AbilityDefinition
	startedAt            time.Time
	activeAt             time.Time
	expiresAt            time.Time
	timestamp            uint64
	releasePacket        []byte
	activationCancel     func()
	expiryCancel         func()
	releaseCancel        func()
	cooldownKey          zoneability.CooldownKey
	abilityID            uint32
	manaCost             float32
	releaseReservation   zoneaction.ReleaseReservation
	isAdmitted           bool
	isPaid               bool
	isActive             bool
	isRetired            bool
	isAllocationReleased bool
}

// The caller has completed ordinary self-modifier admission checks and holds
// registry.mutex. This function consumes that lock on every branch.
func (e campaignAbilityCommandRuntime) startOmniShieldLocked(req campaignCharacterAbilityRequest, ability zonecontent.HeroAbility, cooldownKey zoneability.CooldownKey, sessionKey string, member gameplayPeerSession, definition sim.AbilityDefinition, manaCost float32) ([][]byte, error) {
	defer e.registry.mutex.Unlock()
	if member.zone == nil || e.registry.timer == nil || e.effectPool == nil || e.modifierPool == nil {
		return req.reject("Omni Shield owner unavailable")
	}
	instanceID, err := e.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("omniAllocate: %w", err)
	}
	slot, isAllocated := e.effectPool.Allocate(member.deployedObjectID)
	if !isAllocated {
		releaseErr := e.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("omniSlot: %w", errors.Join(errors.New("attachment unavailable"), releaseErr))
	}
	if !e.effectPool.HoldRemoval(member.deployedObjectID, slot) {
		isReleased := e.effectPool.Release(member.deployedObjectID, slot)
		if !isReleased {
			e.logger.Printf("Omni Shield unheld slot release failed object=%d slot=%d", member.deployedObjectID, slot)
		}
		releaseErr := e.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("omniHold: %w", errors.Join(errors.New("attachment hold unavailable"), releaseErr))
	}
	run := &omniShieldRun{runtime: e, world: member.zone, userID: member.binding.UserID,
		generation: member.generation, objectID: member.deployedObjectID, creatureIndex: member.deployedCreatureIndex,
		instanceID: instanceID, effectSlot: slot, definition: definition, startedAt: req.startTime,
		timestamp: req.packet.SourceTime, cooldownKey: cooldownKey, abilityID: ability.ID, manaCost: manaCost}
	defer run.releaseUnadmittedAllocation()
	packets, err := run.admissionPackets(req, ability.ID)
	if err != nil {
		return nil, fmt.Errorf("omniPresentation: %w", err)
	}
	releaseReservation, isReleaseReserved := member.abilityReleaseSession().Reserve(req.startTime, definition.ReleaseDelay)
	if !isReleaseReserved {
		return req.reject("Omni Shield release unavailable")
	}
	run.releaseReservation = releaseReservation
	err = req.packet.AfterResponseCommit(run.commitAdmission)
	if err != nil {
		member.abilityReleaseSession().Rollback(releaseReservation)
		return nil, fmt.Errorf("omniAdmissionCommit: %w", err)
	}
	err = member.stopPlayerMovement(req.startTime)
	if err != nil {
		member.abilityReleaseSession().Rollback(releaseReservation)
		return nil, fmt.Errorf("omniPayment: %w", err)
	}
	if member.omniShields == nil {
		member.omniShields = make(map[uint32]*omniShieldRun)
	}
	run.isAdmitted = true
	member.omniShieldRegistry = e.registry
	member.omniShields[instanceID] = run
	e.registry.sessions[sessionKey] = member
	return packets, nil
}

func (e *omniShieldRun) admissionPackets(req campaignCharacterAbilityRequest, abilityID uint32) ([][]byte, error) {
	ack, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: abilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds: e.timestamp, SourceCommitMilliseconds: e.timestamp + uint64(e.definition.HitDelay/time.Millisecond),
		SourceEndMilliseconds: e.timestamp + uint64(e.definition.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		return nil, fmt.Errorf("omniAck: %w", err)
	}
	e.releasePacket, err = abilityraknet.ReleaseResponse(req.command.Common.Unknown[0], abilityID, req.command.Ability.Index, e.timestamp, e.definition.HitDelay, e.definition.ReleaseDelay)
	if err != nil {
		return nil, fmt.Errorf("omniRelease: %w", err)
	}
	packets := [][]byte{ack}
	for _, message := range []raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{ObjectID: e.objectID, State: util.HashID(e.definition.AnimationName), Timestamp: e.timestamp, Scale: 1},
	} {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("omniCastMarshal: %w", marshalErr)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *omniShieldRun) releaseUnadmittedAllocation() {
	if !e.isAdmitted {
		e.releaseAllocation()
	}
}

// Root717 pays after the hit wait and before cleanse/RequestModifier. Once
// paid, a rejected child request cannot undo that payment or the cleanse.
func (e *omniShieldRun) payLocked(member *gameplayPeerSession) ([][]byte, error) {
	if e.isPaid {
		return nil, errors.New("Omni Shield already paid")
	}
	if member.deployedManaPoint() < e.manaCost {
		return nil, errors.New("Omni Shield mana unavailable")
	}
	at := e.runtime.now()
	timestamp := advanceHeroArrivalTimestamp(e.timestamp, e.startedAt, at)
	packets := make([][]byte, 0, 2)
	for _, message := range []raknet.ApplicationMessage{
		raknet.CooldownUpdateMessage{ObjectID: e.objectID, AbilityKey: uint64(e.abilityID), DurationMilliseconds: e.definition.Cooldown.Milliseconds(), SourceStartMilliseconds: int64(timestamp)},
		raknet.CombatantDataDeltaMessage{ObjectID: e.objectID, ManaPoints: member.deployedManaPoint() - e.manaCost, IsManaPointChanged: true},
	} {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("omniPaymentMarshal: %w", err)
		}
		packets = append(packets, packet)
	}
	reservation, isReserved := member.abilityCooldownSession().Reserve(e.cooldownKey, at, e.definition.Cooldown)
	if !isReserved {
		return nil, errors.New("Omni Shield cooldown unavailable")
	}
	err := member.setDeployedManaPoints(member.deployedManaPoint() - e.manaCost)
	if err != nil {
		member.abilityCooldownSession().Rollback(reservation)
		return nil, fmt.Errorf("omniPaymentCommit: %w", err)
	}
	e.isPaid = true
	return packets, nil
}

func (e *omniShieldRun) releaseAllocation() {
	if e.isAllocationReleased {
		return
	}
	e.isAllocationReleased = true
	if !e.runtime.effectPool.CommitRemoval(e.objectID, e.effectSlot) {
		e.runtime.logger.Printf("Omni Shield slot commit failed object=%d slot=%d", e.objectID, e.effectSlot)
	}
	err := e.runtime.modifierPool.Release(e.instanceID)
	if err != nil {
		e.runtime.logger.Printf("Omni Shield instance release failed instance=%d: %v", e.instanceID, err)
	}
}

func (e *omniShieldRun) ownerLocked() (string, gameplayPeerSession, *gameplayRetainedSession, bool) {
	for key, member := range e.runtime.registry.sessions {
		if member.zone == e.world && member.binding.UserID == e.userID && member.generation == e.generation {
			return key, member, nil, true
		}
	}
	for _, retained := range e.runtime.registry.retainedSessions {
		member := retained.peerSession
		if member.zone == e.world && member.binding.UserID == e.userID && member.generation == e.generation {
			return "", member, retained, true
		}
	}
	return "", gameplayPeerSession{}, nil, false
}

func (e *omniShieldRun) commitAdmission() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if !e.isAdmitted || e.isRetired || e.activationCancel != nil {
		return
	}
	cancel, err := e.runtime.registry.timer.Schedule(max(time.Duration(0), e.startedAt.Add(e.definition.HitDelay).Sub(e.runtime.now())), e.activate)
	if err != nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		e.runtime.registry.retireOmniShieldRunLocked(nil, e)
		e.runtime.logger.Printf("Omni Shield activation schedule failed instance=%d: %v", e.instanceID, err)
		return
	}
	e.activationCancel = cancel
	cancel, err = e.runtime.registry.timer.Schedule(max(time.Duration(0), e.startedAt.Add(e.definition.ReleaseDelay).Sub(e.runtime.now())), e.release)
	if err != nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		e.runtime.registry.retireOmniShieldRunLocked(nil, e)
		e.runtime.logger.Printf("Omni Shield release schedule failed instance=%d: %v", e.instanceID, err)
		return
	}
	e.releaseCancel = cancel
}

func (e *omniShieldRun) activate() {
	e.runtime.registry.mutex.Lock()
	key, member, retained, isFound := e.ownerLocked()
	if e.isRetired || e.isActive {
		e.runtime.registry.mutex.Unlock()
		return
	}
	if !isFound || retained != nil || member.isZoneTerminal() || member.deployedObjectID != e.objectID || member.deployedHitPoint() <= 0 || e.world.Context().Err() != nil {
		e.runtime.registry.retireOmniShieldRunLocked(nil, e)
		e.runtime.registry.mutex.Unlock()
		return
	}
	paid, payErr := e.payLocked(&member)
	if payErr != nil {
		e.runtime.registry.retireOmniShieldRunLocked(nil, e)
		e.runtime.registry.mutex.Unlock()
		e.runtime.logger.Printf("Omni Shield payment failed instance=%d: %v", e.instanceID, payErr)
		return
	}
	e.runtime.registry.sessions[key] = member
	e.runtime.registry.queueChannelPresentationLocked(e.world, paid)
	e.runtime.registry.mutex.Unlock()
	// The existing named purge takes the registry lock. No child has become
	// active yet; old independent children remain untouched through this wait.
	purged, err := e.runtime.purgeHeroDebuffs(key, e.generation, e.objectID)
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.runtime.registry.queueChannelRemovalsLocked(e.world, purged)
	key, member, retained, isFound = e.ownerLocked()
	if err != nil || !isFound || retained != nil || e.isRetired || member.deployedObjectID != e.objectID || member.deployedHitPoint() <= 0 {
		e.runtime.registry.retireOmniShieldRunLocked(nil, e)
		if err != nil {
			e.runtime.logger.Printf("Omni Shield cleanse failed instance=%d: %v", e.instanceID, err)
		}
		return
	}
	e.activeAt = e.runtime.now()
	e.expiresAt = e.activeAt.Add(e.definition.Duration)
	packets, err := e.presentationPackets(e.activeAt, advanceHeroArrivalTimestamp(e.timestamp, e.startedAt, e.activeAt))
	if err != nil {
		e.runtime.registry.retireOmniShieldRunLocked(nil, e)
		e.runtime.logger.Printf("Omni Shield create failed instance=%d: %v", e.instanceID, err)
		return
	}
	e.isActive = true
	e.runtime.registry.queueChannelPresentationLocked(e.world, packets)
	cancel, err := e.runtime.registry.timer.Schedule(e.definition.Duration, e.expire)
	if err != nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		e.runtime.registry.retireOmniShieldRunLocked(nil, e)
		e.runtime.logger.Printf("Omni Shield expiry schedule failed instance=%d: %v", e.instanceID, err)
		return
	}
	e.expiryCancel = cancel
}

func (e *omniShieldRun) release() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	key, member, retained, isFound := e.ownerLocked()
	if !isFound || (!e.isPaid && e.isRetired) || member.deployedObjectID != e.objectID ||
		!member.abilityReleaseSession().IsCurrent(e.releaseReservation) {
		return
	}
	member.queueCampaignPackets([][]byte{e.releasePacket})
	if retained != nil {
		retained.peerSession = member
	} else {
		e.runtime.registry.sessions[key] = member
	}
}

func (e *omniShieldRun) expire() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.runtime.registry.retireOmniShieldRunLocked(nil, e)
}

func (e gameplayPeerSession) hasOmniShieldAt(targetObjectID uint32, at time.Time) bool {
	for _, run := range e.omniShields {
		if run.objectID == targetObjectID && run.isActive && !run.isRetired && at.Before(run.expiresAt) {
			return true
		}
	}
	return false
}
