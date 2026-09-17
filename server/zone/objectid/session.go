package objectid

import (
	"errors"
	"sync"
)

// Session allocates non-overlapping object-ID ranges for one campaign world.
type Session struct {
	mu    sync.Mutex
	next  uint32
	limit uint32
}

func NewSession(first uint32, limit uint32) (*Session, error) {
	if first == 0 || limit == 0 || first >= limit {
		return nil, errors.New("campaign object id range invalid")
	}
	return &Session{next: first, limit: limit}, nil
}

func (s *Session) Reserve(count uint32) (uint32, error) {
	if s == nil {
		return 0, errors.New("nil campaign object id session")
	}
	if count == 0 {
		return 0, errors.New("campaign object id count invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if count > s.limit-s.next {
		return 0, errors.New("campaign object id range exhausted")
	}
	first := s.next
	s.next += count
	return first, nil
}

func (s *Session) Next() uint32 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next
}

// AdvancePast ensures future allocations cannot collide with a restored
// object. It is idempotent when the allocator is already ahead.
func (s *Session) AdvancePast(objectID uint32) error {
	if s == nil {
		return errors.New("nil campaign object id session")
	}
	if objectID == ^uint32(0) {
		return errors.New("campaign object id range exhausted")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := objectID + 1
	if next >= s.limit {
		return errors.New("campaign object id range exhausted")
	}
	if next > s.next {
		s.next = next
	}
	return nil
}
