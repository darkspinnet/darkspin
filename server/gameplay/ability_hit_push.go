package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const binarySentinelSupportName = "BinarySentinelSupport"
const binarySentinelPushDistance = float32(8)
const binarySentinelPushSpeed = float32(20)
const binarySentinelPushOutro = 250 * time.Millisecond
const binarySentinelPushEffectName = "binary_sentinel_push_attractor.ServerEventDef"

type campaignAcceptedHitPushResetStep struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignAcceptedHitPushResetStep) produce() ([][]byte, error) {
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
		return nil, fmt.Errorf("heroPushResetMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignDamageRuntime) applyAcceptedHitPushes(
	packet raknet.Packet, sessionKey string, generation uint64, sourceObjectID uint32,
	timestamp uint64, plan zoneability.AreaPlan, results []zoneability.AreaResult,
) ([][]byte, error) {
	if plan.Definition.Name != binarySentinelSupportName {
		return nil, nil
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.zone.Navigation() != nil && peerSession.zone.Hero() != nil
	if !isCurrent {
		return nil, nil
	}
	hero, isHeroFound := peerSession.zone.Hero().Snapshot(
		peerSession.binding.UserID, generation,
	)
	if !isHeroFound || hero.ObjectID != sourceObjectID {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, result := range results {
		if result.Damage.IsDamageImmune || result.Damage.IsDefeated ||
			result.Damage.IsTurtleStarted || result.Damage.Damage <= 0 {
			continue
		}
		target, isTargetFound := peerSession.zone.NPCs().NPC(result.Damage.ObjectID)
		if !isTargetFound || target.IsDefeated || target.IsTurtleActive ||
			target.Plan.IsBoss {
			continue
		}
		delta := target.Plan.Position.Sub(hero.Position)
		distance := delta.Length()
		if distance <= 0 {
			delta = game.Vec3{X: 1}
			distance = 1
		}
		desired := target.Plan.Position.Add(
			delta.Scale(binarySentinelPushDistance / distance),
		)
		destination, isDestinationFound, err := navigationClippedMovementDestination(
			peerSession.zone.Navigation(), target.Plan.Position, desired,
			max(target.Plan.NPCProfile.FootprintRadius, float32(0.25)),
		)
		if err != nil {
			return packets, fmt.Errorf("heroPushDestination[%d]: %w", target.Plan.ObjectID, err)
		}
		if !isDestinationFound {
			continue
		}
		attackPlan := zonenpc.AttackPlan{
			SourceObjectID: sourceObjectID,
			TargetObjectID: target.Plan.ObjectID,
			SourcePosition: hero.Position,
			TargetPosition: target.Plan.Position,
			Profile: zonenpc.ActionProfile{
				ForcedMovementSpeed:        binarySentinelPushSpeed,
				ForcedMovementDistance:     binarySentinelPushDistance,
				ForcedMovementReactionName: "react_knockback",
				ForcedMovementEffectName:   binarySentinelPushEffectName,
			},
		}
		movementPackets, err := npcraknet.ForcedMovement(
			attackPlan, destination, timestamp,
		)
		if err != nil {
			return packets, fmt.Errorf("heroPushMarshal[%d]: %w", target.Plan.ObjectID, err)
		}
		err = peerSession.zone.NPCs().SetPosition(
			target.Plan.ObjectID, destination,
		)
		if err != nil {
			return packets, fmt.Errorf("heroPushMove[%d]: %w", target.Plan.ObjectID, err)
		}
		packets = append(packets, movementPackets...)
		movementDistance := destination.Sub(target.Plan.Position).Length()
		movementDuration := time.Duration(
			float64(movementDistance/binarySentinelPushSpeed) * float64(time.Second),
		)
		resetDelay := movementDuration + binarySentinelPushOutro
		reset := campaignAcceptedHitPushResetStep{
			runtime: e, sessionKey: sessionKey, generation: generation,
			objectID:  target.Plan.ObjectID,
			timestamp: timestamp + uint64(resetDelay/time.Millisecond),
		}
		if packet.ScheduleFunc == nil {
			return packets, errors.New("hero push reset schedule unavailable")
		}
		err = packet.ScheduleFunc(resetDelay, reset.produce)
		if err != nil {
			return packets, fmt.Errorf(
				"heroPushResetSchedule[%d]: %w", target.Plan.ObjectID, err,
			)
		}
	}
	e.registry.sessions[sessionKey] = peerSession
	return packets, nil
}
