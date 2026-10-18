package boss

import (
	"github.com/darkspinnet/darkspin/server/game"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

// Boss adds share the native horde roster's minion and special eligibility
// (sub_9F86D0). Slot counts and uniform selection remain local encounter policy,
// not a recovered native challenge budget or special-ceiling distribution.
func bossAddEntries(director game.CampaignDirector) []game.CampaignDirectorEntry {
	entries := append([]game.CampaignDirectorEntry(nil), zonepopulation.HordeEntries(director)...)
	entries = append(entries, zonepopulation.PoolEntries(director, "special")...)
	stage := max(uint32(1), director.Difficulty)
	eligibleEntries := make([]game.CampaignDirectorEntry, 0, len(entries))
	for _, entry := range CompleteEntries(entries, true) {
		if stage < entry.MinimumDifficulty || stage > entry.MaximumDifficulty {
			continue
		}
		eligibleEntries = append(eligibleEntries, entry)
	}
	return eligibleEntries
}

func expandOpeningMarkers(markers []game.CampaignDirectorMarker, count uint32) []game.CampaignDirectorMarker {
	expandedMarkers := make([]game.CampaignDirectorMarker, 0, len(markers)*int(count))
	for _, marker := range markers {
		positions := zonepopulation.GroupPositions([]game.Vec3{marker.Position}, int(count))
		for _, position := range positions {
			copyMarker := marker
			copyMarker.Position = position
			expandedMarkers = append(expandedMarkers, copyMarker)
		}
	}
	return expandedMarkers
}
