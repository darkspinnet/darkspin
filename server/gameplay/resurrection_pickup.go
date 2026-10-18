package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
)

// PickUpResurrectOrb commits on the authored pickup frame, then finishes its
// animation. Keep the capsule noun so the client retains its inspect cursor.
const resurrectionPickupCommitDelay = 300 * time.Millisecond
const resurrectionPickupDuration = time.Second

// The caller holds the registry lock; every return releases it.
func (e campaignInteractionRuntime) beginResurrectionPickupLocked(
	packet raknet.Packet, command raknet.ActionCommandData, sessionKey string,
	peerSession gameplayPeerSession, orb zoneinteract.Orb,
) ([][]byte, error) {
	if peerSession.deployedHitPoint() <= 0 ||
		(!orb.AvailableAt.IsZero() && e.now().Before(orb.AvailableAt)) {
		e.registry.mutex.Unlock()
		return e.rejectPickup(command, "resurrection pickup unavailable")
	}
	maximumDistance := peerSession.campaignPickupMaximumDistance()
	pickup, admission := peerSession.reserveCampaignPickup(command, maximumDistance)
	if admission != zoneinteract.PickupAccepted {
		e.registry.mutex.Unlock()
		if admission == zoneinteract.PickupRejectedReserved &&
			peerSession.campaignScheduleSession().Has(zoneaction.ScheduleEquipment, orb.ObjectID) {
			acceptPacket, err := actionraknet.Accept(command, "PickUpResurrectOrb",
				packet.SourceTime, resurrectionPickupCommitDelay, resurrectionPickupDuration)
			if err != nil {
				return nil, fmt.Errorf("resurrectionDuplicate: %w", err)
			}
			return [][]byte{acceptPacket}, nil
		}
		if admission == zoneinteract.PickupRejectedRange {
			return e.pursuePickup(packet, sessionKey, command, pickup, maximumDistance)
		}
		return e.rejectPickup(command, campaignPickupRejectionReason(admission))
	}
	defer e.registry.mutex.Unlock()
	packets, err := e.scheduleResurrectionPickupLocked(packet, command, sessionKey, peerSession, orb)
	if err != nil {
		peerSession.zone.Pickups().Release(orb.ObjectID)
		return nil, fmt.Errorf("resurrectionBegin: %w", err)
	}
	return packets, nil
}

func (e campaignInteractionRuntime) scheduleResurrectionPickupLocked(
	packet raknet.Packet, command raknet.ActionCommandData, sessionKey string,
	peerSession gameplayPeerSession, orb zoneinteract.Orb,
) ([][]byte, error) {
	if packet.ScheduleGroupResult == nil && packet.ScheduleGroup == nil {
		return nil, errors.New("resurrection scheduler unavailable")
	}
	acceptPacket, err := actionraknet.Accept(command, "PickUpResurrectOrb",
		packet.SourceTime, resurrectionPickupCommitDelay, resurrectionPickupDuration)
	if err != nil {
		return nil, fmt.Errorf("resurrectionAccept: %w", err)
	}
	abilityIndex := uint32(0)
	if command.Ability != nil {
		abilityIndex = command.Ability.Index
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], util.HashID("PickUpResurrectOrb"), abilityIndex,
		packet.SourceTime, resurrectionPickupCommitDelay, resurrectionPickupDuration,
	)
	if err != nil {
		return nil, fmt.Errorf("resurrectionRelease: %w", err)
	}
	animationPacket, err := abilityraknet.Animation(
		peerSession.deployedObjectID, "pickup", packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("resurrectionAnimation: %w", err)
	}
	movementPackets, err := peerSession.stopCampaignPickup(e.now())
	if err != nil {
		return nil, fmt.Errorf("resurrectionStop: %w", err)
	}
	step := resurrectionPickupStep{
		runtime: e, sessionKey: sessionKey, generation: peerSession.generation,
		command: command, orb: orb, releasePacket: releasePacket,
	}
	producers := []raknet.ScheduledPacketProducer{{
		Delay: resurrectionPickupCommitDelay, Produce: step.produce,
	}}
	failure := campaignEquipmentPickupFailure{
		runtime: e, sessionKey: sessionKey, generation: peerSession.generation,
		objectID: orb.ObjectID,
	}
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, failure.handle)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		return nil, fmt.Errorf("resurrectionSchedule: %w", err)
	}
	if cancel == nil {
		return nil, errors.New("resurrection schedule cancellation unavailable")
	}
	err = peerSession.campaignScheduleSession().Add(
		zoneaction.ScheduleEquipment, orb.ObjectID, nil, cancel,
		&campaignPickupScheduleCleaner{pickup: peerSession.zone.Pickups(), objectID: orb.ObjectID},
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("resurrectionTrack: %w", err)
	}
	e.registry.sessions[sessionKey] = peerSession
	packets := append([][]byte{acceptPacket}, movementPackets...)
	return append(packets, animationPacket), nil
}

type resurrectionPickupStep struct {
	runtime       campaignInteractionRuntime
	sessionKey    string
	generation    uint64
	command       raknet.ActionCommandData
	orb           zoneinteract.Orb
	releasePacket []byte
}

func (e resurrectionPickupStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation || peerSession.zone == nil {
		return nil, nil
	}
	isScheduled := peerSession.campaignScheduleSession().Remove(
		zoneaction.ScheduleEquipment, e.orb.ObjectID, nil,
	)
	if !isScheduled {
		return nil, nil
	}
	defer peerSession.zone.Pickups().Release(e.orb.ObjectID)
	if peerSession.isZoneTerminal() || peerSession.deployedObjectID != e.command.Common.ObjectID ||
		peerSession.deployedHitPoint() <= 0 {
		return e.runtime.rejectPickup(e.command, "resurrection actor unavailable")
	}
	currentOrb, isOrbFound := peerSession.zone.Orbs().Orb(e.orb.ObjectID)
	if !isOrbFound {
		return e.runtime.rejectPickup(e.command, "resurrection capsule unavailable")
	}
	recoveries, err := e.recoverPartyLocked(peerSession)
	if err != nil {
		return nil, fmt.Errorf("resurrectionRestore: %w", err)
	}
	if len(recoveries) == 0 {
		return e.runtime.rejectPickup(e.command, "no defeated heroes")
	}
	peerSession = e.runtime.registry.sessions[e.sessionKey]
	packets, err := marshalCampaignOrbPickup(
		currentOrb, peerSession.deployedObjectID, zoneResurrectionOrb, false,
		peerSession.deployedHitPoint(), peerSession.deployedManaPoint(), 1,
	)
	if err != nil {
		rollbackErr := e.rollbackPartyLocked(recoveries)
		return nil, fmt.Errorf("resurrectionMarshal: %w", errors.Join(err, rollbackErr))
	}
	if !peerSession.zone.Pickups().Commit(e.orb.ObjectID) {
		rollbackErr := e.rollbackPartyLocked(recoveries)
		return nil, fmt.Errorf("resurrectionCommit: %w", errors.Join(
			errors.New("resurrection capsule reservation missing"), rollbackErr,
		))
	}
	peerSession.zone.Orbs().Remove(e.orb.ObjectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	for _, recovery := range recoveries {
		packets = append(packets, recovery.packets...)
	}
	for candidateSessionKey, candidate := range e.runtime.registry.sessions {
		if candidateSessionKey == e.sessionKey || candidate.zone != peerSession.zone {
			continue
		}
		publishErr := candidate.publishPackets(packets)
		if publishErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet resurrection pickup peer delivery queued user=%d object=%d: %v",
				candidate.binding.UserID, e.orb.ObjectID, publishErr)
		}
		e.runtime.registry.sessions[candidateSessionKey] = candidate
	}
	isCheckpointSaved := peerSession.zone.SaveCheckpointIfSafe(zonecheckpoint.ReasonSafePickup)
	e.runtime.logger.Printf("RakNet resurrection capsule accepted source=%d target=%d heroes=%d checkpoint_saved=%t",
		peerSession.deployedObjectID, e.orb.ObjectID, len(recoveries), isCheckpointSaved)
	return append(packets, e.releasePacket), nil
}
