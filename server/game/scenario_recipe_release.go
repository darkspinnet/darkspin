//go:build !scenario

package game

type scenarioRecipeState struct{}
type scenarioRecipeBinding struct{}

func (e *Manager) attachFocusedRecipe(binding *GameplayBinding) error { return nil }
func (e *Manager) campaignDestinationRunSeed(instance *Instance) (uint64, error) {
	return instance.RunSeed, nil
}
func (e GameplayBinding) FocusedRecipeInputs() (runID string, isAmbientPopulationSuppressed bool) {
	return "", false
}

func (e GameplayBinding) ScenarioPopulationSeed() (uint32, bool) { return 0, false }
