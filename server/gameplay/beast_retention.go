package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type retainedBeastPassive struct {
	objectID          uint32
	damage            float32
	resurrection      *beastPetResurrectionRun
	activation        summonCompanionActivation
	isActivationFound bool
}

// Detach before transport cleanup and companion snapshotting. Rage keeps its
// existing disconnect cancellation policy; its producers belong to the retired
// connection. Resurrection and its allowances belong to the retained world.
func (e *gameplayPeerSession) detachBeastPassive() (retainedBeastPassive, error) {
	retained := retainedBeastPassive{objectID: e.beastPetObjectID, damage: e.beastPetDamage,
		resurrection: e.beastPetResurrection}
	if retained.objectID == 0 {
		return retained, nil
	}
	if e.beastPetEnrage != nil {
		rage := e.beastPetEnrage
		scalePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: retained.objectID, Value: map[uint8]float32{113: 0},
		})
		if err != nil {
			return retainedBeastPassive{}, fmt.Errorf("rageScale: %w", err)
		}
		deletePacket, err := rage.stop()
		if err != nil {
			return retainedBeastPassive{}, fmt.Errorf("rageStop: %w", err)
		}
		packets := [][]byte{deletePacket, scalePacket}
		for sessionKey, candidate := range rage.runtime.registry.sessions {
			if candidate.zone != e.zone || candidate.isRejoinPending {
				continue
			}
			candidate.queueCampaignPackets(packets)
			rage.runtime.registry.sessions[sessionKey] = candidate
		}
		e.beastPetEnrage = nil
		e.beastPetDamageIncrease = 0
		if e.zone != nil && e.zone.Companion() != nil {
			e.zone.Companion().ClearBodyScale(retained.objectID)
		}
	}
	retained.activation, retained.isActivationFound = e.sagePassiveActivations[retained.objectID]
	if retained.isActivationFound {
		if retained.activation.Attack != nil {
			retained.activation.Attack.Stop()
			retained.activation.Attack = nil
		}
		delete(e.sagePassiveActivations, retained.objectID)
	}
	if retained.resurrection != nil {
		run := retained.resurrection
		run.revision++
		run.resumeRevision++
		if run.resumeCancel != nil {
			run.resumeCancel()
			run.resumeCancel = nil
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
	}
	e.beastPetObjectID = 0
	e.beastPetDamage = 0
	e.beastPetResurrection = nil
	return retained, nil
}

func (e *gameplayPeerSession) restoreBeastPassive(retained retainedBeastPassive) ([][]byte, error) {
	if retained.objectID == 0 {
		return nil, nil
	}
	if e.zone == nil || e.zone.Companion() == nil {
		return e.failBeastPassiveRestore(retained, errors.New("retained beast world unavailable"))
	}
	pet, isFound := e.zone.Companion().Snapshot(retained.objectID)
	if !isFound || pet.UserID != e.binding.UserID || pet.PeerGeneration != e.generation ||
		pet.OwnerObjectID != e.deployedObjectID {
		return e.failBeastPassiveRestore(retained, errors.New("retained beast actor identity changed"))
	}
	e.beastPetObjectID = retained.objectID
	e.beastPetDamage = retained.damage
	e.beastPetResurrection = retained.resurrection
	if retained.isActivationFound {
		if e.sagePassiveActivations == nil {
			e.sagePassiveActivations = make(map[uint32]summonCompanionActivation)
		}
		e.sagePassiveActivations[retained.objectID] = retained.activation
	}
	return nil, nil
}

func (e *gameplayPeerSession) failBeastPassiveRestore(retained retainedBeastPassive, cause error) ([][]byte, error) {
	if retained.resurrection == nil {
		return nil, fmt.Errorf("restoreActor: %w", cause)
	}
	packets, err := retained.resurrection.stop(e)
	return packets, fmt.Errorf("restoreCleanup: %w", errors.Join(cause, err))
}

// The registry lock fences cleanup against the detached world callback.
func (e *gameplaySessionRegistry) restoreBeastPassiveLocked(member *gameplayPeerSession, retained retainedBeastPassive) {
	packets, err := member.restoreBeastPassive(retained)
	if err != nil && e.logger != nil {
		e.logger.Printf("RakNet retained Savage restore failed user=%d: %v", member.binding.UserID, err)
	}
	for sessionKey, candidate := range e.sessions {
		if candidate.zone != member.zone || candidate.isRejoinPending {
			continue
		}
		candidate.queueCampaignPackets(packets)
		e.sessions[sessionKey] = candidate
	}
}

// Call under the registry lock after installing the retained session. Re-arm
// from absolute deadlines even if a timer fired during the registry handover.
func (e *gameplayPeerSession) scheduleRetainedBeastPassive() error {
	if e.beastPetResurrection == nil {
		return nil
	}
	err := e.beastPetResurrection.schedule(time.Millisecond)
	if err != nil {
		return fmt.Errorf("retainedSchedule: %w", err)
	}
	return nil
}

// Rebind only the attack adapter after the new transport is ready. The world
// resurrection clock and its pending hit/release retain their original times.
func (e *gameplayPeerSession) rebindBeastPassive(packet raknet.Packet, sessionKey string) error {
	run := e.beastPetResurrection
	if run == nil {
		return nil
	}
	run.packet = packet.Autonomous()
	run.sessionKey = sessionKey
	if e.isRejoinPending || e.deployedHitPoint() <= 0 || run.state.IsCasting || run.state.IsBurrowing {
		return nil
	}
	err := run.scheduleResume()
	if err != nil {
		return fmt.Errorf("rejoinAttack: %w", err)
	}
	return nil
}
