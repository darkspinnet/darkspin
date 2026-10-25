//go:build !scenario

package game

type scenarioRecipeState struct{}
type scenarioRecipeBinding struct{}

func (e *Manager) attachFocusedRecipe(binding *GameplayBinding) error { return nil }
func (e GameplayBinding) FocusedRecipeInputs() (runID string, isAmbientPopulationSuppressed bool) {
	return "", false
}
