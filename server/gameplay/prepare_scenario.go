//go:build scenario

package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

func prepareMapInputs(
	binding game.GameplayBinding, levelAsset string, levelIndex uint32,
) (uint32, uint32, error) {
	mapSeed, prepareMask, isConfigured, err := binding.ScenarioMapInputs()
	if err != nil {
		return 0, 0, fmt.Errorf("prepareScenario: %w", err)
	}
	if isConfigured {
		return mapSeed, prepareMask, nil
	}
	return zoneVariantSeed(binding.RunSeed, levelAsset, levelIndex), 0, nil
}
