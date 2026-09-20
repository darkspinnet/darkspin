package squad

import (
	"errors"
	"fmt"
	"math"
)

// Resurrect explicitly restores fallen characters while the caller's party
// still owns an active mission. Ordinary healing cannot clear a squad wipe.
func (e *Session) Resurrect(maximumHitPoints [Size]float32) ([]uint32, error) {
	if e == nil {
		return nil, errors.New("nil squad")
	}
	indexes := make([]uint32, 0, Size)
	for index, character := range e.characters {
		if !character.IsAvailable || character.HitPoints > 0 {
			continue
		}
		maximum := maximumHitPoints[index]
		if maximum <= 0 || math.IsNaN(float64(maximum)) || math.IsInf(float64(maximum), 0) {
			return nil, fmt.Errorf("resurrectMaximum[%d]: invalid", index)
		}
		indexes = append(indexes, uint32(index))
	}
	for _, index := range indexes {
		e.characters[index].HitPoints = maximumHitPoints[index]
	}
	if len(indexes) > 0 {
		e.isGameOver = false
		e.isRestartReserved = false
	}
	return indexes, nil
}
