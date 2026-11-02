//go:build scenario

package zone

import "errors"

// ValidateScenarioRecipe checks the settings of the single already-created
// shared zone. It never seeds or changes the ordinary gameplay RNG owners.
func (e *Zone) ValidateScenarioRecipe(runSeed uint64, isAmbientPopulationSuppressed bool) error {
	if e == nil || !e.mu.TryRLock() {
		return errors.New("focused zone settings are unavailable or busy")
	}
	defer e.mu.RUnlock()
	if runSeed == 0 || e.info.RunSeed != runSeed || e.info.IsAmbientPopulationSuppressed != isAmbientPopulationSuppressed {
		return errors.New("focused zone seed or ambient population policy does not match bound cohort")
	}
	return nil
}
