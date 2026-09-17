package gameplay

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

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
	switch modifier.GUID {
	case util.HashID("SleepModifier"):
		peerSession.enemySleepExpiresAt = time.Time{}
	case util.HashID("StalkerShock"):
		peerSession.enemyStunExpiresAt = time.Time{}
		peerSession.enemyStunTargetObjectID = 0
	case util.HashID("SilenceModifier"):
		peerSession.enemySilenceExpiresAt = time.Time{}
	case util.HashID("EntangleModifier"), util.HashID("VerdanthBasicRootmobModifier"):
		peerSession.enemyRootExpiresAt = time.Time{}
		peerSession.enemyRootTargetObjectID = 0
	}
}

func isFieldMedicStatusSupported(guid uint32) bool {
	return guid == util.HashID("SleepModifier") ||
		guid == util.HashID("StalkerShock") ||
		guid == util.HashID("SilenceModifier") ||
		guid == util.HashID("EntangleModifier") ||
		guid == util.HashID("VerdanthBasicRootmobModifier")
}

func applyFieldMedicNPCStatus(
	npc *zonenpc.Session, objectID uint32, guid uint32, expiresAt time.Time,
) error {
	switch guid {
	case util.HashID("SleepModifier"):
		return npc.ApplySleep(objectID, expiresAt)
	case util.HashID("StalkerShock"):
		return npc.ApplyStun(objectID, expiresAt)
	case util.HashID("SilenceModifier"):
		return npc.ApplySilence(objectID, expiresAt)
	case util.HashID("EntangleModifier"), util.HashID("VerdanthBasicRootmobModifier"):
		return npc.ApplyRoot(objectID, expiresAt)
	default:
		return errors.New("unsupported Field Medic status")
	}
}

func clearFieldMedicNPCStatus(
	npc *zonenpc.Session, objectID uint32, guid uint32, expiresAt time.Time,
) {
	if npc == nil {
		return
	}
	switch guid {
	case util.HashID("SleepModifier"):
		npc.ClearSleep(objectID, expiresAt)
	case util.HashID("StalkerShock"):
		npc.ClearStun(objectID, expiresAt)
	case util.HashID("SilenceModifier"):
		npc.ClearSilence(objectID, expiresAt)
	case util.HashID("EntangleModifier"), util.HashID("VerdanthBasicRootmobModifier"):
		npc.ClearRoot(objectID, expiresAt)
	}
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
