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

type timerRun interface {
	ResetDeathTimer(context.Context, time.Duration) ([]time.Duration, bool, error)
}

type deadlineRun interface {
	IsFinalDeadline(time.Duration) bool
}

type repairRun interface {
	IsRepairableCorpse() bool
}

func (e *Session) IsRepairableCorpse(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current, isFound := e.entries[objectID]
	if !isFound {
		return false
	}
	run, isSupported := current.run.(repairRun)
	return isSupported && run.IsRepairableCorpse()
}

func (e *Session) CompleteDeadline(objectID uint32, run Run, deadline time.Duration) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current, isFound := e.entries[objectID]
	if !isFound || current.run != run {
		return false
	}
	timedRun, isSupported := run.(deadlineRun)
	if !isSupported || !timedRun.IsFinalDeadline(deadline) {
		return false
	}
	delete(e.entries, objectID)
	return true
}

// ResetTimer retains the shared corpse across repeated repair applications.
func (e *Session) ResetTimer(ctx context.Context, objectID uint32, delay time.Duration) (Run, []time.Duration, bool, error) {
	if e == nil || ctx == nil || objectID == 0 || delay <= 0 {
		return nil, nil, false, errors.New("invalid death timer reset")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current, isFound := e.entries[objectID]
	if !isFound {
		return nil, nil, false, nil
	}
	run, isSupported := current.run.(timerRun)
	if !isSupported {
		return nil, nil, false, errors.New("death timer unsupported")
	}
	delays, isReset, err := run.ResetDeathTimer(ctx, delay)
	if err != nil {
		return nil, nil, false, fmt.Errorf("deathTimer: %w", err)
	}
	if !isReset {
		return current.run, nil, false, nil
	}
	for _, cancel := range current.cancels {
		cancel()
	}
	current.cancels = nil
	e.entries[objectID] = current
	return current.run, delays, true, nil
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
