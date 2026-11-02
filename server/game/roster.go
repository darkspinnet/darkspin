package game

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

// composeCampaignRoster retains the existing source counts after inclusive
// stage filtering. Replay source draws prefer unused nouns, retaining a repeated
// slot only when that source lacks alternatives. The run seed makes composition
// repeatable; original build-103 server RNG initialization remains unknown.
func composeCampaignRoster(
	director CampaignDirector, seed uint64, partyCompletedStages []uint32,
) (CampaignDirector, error) {
	rosterSeed := uint32(seed) ^ uint32(seed>>32)
	if director.IsReplay {
		// Separate replay rosters by destination and threat while preserving
		// the same selection for every member and checkpoint reload.
		rosterSeed ^= util.HashID(director.Level) ^ director.Difficulty*0x9e3779b9 ^ 0x524f5354
	}
	random := sim.NewSimulatorRandom(rosterSeed)
	director.IsFirstTimeRosterSelected = false
	var firstTimeSpecials []CampaignDirectorEntry
	if !director.IsReplay && director.IsFirstTimeDirectorEnabled && isPartyBelowStage(partyCompletedStages, director.Difficulty) {
		var err error
		director, err = selectFirstTimeRoster(director, random)
		if err != nil {
			return CampaignDirector{}, fmt.Errorf("rosterFirstTime: %w", err)
		}
		if director.IsFirstTimeRosterSelected {
			return director, nil
		}
		// The existing first-time fallback retains specials when its minion
		// buffer is empty.
		for _, pool := range director.Pools {
			if strings.EqualFold(pool.ConfigurationName, "firstTimeConfig") &&
				strings.EqualFold(pool.ConfigKind, "special") {
				firstTimeSpecials = append(firstTimeSpecials, pool.Entries...)
			}
		}
	}
	if len(director.ExternalPools) == 0 || director.PlanetConfigName == "" {
		return director, nil
	}
	// Existing composition mixes secondary, tertiary and
	// quaternary sources; the planet represents the primary source.
	secondaryName, err := campaignScienceConfig(director.SecondaryType, director.Difficulty >= 25)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("rosterSecondary: %w", err)
	}
	tertiaryName, err := campaignScienceConfig(director.TertiaryType, director.Difficulty >= 49)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("rosterTertiary: %w", err)
	}
	quaternaryName, err := campaignScienceConfig(director.QuaternaryType, false)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("rosterQuaternary: %w", err)
	}
	// Existing composition requires all four external resources, even when a
	// branch asks for zero draws from one of them. Empty Planets is valid.
	for _, configName := range []string{director.PlanetConfigName, secondaryName, tertiaryName, quaternaryName} {
		isFound := false
		for _, pool := range director.ExternalPools {
			if strings.EqualFold(pool.ConfigurationName, configName) {
				isFound = true
				break
			}
		}
		if !isFound {
			return CampaignDirector{}, fmt.Errorf("rosterConfig[%s]: missing", configName)
		}
	}
	minions := make([]CampaignDirectorEntry, 0, 3)
	planetMinionCount, tertiaryMinionCount := 1, 0
	if director.Difficulty >= 49 {
		planetMinionCount, tertiaryMinionCount = 0, 1
	}
	draws := []campaignRosterDraw{
		{configurationName: "levelConfig", configKind: "minion", count: 1},
		{configurationName: director.PlanetConfigName, configKind: "minion", count: planetMinionCount, offset: 1, isExternal: true},
		{configurationName: secondaryName, configKind: "minion", count: 1, offset: planetMinionCount + 1, isExternal: true},
		{configurationName: tertiaryName, configKind: "minion", count: tertiaryMinionCount, offset: planetMinionCount + 2, isExternal: true},
	}
	for _, draw := range draws {
		minions, err = drawCampaignRoster(director, minions, draw, random)
		if err != nil {
			return CampaignDirector{}, fmt.Errorf("rosterMinion: %w", err)
		}
	}
	err = shuffleCampaignRoster(minions, random)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("rosterShuffle: %w", err)
	}
	planetSpecialCount, secondarySpecialCount := 1, 1
	tertiarySpecialCount, quaternarySpecialCount := 0, 0
	switch {
	case director.Difficulty >= 49:
		quaternarySpecialCount = 1
	case director.Difficulty >= 25:
		tertiarySpecialCount = 1
	case random.Float64() >= 0.25:
		planetSpecialCount = 2
	default:
		secondarySpecialCount = 2
	}
	specials := append([]CampaignDirectorEntry(nil), firstTimeSpecials...)
	draws = []campaignRosterDraw{
		{configurationName: director.PlanetConfigName, configKind: "special", count: planetSpecialCount, isExternal: true},
		{configurationName: secondaryName, configKind: "special", count: secondarySpecialCount, offset: planetSpecialCount, isExternal: true},
		{configurationName: tertiaryName, configKind: "special", count: tertiarySpecialCount, offset: planetSpecialCount + 1, isExternal: true},
		{configurationName: quaternaryName, configKind: "special", count: quaternarySpecialCount, offset: tertiaryMinionCount + planetSpecialCount + 1, isExternal: true},
	}
	for _, draw := range draws {
		specials, err = drawCampaignRoster(director, specials, draw, random)
		if err != nil {
			return CampaignDirector{}, fmt.Errorf("rosterSpecial: %w", err)
		}
	}
	pools := make([]CampaignDirectorPool, 0, len(director.Pools)+2)
	for _, pool := range director.Pools {
		isMixedKind := strings.EqualFold(pool.ConfigKind, "minion") || strings.EqualFold(pool.ConfigKind, "special")
		if isMixedKind {
			continue
		}
		pools = append(pools, pool)
	}
	pools = append(pools,
		CampaignDirectorPool{ConfigurationName: "levelConfig", ConfigKind: "minion", Entries: minions[:min(3, len(minions))]},
		CampaignDirectorPool{ConfigurationOrdinal: 1, ConfigurationName: "levelConfig", ConfigKind: "special", Entries: specials[:min(3, len(specials))]},
	)
	director.Pools = pools
	return director, nil
}

type campaignRosterDraw struct {
	configurationName string
	configKind        string
	count             int
	offset            int
	isExternal        bool
}

func campaignScienceConfig(scienceType uint32, isPlanet bool) (string, error) {
	scienceNames := [...]string{"Cyber", "Chrono", "Bio", "Plasma", "Necro", "Generic"}
	planetNames := [...]string{"Sentios", "Zelem", "Verdanth", "Cryos", "Nocturna", "Scaldron"}
	if scienceType >= uint32(len(scienceNames)) {
		return "", errors.New("invalid science type")
	}
	if isPlanet {
		return planetNames[scienceType] + ".LevelConfig", nil
	}
	return scienceNames[scienceType] + ".LevelConfig", nil
}

func drawCampaignRoster(
	director CampaignDirector, entries []CampaignDirectorEntry, draw campaignRosterDraw,
	random *sim.SimulatorRandom,
) ([]CampaignDirectorEntry, error) {
	if draw.count == 0 {
		return entries, nil
	}
	if director.IsReplay {
		selectedEntries, err := drawCampaignReplayRoster(director, entries, draw, random)
		if err != nil {
			return nil, fmt.Errorf("replayDraw: %w", err)
		}
		return selectedEntries, nil
	}
	pools := director.Pools
	if draw.isExternal {
		pools = director.ExternalPools
	}
	for _, pool := range pools {
		if !strings.EqualFold(pool.ConfigurationName, draw.configurationName) ||
			!strings.EqualFold(pool.ConfigKind, draw.configKind) {
			continue
		}
		for _, candidate := range pool.Entries {
			if director.Difficulty < candidate.MinimumDifficulty || director.Difficulty > candidate.MaximumDifficulty {
				continue
			}
			isDuplicate := false
			for _, entry := range entries {
				if strings.EqualFold(entry.NounName, candidate.NounName) {
					isDuplicate = true
					break
				}
			}
			if !isDuplicate {
				entries = append(entries, candidate)
			}
		}
		break
	}
	if len(entries) <= draw.offset+draw.count {
		return entries, nil
	}
	err := shuffleCampaignRoster(entries[draw.offset:], random)
	if err != nil {
		return nil, fmt.Errorf("drawShuffle: %w", err)
	}
	return entries[:draw.offset+draw.count], nil
}

// drawCampaignReplayRoster preserves source quotas while preferring nouns that
// have not already been drawn. Repeats fill a quota only when alternatives run out.
func drawCampaignReplayRoster(
	director CampaignDirector, entries []CampaignDirectorEntry, draw campaignRosterDraw,
	random *sim.SimulatorRandom,
) ([]CampaignDirectorEntry, error) {
	pools := director.Pools
	if draw.isExternal {
		pools = director.ExternalPools
	}
	candidates := make([]CampaignDirectorEntry, 0)
	for _, pool := range pools {
		if !strings.EqualFold(pool.ConfigurationName, draw.configurationName) ||
			!strings.EqualFold(pool.ConfigKind, draw.configKind) {
			continue
		}
		for _, candidate := range pool.Entries {
			if director.Difficulty < candidate.MinimumDifficulty || director.Difficulty > candidate.MaximumDifficulty {
				continue
			}
			isDuplicate := false
			for _, selected := range candidates {
				if strings.EqualFold(selected.NounName, candidate.NounName) {
					isDuplicate = true
					break
				}
			}
			if !isDuplicate {
				candidates = append(candidates, candidate)
			}
		}
		break
	}
	if len(candidates) > 1 {
		err := shuffleCampaignRoster(candidates, random)
		if err != nil {
			return nil, fmt.Errorf("replayDrawShuffle: %w", err)
		}
	}
	repeatedCandidates := make([]CampaignDirectorEntry, 0, len(candidates))
	selectedCount := 0
	for _, candidate := range candidates {
		isRepeated := false
		for _, selected := range entries {
			if strings.EqualFold(selected.NounName, candidate.NounName) {
				isRepeated = true
				break
			}
		}
		if isRepeated {
			repeatedCandidates = append(repeatedCandidates, candidate)
			continue
		}
		entries = append(entries, candidate)
		selectedCount++
		if selectedCount == draw.count {
			return entries, nil
		}
	}
	remainingCount := min(draw.count-selectedCount, len(repeatedCandidates))
	return append(entries, repeatedCandidates[:remainingCount]...), nil
}

// shuffleCampaignRoster retains the existing forward Fisher-Yates draw order.
func shuffleCampaignRoster(entries []CampaignDirectorEntry, random *sim.SimulatorRandom) error {
	for index := 1; index < len(entries); index++ {
		otherIndex, err := random.Index(uint32(index + 1))
		if err != nil {
			return fmt.Errorf("shuffleRoll: %w", err)
		}
		entries[index], entries[otherIndex] = entries[otherIndex], entries[index]
	}
	return nil
}
