package action

import (
	"sync"
	"time"
)

type ReleaseReservation struct {
	previousEnd     time.Time
	currentEnd      time.Time
	currentRevision uint64
}

// ReleaseSession owns the shared action-release gate for one participant.
// Rollback is revision-scoped so an older scheduling failure cannot reopen a
// newer accepted action.
type ReleaseSession struct {
	mutex    sync.RWMutex
	end      time.Time
	revision uint64
}

type ReleaseSnapshot struct {
	End      time.Time
	Revision uint64
}

func NewReleaseSession() *ReleaseSession {
	return &ReleaseSession{}
}

func (s *ReleaseSession) IsReady(now time.Time) bool {
	if s == nil {
		return false
	}
	s.mutex.RLock()
	end := s.end
	s.mutex.RUnlock()
	return !now.Before(end)
}

func (s *ReleaseSession) Reserve(
	now time.Time, duration time.Duration,
) (ReleaseReservation, bool) {
	if s == nil || now.IsZero() || duration < 0 {
		return ReleaseReservation{}, false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if now.Before(s.end) {
		return ReleaseReservation{}, false
	}
	previousEnd := s.end
	s.end = now.Add(duration)
	s.revision++
	return ReleaseReservation{
		previousEnd: previousEnd, currentEnd: s.end,
		currentRevision: s.revision,
	}, true
}

func (s *ReleaseSession) Rollback(reservation ReleaseReservation) bool {
	if s == nil || reservation.currentRevision == 0 {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.revision != reservation.currentRevision ||
		!s.end.Equal(reservation.currentEnd) {
		return false
	}
	s.end = reservation.previousEnd
	s.revision++
	return true
}

func (s *ReleaseSession) Reset() bool {
	if s == nil {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	isChanged := !s.end.IsZero()
	s.end = time.Time{}
	s.revision++
	return isChanged
}

func (s *ReleaseSession) Snapshot() ReleaseSnapshot {
	if s == nil {
		return ReleaseSnapshot{}
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return ReleaseSnapshot{End: s.end, Revision: s.revision}
}
