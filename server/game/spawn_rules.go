package game

import "errors"

// EnemyPopulationRules controls the reconstructed standing campaign population.
// Authored spawn points and separate boss/horde encounters still bound the result.
type EnemyPopulationRules struct {
	EnemiesPerSection           uint32
	AgentSelection              string
	AgentsPerSection            uint32
	RosterSelection             string
	ExternalRosterWeightPercent uint32
	IsAmbientEliteEnabled       bool
	AmbientEliteChancePercent   uint32
	OwnedSummonHealthScaling    string
	FixtureHealthScaling        string
}

func DefaultEnemyPopulationRules() EnemyPopulationRules {
	return EnemyPopulationRules{EnemiesPerSection: 15, AgentSelection: "current_coop", AgentsPerSection: 1,
		RosterSelection: "current_composition", ExternalRosterWeightPercent: 50, AmbientEliteChancePercent: 100,
		OwnedSummonHealthScaling: "run_and_party", FixtureHealthScaling: "base"}
}

func (e EnemyPopulationRules) Validate() error {
	if e.EnemiesPerSection < 10 || e.EnemiesPerSection > 60 {
		return errors.New("enemies per section must be between 10 and 60")
	}
	if e.AgentSelection != "current_coop" && e.AgentSelection != "authored_external" {
		return errors.New("invalid agent selection")
	}
	if e.RosterSelection != "current_composition" && e.RosterSelection != "local_and_external" {
		return errors.New("invalid roster selection")
	}
	if e.AgentsPerSection > 3 || e.ExternalRosterWeightPercent > 100 || e.AmbientEliteChancePercent > 100 {
		return errors.New("population settings outside allowed ranges")
	}
	if !isHealthScaling(e.OwnedSummonHealthScaling) || !isHealthScaling(e.FixtureHealthScaling) {
		return errors.New("invalid health scaling")
	}
	return nil
}

func isHealthScaling(policy string) bool {
	return policy == "base" || policy == "run_only" || policy == "run_and_party"
}

func (e EnemyPopulationRules) Resolved() EnemyPopulationRules {
	if e.EnemiesPerSection == 0 {
		return DefaultEnemyPopulationRules()
	}
	defaults := DefaultEnemyPopulationRules()
	if e.AgentSelection == "" {
		e.AgentSelection = defaults.AgentSelection
		e.AgentsPerSection = defaults.AgentsPerSection
	}
	if e.RosterSelection == "" {
		e.RosterSelection = defaults.RosterSelection
		e.ExternalRosterWeightPercent = defaults.ExternalRosterWeightPercent
	}
	if e.OwnedSummonHealthScaling == "" {
		e.OwnedSummonHealthScaling = defaults.OwnedSummonHealthScaling
	}
	if e.FixtureHealthScaling == "" {
		e.FixtureHealthScaling = defaults.FixtureHealthScaling
	}
	return e
}

// BossSpawnRules controls reconstructed encounter policy, not authored NPC stats.
type BossSpawnRules struct {
	OpeningAddsPerAnchor    uint32
	InitialBossAddCount     uint32
	CaptainDelayMillisecond uint32
	LeaderSelection         string
	ReplayLeaderSelection   string
}

func DefaultBossSpawnRules() BossSpawnRules {
	return BossSpawnRules{OpeningAddsPerAnchor: 1, InitialBossAddCount: 2, CaptainDelayMillisecond: 2000,
		LeaderSelection: "current_campaign", ReplayLeaderSelection: "eligible_captain"}
}

func (e BossSpawnRules) Validate() error {
	if e.LeaderSelection != "current_campaign" && e.LeaderSelection != "authored_planet_boss" {
		return errors.New("invalid leader selection")
	}
	if e.ReplayLeaderSelection != "eligible_captain" && e.ReplayLeaderSelection != "same_as_first_clear" {
		return errors.New("invalid replay leader selection")
	}
	if e.OpeningAddsPerAnchor < 1 || e.OpeningAddsPerAnchor > 4 ||
		e.InitialBossAddCount < 1 || e.InitialBossAddCount > 12 ||
		e.CaptainDelayMillisecond > 30000 {
		return errors.New("boss spawn settings outside allowed ranges")
	}
	return nil
}

func (e BossSpawnRules) Resolved() BossSpawnRules {
	if e.OpeningAddsPerAnchor == 0 {
		return DefaultBossSpawnRules()
	}
	if e.LeaderSelection == "" {
		e.LeaderSelection = "current_campaign"
	}
	if e.ReplayLeaderSelection == "" {
		e.ReplayLeaderSelection = "eligible_captain"
	}
	return e
}
