package controlpanel

import (
	"crypto/sha256"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

type accountSessionDTO struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	IsCurrent  bool      `json:"is_current"`
}

func (e *Service) accountSessions(writer http.ResponseWriter, request *http.Request, digest [sha256.Size]byte, entry session, identity sporenet.UserIdentity) {
	if request.Method == http.MethodGet {
		overviews, err := e.listSessions(request.Context(), digest, entry)
		if err != nil {
			writeSessionOperationError(writer, err)
			return
		}
		sessions := make([]accountSessionDTO, 0, len(overviews))
		for _, summary := range overviews {
			sessions = append(sessions, accountSessionDTO{ID: summary.ID, CreatedAt: summary.CreatedAt.UTC(),
				LastUsedAt: summary.LastUsedAt.UTC(), ExpiresAt: summary.ExpiresAt.UTC(), IsCurrent: summary.IsCurrent})
		}
		writeJSON(writer, http.StatusOK, struct {
			Sessions []accountSessionDTO `json:"sessions"`
		}{Sessions: sessions})
		return
	}
	areOthers := request.URL.Path == "/api/control/account/sessions/revoke-others"
	command := struct {
		ID              string `json:"id,omitempty"`
		CurrentPassword string `json:"current_password"`
	}{}
	err := decodeControlRequest(writer, request, &command)
	if err != nil || command.CurrentPassword == "" || len(command.CurrentPassword) > 1024 ||
		!areOthers && (command.ID == "" || len(command.ID) > 128) || areOthers && command.ID != "" {
		writeError(writer, http.StatusBadRequest, "A valid session and current password are required.")
		return
	}
	source, port, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || port == "" {
		writeError(writer, http.StatusForbidden, "Invalid request source.")
		return
	}
	if !e.allowAttempt(source) {
		writer.Header().Set("Retry-After", "60")
		writeError(writer, http.StatusTooManyRequests, "Too many credential attempts. Try again in a minute.")
		return
	}
	verified := sporenet.UserIdentity{}
	revision := entry.revision
	credentialManager, isCredentialSupported := e.config.Authenticator.(credentialAuthenticator)
	if isCredentialSupported {
		verified, revision, err = credentialManager.AuthenticateCredential(request.Context(), identity.LoginName, command.CurrentPassword)
	} else {
		verified, err = e.config.Authenticator.Authenticate(request.Context(), identity.LoginName, command.CurrentPassword)
	}
	if err != nil || verified.ID != entry.accountID {
		writeErrorCode(writer, http.StatusUnauthorized, "invalid_password", "The current password is incorrect.")
		return
	}
	if revision <= 0 || revision != entry.revision {
		writeError(writer, http.StatusUnauthorized, "Account credentials changed. Sign in again.")
		return
	}
	result, err := e.revokeSessions(request.Context(), digest, entry, command.ID, areOthers)
	if err != nil {
		writeSessionOperationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		IsCurrent    bool `json:"is_current"`
		RevokedCount int  `json:"revoked_count"`
	}{IsCurrent: result.IsCurrent, RevokedCount: result.RevokedCount})
}

func writeSessionOperationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSessionUnavailable):
		writeError(writer, http.StatusUnauthorized, "Sign in to this server again.")
	case errors.Is(err, errSessionNotFound):
		writeError(writer, http.StatusNotFound, "Panel session not found.")
	default:
		writeError(writer, http.StatusServiceUnavailable, "Panel sessions are unavailable.")
	}
}
