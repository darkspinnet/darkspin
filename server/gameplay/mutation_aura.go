package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e gameplayPendingRuntime) pollMutationAgentsLocked(member *gameplayPeerSession) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, npc := range member.zone.NPCs().PromoteMutationAllies() {
		messages := []raknet.ApplicationMessage{
			raknet.AttributeDataUpdateMessage{
				ObjectID: npc.Plan.ObjectID, Value: map[uint8]float32{
					4:                              npc.Plan.NPCProfile.HitPoint,
					uint8(game.AttributeBodyScale): zonenpc.EliteBonusForType(npc.Plan.NPCProfile.NPCType).BodyScale,
				},
			},
			raknet.ModifierCreatedMessage{
				TargetID: npc.Plan.ObjectID, SourceID: npc.Plan.ObjectID,
				ModifierGUID:      util.HashID(zonenpc.EliteModifierName),
				InstanceID:        zoneeffect.NounModifierInstanceID(npc.Plan.ObjectID),
				StartMilliseconds: 1, StackCount: 1,
				DurationMilliseconds: uint32(zonenpc.EliteModifierDuration.Milliseconds()),
			},
			raknet.CombatantDataDeltaMessage{
				ObjectID: npc.Plan.ObjectID, HitPoints: npc.HitPoint, IsHitPointChanged: true,
			},
			raknet.ObjectEffectMessage{
				ObjectID: npc.Plan.ObjectID,
				Asset:    util.HashID("mutant_agent_infect_smoke_hit.ServerEventDef"),
			},
		}
		for _, message := range messages {
			packet, err := raknet.MarshalApplication(message)
			if err != nil {
				return nil, fmt.Errorf("mutationMarshal: %w", err)
			}
			packets = append(packets, packet)
		}
	}
	if len(packets) != 0 {
		for sessionKey, ally := range e.registry.sessions {
			if ally.zone != member.zone || ally.binding.UserID == member.binding.UserID {
				continue
			}
			ally.queuePackets(packets)
			e.registry.sessions[sessionKey] = ally
		}
	}
	return packets, nil
}

func mutationAgentDeathPackets(objectID uint32) ([][]byte, error) {
	messages := []raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: 17, ObjectID: objectID, IsRemovalRequested: true, IsHardStop: true,
		},
		raknet.ObjectEffectMessage{
			ObjectID: objectID, Asset: util.HashID("mutation_agent_explosion.ServerEventDef"),
		},
	}
	packets := make([][]byte, 0, len(messages))
	for _, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("mutationDeath: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
