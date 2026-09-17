package unlock

import "sync"

// Session owns the ability boundary presented to one campaign participant.
// Scheduled unlock callbacks use CompareAndSet so an older callback cannot
// overwrite progress published by a newer encounter.
type Session struct {
	mu           sync.RWMutex
	abilityCount uint32
}

func NewSession(abilityCount uint32) *Session {
	return &Session{abilityCount: abilityCount}
}

func (s *Session) AbilityCount() uint32 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.abilityCount
}

func (s *Session) CompareAndSet(previous uint32, next uint32) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.abilityCount != previous {
		return false
	}
	s.abilityCount = next
	return true
}

func (s *Session) Raise(abilityCount uint32) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if abilityCount <= s.abilityCount {
		return false
	}
	s.abilityCount = abilityCount
	return true
}
