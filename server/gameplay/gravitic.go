package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func graviticProjectileGeometry(plan zonenpc.SpawnPlan, halfExtent sim.Position) zonenpc.ProjectileGeometry {
	scale := plan.PlacementScale
	if scale <= 0 {
		scale = 1
	}
	// Rotate the authored 8x10 base into a world-space collision envelope.
	sine, cosine := math.Sincos(float64(plan.Rotation.Z) * math.Pi / 180)
	x := float32(4*math.Abs(cosine)+5*math.Abs(sine)) * scale
	y := float32(4*math.Abs(sine)+5*math.Abs(cosine)) * scale
	return zonenpc.ProjectileGeometry{
		ProjectileHalfExtent: halfExtent,
		TargetMinimum:        sim.Position{X: -x, Y: -y},
		TargetMaximum:        sim.Position{X: x, Y: y, Z: 2.5 * scale},
	}
}

func (e *gameplayPeerSession) graviticMovementSpeedBuff() float32 {
	if e.graviticSlowObjectID == 0 || e.graviticSlowObjectID != e.deployedObjectID {
		return 0
	}
	return zonenpc.GraviticMovementSpeedBuff
}

// Poll each member independently, including stationary heroes whose nearby
// stabilizer was destroyed by an ally. Overlapping fields apply only one slow.
func (e gameplayPendingRuntime) pollGraviticFields(packet raknet.Packet) ([][]byte, error) {
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.transportGeneration != packet.TransportGeneration ||
		member.zone == nil || !member.stage.IsDungeon() ||
		!member.dungeonSetup.IsCommitted() || member.isRejoinPending ||
		member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.updateSinkholeContactLocked(&member, e.now())
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("sinkholeContact: %w", err)
	}
	graviticPackets, err := e.updateGraviticContactLocked(&member, e.now())
	if err == nil {
		e.registry.sessions[sessionKey] = member
	}
	e.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("graviticContact: %w", err)
	}
	packets = append(packets, graviticPackets...)
	err = publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return nil, fmt.Errorf("graviticPublish: %w", err)
	}
	return packets, nil
}

func (e gameplayPendingRuntime) updateGraviticContactLocked(
	member *gameplayPeerSession, now time.Time,
) ([][]byte, error) {
	fields := make([]graviticField, 0)
	for _, plan := range member.zone.FixturePlans() {
		if !zonenpc.IsGraviticStabilizer(plan) {
			continue
		}
		stabilizer, isFound := member.zone.NPCs().NPC(plan.ObjectID)
		if isFound && !stabilizer.IsDefeated && stabilizer.HitPoint > 0 {
			fields = append(fields, graviticField{objectID: plan.ObjectID, position: stabilizer.Plan.Position, radius: zonenpc.GraviticFieldRadius})
		}
	}
	for _, sourceMember := range e.registry.sessions {
		if sourceMember.zone != member.zone {
			continue
		}
		for _, shield := range sourceMember.campaignNPCDragShields {
			if now.Before(shield.expiresAt) {
				fields = append(fields, graviticField{objectID: shield.objectID, position: shield.position, radius: shield.radius})
			}
		}
	}
	packets, err := e.updateGraviticProjectilesLocked(member, fields, now)
	if err != nil {
		return nil, fmt.Errorf("graviticProjectiles: %w", err)
	}
	if len(fields) == 0 && member.graviticSlowObjectID == 0 {
		return packets, nil
	}
	position := game.Vec3(member.playerPosition)
	if member.playerMotion != nil {
		sampledPosition, err := member.playerMotion.SamplePosition(now)
		if err != nil {
			return nil, fmt.Errorf("graviticPosition: %w", err)
		}
		position = game.Vec3(sampledPosition)
	}
	nextObjectID := uint32(0)
	if member.deployedObjectID != 0 && member.deployedHitPoint() > 0 &&
		!member.isHeroSelectionPending &&
		!e.damage.npc.isTargetStatusImmuneLocked(member, member.deployedObjectID) {
		if graviticFieldSource(fields, position) != 0 {
			nextObjectID = member.deployedObjectID
		}
	}
	previousObjectID := member.graviticSlowObjectID
	if previousObjectID == nextObjectID && member.isGraviticSpeedPresented {
		return packets, nil
	}
	presentationPackets, err := e.updateGraviticPresentationLocked(member, nextObjectID)
	if err != nil {
		return nil, fmt.Errorf("graviticPresentation: %w", err)
	}
	packets = append(packets, presentationPackets...)
	member.graviticSlowObjectID = nextObjectID
	// Also clear a switched-out hero, so returning to that squad slot cannot
	// inherit a field it no longer occupies.
	if previousObjectID != 0 && previousObjectID != member.deployedObjectID {
		previousMember := *member
		previousMember.deployedObjectID = previousObjectID
		previousPacket, err := marshalGraviticSpeed(previousMember)
		if err != nil {
			return nil, fmt.Errorf("graviticPrevious: %w", err)
		}
		packets = append(packets, previousPacket)
	}
	if member.deployedObjectID == 0 {
		member.isGraviticSpeedPresented = true
		return packets, nil
	}
	currentPacket, err := marshalGraviticSpeed(*member)
	if err != nil {
		return nil, fmt.Errorf("graviticCurrent: %w", err)
	}
	if member.playerMotion != nil {
		increase := max(float32(-0.9),
			e.registry.passiveMovementIncrease(*member)+member.enemyMovementSpeedBuff())
		err = member.playerMotion.SetSpeed(now, zonePlayerMoveSpeed*(1+increase))
		if err != nil {
			return nil, fmt.Errorf("graviticMotion: %w", err)
		}
	}
	member.isGraviticSpeedPresented = true
	return append(packets, currentPacket), nil
}

func (e gameplayPendingRuntime) updateGraviticPresentationLocked(
	member *gameplayPeerSession, nextObjectID uint32,
) ([][]byte, error) {
	messages := make([]raknet.ApplicationMessage, 0, 2)
	if member.graviticModifierID != 0 && member.graviticSlowObjectID != nextObjectID {
		messages = append(messages, raknet.ModifierDeletedMessage{
			TargetID: member.graviticSlowObjectID, InstanceID: member.graviticModifierID,
		})
		err := e.modifierPool.Release(member.graviticModifierID)
		if err != nil {
			return nil, fmt.Errorf("graviticHeroRelease: %w", err)
		}
		member.graviticModifierID = 0
	}
	if member.isGraviticEffectAttached && member.graviticSlowObjectID != nextObjectID {
		messages = append(messages, raknet.AttachedEffectMessage{
			ObjectID: member.graviticSlowObjectID, Slot: member.graviticEffectSlot + 1,
			IsRemovalRequested: true, IsHardStop: true,
		})
		isReleased := e.effectPool.Release(member.graviticSlowObjectID, member.graviticEffectSlot)
		if !isReleased {
			// Hero switching can already have released all of the old hero's slots.
			e.logger.Printf("Gravitic effect slot already retired object=%d", member.graviticSlowObjectID)
		}
		member.isGraviticEffectAttached = false
	}
	if nextObjectID != 0 {
		if member.graviticModifierID == 0 {
			instanceID, err := e.modifierPool.Allocate()
			if err != nil {
				return nil, fmt.Errorf("graviticHeroAllocate: %w", err)
			}
			member.graviticModifierID = instanceID
		}
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID: nextObjectID, ModifierGUID: util.HashID("ZelemSlow"),
			InstanceID: member.graviticModifierID, StackCount: 1, StartMilliseconds: 1,
		})
		if !member.isGraviticEffectAttached {
			slot, isAllocated := e.effectPool.Allocate(nextObjectID)
			if !isAllocated {
				return nil, fmt.Errorf("graviticEffectSlot: exhausted for %d", nextObjectID)
			}
			member.graviticEffectSlot = slot
			member.isGraviticEffectAttached = true
		}
		// ZelemSlow's authored statusFootEffect owns the entry and sustained
		// snare presentation; removing the attachment stops its looping effect.
		messages = append(messages, raknet.AttachedEffectMessage{
			ObjectID: nextObjectID, Slot: member.graviticEffectSlot + 1,
			Asset: util.HashID("status_snared.ServerEventDef"), IsForceAttached: true,
		})
	}
	packets := make([][]byte, 0, len(messages))
	for _, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("graviticEffectMarshal: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func marshalGraviticSpeed(member gameplayPeerSession) ([]byte, error) {
	packet, err := member.marshalHeroSlowAttributes(
		member.deployedObjectID, member.enemyAttackSpeed(), member.enemyMovementSpeedBuff(),
	)
	if err != nil {
		return nil, fmt.Errorf("graviticAttribute: %w", err)
	}
	return packet, nil
}
