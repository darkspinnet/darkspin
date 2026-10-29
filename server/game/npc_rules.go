package game

import (
	"fmt"
	"slices"
	"strings"

	"github.com/darkspinnet/darkspin/server/sim"
)

type CampaignEliteTuning struct {
	Stage                                              uint32
	MinimumAffixCount, MaximumAffixCount               int32
	Chance                                             float32
	SpecialMinimumAffixCount, SpecialMaximumAffixCount int32
	SpecialChance                                      float32
}

type CampaignEliteAffix struct {
	Name, ModifierName                   string
	ParentName, ChildName                string
	ModifierID                           uint32
	MinimumDifficulty, MaximumDifficulty int32
}

// ApplyPopulationRules reconstructs source policy after authored stage filtering.
// Ordinary agents have their own roster so enabling them cannot change hordes.
func ApplyPopulationRules(director CampaignDirector) (CampaignDirector, error) {
	rules := director.EnemyPopulationRules.Resolved()
	director.EnemyPopulationRules = rules
	secondary, err := campaignScienceConfig(director.SecondaryType, director.Difficulty >= 25)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("policySecondary: %w", err)
	}
	tertiary, err := campaignScienceConfig(director.TertiaryType, director.Difficulty >= 49)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("policyTertiary: %w", err)
	}
	quaternary, err := campaignScienceConfig(director.QuaternaryType, false)
	if err != nil {
		return CampaignDirector{}, fmt.Errorf("policyQuaternary: %w", err)
	}
	sources := []string{director.PlanetConfigName, secondary, tertiary, quaternary}
	selectedPools := make([]CampaignDirectorPool, 0)
	for _, pool := range director.ExternalPools {
		isSelected := false
		for _, source := range sources {
			isSelected = isSelected || strings.EqualFold(source, pool.ConfigurationName)
		}
		if isSelected {
			selectedPools = append(selectedPools, pool)
		}
	}
	if rules.AgentSelection == "authored_external" {
		director.OrdinaryAgentEntries = eligiblePolicyEntries(selectedPools, "agent", director.Difficulty, true)
	}
	if rules.RosterSelection != "local_and_external" {
		return director, nil
	}
	director.IsFirstTimeRosterSelected = false
	pools := make([]CampaignDirectorPool, 0, len(director.Pools))
	for _, pool := range director.Pools {
		if !strings.EqualFold(pool.ConfigKind, "minion") && !strings.EqualFold(pool.ConfigKind, "special") {
			pools = append(pools, pool)
		}
	}
	for _, kind := range []string{"minion", "special"} {
		entries := eligiblePolicyEntries(director.LocalPools, kind, director.Difficulty, false)
		externals := eligiblePolicyEntries(selectedPools, kind, director.Difficulty, true)
		for _, candidate := range externals {
			isDuplicate := false
			for _, entry := range entries {
				isDuplicate = isDuplicate || strings.EqualFold(entry.NounName, candidate.NounName)
			}
			if !isDuplicate {
				entries = append(entries, candidate)
			}
		}
		pools = append(pools, CampaignDirectorPool{ConfigurationName: "levelConfig", ConfigKind: kind, Entries: entries})
	}
	director.Pools = pools
	return director, nil
}

func eligiblePolicyEntries(pools []CampaignDirectorPool, kind string, stage uint32, isExternal bool) []CampaignDirectorEntry {
	entries := make([]CampaignDirectorEntry, 0)
	for _, pool := range pools {
		if !strings.EqualFold(pool.ConfigKind, kind) {
			continue
		}
		for _, entry := range pool.Entries {
			if stage < entry.MinimumDifficulty || stage > entry.MaximumDifficulty || !entry.NPCProfile.IsKnown || entry.NPCProfile.HitPoint <= 0 {
				continue
			}
			isDuplicate := false
			for _, selected := range entries {
				isDuplicate = isDuplicate || strings.EqualFold(selected.NounName, entry.NounName)
			}
			if isDuplicate {
				continue
			}
			entry.IsExternalSource = isExternal
			entries = append(entries, entry)
		}
	}
	slices.SortStableFunc(entries, func(a, b CampaignDirectorEntry) int {
		return strings.Compare(strings.ToLower(a.NounName), strings.ToLower(b.NounName))
	})
	return entries
}

// RosterIndex treats weight as source probability, then draws uniformly within
// that eligible side. An empty side falls back without relaxing stage bounds.
func RosterIndex(entries []CampaignDirectorEntry, random *sim.SimulatorRandom, isWeighted bool, externalPercent uint32) (uint32, error) {
	if !isWeighted {
		index, err := random.Index(uint32(len(entries)))
		if err != nil {
			return 0, fmt.Errorf("rosterIndex: %w", err)
		}
		return index, nil
	}
	locals, externals := make([]uint32, 0), make([]uint32, 0)
	for index, entry := range entries {
		if entry.IsExternalSource {
			externals = append(externals, uint32(index))
		} else {
			locals = append(locals, uint32(index))
		}
	}
	indexes := locals
	if len(locals) == 0 {
		indexes = externals
	} else if len(externals) > 0 && random.Float64() < float64(externalPercent)/100 {
		indexes = externals
	}
	index, err := random.Index(uint32(len(indexes)))
	if err != nil {
		return 0, fmt.Errorf("sourceIndex: %w", err)
	}
	return indexes[index], nil
}
