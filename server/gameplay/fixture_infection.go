package gameplay

import (
	"fmt"

	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

// The caller holds the registry lock after accepting the new scheduler group.
// Direct ScalDiseasePlant requests replace only the spread GUID; the primary
// and independently owned BioEntangle root never change here.
func (e campaignFixtureBlastStep) acceptSpreadLocked(run *campaignNPCModifierRun) ([][]byte, []func(), bool, error) {
	state := run.fixtureBurn
	targetID := run.record.TargetObjectID
	run.mu.Lock()
	if run.isCreated || run.isReleased {
		run.mu.Unlock()
		return nil, nil, false, nil
	}
	previousIDs := state.npc.InfectionSpreadInstances(targetID)
	deletesByID := make(map[uint32][]byte, len(previousIDs))
	for _, instanceID := range previousIDs {
		packet, err := effectraknet.ModifierDelete(targetID, instanceID)
		if err != nil {
			run.mu.Unlock()
			return nil, nil, false, fmt.Errorf("fixtureReplaceDelete: %w", err)
		}
		deletesByID[instanceID] = packet
	}
	replacedIDs, isAccepted := state.npc.AcceptInfectionSpread(targetID, run.instanceID, e.runtime.npc.now())
	if !isAccepted {
		run.mu.Unlock()
		return nil, nil, false, nil
	}
	run.isCreated = true
	run.mu.Unlock()
	replacedInstances := make(map[uint32]struct{}, len(replacedIDs))
	for _, instanceID := range replacedIDs {
		replacedInstances[instanceID] = struct{}{}
	}
	cancels := make([]func(), 0)
	deferredInstances := make(map[uint32]bool)
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone == nil || candidate.zone.NPCs() != state.npc {
			continue
		}
		for infection := range candidate.heroInfections {
			for _, instanceID := range replacedIDs {
				infection.retireReplaced(targetID, instanceID, 0)
			}
		}
		for instanceID, current := range candidate.campaignNPCModifiers {
			if _, isReplaced := replacedInstances[instanceID]; !isReplaced || current == run {
				continue
			}
			isCreated, isDeferred, err := retireFixtureRunLocked(current, e.runtime.npc.modifierPool)
			if err != nil {
				return nil, nil, true, fmt.Errorf("fixtureReplaceRelease: %w", err)
			}
			if !isCreated {
				continue
			}
			if isDeferred {
				deferredInstances[instanceID] = true
			}
			if !isDeferred {
				candidate.untrackCampaignNPCModifier(current)
			}
			if !isDeferred && current.cancel != nil {
				cancels = append(cancels, current.cancel)
				current.cancel = nil
			}
		}
		e.runtime.registry.sessions[sessionKey] = candidate
	}
	packets := make([][]byte, 0, len(previousIDs))
	for _, instanceID := range previousIDs {
		if !deferredInstances[instanceID] {
			packets = append(packets, deletesByID[instanceID])
		}
	}
	return packets, cancels, true, nil
}
