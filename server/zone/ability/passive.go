package ability

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

type ModifierAllocator interface {
	Allocate() (uint32, error)
	Release(uint32) error
}

// AllocatePassiveModifiers reserves one modifier identity for each equipped
// hero passive. A partial allocation is rolled back before returning.
func AllocatePassiveModifiers(
	allocator ModifierAllocator, creatures [3]game.GameplayCreature,
) ([3]uint32, error) {
	if allocator == nil {
		return [3]uint32{}, errors.New("nil modifier allocator")
	}
	var instance [3]uint32
	for index, creature := range creatures {
		if creature.Noun == 0 || creature.PassiveAbility == 0 {
			continue
		}
		instanceID, err := allocator.Allocate()
		if err != nil {
			for _, allocatedInstanceID := range instance {
				if allocatedInstanceID != 0 {
					_ = allocator.Release(allocatedInstanceID)
				}
			}
			return [3]uint32{}, fmt.Errorf(
				"passiveAllocate[%d]: %w", index, err,
			)
		}
		instance[index] = instanceID
	}
	return instance, nil
}
