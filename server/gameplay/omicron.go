package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const omicronCageAbility = "NomadCyberOne_Cage"

// Build-103 chunk 184 Tick moves toward a target farther than safeDistance
// before selecting another retreat. Keep this as live-target pursuit so a
// moving target cannot leave Omicron waiting indefinitely at a stale point.
func (e campaignNPCActionRuntime) produceOmicronApproach(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	profile zonenpc.ActionProfile, resume campaignNPCSupportIdleResume, timestamp uint64,
) ([][]byte, error) {
	profile.AbilityName = "OmicronApproach"
	profile.Range = 15
	plan := zonenpc.FirstActionPlan{
		ObjectID: source.Plan.ObjectID, TargetObjectID: targetObjectID,
		ActionGeneration: source.ActionGeneration, SourcePosition: source.Plan.Position,
		TargetPosition: targetPosition, Profile: profile, IsPursuitNeeded: true,
	}
	packets, err := npcraknet.Pursuit(plan)
	if err != nil {
		return nil, fmt.Errorf("omicronApproachMarshal: %w", err)
	}
	err = e.pursuit.schedule(packet, sessionKey, generation, source.Plan.ObjectID,
		timestamp, targetPosition, profile, resume.produce)
	if err != nil {
		return nil, fmt.Errorf("omicronApproachSchedule: %w", err)
	}
	return packets, nil
}

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
