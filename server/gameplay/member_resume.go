package gameplay

func (e Lifecycle) CanResumeDefeatedMember(gameID uint32, userID uint64) bool {
	return e.memberResumePolicy != nil &&
		e.memberResumePolicy.CanResumeDefeatedMember(gameID, userID)
}
