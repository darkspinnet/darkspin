package gameplay

import "time"

// A deferred endpoint install must put back the exact retained aggregate that
// takeRetained removed, with its original expiry rather than a new grace period.
type fixtureRetainedJoinRestore struct {
	registry *gameplaySessionRegistry
	key      gameplayMemberKey
	retained *gameplayRetainedSession
}

func (e fixtureRetainedJoinRestore) restore() {
	if e.retained == nil {
		return
	}
	e.registry.mutex.Lock()
	if e.registry.isClosed {
		e.registry.mutex.Unlock()
		leaveGameplayPeerMembership(e.retained.peerSession)
		return
	}
	if e.registry.retainedSessions[e.key] != nil {
		e.registry.mutex.Unlock()
		return
	}
	e.registry.retainedSessions[e.key] = e.retained
	handover := e.registry.voodooCharms.handovers[e.key]
	if handover != nil && handover.member.zone == e.retained.peerSession.zone &&
		handover.member.generation == e.retained.peerSession.generation {
		delete(e.registry.voodooCharms.handovers, e.key)
	}
	delay := max(time.Duration(0), e.retained.expiresAt.Sub(e.registry.now()))
	e.registry.mutex.Unlock()
	if e.registry.timer == nil {
		e.expire()
		return
	}
	cancel, err := e.registry.timer.Schedule(delay, e.expire)
	if err != nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		if e.registry.logger != nil {
			e.registry.logger.Printf("Fixture deferred rejoin expiry failed game=%d user=%d: %v", e.key.gameID, e.key.userID, err)
		}
		e.expire()
		return
	}
	e.registry.mutex.Lock()
	isCurrent := e.registry.retainedSessions[e.key] == e.retained
	if isCurrent {
		e.retained.cancel = cancel
	}
	e.registry.mutex.Unlock()
	if !isCurrent {
		cancel()
	}
}

func (e fixtureRetainedJoinRestore) expire() {
	e.registry.lifecycle.expireRetained(e.key, e.retained)
}
