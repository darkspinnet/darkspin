//go:build !scenario

package game

// Release builds retain only empty composition seams. Scenario configuration
// and its mutable state are excluded by the build constraint.
type scenarioMapState struct{}

type scenarioMapBinding struct{}

func (e *Manager) IsScenarioContinueHost(gameID uint32, userID int64) bool {
	return false
}

func (e *Manager) attachScenarioMap(binding *GameplayBinding) error {
	return nil
}

func (e GameplayBinding) ScenarioMapInputs() (
	mapSeed uint32, prepareMask uint32, isConfigured bool, err error,
) {
	return 0, 0, false, nil
}
