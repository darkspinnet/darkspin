package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type sinkholeField struct {
	expiresAt time.Time
	radius    float32
	strength  float32
}

type sinkholeWind struct {
	objectID   uint32
	sourceID   uint32
	slot       uint8
	isAttached bool
}

// The Lua well supplies radius 10 and mover strength -2/-2.5/-3, rather
// than MoveTo/Pull commands. Project the inward influence onto voluntary
// movement so escaping slows and approaching accelerates without cancelling
// the player's path. This projection is server integration policy; the
// exact native gravity solver has not been recovered.
func (e gameplayPendingRuntime) updateSinkholeContactLocked(
	member *gameplayPeerSession, now time.Time,
) ([][]byte, error) {
	isFieldTracked := false
	for _, peer := range e.registry.sessions {
		if peer.zone == member.zone && len(peer.campaignNPCSinkholeFields) > 0 {
			isFieldTracked = true
			break
		}
	}
	if !isFieldTracked && !member.sinkholeWind.isAttached && member.sinkholeMovementBuff == 0 {
		return nil, nil
	}
	position := game.Vec3(member.playerPosition)
	direction := game.Vec3{}
	if member.playerMotion != nil {
		sampled, err := member.playerMotion.SamplePosition(now)
		if err != nil {
			return nil, fmt.Errorf("sinkholePosition: %w", err)
		}
		position = game.Vec3(sampled)
		direction = game.Vec3(member.playerMotion.Velocity())
		goal := member.playerMotion.Snapshot().Movement().Goal
		if game.Vec3(goal).Sub(position).Length() < 0.01 {
			direction = game.Vec3{}
		}
	}
	direction.Z = 0
	directionLength := float32(math.Sqrt(float64(direction.X*direction.X + direction.Y*direction.Y)))
	strength := float32(0)
	sourceID := uint32(0)
	isEligible := member.deployedObjectID != 0 && member.deployedHitPoint() > 0 &&
		!member.isHeroSelectionPending && !e.damage.npc.isTargetStatusImmuneLocked(member, member.deployedObjectID)
	for _, peer := range e.registry.sessions {
		if !isEligible || peer.zone != member.zone {
			continue
		}
		for objectID, field := range peer.campaignNPCSinkholeFields {
			source, isFound := member.zone.NPCs().NPC(objectID)
			if !now.Before(field.expiresAt) || !isFound || source.IsDefeated {
				continue
			}
			delta := source.Plan.Position.Sub(position)
			if delta.Length() > field.radius {
				continue
			}
			delta.Z = 0
			distance := float32(math.Sqrt(float64(delta.X*delta.X + delta.Y*delta.Y)))
			if sourceID == 0 || objectID < sourceID {
				sourceID = objectID
			}
			if distance > 0 && directionLength > 0 {
				strength += field.strength * (direction.X*delta.X + direction.Y*delta.Y) / (directionLength * distance)
			}
		}
	}
	packets, err := member.updateSinkholeWind(sourceID, e.effectPool)
	if err != nil {
		return nil, fmt.Errorf("sinkholeWind: %w", err)
	}
	buff := strength / zonePlayerMoveSpeed
	if float32(math.Abs(float64(buff-member.sinkholeMovementBuff))) < 0.001 {
		return packets, nil
	}
	member.sinkholeMovementBuff = buff
	speedPacket, err := marshalGraviticSpeed(*member)
	if err != nil {
		return nil, fmt.Errorf("sinkholeSpeed: %w", err)
	}
	if member.playerMotion != nil {
		increase := max(float32(-0.9), e.registry.passiveMovementIncrease(*member)+member.enemyMovementSpeedBuff())
		err = member.playerMotion.SetSpeed(now, zonePlayerMoveSpeed*(1+increase))
		if err != nil {
			return nil, fmt.Errorf("sinkholeMotion: %w", err)
		}
	}
	return append(packets, speedPacket), nil
}

func (e *gameplayPeerSession) updateSinkholeWind(sourceID uint32, effectPool *attachedEffectPool) ([][]byte, error) {
	packets := make([][]byte, 0, 2)
	wind := e.sinkholeWind
	if wind.isAttached && (sourceID != wind.sourceID || wind.objectID != e.deployedObjectID) {
		packet, err := npcraknet.ForcedMovementEffect(wind.objectID, wind.slot, "", true)
		if err != nil {
			return nil, fmt.Errorf("windRemove: %w", err)
		}
		if effectPool != nil {
			isReleased := effectPool.Release(wind.objectID, wind.slot)
			if !isReleased {
				// Cleanup must still reach the client after slot reclamation.
			}
		}
		e.sinkholeWind = sinkholeWind{}
		packets = append(packets, packet)
		if wind.objectID != e.deployedObjectID {
			resetPacket, resetErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
				ObjectID: wind.objectID, Value: map[uint8]float32{48: 0},
			})
			if resetErr != nil {
				return nil, fmt.Errorf("windPreviousSpeed: %w", resetErr)
			}
			packets = append(packets, resetPacket)
		}
	}
	if sourceID == 0 || e.sinkholeWind.isAttached || effectPool == nil {
		return packets, nil
	}
	slot, isAllocated := effectPool.Allocate(e.deployedObjectID)
	if !isAllocated {
		return packets, nil
	}
	e.sinkholeWind = sinkholeWind{objectID: e.deployedObjectID, sourceID: sourceID, slot: slot, isAttached: true}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		ObjectID: e.deployedObjectID, SecondaryObjectID: sourceID,
		Slot: slot + 1, Asset: util.HashID("gravity_orb_wind.ServerEventDef"), IsForceAttached: true,
	})
	if err != nil {
		return nil, fmt.Errorf("windAttach: %w", err)
	}
	return append(packets, packet), nil
}

// The action may belong to a different connection than the killing player.
// Retire every owner in the shared zone, including the affected heroes.
func (e campaignDamageRuntime) stopSinkholeFields(sessionKey string, generation uint64, objectID uint32) ([][]byte, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	source, isFound := e.registry.sessions[sessionKey]
	if !isFound || source.generation != generation || source.zone == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for key, member := range e.registry.sessions {
		if member.zone != source.zone {
			continue
		}
		delete(member.campaignNPCSinkholeFields, objectID)
		packet, err := member.stopCampaignSinkholeEffect(objectID, e.effectPool)
		if err != nil {
			return nil, fmt.Errorf("sinkholeSourceRemove: %w", err)
		}
		if len(packet) > 0 {
			packets = append(packets, packet)
		}
		e.registry.sessions[key] = member
	}
	pending := gameplayPendingRuntime{registry: e.registry, effectPool: e.effectPool, damage: e}
	for key, member := range e.registry.sessions {
		if member.zone != source.zone {
			continue
		}
		cleanupPackets, err := pending.updateSinkholeContactLocked(&member, e.npc.now())
		if err != nil {
			return nil, fmt.Errorf("sinkholeTargetRemove: %w", err)
		}
		packets = append(packets, cleanupPackets...)
		e.registry.sessions[key] = member
	}
	return packets, nil
}
