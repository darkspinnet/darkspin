package zone

import (
	"errors"
	"fmt"
)

// ExperienceAward is one accepted live-to-dead NPC reward. BaseExperience
// preserves the authored class value before member equipment modifiers.
type ExperienceAward struct {
	ObjectID       uint32
	BaseExperience uint32
	MemberAwards   map[uint64]uint32
}

// ExperienceCommit is the idempotent successful-mission persistence state for
// one zone member.
type ExperienceCommit struct {
	Amount       uint32
	CumulativeXP uint32
	Level        uint32
	IsReserved   bool
	IsCommitted  bool
}

// RecordNPCExperience accepts one NPC reward exactly once and adds each
// member-specific modified award to the current mission ledger.
func (e *Zone) RecordNPCExperience(
	objectID uint32, baseExperience uint32, memberAwards map[uint64]uint32,
) (map[uint64]uint32, bool, error) {
	if e == nil || objectID == 0 || baseExperience == 0 || len(memberAwards) == 0 {
		return nil, false, errors.New("zone experience invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return nil, false, errors.New("zone experience inactive")
	}
	if _, isFound := e.experienceAwards[objectID]; isFound {
		return nil, false, nil
	}
	award := ExperienceAward{
		ObjectID: objectID, BaseExperience: baseExperience,
		MemberAwards: make(map[uint64]uint32, len(memberAwards)),
	}
	totals := make(map[uint64]uint32, len(memberAwards))
	for userID, experience := range memberAwards {
		if userID == 0 || experience == 0 {
			continue
		}
		if experience > ^uint32(0)-e.experienceTotals[userID] {
			return nil, false, fmt.Errorf("zone experience overflow: user %d", userID)
		}
		award.MemberAwards[userID] = experience
		totals[userID] = e.experienceTotals[userID] + experience
	}
	if len(award.MemberAwards) == 0 {
		return nil, false, errors.New("zone experience members empty")
	}
	for userID, experience := range totals {
		e.experienceTotals[userID] = experience
	}
	e.experienceAwards[objectID] = award
	return totals, true, nil
}

// ProvisionalExperience returns one member's uncommitted current-mission XP.
func (e *Zone) ProvisionalExperience(userID uint64) uint32 {
	if e == nil || userID == 0 {
		return 0
	}
	e.mu.RLock()
	experience := e.experienceTotals[userID]
	e.mu.RUnlock()
	return experience
}

// ReserveExperienceCommit serializes the successful-mission persistence
// boundary. A committed result is returned for presentation retries.
func (e *Zone) ReserveExperienceCommit(userID uint64) (ExperienceCommit, bool) {
	if e == nil || userID == 0 {
		return ExperienceCommit{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.experienceCommits[userID]
	if current.IsCommitted || current.IsReserved {
		return current, false
	}
	current.Amount = e.experienceTotals[userID]
	current.IsReserved = true
	e.experienceCommits[userID] = current
	return current, true
}

// CommitExperience records the persisted cumulative account result.
func (e *Zone) CommitExperience(userID uint64, cumulativeXP uint32, level uint32) bool {
	if e == nil || userID == 0 || level == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.experienceCommits[userID]
	if !current.IsReserved || current.IsCommitted {
		return false
	}
	current.CumulativeXP = cumulativeXP
	current.Level = level
	current.IsReserved = false
	current.IsCommitted = true
	e.experienceCommits[userID] = current
	return true
}

// RollbackExperienceCommit releases a failed persistence reservation.
func (e *Zone) RollbackExperienceCommit(userID uint64) {
	if e == nil || userID == 0 {
		return
	}
	e.mu.Lock()
	current := e.experienceCommits[userID]
	if current.IsReserved && !current.IsCommitted {
		current.IsReserved = false
		e.experienceCommits[userID] = current
	}
	e.mu.Unlock()
}
