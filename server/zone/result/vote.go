package result

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const VoteDuration = 2*time.Minute + 30*time.Second

type VoteChoice uint8

const (
	VoteChoicePending VoteChoice = iota
	VoteChoiceContinue
	VoteChoiceCashOut
)

type VoteDecision uint8

const (
	VoteDecisionPending VoteDecision = iota
	VoteDecisionContinue
	VoteDecisionCashOut
)

type Voter struct {
	UserID         uint64
	PeerGeneration uint64
}

type VoteSnapshot struct {
	Epoch         uint64
	MemberCount   uint16
	ContinueCount uint16
	CashOutCount  uint16
	Decision      VoteDecision
}

type voteMember struct {
	voter  Voter
	choice VoteChoice
}

// VoteSession owns the shared Continue or Cash Out decision for one campaign
// result epoch. Any Cash Out vote resolves conservatively to Cash Out, while
// Continue requires every current member.
type VoteSession struct {
	mu       sync.RWMutex
	epoch    uint64
	decision VoteDecision
	voters   map[uint64]voteMember
}

func NewVoteSession() *VoteSession {
	return &VoteSession{voters: make(map[uint64]voteMember)}
}

// RestoreVoter reserves one durable participant without treating an old
// process-local transport generation as current authority.
func (s *VoteSession) RestoreVoter(userID uint64) error {
	if s == nil {
		return errors.New("nil campaign result vote")
	}
	if userID == 0 {
		return errors.New("campaign result voter invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, isFound := s.voters[userID]; isFound {
		return nil
	}
	s.voters[userID] = voteMember{voter: Voter{UserID: userID}}
	return nil
}

func (s *VoteSession) Join(voter Voter) error {
	if s == nil {
		return errors.New("nil campaign result vote")
	}
	if voter.UserID == 0 || voter.PeerGeneration == 0 {
		return errors.New("campaign result voter invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.voters[voter.UserID]
	if isFound && current.voter.PeerGeneration != 0 &&
		voter.PeerGeneration < current.voter.PeerGeneration {
		return fmt.Errorf(
			"campaign result voter generation stale: got %d, want >= %d",
			voter.PeerGeneration, current.voter.PeerGeneration,
		)
	}
	if isFound && voter.PeerGeneration == current.voter.PeerGeneration {
		return nil
	}
	current.voter = voter
	s.voters[voter.UserID] = current
	if s.decision == VoteDecisionPending {
		s.resolve()
	}
	return nil
}

func (s *VoteSession) Leave(voter Voter) bool {
	if s == nil || voter.UserID == 0 || voter.PeerGeneration == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.voters[voter.UserID]
	if !isFound || current.voter.PeerGeneration != voter.PeerGeneration {
		return false
	}
	delete(s.voters, voter.UserID)
	if s.decision == VoteDecisionPending {
		s.resolve()
	}
	return true
}

func (s *VoteSession) Begin(epoch uint64) error {
	if s == nil {
		return errors.New("nil campaign result vote")
	}
	if epoch == 0 {
		return errors.New("campaign result vote epoch invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch == epoch {
		return nil
	}
	if s.epoch != 0 {
		return errors.New("campaign result vote already begun")
	}
	s.epoch = epoch
	s.decision = VoteDecisionPending
	for userID, current := range s.voters {
		current.choice = VoteChoicePending
		s.voters[userID] = current
	}
	return nil
}

func (s *VoteSession) Cast(
	voter Voter, epoch uint64, choice VoteChoice,
) (VoteSnapshot, bool) {
	if s == nil || epoch == 0 ||
		(choice != VoteChoiceContinue && choice != VoteChoiceCashOut) {
		return VoteSnapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, isFound := s.voters[voter.UserID]
	if !isFound || current.voter.PeerGeneration != voter.PeerGeneration ||
		s.epoch != epoch || s.decision != VoteDecisionPending {
		return s.snapshot(), false
	}
	if current.choice == choice {
		return s.snapshot(), false
	}
	if current.choice != VoteChoicePending {
		return s.snapshot(), false
	}
	current.choice = choice
	s.voters[voter.UserID] = current
	s.resolve()
	return s.snapshot(), true
}

func (s *VoteSession) Snapshot() VoteSnapshot {
	if s == nil {
		return VoteSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot()
}

func (s *VoteSession) Decision(
	voter Voter, epoch uint64,
) (VoteDecision, bool) {
	if s == nil || epoch == 0 {
		return VoteDecisionPending, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, isFound := s.voters[voter.UserID]
	if !isFound || current.voter.PeerGeneration != voter.PeerGeneration ||
		s.epoch != epoch || s.decision == VoteDecisionPending {
		return VoteDecisionPending, false
	}
	return s.decision, true
}

func (s *VoteSession) resolve() {
	if s.epoch == 0 || len(s.voters) == 0 {
		return
	}
	isAllContinue := true
	for _, current := range s.voters {
		switch current.choice {
		case VoteChoiceCashOut:
			s.decision = VoteDecisionCashOut
			return
		case VoteChoiceContinue:
		default:
			isAllContinue = false
		}
	}
	if isAllContinue {
		s.decision = VoteDecisionContinue
	}
}

func (s *VoteSession) snapshot() VoteSnapshot {
	snapshot := VoteSnapshot{
		Epoch:       s.epoch,
		MemberCount: uint16(len(s.voters)),
		Decision:    s.decision,
	}
	for _, current := range s.voters {
		switch current.choice {
		case VoteChoiceContinue:
			snapshot.ContinueCount++
		case VoteChoiceCashOut:
			snapshot.CashOutCount++
		}
	}
	return snapshot
}
