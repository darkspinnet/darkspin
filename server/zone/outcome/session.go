package outcome

import (
	"errors"
	"fmt"
	"sync"
)

type Phase uint8

const (
	PhasePending Phase = iota
	PhaseReserved
	PhaseCommitted
)

type Member struct {
	UserID         uint64
	PeerGeneration uint64
}

type memberState struct {
	member       Member
	phase        Phase
	bossObjectID uint32
}

// Session owns each campaign member's admission into the personal result
// transaction after the shared encounter completes.
type Session struct {
	mu      sync.RWMutex
	members map[uint64]memberState
}

func NewSession() *Session {
	return &Session{members: make(map[uint64]memberState)}
}

// RestoreMember reserves one durable participant without carrying a
// process-local peer generation across a server restart.
func (s *Session) RestoreMember(userID uint64) error {
	if s == nil {
		return errors.New("nil campaign outcome")
	}
	if userID == 0 {
		return errors.New("campaign outcome member invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFound := s.members[userID]; isFound {
		return nil
	}
	s.members[userID] = memberState{member: Member{UserID: userID}}
	return nil
}

func (s *Session) Join(member Member) error {
	if s == nil {
		return errors.New("nil campaign outcome")
	}
	if member.UserID == 0 || member.PeerGeneration == 0 {
		return errors.New("campaign outcome member invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.members[member.UserID]
	if isFound && current.member.PeerGeneration != 0 &&
		member.PeerGeneration < current.member.PeerGeneration {
		return fmt.Errorf(
			"campaign outcome generation stale: got %d, want >= %d",
			member.PeerGeneration, current.member.PeerGeneration,
		)
	}
	if isFound && member.PeerGeneration == current.member.PeerGeneration {
		return nil
	}
	current.member = member
	s.members[member.UserID] = current
	return nil
}

func (s *Session) Leave(member Member) bool {
	if s == nil || member.UserID == 0 || member.PeerGeneration == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.members[member.UserID]
	if !isFound || current.member.PeerGeneration != member.PeerGeneration {
		return false
	}
	delete(s.members, member.UserID)
	return true
}

func (s *Session) Reserve(member Member, bossObjectID uint32) bool {
	if s == nil || bossObjectID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.members[member.UserID]
	if !isFound || current.member.PeerGeneration != member.PeerGeneration ||
		current.phase != PhasePending {
		return false
	}
	current.phase = PhaseReserved
	current.bossObjectID = bossObjectID
	s.members[member.UserID] = current
	return true
}

func (s *Session) Rollback(member Member) error {
	if s == nil {
		return errors.New("nil campaign outcome")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.members[member.UserID]
	if !isFound || current.member.PeerGeneration != member.PeerGeneration ||
		current.phase != PhaseReserved {
		return errors.New("campaign outcome rollback invalid")
	}
	current.phase = PhasePending
	current.bossObjectID = 0
	s.members[member.UserID] = current
	return nil
}

func (s *Session) Commit(member Member) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.members[member.UserID]
	if !isFound || current.member.PeerGeneration != member.PeerGeneration ||
		current.phase != PhaseReserved {
		return false
	}
	current.phase = PhaseCommitted
	s.members[member.UserID] = current
	return true
}

func (s *Session) ReservedBossObjectID(member Member) (uint32, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, isFound := s.members[member.UserID]
	if !isFound || current.member.PeerGeneration != member.PeerGeneration ||
		current.phase != PhaseReserved || current.bossObjectID == 0 {
		return 0, false
	}
	return current.bossObjectID, true
}

func (s *Session) Phase(member Member) (Phase, bool) {
	if s == nil {
		return PhasePending, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, isFound := s.members[member.UserID]
	if !isFound || current.member.PeerGeneration != member.PeerGeneration {
		return PhasePending, false
	}
	return current.phase, true
}

// AreAllCommitted reports whether every currently admitted member has entered
// its personal result transaction. Encounter completion is shared, but one
// player's result must not retire the zone while another player is still
// leaving gameplay.
func (e *Session) AreAllCommitted() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.members) == 0 {
		return false
	}
	for _, current := range e.members {
		if current.phase != PhaseCommitted {
			return false
		}
	}
	return true
}
