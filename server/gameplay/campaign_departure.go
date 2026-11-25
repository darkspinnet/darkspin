package gameplay

// The next mission reuses this transport's native CPlayer. Retire the old
// world's heroes and producers, but do not publish PlayerDeparted for it.
func stopContinuedGameplayPeerSession(
	peerSession gameplayPeerSession, modifierInstancePool *modifierPool,
	effectPool *attachedEffectPool,
) {
	leaveGameplayPeerMembershipForTransition(peerSession, true)
	stopGameplayPeerRuntime(&peerSession, modifierInstancePool, effectPool)
	peerSession.settleChannelRemovals()
}
