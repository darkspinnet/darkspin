package gameplay

import "fmt"

// Capture and queue a returning owner's pets together. A scheduled completion
// uses this same registry lock, so its final pose cannot precede a stale create.
func (e gameplaySetupRuntime) publishRejoinedCompanions(
	sessionKey string, peerSession gameplayPeerSession,
) error {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	current, isFound := e.registry.sessions[sessionKey]
	if !isFound || current.zone != peerSession.zone ||
		current.generation != peerSession.generation ||
		current.transportGeneration != peerSession.transportGeneration ||
		current.isRejoinPending || current.isZoneTerminal() {
		return nil
	}
	packets, err := marshalGameplayCompanions(current, current.binding.UserID, e.now())
	if err != nil {
		return fmt.Errorf("ownerBaseline: %w", err)
	}
	for candidateKey, candidate := range e.registry.sessions {
		if candidateKey == sessionKey || candidate.zone != current.zone ||
			candidate.isRejoinPending || candidate.isZoneTerminal() {
			continue
		}
		queueErr := candidate.queueCampaignPresentation(packets)
		if queueErr != nil && e.registry.logger != nil {
			e.registry.logger.Printf("RakNet reconnect companion queue failed user=%d: %v",
				candidate.binding.UserID, queueErr)
		}
		e.registry.sessions[candidateKey] = candidate
	}
	return nil
}
