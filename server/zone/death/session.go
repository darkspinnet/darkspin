package death

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type Cancel func()

type Run interface {
	AdvanceDeath(context.Context, time.Duration) ([]zonenpc.DeathEvent, error)
	ReviveDeath(context.Context) ([]zonenpc.DeathEvent, bool, error)
	StopDeath()
}

type entry struct {
	run     Run
	cancels []Cancel
}

// Session owns retained enemy-death simulations for one campaign instance.
type Session struct {
	mu      sync.Mutex
	entries map[uint32]entry
}

func NewSession() *Session {
	return &Session{entries: make(map[uint32]entry)}
}

func (s *Session) Add(objectID uint32, run Run) error {
	if s == nil {
		return errors.New("death add: nil session")
	}
	if objectID == 0 || run == nil {
		return errors.New("death add: invalid run")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFound := s.entries[objectID]; isFound {
		return fmt.Errorf("deathDuplicate: %d", objectID)
	}
	s.entries[objectID] = entry{run: run}
	return nil
}

func (s *Session) AddCancel(objectID uint32, run Run, cancel Cancel) error {
	if s == nil || cancel == nil {
		return errors.New("death cancel: invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.entries[objectID]
	if !isFound || current.run != run {
		return fmt.Errorf("deathMissing: %d", objectID)
	}
	current.cancels = append(current.cancels, cancel)
	s.entries[objectID] = current
	return nil
}

func (s *Session) Advance(
	ctx context.Context, objectID uint32, run Run, deadline time.Duration,
) ([]zonenpc.DeathEvent, bool, error) {
	if ctx == nil {
		return nil, false, errors.New("death advance: nil context")
	}
	s.mu.Lock()
	current, isFound := s.entries[objectID]
	if !isFound || current.run != run {
		s.mu.Unlock()
		return nil, false, nil
	}
	event, err := current.run.AdvanceDeath(ctx, deadline)
	s.mu.Unlock()
	if err != nil {
		return nil, true, fmt.Errorf("deathAdvance: %w", err)
	}
	return event, true, nil
}

func (s *Session) Complete(objectID uint32, run Run) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	current, isFound := s.entries[objectID]
	if isFound && current.run == run {
		delete(s.entries, objectID)
	}
	s.mu.Unlock()
	return isFound && current.run == run
}

func (s *Session) Revive(
	ctx context.Context, objectID uint32,
) ([]zonenpc.DeathEvent, bool, error) {
	if s == nil || ctx == nil || objectID == 0 {
		return nil, false, errors.New("death revive: invalid")
	}
	s.mu.Lock()
	current, isFound := s.entries[objectID]
	if !isFound {
		s.mu.Unlock()
		return nil, false, nil
	}
	event, isRevived, err := current.run.ReviveDeath(ctx)
	if err == nil && isRevived {
		delete(s.entries, objectID)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, false, fmt.Errorf("deathRevive: %w", err)
	}
	if !isRevived {
		return nil, false, nil
	}
	for _, cancel := range current.cancels {
		cancel()
	}
	return event, true, nil
}

func (s *Session) Remove(objectID uint32, run Run) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	current, isFound := s.entries[objectID]
	if isFound && current.run == run {
		delete(s.entries, objectID)
	}
	s.mu.Unlock()
	if !isFound || current.run != run {
		return false
	}
	for _, cancel := range current.cancels {
		cancel()
	}
	current.run.StopDeath()
	return true
}

func (s *Session) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	entries := make([]entry, 0, len(s.entries))
	for objectID, current := range s.entries {
		entries = append(entries, current)
		delete(s.entries, objectID)
	}
	s.mu.Unlock()
	for _, current := range entries {
		for _, cancel := range current.cancels {
			cancel()
		}
		current.run.StopDeath()
	}
}
