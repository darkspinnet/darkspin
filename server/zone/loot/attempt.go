package loot

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
)

// CrystalAttempts follows sub_9D3110: one attempt per stored participant,
// with one extra round on the boss branch when its draw is strictly above 0.5.
func CrystalAttempts(npcType uint32, participantCount uint32, random *sim.SimulatorRandom) (uint32, error) {
	if participantCount == 0 || participantCount > 255 || random == nil {
		return 0, errors.New("invalid crystal attempt input")
	}
	if npcType > 7 {
		return 0, nil
	}
	if npcType == 2 && random.Float64() > 0.5 {
		return 2 * participantCount, nil
	}
	return participantCount, nil
}

// EquipmentAttempts does not guarantee a successful drop; each returned
// attempt must still pass the party-scaled equipment threshold.
func EquipmentAttempts(npcType uint32, participantCount uint32, random *sim.SimulatorRandom) (uint32, error) {
	if participantCount == 0 || participantCount > 255 || random == nil {
		return 0, errors.New("invalid equipment attempt input")
	}
	switch npcType {
	case 0, 1, 5:
		return 1, nil
	case 2:
		extra, err := random.Index(2*participantCount + 1)
		if err != nil {
			return 0, fmt.Errorf("bossAttempt: %w", err)
		}
		return 3*participantCount + extra, nil
	case 4, 7:
		return participantCount, nil
	default:
		return 0, nil
	}
}
