package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
)

func marshalCampaignCharacterSwitch(
	playerIndex uint8, sourceObjectID uint32, targetObjectID uint32, creatureIndex uint32,
	sourceCreature, targetCreature game.GameplayCreature,
	sourceHitPoint, sourcePowerPoint, targetHitPoint, targetPowerPoint float32,
	position raknet.Vector3, orientation raknet.Quaternion, animationTimestamp uint64,
	deployCooldownDeadline uint64, isDeathSelection bool,
) ([][]byte, error) {
	sourceBeam := campaignCharacterBeam(sourceCreature, false)
	messages := make([]raknet.ApplicationMessage, 0, 15)
	if !isDeathSelection {
		messages = append(messages,
			raknet.PositionedEffectMessage{
				Asset: util.HashID(sourceBeam + ".ServerEventDef"), Position: position,
			},
			raknet.SetAnimationStateMessage{
				ObjectID: sourceObjectID, State: util.HashID("character_teleport_out"),
				Timestamp: animationTimestamp, Scale: 1,
			},
		)
	}
	messages = append(messages, raknet.ObjectUpdateMessage{
		ObjectID: sourceObjectID, PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		IsVisible: false,
	})
	messages = append(messages,
		raknet.CombatantDataDeltaMessage{
			ObjectID: sourceObjectID, HitPoints: sourceHitPoint, IsHitPointChanged: true,
			ManaPoints: sourcePowerPoint, IsManaPointChanged: true,
		},
		raknet.LabsPlayerControlledObjectMessage{Slot: playerIndex, ObjectID: targetObjectID},
		raknet.PlayerCharacterDeployMessage{
			PlayerIndex: playerIndex, CreatureIndex: creatureIndex, ObjectID: targetObjectID,
		},
		raknet.LabsPlayerDeployCooldownMessage{
			PlayerSlot: playerIndex, DeployedCreatureIndex: creatureIndex,
			DeadlineMilliseconds: deployCooldownDeadline,
		},
	)
	messages = append(messages, campaignCharacterArrivalMessages(
		targetObjectID, targetCreature, position, orientation,
		animationTimestamp,
	)...)
	// Deploy can replace the target object's local combatant state. Publish the
	// authoritative resource state after arrival so the power bar and the
	// client's ability admission read the same value.
	messages = append(messages, raknet.CombatantDataDeltaMessage{
		ObjectID: targetObjectID, HitPoints: targetHitPoint, IsHitPointChanged: true,
		ManaPoints: targetPowerPoint, IsManaPointChanged: true,
	})
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("switchPacket[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func campaignCharacterArrivalMessages(
	targetObjectID uint32, targetCreature game.GameplayCreature,
	position raknet.Vector3, orientation raknet.Quaternion,
	animationTimestamp uint64,
) []raknet.ApplicationMessage {
	if orientation.X == 0 && orientation.Y == 0 &&
		orientation.Z == 0 && orientation.W == 0 {
		orientation.W = 1
	}
	targetBeam := campaignCharacterBeam(targetCreature, true)
	messages := []raknet.ApplicationMessage{
		raknet.ObjectTeleportMessage{
			ObjectID: targetObjectID, Position: position, Orientation: orientation,
		},
		raknet.ObjectUpdateMessage{
			ObjectID: targetObjectID, PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
			IsVisible: true,
		},
	}
	return append(messages, heroraknet.ArrivalMessages(
		targetObjectID, targetBeam, animationTimestamp,
	)...)
}

// DeployModifier holds Immobilized until WaitUntilTime(1), after the landing.
const heroArrivalLockDuration = time.Second

// DeployModifier (Modifiers/0xAFF01FD0.lua) waits until 0.5 before
// applying its knockback, then until 1.0 before completing deployment.
const heroArrivalKnockbackDelay = 500 * time.Millisecond

func (e gameplayPeerSession) isHeroArrivalProtected(at time.Time) bool {
	return e.heroInputLockedObjectID != 0 && at.Before(e.heroInputLockedUntil)
}

func (e gameplayPeerSession) isHeroDamageImmune(at time.Time) bool {
	return e.isGodModeEnabled || e.campaignTunnelTransfer != nil || e.isHeroArrivalProtected(at) || e.heroModifierRun.IsDamageImmune() ||
		e.heroQuantumBlink != nil || e.hasOmniShieldAt(e.deployedObjectID, at)
}

func advanceHeroArrivalTimestamp(
	timestamp uint64, startedAt time.Time, currentTime time.Time,
) uint64 {
	if startedAt.IsZero() || !currentTime.After(startedAt) {
		return timestamp
	}
	elapsedMilliseconds := uint64(currentTime.Sub(startedAt) / time.Millisecond)
	if ^uint64(0)-timestamp < elapsedMilliseconds {
		return ^uint64(0)
	}
	return timestamp + elapsedMilliseconds
}

func (e *gameplayPeerSession) beginHeroArrival(at time.Time) {
	e.heroInputLockedObjectID = e.deployedObjectID
	e.heroInputLockedUntil = at.Add(heroArrivalLockDuration)
}

func campaignCharacterEntry(creature game.GameplayCreature) string {
	return strings.Replace(
		campaignCharacterBeam(creature, true), "character_beam_in_", "character_entry_", 1,
	)
}

// Start the mission-entry lock at response commit so scene preparation does
// not consume the landing interval before the client receives its animation.
type heroArrivalCommit struct {
	registry   *gameplaySessionRegistry
	sessionKey string
	generation uint64
	objectID   uint32
	now        func() time.Time
}

func (e heroArrivalCommit) commit() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		peerSession.deployedObjectID != e.objectID {
		return
	}
	at := e.now()
	peerSession.beginHeroArrival(at)
	packets, err := peerSession.refreshEnemyImmobilizedAttributes(at)
	if err != nil {
		e.registry.logger.Printf("RakNet hero arrival movement hold failed object=%d: %v", e.objectID, err)
	}
	e.registry.sessions[e.sessionKey] = peerSession
	e.registry.queueProjectileRetirementLocked(peerSession.zone, packets)
}

func marshalCampaignCharacterDeparture(
	sourceObjectID uint32, sourceCreature game.GameplayCreature,
	position raknet.Vector3, animationTimestamp uint64,
) ([][]byte, error) {
	sourceBeam := campaignCharacterBeam(sourceCreature, false)
	messages := []raknet.ApplicationMessage{
		raknet.PositionedEffectMessage{
			Asset: util.HashID(sourceBeam + ".ServerEventDef"), Position: position,
		},
		raknet.SetAnimationStateMessage{
			ObjectID: sourceObjectID, State: util.HashID("character_teleport_out"),
			Timestamp: animationTimestamp, Scale: 1,
		},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("departurePacket[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
