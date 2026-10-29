package gameplay

import "fmt"

// Statuses can be owned by a different party member from the killing player.
// Retire every matching run in this zone before publishing source defeat.
func (e campaignNPCActionRuntime) stopAdditionalNPCAbilities(
	sessionKey string, generation uint64, objectID uint32,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	source, isFound := e.registry.sessions[sessionKey]
	if !isFound || source.generation != generation || source.zone == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	operativePackets, operativeErr := e.registry.retireOperativeSourceLocked(source.zone, objectID, e.modifierPool)
	if operativeErr != nil {
		return nil, fmt.Errorf("operativeCleanup: %w", operativeErr)
	}
	packets = append(packets, operativePackets...)
	for memberKey, member := range e.registry.sessions {
		if member.zone != source.zone {
			continue
		}
		member.stopCampaignMerakStorms(objectID)
		blinkErr := member.retireSpacetimeBlinkLocked(objectID, 0)
		if blinkErr != nil {
			return nil, fmt.Errorf("blinkCleanup: %w", blinkErr)
		}
		moveErr := member.retireAdditionalKitMovesLocked(objectID, 0)
		if moveErr != nil {
			return nil, fmt.Errorf("additionalMoveCleanup: %w", moveErr)
		}
		cursePackets, err := member.stopNashiraCurses(objectID, e.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("curseCleanup: %w", err)
		}
		packets = append(packets, cursePackets...)
		supportPackets, supportErr := member.stopNPCSupportModifiers(objectID, e.modifierPool)
		if supportErr != nil {
			return nil, fmt.Errorf("supportCleanup: %w", supportErr)
		}
		packets = append(packets, supportPackets...)
		passiveErr := member.retireAgentPassiveEffectsLocked(objectID)
		if passiveErr != nil {
			return nil, fmt.Errorf("agentPassiveCleanup: %w", passiveErr)
		}
		delete(member.agentSupports, objectID)
		delete(member.droneFollows, objectID)
		member.clearNPCSupportIdles(objectID)
		delete(member.agentBlinkReadiness, objectID)
		e.registry.sessions[memberKey] = member
	}
	return packets, nil
}
