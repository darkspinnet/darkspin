package pvp

// CancelForMember is trusted administrative removal. A party reservation is one
// queue admission, so losing any participant cancels it for every participant.
// The returned snapshots let the transport notify the affected peers.
func (e *Matchmaking) CancelForMember(userID int64) []MatchmakingSession {
	if e == nil || userID <= 0 {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	sessionID, isFound := e.sessionByUsers[userID]
	if !isFound {
		return nil
	}
	reservation, isFound := e.sessions[sessionID]
	if !isFound {
		delete(e.sessionByUsers, userID)
		return nil
	}
	delete(e.sessions, sessionID)
	sessions := make([]MatchmakingSession, 0, len(reservation.UserIDs))
	for _, participantID := range reservation.UserIDs {
		delete(e.sessionByUsers, participantID)
		sessions = append(sessions, MatchmakingSession{
			ID: reservation.ID, OwnerID: reservation.OwnerID, UserID: participantID,
			PartyID: reservation.PartyID, Criteria: reservation.Criteria,
		})
	}
	return sessions
}
