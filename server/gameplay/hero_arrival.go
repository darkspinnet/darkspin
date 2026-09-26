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
	isDeathSelection bool,
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
			// Build 103 compares this field directly with a private local
			// gameplay clock that no client request publishes. Keep the
			// authoritative cooldown in the squad session and clear the
			// client field rather than extending its lock with another epoch.
			DeadlineMilliseconds: 0,
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
	peerSession.beginHeroArrival(e.now())
	e.registry.sessions[e.sessionKey] = peerSession
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
