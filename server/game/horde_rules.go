package game

import (
	"errors"
	"fmt"
	"strings"
)

// ZoneHordeEnemy specifies an exact ordinary enemy count. The Mutation Agent
// is added separately on the final wave when the encounter is eligible.
type ZoneHordeEnemy struct {
	NounName string
	Count    uint32
}

func CloneZoneHordeWaves(waves [][]ZoneHordeEnemy) [][]ZoneHordeEnemy {
	if waves == nil {
		return nil
	}
	clones := make([][]ZoneHordeEnemy, len(waves))
	for index, enemies := range waves {
		clones[index] = append([]ZoneHordeEnemy(nil), enemies...)
	}
	return clones
}

func ValidateZoneHordeWaves(waves [][]ZoneHordeEnemy, waveCount int) error {
	if len(waves) == 0 {
		return nil
	}
	if len(waves) != waveCount {
		return errors.New("fixed horde roster must match wave count")
	}
	for index, enemies := range waves {
		count := uint32(0)
		for _, enemy := range enemies {
			if strings.TrimSpace(enemy.NounName) == "" || enemy.Count == 0 || enemy.Count > 15 ||
				strings.EqualFold(enemy.NounName, "MutationAgent.Noun") {
				return fmt.Errorf("hordeEnemy[%d]: invalid roster entry", index+1)
			}
			count += enemy.Count
			if count > 15 {
				return fmt.Errorf("hordeCount[%d]: exceeds 15 ordinary enemies", index+1)
			}
		}
		if count == 0 {
			return fmt.Errorf("hordeCount[%d]: empty wave", index+1)
		}
	}
	return nil
}
