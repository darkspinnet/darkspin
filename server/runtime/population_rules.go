package runtime

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/game"
)

type EnemyPopulationConfiguration struct {
	Version                     uint32 `json:"version" toml:"version"`
	EnemiesPerSection           uint32 `json:"enemiesPerSection" toml:"enemies_per_section"`
	AgentSelection              string `json:"agentSelection" toml:"agent_selection"`
	AgentsPerSection            uint32 `json:"agentsPerSection" toml:"agents_per_section"`
	RosterSelection             string `json:"rosterSelection" toml:"roster_selection"`
	ExternalRosterWeightPercent uint32 `json:"externalRosterWeightPercent" toml:"external_roster_weight_percent"`
	IsAmbientEliteEnabled       bool   `json:"isAmbientEliteEnabled" toml:"is_ambient_elite_enabled"`
	AmbientEliteChancePercent   uint32 `json:"ambientEliteChancePercent" toml:"ambient_elite_chance_percent"`
	OwnedSummonHealthScaling    string `json:"ownedSummonHealthScaling" toml:"owned_summon_health_scaling"`
	FixtureHealthScaling        string `json:"fixtureHealthScaling" toml:"fixture_health_scaling"`
}

func enemyPopulationConfiguration(rules game.EnemyPopulationRules) EnemyPopulationConfiguration {
	return EnemyPopulationConfiguration{Version: 1, EnemiesPerSection: rules.EnemiesPerSection, AgentSelection: rules.AgentSelection, AgentsPerSection: rules.AgentsPerSection, RosterSelection: rules.RosterSelection, ExternalRosterWeightPercent: rules.ExternalRosterWeightPercent, IsAmbientEliteEnabled: rules.IsAmbientEliteEnabled, AmbientEliteChancePercent: rules.AmbientEliteChancePercent, OwnedSummonHealthScaling: rules.OwnedSummonHealthScaling, FixtureHealthScaling: rules.FixtureHealthScaling}
}

func (e EnemyPopulationConfiguration) rules() game.EnemyPopulationRules {
	return game.EnemyPopulationRules{EnemiesPerSection: e.EnemiesPerSection, AgentSelection: e.AgentSelection, AgentsPerSection: e.AgentsPerSection, RosterSelection: e.RosterSelection, ExternalRosterWeightPercent: e.ExternalRosterWeightPercent, IsAmbientEliteEnabled: e.IsAmbientEliteEnabled, AmbientEliteChancePercent: e.AmbientEliteChancePercent, OwnedSummonHealthScaling: e.OwnedSummonHealthScaling, FixtureHealthScaling: e.FixtureHealthScaling}
}

func decodeEnemyPopulationConfiguration(payload []byte) (EnemyPopulationConfiguration, bool, error) {
	configuration := enemyPopulationConfiguration(game.DefaultEnemyPopulationRules())
	var raw struct {
		EnemyPopulation toml.Primitive `toml:"enemy_population"`
	}
	metadata, err := toml.Decode(string(payload), &raw)
	if err != nil {
		return configuration, false, fmt.Errorf("populationDecode: %w", err)
	}
	if !metadata.IsDefined("enemy_population") {
		return configuration, true, nil
	}
	err = metadata.PrimitiveDecode(raw.EnemyPopulation, &configuration)
	if err != nil {
		return configuration, false, fmt.Errorf("populationFields: %w", err)
	}
	if configuration.Version != 1 {
		return configuration, false, errors.New("enemy population settings version unsupported")
	}
	err = configuration.rules().Validate()
	if err != nil {
		return configuration, false, fmt.Errorf("populationRules: %w", err)
	}
	isAdded := !metadata.IsDefined("enemy_population", "agent_selection") || !metadata.IsDefined("enemy_population", "fixture_health_scaling")
	return configuration, isAdded, nil
}

// A mission captures its target once; editing rules cannot repopulate a cleared zone.
func (e *serverRuleStore) EnemyPopulationRules() game.EnemyPopulationRules {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.populationRules
}

func (e *serverRuleStore) SpawnRulesSnapshot() (game.EnemyPopulationRules, game.BossSpawnRules) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.populationRules, e.spawnRules
}
