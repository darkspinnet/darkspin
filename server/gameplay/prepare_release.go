//go:build !scenario

package gameplay

import "github.com/darkspinnet/darkspin/server/game"

func prepareMapInputs(
	binding game.GameplayBinding, levelAsset string, levelIndex uint32,
) (uint32, uint32, error) {
	return zoneVariantSeed(binding.RunSeed, levelAsset, levelIndex), 0, nil
}
