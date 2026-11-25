package blaze

import (
	"context"
	"fmt"
)

const sessionWelcomeKey = "darkspin.welcomeQueued"

// WelcomeProvider projects server-owned configuration into safe chat lines.
type WelcomeProvider interface {
	Welcome(context.Context) ([]string, error)
}

func (e *Registry) UseWelcomeProvider(provider WelcomeProvider) {
	e.mu.Lock()
	e.welcomeProvider = provider
	e.mu.Unlock()
}

// queueSessionWelcome runs after successful persona login encoding, when the
// ordinary user-session notifications are already queued. Request dispatch is
// serialized by the session owner; no additional sender or listener is needed.
func (e *Registry) queueSessionWelcome(ctx context.Context, req *Request) {
	if req.Component != AuthComponentID || req.Command != authLoginPersona || req.Session == nil {
		return
	}
	user := requestUser(req)
	if user == nil {
		return
	}
	marker, isQueued := req.Session.Get(sessionWelcomeKey)
	if isQueued && marker == true {
		return
	}
	e.mu.RLock()
	provider := e.welcomeProvider
	e.mu.RUnlock()
	if provider == nil {
		return
	}
	lines, err := provider.Welcome(ctx)
	if err != nil {
		logWelcomeFailure(req.Session, fmt.Errorf("welcomeRead: %w", err))
		return
	}
	frames := make([]Frame, 0, len(lines))
	for _, line := range lines {
		frame, encodeErr := encodeNotification(MessagingComponentID, 0x01,
			systemChatNotification(user.Account.ID, 0, line))
		if encodeErr != nil {
			logWelcomeFailure(req.Session, fmt.Errorf("welcomeEncode: %w", encodeErr))
			return
		}
		frames = append(frames, frame)
	}
	req.notifications = append(req.notifications, frames...)
	req.Session.Set(sessionWelcomeKey, true)
}

func logWelcomeFailure(session *Session, err error) {
	if session.server == nil || session.server.logger == nil {
		return
	}
	session.server.logger.Printf("Blaze welcome unavailable session=%d: %v", session.ID, err)
}
