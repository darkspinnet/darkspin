package timeline

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Schedule func(time.Duration, func()) (func(), error)

// Session owns named delayed work for one campaign instance.
type Session struct {
	mu        sync.Mutex
	cancels   map[string]func()
	deadlines map[string]time.Time
	isStopped bool
}

func NewSession() *Session {
	return &Session{
		cancels: make(map[string]func()), deadlines: make(map[string]time.Time),
	}
}

type Snapshot struct {
	Key       string
	DueAt     time.Time
	Remaining time.Duration
}

func (s *Session) Schedule(
	key string, delay time.Duration, execute func(), schedule Schedule,
) error {
	if s == nil {
		return errors.New("nil campaign timeline")
	}
	if key == "" || delay < 0 || execute == nil || schedule == nil {
		return errors.New("campaign timeline task invalid")
	}
	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return errors.New("campaign timeline stopped")
	}
	if _, isFound := s.cancels[key]; isFound {
		s.mu.Unlock()
		return fmt.Errorf("campaign timeline duplicate: %s", key)
	}
	s.cancels[key] = nil
	s.deadlines[key] = time.Now().Add(delay)
	s.mu.Unlock()

	cancel, err := schedule(delay, func() {
		if !s.begin(key) {
			return
		}
		execute()
	})
	if err != nil {
		s.remove(key)
		return fmt.Errorf("campaignTimelineSchedule: %w", err)
	}
	if cancel == nil {
		s.remove(key)
		return errors.New("campaign timeline cancellation unavailable")
	}
	s.mu.Lock()
	_, isFound := s.cancels[key]
	if isFound {
		s.cancels[key] = cancel
	}
	s.mu.Unlock()
	if !isFound {
		cancel()
	}
	return nil
}

func (s *Session) begin(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isStopped {
		return false
	}
	if _, isFound := s.cancels[key]; !isFound {
		return false
	}
	delete(s.cancels, key)
	delete(s.deadlines, key)
	return true
}

func (s *Session) remove(key string) {
	s.mu.Lock()
	delete(s.cancels, key)
	delete(s.deadlines, key)
	s.mu.Unlock()
}

func (s *Session) Cancel(key string) bool {
	if s == nil || key == "" {
		return false
	}
	s.mu.Lock()
	cancel, isFound := s.cancels[key]
	if isFound {
		delete(s.cancels, key)
		delete(s.deadlines, key)
	}
	s.mu.Unlock()
	if isFound && cancel != nil {
		cancel()
	}
	return isFound
}

func (s *Session) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return
	}
	s.isStopped = true
	cancel := make([]func(), 0, len(s.cancels))
	for key, current := range s.cancels {
		if current != nil {
			cancel = append(cancel, current)
		}
		delete(s.cancels, key)
		delete(s.deadlines, key)
	}
	s.mu.Unlock()
	for _, current := range cancel {
		current()
	}
}

func (s *Session) Keys() []string {
	snapshots := s.Snapshots(time.Time{})
	keys := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		keys = append(keys, snapshot.Key)
	}
	return keys
}

func (s *Session) Snapshots(now time.Time) []Snapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	snapshots := make([]Snapshot, 0, len(s.cancels))
	for key := range s.cancels {
		deadline := s.deadlines[key]
		remaining := time.Duration(0)
		if !now.IsZero() && deadline.After(now) {
			remaining = deadline.Sub(now)
		}
		snapshots = append(snapshots, Snapshot{
			Key: key, DueAt: deadline, Remaining: remaining,
		})
	}
	s.mu.Unlock()
	sort.Slice(snapshots, func(left int, right int) bool {
		return snapshots[left].Key < snapshots[right].Key
	})
	return snapshots
}
