package gameplay

import (
	"fmt"

	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Build 103 StealthModifier removes itself on TookDamage, and StealthAttack
// terminates when that modifier is lost. Both the travel and scream callbacks
// retain the modifier identity, so retiring it prevents a stale attack.
func (e campaignNPCActionRuntime) interruptStealtherDamage(sessionKey string, generation uint64, objectID uint32, timestamp uint64) ([][]byte, error) {
	e.registry.mutex.Lock()
	source, isFound := e.registry.sessions[sessionKey]
	if !isFound || source.generation != generation || source.zone == nil {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	schedules := make([]campaignStealtherSchedule, 0)
	for _, member := range e.registry.sessions {
		if member.zone != source.zone {
			continue
		}
		for _, run := range member.campaignNPCModifiers {
			if run.record.TargetObjectID == objectID && run.record.GUID == stealtherStealthModifierID &&
				run.stealther != nil {
				schedule := *run.stealther
				actor, isReplaced := source.zone.NPCs().SupersedeAction(objectID, schedule.plan.ActionGeneration)
				if isReplaced {
					schedule.plan.ActionGeneration = actor.ActionGeneration
				}
				schedules = append(schedules, schedule)
			}
		}
	}
	e.registry.mutex.Unlock()
	packets := make([][]byte, 0)
	if len(schedules) > 0 {
		actor, isFound := source.zone.NPCs().NPC(objectID)
		if isFound && !actor.IsDefeated {
			stops, err := npcraknet.StopAtPose(objectID, actor.Plan.Position, actor.Facing)
			if err != nil {
				return nil, fmt.Errorf("stealthStop: %w", err)
			}
			packets = append(packets, stops...)
		}
	}
	for _, schedule := range schedules {
		packets = append(packets, schedule.finishStealth(false, timestamp)...)
	}
	return packets, nil
}
