package gameplay

import (
	"fmt"

	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

// DOT149 deactivates OnAgentDeath of its recipient. Initiator/source death
// does not match this selector and cannot stop another actor's retained poison.
func (e campaignNPCActionRuntime) retireEggPoisonOnDeath(sessionKey string, generation uint64, objectID uint32) ([][]byte, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := member.zone.NPCs().NPC(objectID)
	if !isTargetFound || !target.IsDefeated || target.HitPoint > 0 {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	runs := make(map[uint32]*campaignNPCModifierRun)
	for _, candidate := range e.registry.sessions {
		if candidate.zone != member.zone {
			continue
		}
		for instanceID, run := range candidate.campaignNPCModifiers {
			if run != nil && run.fixtureBurn != nil && run.fixtureBurn.profile.ModifierName == "EggPoison" && run.record.TargetObjectID == objectID && !run.fixtureBurn.isRetiring && run.isActive() {
				runs[instanceID] = run
			}
		}
	}
	packets := make([][]byte, 0, len(runs))
	cancels := make([]func(), 0, len(runs))
	var retireErr error
	for instanceID, run := range runs {
		packet, err := effectraknet.ModifierDelete(objectID, instanceID)
		if err != nil {
			retireErr = fmt.Errorf("eggDeathDelete: %w", err)
			break
		}
		isCreated, isDeferred, err := retireFixtureRunLocked(run, e.modifierPool)
		if err != nil {
			retireErr = fmt.Errorf("eggDeathRelease: %w", err)
			break
		}
		if isDeferred {
			continue
		}
		for key, candidate := range e.registry.sessions {
			if candidate.zone == member.zone {
				candidate.untrackCampaignNPCModifier(run)
				e.registry.sessions[key] = candidate
			}
		}
		run.mu.Lock()
		if run.cancel != nil {
			cancels = append(cancels, run.cancel)
			run.cancel = nil
		}
		run.mu.Unlock()
		if isCreated {
			packets = append(packets, packet)
		}
	}
	e.registry.mutex.Unlock()
	// A producer may need registry.mutex to settle; never cancel under it.
	for _, cancel := range cancels {
		cancel()
	}
	if retireErr != nil {
		return packets, fmt.Errorf("eggDeathRetire: %w", retireErr)
	}
	return packets, nil
}
