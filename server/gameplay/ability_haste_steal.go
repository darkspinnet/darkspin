package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (r campaignAbilityCommandRuntime) applyLightspeedHasteSteal(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, hitTimestamp uint64, plan zoneability.AreaPlan,
	results []zoneability.AreaResult,
) ([][]byte, error) {
	if plan.Definition.Name != "LightspeedTempestActive" {
		return nil, nil
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.zone == nil || peerSession.zone.Effect() == nil ||
		peerSession.deployedObjectID != sourceObjectID {
		return nil, nil
	}
	transferPacket := packet
	transferPacket.SourceTime = hitTimestamp
	helper := fieldMedicActiveSchedule{
		runtime: r, packet: transferPacket, sessionKey: sessionKey,
		generation: generation, sourceObjectID: sourceObjectID,
	}
	packets := make([][]byte, 0)
	for _, result := range results {
		if result.Damage.IsDamageImmune || result.Damage.Damage <= 0 {
			continue
		}
		modifiers := lightspeedTargetHaste(
			peerSession.zone.Effect().Snapshot(), result.Damage.ObjectID,
		)
		isHasteTransferred := false
		for _, modifier := range modifiers {
			stackCount := max(uint32(1), modifier.StackCount)
			isEveryStackTransferred := true
			for range stackCount {
				stack := modifier
				stack.StackCount = 1
				stack.Duration = modifier.Duration
				transferPacket, isTransferred, err :=
					helper.transferBuffToHeroLocked(sessionKey, &peerSession, stack)
				if err != nil {
					return nil, fmt.Errorf(
						"lightspeedTransfer[%d/%#x]: %w",
						result.Damage.ObjectID, modifier.GUID, err,
					)
				}
				if isTransferred {
					packets = append(packets, transferPacket)
					isHasteTransferred = true
				} else {
					isEveryStackTransferred = false
				}
			}
			if !isEveryStackTransferred {
				continue
			}
			r.registry.sessions[sessionKey] = peerSession
			removed, removePackets, err := helper.removeEnemyBuffOriginalLocked(
				&peerSession, modifier,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"lightspeedOriginal[%d/%#x]: %w",
					result.Damage.ObjectID, modifier.GUID, err,
				)
			}
			if removed {
				packets = append(packets, removePackets...)
			}
			peerSession = r.registry.sessions[sessionKey]
		}
		if isHasteTransferred {
			effectPacket, err := npcraknet.PositionedEffect(
				"spacetime_AOE_hasteMark_effect.ServerEventDef",
				result.Snapshot.Plan.Position,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"lightspeedHasteMark[%d]: %w", result.Damage.ObjectID, err,
				)
			}
			packets = append(packets, effectPacket)
		}
	}
	r.registry.sessions[sessionKey] = peerSession
	return packets, nil
}

func lightspeedTargetHaste(
	modifiers []zoneeffect.Modifier, targetObjectID uint32,
) []zoneeffect.Modifier {
	selected := make([]zoneeffect.Modifier, 0)
	for _, modifier := range modifiers {
		if modifier.TargetObjectID != targetObjectID || !modifier.IsHaste ||
			modifier.Kind != zoneeffect.ModifierKindBuff {
			continue
		}
		selected = append(selected, modifier)
	}
	return selected
}
