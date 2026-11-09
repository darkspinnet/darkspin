package gameplay

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Build-103 SleepModifier ends on every TookDamage event, including DoT.
// SleepingCloud_SleepModifier has its own authored descriptor filter instead.
func (e *gameplaySessionRegistry) wakeTransferredSleepLocked(
	originalZone *zone.Zone, objectID uint32, pool *modifierPool,
) error {
	runsByIDs := make(map[uint32]*campaignNPCModifierRun)
	for _, member := range e.sessions {
		if member.zone != originalZone {
			continue
		}
		for instanceID, run := range member.campaignNPCModifiers {
			runsByIDs[instanceID] = run
		}
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone != originalZone {
			continue
		}
		for instanceID, run := range retained.peerSession.campaignNPCModifiers {
			runsByIDs[instanceID] = run
		}
	}
	runs := make([]*campaignNPCModifierRun, 0)
	packets := make([][]byte, 0)
	for instanceID, run := range runsByIDs {
		if run == nil || run.record.TargetObjectID != objectID ||
			run.record.GUID != util.HashID("SleepModifier") || !run.isActive() {
			continue
		}
		packet, err := effectraknet.ModifierDelete(objectID, instanceID)
		if err != nil {
			return fmt.Errorf("transferWakeDelete: %w", err)
		}
		runs = append(runs, run)
		packets = append(packets, packet)
	}
	// Preflight every deletion, then queue before releasing any handle so a
	// new allocation cannot overtake its old deletion on an outgoing queue.
	for candidateKey, candidate := range e.sessions {
		if candidate.zone != originalZone {
			continue
		}
		candidate.queuePackets(packets)
		e.sessions[candidateKey] = candidate
	}
	var wakeErr error
	for _, run := range runs {
		originalZone.NPCs().ClearSleepModifier(objectID, run.instanceID)
		originalZone.Effect().Remove(run.instanceID)
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		for sessionKey, member := range e.sessions {
			if member.zone == originalZone {
				member.untrackCampaignNPCModifier(run)
				e.sessions[sessionKey] = member
			}
		}
		for _, retained := range e.retainedSessions {
			if retained.peerSession.zone == originalZone {
				retained.peerSession.untrackCampaignNPCModifier(run)
			}
		}
		isCreated, releaseErr := run.release(pool)
		if releaseErr != nil {
			wakeErr = errors.Join(wakeErr, fmt.Errorf("transferWakeRelease: %w", releaseErr))
			continue
		}
		if !isCreated {
			wakeErr = errors.Join(wakeErr, errors.New("active transferred sleep was unpublished"))
		}
	}
	if wakeErr != nil {
		return fmt.Errorf("transferWake: %w", wakeErr)
	}
	return nil
}

func fieldMedicCapturedDebuffs(
	peerSession gameplayPeerSession, center game.Vec3, radius float32,
) fieldMedicDebuffCapture {
	selected := make(map[uint32]zoneeffect.Modifier)
	originals := make([]zoneeffect.Modifier, 0)
	for _, modifier := range peerSession.zone.Effect().Snapshot() {
		if modifier.Kind != zoneeffect.ModifierKindDebuff || modifier.IsChannel ||
			!isFieldMedicStatusSupported(modifier.GUID) ||
			!fieldMedicAllyInRange(peerSession, modifier.TargetObjectID, center, radius) {
			continue
		}
		originals = append(originals, modifier)
		current, isFound := selected[modifier.GUID]
		if !isFound || current.Rank < modifier.Rank {
			selected[modifier.GUID] = modifier
		}
	}
	winners := make([]zoneeffect.Modifier, 0, len(selected))
	for _, current := range selected {
		winners = append(winners, current)
	}
	sort.Slice(winners, func(left, right int) bool {
		if winners[left].GUID == winners[right].GUID {
			return winners[left].InstanceID < winners[right].InstanceID
		}
		return winners[left].GUID < winners[right].GUID
	})
	return fieldMedicDebuffCapture{winners: winners, originals: originals}
}

func fieldMedicAllyInRange(
	peerSession gameplayPeerSession, objectID uint32,
	center game.Vec3, radius float32,
) bool {
	if actor, isFound := peerSession.zone.Hero().SnapshotByObjectID(objectID); isFound {
		return actor.HitPoint > 0 && zonegeometry.Distance(center, actor.Position) <= radius
	}
	actor, isFound := peerSession.zone.Companion().Snapshot(objectID)
	return isFound && actor.HitPoint > 0 && actor.IsTargetable &&
		zonegeometry.Distance(center, actor.Position) <= radius
}

func clearFieldMedicHeroStatus(
	peerSession *gameplayPeerSession, modifier zoneeffect.Modifier,
) {
	if peerSession == nil || peerSession.deployedObjectID != modifier.TargetObjectID {
		return
	}
	peerSession.clearHeroControl(modifier.InstanceID, modifier.TargetObjectID)
}

func isFieldMedicStatusSupported(guid uint32) bool {
	return guid == util.HashID("SleepModifier") ||
		guid == util.HashID("StalkerShock") ||
		guid == util.HashID("SilenceModifier") ||
		guid == util.HashID("CitadelSpecialTwo_SilenceModifier") ||
		guid == util.HashID("EntangleModifier") ||
		guid == util.HashID("VerdanthBasicRootmobModifier") ||
		guid == util.HashID("NomadSnipe_SlowDebuff")
}

func applyFieldMedicNPCStatus(
	npc *zonenpc.Session, objectID, instanceID uint32, modifier zoneeffect.Modifier, at, expiresAt time.Time,
) (bool, error) {
	switch modifier.GUID {
	case util.HashID("SleepModifier"):
		isAdmitted, err := npc.AdmitSleepModifier(objectID, instanceID, at, expiresAt)
		if err != nil {
			return false, fmt.Errorf("fieldMedicSleep: %w", err)
		}
		return isAdmitted, nil
	case util.HashID("EntangleModifier"), util.HashID("VerdanthBasicRootmobModifier"):
		isAdmitted, err := npc.AdmitRootModifier(objectID, instanceID, at, expiresAt)
		if err != nil {
			return false, fmt.Errorf("fieldMedicRoot: %w", err)
		}
		return isAdmitted, nil
	case util.HashID("NomadSnipe_SlowDebuff"):
		err := npc.ApplySlowModifier(objectID, instanceID, expiresAt,
			max(float32(0.05), 1+modifier.MovementSpeedBuff),
			max(float32(0.05), 1+modifier.AttackSpeed))
		if err != nil {
			return false, fmt.Errorf("fieldMedicSlow: %w", err)
		}
		return true, nil
	default:
		return false, errors.New("unsupported Field Medic status")
	}
}

func clearFieldMedicNPCStatus(
	npc *zonenpc.Session, objectID, instanceID uint32, guid uint32, expiresAt time.Time,
) {
	if npc == nil {
		return
	}
	switch guid {
	case util.HashID("SleepModifier"):
		npc.ClearSleepModifier(objectID, instanceID)
	case util.HashID("StalkerShock"):
		npc.ClearStunModifier(objectID, instanceID)
	case util.HashID("SilenceModifier"), util.HashID("CitadelSpecialTwo_SilenceModifier"):
		npc.ClearSilenceModifier(objectID, instanceID)
	case util.HashID("EntangleModifier"), util.HashID("VerdanthBasicRootmobModifier"):
		npc.ClearRootModifier(objectID, instanceID)
	case util.HashID("NomadSnipe_SlowDebuff"):
		npc.ClearSlowModifier(objectID, instanceID)
	}
}

func fieldMedicSlowAttributes(peerSession gameplayPeerSession, modifier zoneeffect.Modifier, at time.Time) ([]byte, error) {
	if modifier.GUID != util.HashID("NomadSnipe_SlowDebuff") {
		return nil, nil
	}
	movementScale, attackScale := peerSession.zone.NPCs().SlowProfile(modifier.TargetObjectID, at)
	packet, err := marshalCampaignNPCSlowAttributes(modifier.TargetObjectID, attackScale-1, movementScale-1)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicSlowAttributes: %w", err)
	}
	return packet, nil
}

func fieldMedicEffectPacket(effectName string, objectID uint32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(effectName), ObjectID: objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("fieldMedicEffectMarshal: %w", err)
	}
	return packet, nil
}
