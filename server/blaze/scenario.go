//go:build scenario

package blaze

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// ScenarioObservation observes accepted login on a connected Blaze session.
// Account registration, broker credential verification, and JWT issuance do
// not populate this server's authenticated connection index. The caller must
// never activate the isolated account itself to prepare the scenario.
func (e *Server) ScenarioObservation(
	ctx context.Context, userID int64, kind scenario.StepKind,
) (scenario.Observation, error) {
	if ctx == nil {
		return scenario.Observation{}, errors.New("scenario observation requires context")
	}
	err := ctx.Err()
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("observationContext: %w", err)
	}
	if userID <= 0 {
		return scenario.Observation{}, errors.New("scenario observation requires isolated user identity")
	}
	observation := scenario.Observation{
		Outcome: scenario.Inconclusive,
		Detail:  "no accepted live Blaze login for the isolated user",
	}
	if kind != scenario.Authenticated {
		observation.Detail = fmt.Sprintf("Blaze observation for %s is not implemented", kind)
		return observation, nil
	}
	if e == nil {
		observation.Detail = "Blaze observation provider is unavailable"
		return observation, nil
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mu.TryLock() {
		select {
		case <-ctx.Done():
			return scenario.Observation{}, fmt.Errorf("sessionLock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mu.Unlock()
	if e.listener == nil || e.serveGeneration == 0 {
		observation.Detail = "Blaze listener is not active"
		return observation, nil
	}
	var selectedSessionID uint32
	acceptedSessionCount := 0
	for sessionID, session := range e.sessionsByUser[userID] {
		if session == nil || sessionID == 0 || session.ID != sessionID ||
			session.server != e || session.conn == nil || session.userID != userID ||
			e.sessions[sessionID] != session {
			observation.Detail = "Blaze authenticated connection index is inconsistent"
			return observation, nil
		}
		storedUser, isPresent := session.Get(sessionUserKey)
		user, isUser := storedUser.(*sporenet.User)
		if !isPresent || !isUser || user == nil {
			// Delete removes the session value before taking this index lock.
			// That transitional logout state cannot prove accepted authentication.
			observation.Detail = "Blaze authenticated session binding is unavailable"
			return observation, nil
		}
		// Account.ID is assigned before registration/login publishes the User
		// and is immutable for that instance. Set publishes the new session value
		// before bindUserSession takes this lock, so verify that it still belongs
		// to the indexed identity rather than accepting a relogin transition.
		if user.Account.ID != userID {
			observation.Detail = "Blaze authenticated session identity is changing"
			return observation, nil
		}
		acceptedSessionCount++
		if selectedSessionID == 0 || sessionID < selectedSessionID {
			selectedSessionID = sessionID
		}
	}
	err = ctx.Err()
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("sessionScan: %w", err)
	}
	if acceptedSessionCount == 0 {
		return observation, nil
	}
	observation.Outcome = scenario.Passed
	observation.Detail = fmt.Sprintf(
		"accepted live Blaze login observed with %d authenticated connections", acceptedSessionCount,
	)
	// The oldest still-indexed connection supplies a stable identity while it
	// remains live. Connection count is not a count of client processes. Later
	// gameplay evidence must explicitly correlate this identity to its peer;
	// this observation proves neither gameplay admission nor a zone generation.
	observation.SessionID = fmt.Sprintf("blaze:%d/session:%d", e.serveGeneration, selectedSessionID)
	return observation, nil
}
