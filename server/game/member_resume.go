package game

// MemberResumePolicy consults the live mission, rather than inferring defeat
// from a client's return-to-ship message.
type MemberResumePolicy interface {
	CanResumeDefeatedMember(gameID uint32, userID uint64) bool
}

func (e *Manager) UseMemberResumePolicy(policy MemberResumePolicy) {
	e.mu.Lock()
	e.memberResumePolicy = policy
	e.mu.Unlock()
}

func (e *Manager) CanResumeDefeatedMember(gameID uint32, userID int64) bool {
	if e == nil || userID <= 0 {
		return false
	}
	e.mu.RLock()
	policy := e.memberResumePolicy
	instance := e.games[gameID]
	e.mu.RUnlock()
	if policy == nil || instance == nil {
		return false
	}
	instance.mu.RLock()
	_, isMember := instance.players[userID]
	isCampaign := instance.Info.Mode == ModeChain
	instance.mu.RUnlock()
	return isMember && isCampaign && policy.CanResumeDefeatedMember(gameID, uint64(userID))
}
