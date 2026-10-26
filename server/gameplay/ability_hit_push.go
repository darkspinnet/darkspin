package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
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
	runtime          campaignDamageRuntime
	sessionKey       string
	generation       uint64
	objectID         uint32
	timestamp        uint64
	actionGeneration uint64
	positionRevision uint64
	effects          *channelEffects
}

func (e campaignAcceptedHitPushResetStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if isCurrent && peerSession.campaignAcceptedHitPushEffects[e.objectID] == e.effects {
		delete(peerSession.campaignAcceptedHitPushEffects, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.effects.Retire()
	e.runtime.registry.commitChannelRemovalsLocked()
	if isCurrent {
		target, isTargetFound := peerSession.zone.NPCs().LiveNPC(e.objectID)
		isCurrent = isTargetFound && target.ActionGeneration == e.actionGeneration &&
			target.PositionRevision == e.positionRevision
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := npcraknet.ResetAnimation(e.objectID, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("heroPushResetMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *gameplayPeerSession) retireAcceptedHitPushEffect(objectID uint32) {
	effects := e.campaignAcceptedHitPushEffects[objectID]
	if effects == nil {
		return
	}
	effects.Retire()
	delete(e.campaignAcceptedHitPushEffects, objectID)
}

func (e *gameplaySessionRegistry) retireAcceptedHitPushEffectForDeath(
	sessionKey string, generation uint64, objectID uint32,
) {
	e.mutex.Lock()
	member, isFound := e.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.mutex.Unlock()
		return
	}
	for candidateKey, candidate := range e.sessions {
		if candidate.zone == member.zone {
			candidate.retireAcceptedHitPushEffect(objectID)
			e.sessions[candidateKey] = candidate
		}
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == member.zone {
			retained.peerSession.retireAcceptedHitPushEffect(objectID)
		}
	}
	e.commitChannelRemovalsLocked()
	e.mutex.Unlock()
}

func (e campaignDamageRuntime) applyAcceptedHitPushes(
	packet raknet.Packet, sessionKey string, generation uint64, sourceObjectID uint32,
	timestamp uint64, plan zoneability.AreaPlan, results []zoneability.AreaResult,
) ([][]byte, error) {
	pushDistance := binarySentinelPushDistance
	pushSpeed := binarySentinelPushSpeed
	pushOutro := binarySentinelPushOutro
	isComet := plan.Definition.Name == "SpacetimeRandom3"
	if isComet {
		// Build 103 CometKnockback inherits the shared jump template and
		// overrides distance, speed and recovery, without an attached effect.
		pushDistance = 4
		pushSpeed = 12
		pushOutro = 500 * time.Millisecond
	} else if plan.Definition.Name != binarySentinelSupportName {
		return nil, nil
	}
	if packet.ScheduleFunc == nil {
		return nil, errors.New("hero push reset schedule unavailable")
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
			target.IsMutationKnockbackImmune() || target.Plan.IsBoss || target.HasAffixModifier(surefootedNPCAffixModifierName) {
			continue
		}
		if peerSession.zone.NPCs().RootRemaining(target.Plan.ObjectID, e.npc.now()) > 0 {
			continue
		}
		delta := target.Plan.Position.Sub(hero.Position)
		distance := delta.Length()
		if distance <= 0 {
			delta = game.Vec3{X: 1}
			distance = 1
		}
		desired := target.Plan.Position.Add(
			delta.Scale(pushDistance / distance),
		)
		destination, isDestinationFound, err := navigationClippedMovementDestination(
			peerSession.zone.Navigation(), target.Plan.Position, desired,
			max(target.NavigationRadius(), float32(0.25)), target.Navigation,
		)
		if err != nil {
			return packets, fmt.Errorf("heroPushDestination[%d]: %w", target.Plan.ObjectID, err)
		}
		if !isDestinationFound {
			continue
		}
		interruptionPackets, err :=
			e.registry.interruptCampaignNPCForForcedMovementLocked(
				sessionKey, &peerSession, target.Plan.ObjectID,
			)
		if err != nil {
			return packets, fmt.Errorf(
				"heroPushInterrupt[%d]: %w", target.Plan.ObjectID, err,
			)
		}
		packets = append(packets, interruptionPackets...)
		attackPlan := zonenpc.AttackPlan{
			SourceObjectID: sourceObjectID,
			TargetObjectID: target.Plan.ObjectID,
			SourcePosition: hero.Position,
			TargetPosition: target.Plan.Position,
			Profile: zonenpc.ActionProfile{
				ForcedMovementSpeed:        pushSpeed,
				ForcedMovementDistance:     pushDistance,
				ForcedMovementReactionName: "react_knockback",
			},
		}
		// The shared knockback template supplies a two-unit arc and a
		// four-unit distance threshold; Kinetic Wave overrides speed/distance.
		movementPackets, err := npcraknet.ForcedJump(
			attackPlan, destination, timestamp, [3]float32{2, 2, 4},
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
			float64(movementDistance/pushSpeed) * float64(time.Second),
		)
		resetDelay := movementDuration + pushOutro
		// JumpInDirection owns locomotion until landing and the modifier's
		// outro completes; do not let AI pursuit overwrite that client mover.
		err = peerSession.zone.NPCs().ApplyStun(
			target.Plan.ObjectID, e.npc.now().Add(resetDelay),
		)
		if err != nil {
			return packets, fmt.Errorf("heroPushMotionHold[%d]: %w", target.Plan.ObjectID, err)
		}
		if isComet {
			latestTarget, isLatestTargetFound := peerSession.zone.NPCs().NPC(target.Plan.ObjectID)
			if !isLatestTargetFound {
				continue
			}
			reset := campaignAcceptedHitPushResetStep{
				runtime: e, sessionKey: sessionKey, generation: generation,
				objectID:         target.Plan.ObjectID,
				timestamp:        timestamp + uint64(resetDelay/time.Millisecond),
				actionGeneration: latestTarget.ActionGeneration,
				positionRevision: latestTarget.PositionRevision,
			}
			err = packet.ScheduleFunc(resetDelay, reset.produce)
			if err != nil {
				return packets, fmt.Errorf("cometResetSchedule[%d]: %w", target.Plan.ObjectID, err)
			}
			continue
		}
		effects, err := e.registry.allocateChannelEffectsLocked(
			peerSession.zone, e.effectPool, []uint32{target.Plan.ObjectID},
		)
		if err != nil {
			return packets, fmt.Errorf("heroPushEffectSlot[%d]: %w", target.Plan.ObjectID, err)
		}
		slot := effects.Slots()[0].slot
		effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: slot + 1, ObjectID: target.Plan.ObjectID,
			Asset: util.HashID(binarySentinelPushEffectName), IsForceAttached: true,
		})
		if err != nil {
			effects.Retire()
			e.registry.commitChannelRemovalsLocked()
			return packets, fmt.Errorf("heroPushEffectMarshal[%d]: %w", target.Plan.ObjectID, err)
		}
		reset := campaignAcceptedHitPushResetStep{
			runtime: e, sessionKey: sessionKey, generation: generation,
			objectID: target.Plan.ObjectID, effects: effects,
			timestamp: timestamp + uint64(resetDelay/time.Millisecond),
		}
		latestTarget, isLatestTargetFound := peerSession.zone.NPCs().NPC(target.Plan.ObjectID)
		if !isLatestTargetFound {
			effects.Retire()
			e.registry.commitChannelRemovalsLocked()
			continue
		}
		reset.actionGeneration = latestTarget.ActionGeneration
		reset.positionRevision = latestTarget.PositionRevision
		err = packet.ScheduleFunc(resetDelay, reset.produce)
		if err != nil {
			effects.Retire()
			e.registry.commitChannelRemovalsLocked()
			return packets, fmt.Errorf(
				"heroPushResetSchedule[%d]: %w", target.Plan.ObjectID, err,
			)
		}
		peerSession.retireAcceptedHitPushEffect(target.Plan.ObjectID)
		e.registry.commitChannelRemovalsLocked()
		if peerSession.campaignAcceptedHitPushEffects == nil {
			peerSession.campaignAcceptedHitPushEffects = make(map[uint32]*channelEffects)
		}
		peerSession.campaignAcceptedHitPushEffects[target.Plan.ObjectID] = effects
		e.registry.sessions[sessionKey] = peerSession
		err = e.registry.publishChannelEffectsLocked(effects, [][]byte{effectPacket})
		if err != nil {
			peerSession.retireAcceptedHitPushEffect(target.Plan.ObjectID)
			e.registry.commitChannelRemovalsLocked()
			return packets, fmt.Errorf("heroPushEffectPublish[%d]: %w", target.Plan.ObjectID, err)
		}
		peerSession = e.registry.sessions[sessionKey]
	}
	e.registry.sessions[sessionKey] = peerSession
	return packets, nil
}
