package zone

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// hydrateCheckpointClassBases upgrades legacy actors with the raw metadata
// required by newly created mirrors. Saved combat stats, projections, identity,
// and current health remain authoritative; this is not a profile replacement.
func hydrateCheckpointClassBases(snapshot zonenpc.Snapshot, director game.CampaignDirector) zonenpc.Snapshot {
	if snapshot.Plan.NPCProfile.AreClassAttributeBasesKnown {
		return snapshot
	}
	profile, isFound := director.NPCProfilesByNoun[strings.ToLower(snapshot.Plan.NounName)]
	if !isFound || !profile.IsKnown || !profile.AreClassAttributeBasesKnown {
		return snapshot
	}
	// The noun lookup is exact (including authored aliases). Where a saved
	// class identity exists, never attach coefficients from a different class.
	savedClassID := snapshot.Plan.NPCProfile.NPCClassInstanceID
	if savedClassID != 0 && savedClassID != profile.NPCClassInstanceID {
		return snapshot
	}
	snapshot.Plan.NPCProfile.ClassAttributeBases = profile.ClassAttributeBases
	snapshot.Plan.NPCProfile.AreClassAttributeBasesKnown = true
	return snapshot
}
