package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
)

// SpritePet.Phase uses SpritePetHeal: three-second cooldown, a one-second
// sting animation, and healing at 0.333333343 seconds. Its flying idle follows
// the owner at averageDistance=4; its class movement speed is 12.
func (e *heroSummonRun) sting() ([][]byte, error) {
	return e.followOrSting(true)
}

func (e *heroSummonRun) resumeFollow() ([][]byte, error) {
	return e.followOrSting(false)
}

func (e *heroSummonRun) followOrSting(isSting bool) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.squad == nil || peerSession.deployedObjectID != e.ownerID {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	at := e.runtime.now()
	if !isSting && at.Before(e.healUntil) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isStingReleased := !isSting && !e.healUntil.IsZero()
	e.healUntil = time.Time{}
	companion, isFound := peerSession.zone.Companion().Snapshot(e.objectID)
	if !isFound || companion.HitPoint <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.runtime.registry.timer == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("sprite follow timer unavailable")
	}
	plans, err := peerSession.zone.Companion().FollowActor(
		peerSession.binding.UserID, e.generation, e.ownerID, e.objectID,
		game.Vec3(peerSession.playerPosition), spriteFollowDistance, spriteMovementSpeed, at,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("spriteFollow: %w", err)
	}
	ownerPosition := peerSession.playerPosition
	character, isCharacterFound := peerSession.squad.Character(peerSession.squad.DeployedIndex())
	isHealing := isSting && len(plans) == 0 && isCharacterFound && character.IsAvailable &&
		character.HitPoints > 0 && character.HitPoints <
		peerSession.characterHitPointMaximum(peerSession.squad.DeployedIndex())
	if isHealing {
		healUntil := at.Add(time.Second)
		companion, err = peerSession.zone.Companion().HoldFollow(e.objectID, at, healUntil)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("spriteStingHold: %w", err)
		}
		e.healUntil = healUntil
	}
	e.runtime.registry.mutex.Unlock()
	packets, err := companionraknet.Follow(plans)
	if err != nil {
		return nil, fmt.Errorf("spriteFollowMarshal: %w", err)
	}
	if isStingReleased {
		resetPacket, resetErr := abilityraknet.AnimationReset(e.objectID,
			e.packet.SourceTime+uint64(at.Sub(e.castStartedAt)/time.Millisecond))
		if resetErr != nil {
			return nil, fmt.Errorf("spriteStingReset: %w", resetErr)
		}
		packets = append([][]byte{resetPacket}, packets...)
	}
	for _, plan := range plans {
		step := campaignCompanionFollowStep{
			registry: e.runtime.registry, zone: peerSession.zone, userID: peerSession.binding.UserID,
			generation: e.generation, plan: plan, logger: e.runtime.logger,
		}
		cancel, scheduleErr := e.runtime.registry.timer.Schedule(plan.TravelDuration, step.execute)
		if scheduleErr != nil || cancel == nil {
			isCancelled := peerSession.zone.Companion().CancelFollow(plan.ObjectID, plan.Revision, at)
			if !isCancelled {
				// A newer owner movement may already have replaced this reservation.
			}
			if scheduleErr != nil {
				return nil, fmt.Errorf("spriteFollowSchedule: %w", scheduleErr)
			}
			return nil, fmt.Errorf("spriteFollowSchedule: cancellation unavailable")
		}
	}
	if !isHealing {
		e.runtime.registry.queuePeerPresentation(
			gameplayProducerIdentityFromSession(e.sessionKey, peerSession, true), packets,
		)
		return packets, nil
	}
	facing := game.Vec3(ownerPosition).Sub(companion.Position)
	if facing.Length() > 0 {
		facing = facing.Scale(1 / facing.Length())
	} else {
		facing = game.Vec3{X: 1}
	}
	posePackets, err := marshalZonePlayerAttackPose(e.objectID,
		raknet.Vector3(companion.Position), raknet.Vector3(facing), ownerPosition)
	if err != nil {
		return nil, fmt.Errorf("spriteStingPose: %w", err)
	}
	packets = append(packets, posePackets...)
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.objectID, State: util.HashID("LF_pet_healer_heal"), Scale: 1,
		Timestamp: e.packet.SourceTime + uint64(at.Sub(e.castStartedAt)/time.Millisecond),
	})
	if err != nil {
		return nil, fmt.Errorf("spriteStingAnimation: %w", err)
	}
	packets = append(packets, animationPacket)
	e.runtime.registry.queuePeerPresentation(
		gameplayProducerIdentityFromSession(e.sessionKey, peerSession, true), packets,
	)
	return packets, nil
}
