package gameplay

import "github.com/darkspinnet/darkspin/server/game"

func blitzOverdriveCriticalIncrease(creature game.GameplayCreature) float32 {
	// Chunk 845's LightningRoguePassive uses {0, 0, 20, 20} percentage
	// points. Common chunk 827's IsInOverdrive checks GetRank() == 3,
	// so the passive's overdrive activation selects the third entry.
	return 20 * creature.CriticalRatingPerPercent
}
