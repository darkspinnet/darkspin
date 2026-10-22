package gameplay

import (
	"fmt"
	"sort"

	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func isFixtureBurnNamespace(name string) bool {
	return name == "LifePlagueSpread" || name == "PlasmaSentinelBurn" || name == "CitadelPlasmaBurn"
}

// The caller holds the registry lock; NPC scalar debuffs do not own these runs.
func (e *gameplaySessionRegistry) hasFixtureBurnLocked(npc *zonenpc.Session, targetID uint32) bool {
	for _, member := range e.sessions {
		if member.zone == nil || member.zone.NPCs() != npc {
			continue
		}
		for _, run := range member.campaignNPCModifiers {
			if run == nil || run.fixtureBurn == nil || run.record.TargetObjectID != targetID ||
				!isFixtureBurnNamespace(run.fixtureBurn.profile.ModifierName) || run.fixtureBurn.isRetiring || !run.isActive() {
				continue
			}
			if run.record.IsIndefinite || run.fixtureBurn.blast.runtime.npc.now().Before(run.fixtureBurn.expiresAt) {
				return true
			}
		}
	}
	return false
}

// Retire all recognized same-zone fixture instances for a cleansed target.
// Inventory, queued producers and accepted disease immunity share one release.
func (e *gameplaySessionRegistry) retireFixtureBurnsLocked(npc *zonenpc.Session, targetID uint32, pool *modifierPool) ([][]byte, error) {
	runs := make(map[uint32]*campaignNPCModifierRun)
	for _, member := range e.sessions {
		if member.zone == nil || member.zone.NPCs() != npc {
			continue
		}
		for instanceID, run := range member.campaignNPCModifiers {
			if run != nil && run.fixtureBurn != nil && run.record.TargetObjectID == targetID &&
				isFixtureBurnNamespace(run.fixtureBurn.profile.ModifierName) && !run.fixtureBurn.isRetiring {
				runs[instanceID] = run
			}
		}
	}
	instanceIDs := make([]uint32, 0, len(runs))
	for instanceID := range runs {
		instanceIDs = append(instanceIDs, instanceID)
	}
	sort.Slice(instanceIDs, func(left, right int) bool { return instanceIDs[left] < instanceIDs[right] })
	packets := make([][]byte, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		run := runs[instanceID]
		packet, err := effectraknet.ModifierDelete(targetID, instanceID)
		if err != nil {
			return packets, fmt.Errorf("fixtureCleanseDelete: %w", err)
		}
		isCreated, isDeferred, err := retireFixtureRunLocked(run, pool)
		if err != nil {
			return packets, fmt.Errorf("fixtureCleanseRelease: %w", err)
		}
		if !isDeferred {
			for sessionKey, member := range e.sessions {
				if member.zone == nil || member.zone.NPCs() != npc {
					continue
				}
				member.untrackCampaignNPCModifier(run)
				e.sessions[sessionKey] = member
			}
		}
		run.mu.Lock()
		cancel := run.cancel
		if isDeferred {
			cancel = nil
		} else {
			run.cancel = nil
		}
		run.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if isCreated && !isDeferred {
			packets = append(packets, packet)
		}
	}
	return packets, nil
}

// A consumer may retire inventory/token state while a producer is publishing
// an already accepted hit. Keep that exact allocation and publication indexed
// until its feedback and deletion have durable queue owners.
func retireFixtureRunLocked(run *campaignNPCModifierRun, pool *modifierPool) (bool, bool, error) {
	state := run.fixtureBurn
	if state == nil {
		isCreated, err := run.release(pool)
		if err != nil {
			return false, false, fmt.Errorf("fixtureRelease: %w", err)
		}
		return isCreated, false, nil
	}
	if state.isRetiring {
		return false, true, nil
	}
	publication := state.publication
	if publication == nil || (!publication.isProducing && (!publication.isProduced || publication.isCommitted)) {
		isCreated, err := run.release(pool)
		if err != nil {
			return false, false, fmt.Errorf("fixtureRelease: %w", err)
		}
		return isCreated, false, nil
	}
	packet, err := effectraknet.ModifierDelete(run.record.TargetObjectID, run.instanceID)
	if err != nil {
		return false, false, fmt.Errorf("fixtureRetireDelete: %w", err)
	}
	run.mu.Lock()
	isCreated := run.isCreated && !run.isReleased
	if isCreated {
		run.retireFixtureInfection()
		state.isRetiring = true
		state.pendingDeletes = append(state.pendingDeletes, packet)
	}
	run.mu.Unlock()
	return isCreated, isCreated, nil
}
