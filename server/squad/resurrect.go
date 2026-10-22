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

// ResurrectCharacter applies an explicit recovery to one selected character.
// The caller chooses the fallen slot and computes its clamped recovery amount.
// Ordinary healing continues to respect the squad's terminal latch.
func (e *Session) ResurrectCharacter(index uint32, hitPoint float32) error {
	if e == nil || index >= Size {
		return errors.New("resurrection character unavailable")
	}
	if !e.characters[index].IsAvailable {
		return fmt.Errorf("resurrectionCharacter: %w", ErrCharacterUnavailable)
	}
	if hitPoint < 0 || math.IsNaN(float64(hitPoint)) || math.IsInf(float64(hitPoint), 0) {
		return errors.New("resurrection health invalid")
	}
	e.characters[index].HitPoints = hitPoint
	if hitPoint > 0 {
		e.isResurrectionPending = false
		e.isGameOver = false
		e.isRestartReserved = false
	}
	return nil
}

// ReserveResurrection keeps a pending self-revival from committing a squad wipe.
// The use counter belongs to the mission's player, across hero and pet changes.
func (e *Session) ReserveResurrection() {
	if e != nil {
		e.isResurrectionPending = true
	}
}

func (e *Session) IsResurrectionPending() bool {
	return e != nil && e.isResurrectionPending
}

func (e *Session) IsBeastResurrectionUsed() bool {
	return e != nil && e.isBeastResurrectionUsed
}

func (e *Session) ConsumeBeastResurrection() {
	if e != nil {
		e.isBeastResurrectionUsed = true
	}
}

func (e *Session) CancelResurrection() error {
	if e == nil || !e.isResurrectionPending {
		return nil
	}
	e.isResurrectionPending = false
	character, isFound := e.DeployedCharacter()
	if !isFound {
		return errors.New("resurrection character unavailable")
	}
	isGameOver, err := e.SetHitPoints(e.characterIndex, character.HitPoints)
	if err != nil {
		return fmt.Errorf("cancelHealth: %w", err)
	}
	if isGameOver {
		e.isRestartReserved = false
	}
	return nil
}
