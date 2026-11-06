package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

const omicronCageAbility = "NomadCyberOne_Cage"

func (e campaignNPCActionRuntime) produceOmicronRetreat(
	packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(objectID)
	if !isSourceFound || campaignDifficultyNounFamily(source.Plan.NounName) != "nomadcyberone" ||
		source.IsDefeated || !source.IsActionStarted || source.IsScenarioControlled() && !source.IsScenarioAIEnabled() {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	key := campaignNPCSupportKey{objectID: objectID, abilityName: omicronCageAbility}
	isRetreating := timestamp < member.campaignNPCSupportReadiness[key] ||
		member.zone.NPCs().IsOmicronRecentlyHit(objectID, e.now())
	if !isRetreating {
		if member.npcSupportIdles[objectID] != nil {
			member.clearNPCSupportIdles(objectID)
			e.registry.sessions[sessionKey] = member
		}
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	e.registry.mutex.Unlock()
	packets, err := e.produceNPCSupportIdle(packet, sessionKey, generation, source, timestamp)
	if err != nil {
		return nil, true, fmt.Errorf("omicronRetreat: %w", err)
	}
	return packets, true, nil
}
