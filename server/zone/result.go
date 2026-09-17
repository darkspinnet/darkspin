package zone

import (
	"errors"
	"fmt"

	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zoneoutcome "github.com/darkspinnet/darkspin/server/zone/outcome"
	zoneresult "github.com/darkspinnet/darkspin/server/zone/result"
)

// ReserveResult admits one current zone member into the result transaction
// after the shared boss encounter reaches its completed phase.
func (e *Zone) ReserveResult(member Member) (uint32, bool) {
	if e == nil {
		return 0, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.isCurrentMember(member) || e.info.Boss == nil ||
		e.info.Outcome == nil {
		return 0, false
	}
	boss := e.info.Boss.Snapshot()
	if boss.Phase != zoneboss.PhaseComplete || boss.LeaderObjectID == 0 {
		return 0, false
	}
	isReserved := e.info.Outcome.Reserve(
		outcomeMember(member), boss.LeaderObjectID,
	)
	if !isReserved {
		return 0, false
	}
	return boss.LeaderObjectID, true
}

func (e *Zone) ReservedResultBossObjectID(member Member) (uint32, bool) {
	if e == nil {
		return 0, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.isCurrentMember(member) || e.info.Outcome == nil {
		return 0, false
	}
	return e.info.Outcome.ReservedBossObjectID(outcomeMember(member))
}

func (e *Zone) RollbackResult(member Member) error {
	if e == nil {
		return errors.New("nil zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.isCurrentMember(member) || e.info.Outcome == nil {
		return errors.New("zone result member unavailable")
	}
	err := e.info.Outcome.Rollback(outcomeMember(member))
	if err != nil {
		return fmt.Errorf("resultRollback: %w", err)
	}
	return nil
}

// CommitResult coordinates the zone-owned vote, result ledger, boss outcome,
// and member outcome authorities. Transport adapters only project the result.
func (e *Zone) CommitResult(
	member Member, snapshot zoneresult.Snapshot,
) (*zoneresult.Session, error) {
	if e == nil {
		return nil, errors.New("nil zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.isCurrentMember(member) || e.info.Boss == nil ||
		e.info.Outcome == nil || e.info.ResultVote == nil ||
		e.info.Result == nil {
		return nil, errors.New("zone result authority unavailable")
	}
	bossObjectID, isReserved := e.info.Outcome.ReservedBossObjectID(
		outcomeMember(member),
	)
	if !isReserved {
		return nil, errors.New("zone result not reserved")
	}
	if snapshot.BossObjectID != 0 && snapshot.BossObjectID != bossObjectID {
		return nil, errors.New("zone result boss mismatch")
	}
	snapshot.BossObjectID = bossObjectID
	voteSnapshot := e.info.ResultVote.Snapshot()
	if voteSnapshot.Epoch == 0 {
		err := e.info.ResultVote.Begin(snapshot.ResultID)
		if err != nil {
			rollbackErr := e.info.Outcome.Rollback(outcomeMember(member))
			return nil, fmt.Errorf(
				"resultVote: %w", errors.Join(err, rollbackErr),
			)
		}
	}
	resultSession, err := e.info.Result.Put(resultVoter(member), snapshot)
	if err != nil {
		rollbackErr := e.info.Outcome.Rollback(outcomeMember(member))
		return nil, fmt.Errorf(
			"resultLedger: %w", errors.Join(err, rollbackErr),
		)
	}
	if !e.info.Boss.CommitOutcome() ||
		!e.info.Outcome.Commit(outcomeMember(member)) {
		e.info.Result.Remove(resultVoter(member))
		return nil, errors.New("zone result commit rejected")
	}
	isAllCommitted := e.info.Outcome.AreAllCommitted()
	if isAllCommitted {
		e.state = StateComplete
		// A pre-result checkpoint cannot faithfully represent the shared vote.
		// Keep it while any participant still needs to enter results so a process
		// exit cannot strand that participant, then retire it at the shared
		// completion boundary. Durable XP receipts make encounter replay safe.
		if e.info.Checkpoint != nil {
			e.info.Checkpoint.Discard(e.id)
		}
	}
	return resultSession, nil
}

func (e *Zone) CastResultVote(
	member Member, choice zoneresult.VoteChoice,
) (zoneresult.VoteSnapshot, bool) {
	if e == nil {
		return zoneresult.VoteSnapshot{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.isCurrentMember(member) || e.info.ResultVote == nil {
		return zoneresult.VoteSnapshot{}, false
	}
	voteSnapshot := e.info.ResultVote.Snapshot()
	return e.info.ResultVote.Cast(
		resultVoter(member), voteSnapshot.Epoch, choice,
	)
}

func (e *Zone) ResultVoteDecision(
	member Member,
) (zoneresult.VoteDecision, bool) {
	if e == nil {
		return zoneresult.VoteDecisionPending, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.isCurrentMember(member) || e.info.ResultVote == nil {
		return zoneresult.VoteDecisionPending, false
	}
	voteSnapshot := e.info.ResultVote.Snapshot()
	return e.info.ResultVote.Decision(
		resultVoter(member), voteSnapshot.Epoch,
	)
}

// CompleteForDeveloper advances the ordinary boss authority to its completed
// phase. It creates only the stable leader identity needed by the normal
// result transaction when the authored boss was not spawned.
func (e *Zone) CompleteForDeveloper() (uint32, error) {
	if e == nil {
		return 0, errors.New("nil zone")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.info.Boss == nil || e.info.ObjectID == nil {
		return 0, errors.New("zone developer completion unavailable")
	}
	bossObjectID := e.info.Boss.Snapshot().LeaderObjectID
	if bossObjectID == 0 {
		var err error
		bossObjectID, err = e.info.ObjectID.Reserve(1)
		if err != nil {
			return 0, fmt.Errorf("resultObjectID: %w", err)
		}
	}
	err := e.info.Boss.CompleteWithLeaderForDeveloper(bossObjectID)
	if err != nil {
		return 0, fmt.Errorf("resultBoss: %w", err)
	}
	return bossObjectID, nil
}

func (e *Zone) isCurrentMember(member Member) bool {
	current, isFound := e.members[member.UserID]
	return isFound && current.PeerGeneration == member.PeerGeneration
}

func outcomeMember(member Member) zoneoutcome.Member {
	return zoneoutcome.Member{
		UserID: member.UserID, PeerGeneration: member.PeerGeneration,
	}
}

func resultVoter(member Member) zoneresult.Voter {
	return zoneresult.Voter{
		UserID: member.UserID, PeerGeneration: member.PeerGeneration,
	}
}
