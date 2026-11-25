package gameplay

import (
	"errors"
	"fmt"
	"slices"

	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

// Build-103 Unique (1) replaces same-definition instances on this recipient;
// CasterUnique (2) retires the same caster's matching instance instead.
// Callers preflight before publication
// and commit only after the new request is admitted and its expiry is scheduled.
// Keep this bounded to independently verified modifier definitions.
type nativeUniqueReplacement struct {
	run    *campaignNPCModifierRun
	packet []byte
}

func (e campaignAbilityCommandRuntime) prepareNativeUniqueReplacementsLocked(member *gameplayPeerSession, admitted *campaignNPCModifierRun) ([]nativeUniqueReplacement, error) {
	if admitted == nil || !isVerifiedNativeReplacingModifier(admitted.record.GUID) {
		return nil, errors.New("unverified replacing modifier")
	}
	runsByIDs := make(map[uint32]*campaignNPCModifierRun)
	for _, run := range member.campaignNPCModifiers {
		if run != nil {
			runsByIDs[run.instanceID] = run
		}
	}
	for _, candidate := range e.registry.sessions {
		if candidate.zone != member.zone {
			continue
		}
		for _, run := range candidate.campaignNPCModifiers {
			if run != nil {
				runsByIDs[run.instanceID] = run
			}
		}
	}
	for _, retained := range e.registry.retainedSessions {
		if retained.peerSession.zone != member.zone {
			continue
		}
		for _, run := range retained.peerSession.campaignNPCModifiers {
			if run != nil {
				runsByIDs[run.instanceID] = run
			}
		}
	}
	instanceIDs := make([]uint32, 0)
	for instanceID, run := range runsByIDs {
		if run == admitted || run.record.GUID != admitted.record.GUID || run.record.TargetObjectID != admitted.record.TargetObjectID {
			continue
		}
		if isVerifiedNativeCasterUniqueModifier(admitted.record.GUID) && run.record.SourceObjectID != admitted.record.SourceObjectID {
			continue
		}
		instanceIDs = append(instanceIDs, instanceID)
	}
	slices.Sort(instanceIDs)
	// Native branch 2 retires the first matching caster handle, then creates.
	// Ordinary admission owns at most one such run; keep retirement bounded
	// even when an older server left duplicate ownership records behind.
	if isVerifiedNativeCasterUniqueModifier(admitted.record.GUID) && len(instanceIDs) > 1 {
		instanceIDs = instanceIDs[:1]
	}
	replacements := make([]nativeUniqueReplacement, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		run := runsByIDs[instanceID]
		packet, err := effectraknet.ModifierDelete(run.record.TargetObjectID, instanceID)
		if err != nil {
			return nil, fmt.Errorf("replacementDelete: %w", err)
		}
		replacements = append(replacements, nativeUniqueReplacement{run: run, packet: packet})
	}
	return replacements, nil
}

func (e campaignAbilityCommandRuntime) commitNativeUniqueReplacementsLocked(sessionKey string, member *gameplayPeerSession, replacements []nativeUniqueReplacement) [][]byte {
	if len(replacements) == 0 {
		return nil
	}
	packets := make([][]byte, 0, len(replacements))
	e.registry.sessions[sessionKey] = *member
	for _, replacement := range replacements {
		if replacement.run.isActive() {
			packets = append(packets, replacement.packet)
		}
	}
	e.registry.queueFieldMedicPacketsLocked(member.zone, packets, member.binding.UserID)
	for _, replacement := range replacements {
		run := replacement.run
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		member.zone.Effect().Remove(run.instanceID)
		for key, candidate := range e.registry.sessions {
			if candidate.zone != member.zone {
				continue
			}
			candidate.untrackCampaignNPCModifier(run)
			e.registry.sessions[key] = candidate
		}
		for _, retained := range e.registry.retainedSessions {
			if retained.peerSession.zone == member.zone {
				retained.peerSession.untrackCampaignNPCModifier(run)
			}
		}
		wasCreated, err := run.release(e.modifierPool)
		if err != nil && e.logger != nil {
			// Exact attributes are already cleared. Keep the accepted replacement
			// committed even when the obsolete handle cannot be recycled.
			e.logger.Printf("Native Unique modifier release failed instance=%d: %v", run.instanceID, err)
		}
		if !wasCreated && err == nil {
			// An already-retired alias requires no further presentation.
		}
	}
	*member = e.registry.sessions[sessionKey]
	return packets
}
