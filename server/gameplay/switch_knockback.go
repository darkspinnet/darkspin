package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const heroSwapKnockbackRadius = float32(6)
const heroSwapKnockbackDistance = float32(4)
const heroSwapKnockbackSpeed = float32(20)
const heroSwapKnockbackOutro = time.Second
const surefootedNPCAffixModifierName = "Surefooted_NPCAffixModifier"

type gameplaySwitchKnockbackResetStep struct {
	runtime    gameplaySwitchRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e gameplaySwitchKnockbackResetStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if isCurrent {
		_, isCurrent = peerSession.zone.NPCs().LiveNPC(e.objectID)
	}
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := npcraknet.ResetAnimation(e.objectID, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("swapKnockbackResetMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r gameplaySwitchRuntime) heroSwapArrivalKnockback(
	packet raknet.Packet, sessionKey string, generation uint64,
	targetObjectID uint32, timestamp uint64,
) ([][]byte, error) {
	if r.registry == nil || sessionKey == "" || generation == 0 ||
		targetObjectID == 0 {
		return nil, fmt.Errorf("swap knockback input invalid")
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.deployedObjectID == targetObjectID &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.zone.Navigation() != nil && peerSession.zone.Hero() != nil
	if !isCurrent {
		return nil, nil
	}
	hero, isHeroFound := peerSession.zone.Hero().Snapshot(
		peerSession.binding.UserID, generation,
	)
	if !isHeroFound || hero.ObjectID != targetObjectID {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if !isHeroSwapKnockbackTarget(target, hero.Position) {
			continue
		}
		delta := target.Plan.Position.Sub(hero.Position)
		distance := delta.Length()
		if distance <= 0 {
			delta = game.Vec3{X: 1}
			distance = 1
		}
		desired := target.Plan.Position.Add(
			delta.Scale(heroSwapKnockbackDistance / distance),
		)
		destination, isDestinationFound, err :=
			zoneaction.NPCDirectMovementDestination(
				peerSession.zone.Navigation(), target.Plan.Position, desired,
				max(target.Plan.NPCProfile.FootprintRadius, float32(0.25)),
			)
		if err != nil {
			return packets, fmt.Errorf(
				"swapKnockbackDestination[%d]: %w", target.Plan.ObjectID, err,
			)
		}
		if !isDestinationFound {
			continue
		}
		plan := zonenpc.AttackPlan{
			SourceObjectID: targetObjectID,
			TargetObjectID: target.Plan.ObjectID,
			SourcePosition: hero.Position,
			TargetPosition: target.Plan.Position,
			Profile: zonenpc.ActionProfile{
				ForcedMovementSpeed:        heroSwapKnockbackSpeed,
				ForcedMovementDistance:     heroSwapKnockbackDistance,
				ForcedMovementReactionName: "react_knockback",
			},
		}
		movementPackets, err := npcraknet.ForcedMovement(
			plan, destination, timestamp,
		)
		if err != nil {
			return packets, fmt.Errorf(
				"swapKnockbackMarshal[%d]: %w", target.Plan.ObjectID, err,
			)
		}
		err = peerSession.zone.NPCs().SetPosition(
			target.Plan.ObjectID, destination,
		)
		if err != nil {
			return packets, fmt.Errorf(
				"swapKnockbackMove[%d]: %w", target.Plan.ObjectID, err,
			)
		}
		peerSession.zone.PublishNPCForcedMovement(
			zonenpc.ForcedMovementEvent{
				Plan: plan, Destination: destination, Timestamp: timestamp,
			},
			peerSession.binding.UserID, generation,
		)
		packets = append(packets, movementPackets...)
		movementDistance := destination.Sub(target.Plan.Position).Length()
		movementDuration := time.Duration(
			float64(movementDistance/heroSwapKnockbackSpeed) * float64(time.Second),
		)
		resetDelay := movementDuration + heroSwapKnockbackOutro
		resetTimestamp := timestamp + uint64(resetDelay/time.Millisecond)
		reset := gameplaySwitchKnockbackResetStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			objectID: target.Plan.ObjectID, timestamp: resetTimestamp,
		}
		resetScheduleErr := errors.New("schedule unavailable")
		if packet.ScheduleFunc != nil {
			resetScheduleErr = packet.ScheduleFunc(resetDelay, reset.produce)
		}
		if resetScheduleErr == nil {
			continue
		}
		resetPacket, resetErr := npcraknet.ResetAnimation(
			target.Plan.ObjectID, timestamp,
		)
		if resetErr != nil {
			return packets, fmt.Errorf(
				"swapKnockbackResetFallback[%d]: %w",
				target.Plan.ObjectID, resetErr,
			)
		}
		packets = append(packets, resetPacket)
		if r.logger != nil {
			r.logger.Printf(
				"RakNet hero swap knockback reset sent immediately target=%d schedule_error=%v",
				target.Plan.ObjectID, resetScheduleErr,
			)
		}
	}
	return packets, nil
}

func isHeroSwapKnockbackTarget(
	target zonenpc.Snapshot, center game.Vec3,
) bool {
	if target.Faction != zonenpc.FactionNonPlayerAligned ||
		!target.IsPublished || target.IsDefeated || target.Plan.IsFixture ||
		target.Plan.IsBoss ||
		target.Plan.BossIdentity.HasModifier(surefootedNPCAffixModifierName) {
		return false
	}
	contactRadius := heroSwapKnockbackRadius +
		max(float32(0), target.Plan.NPCProfile.FootprintRadius)
	return target.Plan.Position.Sub(center).Length() <= contactRadius
}
