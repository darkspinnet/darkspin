package gameplay

import (
	"github.com/darkspinnet/darkspin/server/util"
)

// Build-103 RootTemplate (chunk 281) authors Unique. Entangle (211) and
// VerdanthBasicRootmob (212) inherit it without overriding activationType.
// Sleep and TimeRavagerSupport have independently verified Unique declarations.
func isVerifiedNativeUniqueModifier(guid uint32) bool {
	switch guid {
	case util.HashID("TimeRavagerSupportModifier"), util.HashID("SleepModifier"),
		util.HashID("EntangleModifier"), util.HashID("VerdanthBasicRootmobModifier"):
		return true
	default:
		return false
	}
}

// Chilled (810) and SlowNova (92) inherit SpeedTemplate453's CasterUnique
// activation without overriding it. Different sources retain distinct runs.
func isVerifiedNativeCasterUniqueModifier(guid uint32) bool {
	return guid == util.HashID("Chilled") || guid == util.HashID("SlowNovaModifier")
}

func isVerifiedNativeReplacingModifier(guid uint32) bool {
	return isVerifiedNativeUniqueModifier(guid) || isVerifiedNativeCasterUniqueModifier(guid)
}

// NomadSnipe (chunk 13, root PCs 28-31) overrides its speed template with
// UniqueIrreplaceable. Build-103 admission 9E1974 rejects whenever any matching
// definition handle is still owned by the recipient, without a caster check.
// Call under the registry lock before tracking the incoming run. Registered
// pending runs reserve admission before their Effect inventory publication.
func (e *gameplaySessionRegistry) isNativeIrreplaceableModifierBlocked(member *gameplayPeerSession, objectID, guid uint32) bool {
	if guid != util.HashID("NomadSnipe_SlowDebuff") {
		return false
	}
	runsByIDs := make(map[uint32]*campaignNPCModifierRun)
	for instanceID, run := range member.campaignNPCModifiers {
		runsByIDs[instanceID] = run
	}
	for _, candidate := range e.sessions {
		if candidate.zone != member.zone {
			continue
		}
		for instanceID, run := range candidate.campaignNPCModifiers {
			runsByIDs[instanceID] = run
		}
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone != member.zone {
			continue
		}
		for instanceID, run := range retained.peerSession.campaignNPCModifiers {
			runsByIDs[instanceID] = run
		}
	}
	for _, run := range runsByIDs {
		if run != nil && run.record.TargetObjectID == objectID && run.record.GUID == guid && run.isOwned() {
			return true
		}
	}
	for _, modifier := range member.zone.Effect().Snapshot() {
		if modifier.TargetObjectID != objectID || modifier.GUID != guid {
			continue
		}
		run := runsByIDs[modifier.InstanceID]
		if run != nil && !run.isOwned() {
			continue
		}
		return true
	}
	return false
}
