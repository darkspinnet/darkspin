package gameplay

// Continue replaces the human's campaign session in place. Its socketless
// peers still point at the previous zone until they are retired and admitted
// again against the next level's binding.
func (e campaignResultRuntime) retireCampaignServerBots(owner gameplayPeerSession) {
	if e.registry == nil || owner.binding.IsServerBot {
		return
	}
	e.registry.mutex.Lock()
	members := make([]gameplayPeerSession, 0)
	for sessionKey, member := range e.registry.sessions {
		if !member.binding.IsServerBot || member.binding.GameID != owner.binding.GameID ||
			member.binding.BotOwnerUserID != owner.binding.UserID {
			continue
		}
		members = append(members, member)
		delete(e.registry.sessions, sessionKey)
		e.registry.clearActionLeasesLocked(sessionKey, 0)
		memberKey := gameplaySessionMemberKey(member)
		if e.registry.memberTransports[memberKey] == member.transportGeneration {
			delete(e.registry.memberTransports, memberKey)
			delete(e.registry.memberEndpoints, memberKey)
		}
	}
	e.registry.mutex.Unlock()
	for _, member := range members {
		stopGameplayPeerSession(member, e.modifierPool, e.effectPool)
	}
}
