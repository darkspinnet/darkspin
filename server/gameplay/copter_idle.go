package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// The caller holds registry.mutex. nBehavior_Wander (chunk 973) uses a
// 5-unit area and a 35% chance of a 4.4-second flavor pause between goals.
func (e *campaignScaldronCopterRun) idleLocked(
	member *gameplayPeerSession, source zonenpc.Snapshot,
) ([][]byte, error) {
	now := e.runtime.now()
	if now.Before(e.idlePauseUntil) {
		return nil, nil
	}
	if !e.isIdleMoving {
		angle := member.zone.NPCRandom().Float64() * 2 * math.Pi
		distance := float32(member.zone.NPCRandom().Float64()) * 5
		desired := e.spawnPosition.Add(game.Vec3{
			X: float32(math.Cos(angle)) * distance, Y: float32(math.Sin(angle)) * distance,
		})
		destination, isFound, err := zoneaction.NPCDirectMovementDestination(
			member.zone.Navigation(), source.Plan.Position, desired, source.NavigationRadius(), source.Navigation,
		)
		if err != nil {
			return nil, fmt.Errorf("copterWanderGoal: %w", err)
		}
		if !isFound {
			e.idlePauseUntil = now.Add(campaignScaldronCopterTick)
			return nil, nil
		}
		e.idleDestination = destination
		e.isIdleMoving = true
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isProfileFound {
		return nil, nil
	}
	step, err := member.zone.NPCs().AdvancePursuit(member.zone.Navigation(), e.objectID,
		e.idleDestination, 0.1, profile.NonCombatMovementSpeed, source.NavigationRadius(), campaignScaldronCopterMoveTick)
	if err != nil {
		return nil, fmt.Errorf("copterWanderMove: %w", err)
	}
	e.strafeTimestamp += uint64(campaignScaldronCopterMoveTick / time.Millisecond)
	if step.IsInRange || step.IsBlocked {
		e.isIdleMoving = false
		packets, stopErr := npcraknet.StopAtPose(e.objectID, step.Position, source.Facing)
		if stopErr != nil {
			return nil, fmt.Errorf("copterWanderStop: %w", stopErr)
		}
		if member.zone.NPCRandom().Float64() < 0.35 {
			e.idlePauseUntil = now.Add(4400 * time.Millisecond)
			animationPacket, animationErr := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
				ObjectID: e.objectID, State: util.HashID("wander_flavor"), Timestamp: e.strafeTimestamp, Scale: 1,
			})
			if animationErr != nil {
				return nil, fmt.Errorf("copterWanderAnimation: %w", animationErr)
			}
			packets = append(packets, animationPacket)
		}
		return packets, nil
	}
	position := step.Position
	packet, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: e.objectID, GoalFlags: 0x20, GoalPosition: raknet.Vector3(e.idleDestination),
	})
	if err != nil {
		return nil, fmt.Errorf("copterWanderPacket: %w", err)
	}
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectPositionUpdateMessage{
		ObjectID: e.objectID, PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
	})
	if err != nil {
		return nil, fmt.Errorf("copterWanderPosition: %w", err)
	}
	return [][]byte{positionPacket, packet}, nil
}
