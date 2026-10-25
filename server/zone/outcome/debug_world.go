//go:build scenario

package outcome

import "github.com/darkspinnet/darkspin/server/scenario"

// ScenarioOutcome observes the existing result commitment for one exact member
// generation. Only Zone.CommitResult commits this authority after boss completion
// and result-ledger admission. Zone terminal/discard state is a separate fact.
func (e *Session) ScenarioOutcome(member Member) scenario.WorldOutcome {
	if e == nil || member.UserID == 0 || member.PeerGeneration == 0 || !e.mu.TryRLock() {
		return scenario.WorldOutcome{}
	}
	defer e.mu.RUnlock()
	current, isFound := e.members[member.UserID]
	if !isFound || current.member.PeerGeneration != member.PeerGeneration {
		return scenario.WorldOutcome{}
	}
	return scenario.WorldOutcome{IsAvailable: true, IsVictoryCommitted: current.phase == PhaseCommitted && current.bossObjectID != 0}
}
