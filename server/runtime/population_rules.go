package runtime

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/game"
)

type EnemyPopulationConfiguration struct {
	Version           uint32 `json:"version" toml:"version"`
	EnemiesPerSection uint32 `json:"enemiesPerSection" toml:"enemies_per_section"`
}

func enemyPopulationConfiguration(rules game.EnemyPopulationRules) EnemyPopulationConfiguration {
	return EnemyPopulationConfiguration{Version: 1, EnemiesPerSection: rules.EnemiesPerSection}
}

func (e EnemyPopulationConfiguration) rules() game.EnemyPopulationRules {
	return game.EnemyPopulationRules{EnemiesPerSection: e.EnemiesPerSection}
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
	return configuration, false, nil
}

// A mission captures its target once; editing rules cannot repopulate a cleared zone.
func (e *serverRuleStore) EnemyPopulationRules() game.EnemyPopulationRules {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.populationRules
}
