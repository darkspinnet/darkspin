package result

import (
	"errors"
	"fmt"
	"sync"
)

type ledgerEntry struct {
	voter   Voter
	session *Session
}

// Ledger owns each participant's result transaction for one campaign
// instance. Entries are generation-scoped so a stale peer cannot replace or
// remove the active participant's result.
type Ledger struct {
	mutex   sync.RWMutex
	entries map[uint64]ledgerEntry
}

func NewLedger() *Ledger {
	return &Ledger{entries: make(map[uint64]ledgerEntry)}
}

func (l *Ledger) Put(voter Voter, snapshot Snapshot) (*Session, error) {
	if l == nil {
		return nil, errors.New("nil campaign result ledger")
	}
	if voter.UserID == 0 || voter.PeerGeneration == 0 ||
		snapshot.ResultID == 0 || snapshot.UserID != voter.UserID ||
		snapshot.BossObjectID == 0 {
		return nil, errors.New("campaign result ledger entry invalid")
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	current, isFound := l.entries[voter.UserID]
	if isFound && voter.PeerGeneration < current.voter.PeerGeneration {
		return nil, fmt.Errorf(
			"campaign result ledger generation stale: got %d, want >= %d",
			voter.PeerGeneration, current.voter.PeerGeneration,
		)
	}
	if isFound && voter.PeerGeneration == current.voter.PeerGeneration {
		if current.session.Snapshot().ResultID != snapshot.ResultID {
			return nil, errors.New("campaign result ledger entry conflicts")
		}
		return current.session, nil
	}
	session := NewSession(snapshot)
	l.entries[voter.UserID] = ledgerEntry{voter: voter, session: session}
	return session, nil
}

func (l *Ledger) Get(voter Voter) (*Session, bool) {
	if l == nil || voter.UserID == 0 || voter.PeerGeneration == 0 {
		return nil, false
	}
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	current, isFound := l.entries[voter.UserID]
	if !isFound || current.voter.PeerGeneration != voter.PeerGeneration {
		return nil, false
	}
	return current.session, true
}

// Rebind advances one retained result transaction to a replacement peer
// generation without discarding its result phase or durable receipt.
func (l *Ledger) Rebind(voter Voter) bool {
	if l == nil || voter.UserID == 0 || voter.PeerGeneration == 0 {
		return false
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	current, isFound := l.entries[voter.UserID]
	if !isFound || voter.PeerGeneration < current.voter.PeerGeneration {
		return false
	}
	current.voter = voter
	l.entries[voter.UserID] = current
	return true
}

func (l *Ledger) Remove(voter Voter) bool {
	if l == nil || voter.UserID == 0 || voter.PeerGeneration == 0 {
		return false
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	current, isFound := l.entries[voter.UserID]
	if !isFound || current.voter.PeerGeneration != voter.PeerGeneration {
		return false
	}
	delete(l.entries, voter.UserID)
	return true
}
