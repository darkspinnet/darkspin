package raknet103

import (
	"fmt"

	zone "github.com/darkspinnet/darkspin/server/zone"
)

func Population(transition zone.PopulationTransition) ([][]byte, error) {
	packets, err := DormantSpawns(transition.SpawnPlans)
	if err != nil {
		return nil, fmt.Errorf("populationSpawn: %w", err)
	}
	targetPacket, err := TargetUpdates(transition.Acquired)
	if err != nil {
		return nil, fmt.Errorf("populationTarget: %w", err)
	}
	return append(packets, targetPacket...), nil
}
