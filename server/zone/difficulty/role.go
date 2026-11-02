package difficulty

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

func projectRoleHealth(profile game.CampaignNPCProfile, stage uint32, participantCount uint16, tuning sim.DifficultyCombatTuning, policy string) (float32, error) {
	if policy == "base" {
		return profile.HitPoint, nil
	}
	coefficient := float32(0)
	if policy == "run_and_party" {
		coefficient = float32(participantCount - 1)
	}
	health, err := sim.ProjectNonPlayerHealthForParty(profile.HitPoint, stage, 0, coefficient, profile.PlayerCountHealthScale, tuning)
	if err != nil {
		return 0, fmt.Errorf("roleHealth: %w", err)
	}
	return health, nil
}

// ProjectOwnedSummon resolves the child's own imported metadata and the mission's
// already projected health. Owner state and explicitly supplied HP stay separate.
func ProjectOwnedSummon(director game.CampaignDirector, nounName string) (game.CampaignNPCProfile, error) {
	profile, isFound := director.OwnedSummonProfilesByNoun[strings.ToLower(nounName)]
	if !isFound {
		profile, isFound = director.NPCProfilesByNoun[strings.ToLower(nounName)]
	}
	if !isFound || !profile.IsKnown || profile.HitPoint <= 0 {
		return game.CampaignNPCProfile{}, fmt.Errorf("summonProfile: missing %q", nounName)
	}
	return profile, nil
}

func ProjectFixtures(director game.CampaignDirector, markers []game.CampaignDirectorMarker) []game.CampaignDirectorMarker {
	projectedMarkers := append([]game.CampaignDirectorMarker(nil), markers...)
	for index := range projectedMarkers {
		profile, isFound := director.FixtureProfilesByNoun[strings.ToLower(projectedMarkers[index].NounName)]
		if isFound {
			projectedMarkers[index].NPCProfile = profile
		}
	}
	return projectedMarkers
}
