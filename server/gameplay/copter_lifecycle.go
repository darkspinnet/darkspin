package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e *campaignScaldronCopterRun) isCurrent(member gameplayPeerSession) bool {
	if member.generation != e.generation || member.zone == nil || member.isZoneTerminal() ||
		member.zone.NPCs() == nil || member.campaignNPCCopterRuns[e.objectID] != e {
		return false
	}
	source, isFound := member.zone.NPCs().NPC(e.objectID)
	return isFound && source.IsPublished && !source.IsDefeated && source.HitPoint > 0
}

// The passive is independent of CircleTarget and starts before aggro, just as
// AIDefinition.PassiveAbility does. Only one peer owns each shared controller.
func (e campaignNPCActionRuntime) startCopterControllers(
	packet raknet.Packet, sessionKey string, generation uint64,
	plans []zonenpc.SpawnPlan, timestamp uint64,
) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, plan := range plans {
		profile, isFound := zonenpc.ActionProfileForPlan(plan)
		if !isFound || profile.AbilityName != "ScaldronBasicCopter_Passive" {
			continue
		}
		startPackets, err := e.produceScaldronBasicCopter(packet, sessionKey, generation, plan.ObjectID, timestamp)
		if err != nil {
			return nil, fmt.Errorf("copterControllerStart: %w", err)
		}
		packets = append(packets, startPackets...)
	}
	return packets, nil
}

// ScaldronBasicCopter_Passive deactivates on death. Remove both beams owned
// by the dead bot and incoming links owned by surviving peers immediately.
func (e campaignNPCActionRuntime) stopCopterLinks(
	sessionKey string, generation uint64, defeatedObjectID uint32,
) ([][]byte, error) {
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
		for objectID, run := range member.campaignNPCCopterRuns {
			if objectID == defeatedObjectID {
				cleanupPackets, err := run.releaseLinks()
				if err != nil {
					return nil, fmt.Errorf("copterDeathLinks: %w", err)
				}
				packets = append(packets, cleanupPackets...)
				delete(member.campaignNPCCopterRuns, objectID)
				continue
			}
			slot, isLinked := run.effectSlots[defeatedObjectID]
			if !isLinked {
				continue
			}
			packet, err := npcraknet.CopterLinkEffect(objectID, 0, slot, "", true)
			if err != nil {
				return nil, fmt.Errorf("copterDeathIncoming: %w", err)
			}
			run.runtime.effectPool.Release(objectID, slot)
			delete(run.effectSlots, defeatedObjectID)
			packets = append(packets, packet)
		}
		e.registry.sessions[key] = member
	}
	return packets, nil
}
