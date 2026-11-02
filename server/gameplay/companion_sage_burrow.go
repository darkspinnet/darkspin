package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

// LifeTempestPetBurrow, chunk 130: immobilize, animate, wait two seconds,
// then MarkForDelete from Deactivate. Distance deletion is a separate branch.
const sageBurrowDuration = 2 * time.Second
const sageBurrowAnimation = "lifetempest_pet_burrow"

type sageBurrowStep struct {
	runtime gameplaySwitchRuntime
	zone    *zone.Zone
	actor   zonecompanion.Actor
}

func (e *sagePassivePoll) stopOwner(member *gameplayPeerSession) ([][]byte, error) {
	requests, err := e.run.Stop()
	member.sagePassive = nil
	member.sagePassiveSchedule = nil
	if err != nil {
		return nil, fmt.Errorf("sageStop: %w", err)
	}
	at := e.runtime.now()
	packets := make([][]byte, 0)
	for _, req := range requests {
		activation := member.sagePassiveActivations[req.ObjectID]
		if activation.Attack != nil {
			activation.Attack.Stop()
		}
		delete(member.sagePassiveActivations, req.ObjectID)
		actor, isFound := e.zone.Companion().Snapshot(req.ObjectID)
		if !isFound || actor.HitPoint <= 0 || actor.OwnerObjectID != e.ownerObjectID ||
			actor.UserID != e.userID || actor.PeerGeneration != e.generation {
			continue
		}
		err = e.zone.Companion().Recall(actor.ObjectID, at, at.Add(sageBurrowDuration), sageBurrowAnimation)
		if err != nil {
			return packets, fmt.Errorf("sageRecall: %w", err)
		}
		stopPacket, marshalErr := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
			ObjectID: actor.ObjectID, GoalFlags: 0x20,
		})
		if marshalErr != nil {
			return packets, fmt.Errorf("sageRecallStop: %w", marshalErr)
		}
		animation, marshalErr := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
			ObjectID: actor.ObjectID, State: util.HashID(sageBurrowAnimation),
			Timestamp: advanceHeroArrivalTimestamp(e.sourceTime, e.startedAt, at), Scale: 1,
		})
		if marshalErr != nil {
			return packets, fmt.Errorf("sageRecallAnimation: %w", marshalErr)
		}
		step := sageBurrowStep{runtime: e.runtime, zone: e.zone, actor: actor}
		cancel, timerErr := e.runtime.npc.timer.Schedule(sageBurrowDuration, step.execute)
		if timerErr != nil {
			return packets, fmt.Errorf("sageRecallTimer: %w", timerErr)
		}
		if cancel == nil {
			return packets, fmt.Errorf("sageRecallCancel: unavailable")
		}
		// This deletion belongs to the old pet, independently of the replacement
		// passive or its owner's transport. Its identity guard handles zone leave.
		packets = append(packets, stopPacket, animation)
	}
	return packets, nil
}

func (e sageBurrowStep) execute() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	actor, isFound := e.zone.Companion().Snapshot(e.actor.ObjectID)
	if !isFound || actor.UserID != e.actor.UserID || actor.PeerGeneration != e.actor.PeerGeneration ||
		actor.OwnerObjectID != e.actor.OwnerObjectID || actor.AbilityAnimation != sageBurrowAnimation {
		return
	}
	request := summonPassiveWorldRequest{ObjectID: actor.ObjectID, OwnerObjectID: actor.OwnerObjectID, IsDespawn: true}
	packets, activation, err := marshalSummonPassiveWorldRequest(request, raknet.Vector3{})
	if err != nil {
		e.runtime.logger.Printf("RakNet Sage burrow deletion failed object=%d: %v", actor.ObjectID, err)
		return
	}
	if activation != nil {
		e.runtime.logger.Printf("RakNet Sage burrow deletion produced activation object=%d", actor.ObjectID)
		return
	}
	e.zone.Companion().Remove(actor.ObjectID)
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone != e.zone || member.isRejoinPending || member.isZoneTerminal() {
			continue
		}
		member.cancelPetPlasmaBurn(actor.ObjectID)
		member.queueCampaignPackets(packets)
		e.runtime.registry.sessions[sessionKey] = member
	}
}
