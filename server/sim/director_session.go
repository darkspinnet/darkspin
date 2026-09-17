package sim

import (
	"errors"
	"fmt"
	"sync"
)

type DirectorSession struct {
	mu       sync.RWMutex
	director *Director
}

func NewDirectorSession(director *Director) (*DirectorSession, error) {
	if director == nil {
		return nil, errors.New("nil director")
	}
	return &DirectorSession{director: director}, nil
}

func (e *DirectorSession) Phase() Phase {
	if e == nil {
		return ""
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.director.Simulator().Phase()
}

func (e *DirectorSession) IsCommandAccepted(command Command) bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.director.IsCommandAccepted(command)
}

func (e *DirectorSession) IsRoleActive(role Role) bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.director.IsRoleActive(role)
}

func (e *DirectorSession) FactCount(fact Fact) int64 {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.director.FactCount(fact)
}

func (e *DirectorSession) SetFact(fact Fact, count int64) error {
	if e == nil {
		return errors.New("nil director session")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.director.SetFact(fact, count)
	if err != nil {
		return fmt.Errorf("directorFact: %w", err)
	}
	return nil
}

func (e *DirectorSession) SetFactAndTransition(
	fact Fact, count int64, nextPhase Phase,
) error {
	if e == nil {
		return errors.New("nil director session")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	previousCount := e.director.FactCount(fact)
	err := e.director.SetFact(fact, count)
	if err != nil {
		return fmt.Errorf("directorFact: %w", err)
	}
	err = e.director.Transition(nextPhase)
	if err != nil {
		restoreErr := e.director.SetFact(fact, previousCount)
		return fmt.Errorf(
			"directorTransition: %w",
			errors.Join(err, restoreErr),
		)
	}
	return nil
}
