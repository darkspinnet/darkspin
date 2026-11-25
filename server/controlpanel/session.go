// Package controlpanel owns the short-lived, credentialed remote panel sessions.
package controlpanel

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

const (
	sessionLifetime        = 30 * time.Minute
	maximumSessions        = 1024
	maximumAccountSessions = 4
	maximumAttemptSources  = 4096
	maximumAttempts        = 8
	attemptWindow          = time.Minute
)

// Authenticator verifies credentials without activating a game session.
type Authenticator interface {
	Authenticate(context.Context, string, string) (sporenet.UserIdentity, error)
}

type credentialAuthenticator interface {
	AuthenticateCredential(context.Context, string, string) (sporenet.UserIdentity, int64, error)
}

// IdentityReader resolves an existing durable identity; it must never create one.
type IdentityReader interface {
	Identity(context.Context, int64) (sporenet.UserIdentity, error)
}

// AccessReader supplies current, durable authorization state on every request.
type AccessReader interface {
	Read(context.Context, int64) (accountaccess.Access, error)
}

// Observer supplies real observations rather than estimated historical metrics.
type Observer interface {
	ServerSummary(context.Context) (ServerSummary, error)
}

type ServerSummary struct {
	Name               string
	Version            string
	StartedAt          time.Time
	OnlineAccountCount int
	Description        string
	Welcome            string
	Rules              string
	ContactURL         string
	HealthStatus       string
}

type Config struct {
	Host                  string
	IsDedicated           bool
	Authenticator         Authenticator
	IdentityReader        IdentityReader
	AccessReader          AccessReader
	Observer              Observer
	ActivityReader        ActivityReader
	UserActivityReader    UserActivityReader
	MetricsReviewer       MetricsReviewer
	LoginHistoryReviewer  LoginHistoryReviewer
	AccountController     AccountController
	Moderator             Moderator
	ConnectionControl     ConnectionControl
	HealthReader          HealthReader
	Information           Information
	Matchmaking           Matchmaking
	Capacity              Capacity
	MaintenanceController MaintenanceController
	UpdateReader          UpdateReader
	Diagnostic            Diagnostic
}

type session struct {
	isLocal    bool
	id         string
	accountID  int64
	revision   int64
	createdAt  time.Time
	lastUsedAt time.Time
	expiresAt  time.Time
}

type attempt struct {
	count     int
	expiresAt time.Time
}

// Service stores token digests only. Its sessions die with this server instance.
type Service struct {
	diagnosticDownloadMu sync.Mutex
	config               Config
	mu                   sync.Mutex
	sessions             map[[sha256.Size]byte]session
	attempts             map[string]attempt
}

func New(config Config) (*Service, error) {
	if config.Host == "" || config.Authenticator == nil || config.IdentityReader == nil ||
		config.AccessReader == nil || config.Observer == nil {
		return nil, errors.New("control panel dependencies unavailable")
	}
	return &Service{config: config, sessions: make(map[[sha256.Size]byte]session),
		attempts: make(map[string]attempt)}, nil
}

func (e *Service) allowAttempt(source string) bool {
	// Loopback requests still verify passwords, including dedicated servers
	// behind a local proxy. They must not bypass credential attempt limits.
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for key, entry := range e.attempts {
		if !now.Before(entry.expiresAt) {
			delete(e.attempts, key)
		}
	}
	entry, isFound := e.attempts[source]
	if !isFound && len(e.attempts) >= maximumAttemptSources {
		return false
	}
	if entry.count >= maximumAttempts {
		return false
	}
	if !isFound {
		entry.expiresAt = now.Add(attemptWindow)
	}
	entry.count++
	e.attempts[source] = entry
	return true
}

var errCapacity = errors.New("panel session capacity reached")

func (e *Service) createSession(accountID, revision int64) (string, session, error) {
	payload := make([]byte, 32)
	byteCount, err := rand.Read(payload)
	if err != nil {
		return "", session{}, fmt.Errorf("tokenRandom: %w", err)
	}
	if byteCount != len(payload) {
		return "", session{}, errors.New("incomplete panel token randomness")
	}
	token := "cps_" + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(token))
	idPayload := make([]byte, 24)
	byteCount, err = rand.Read(idPayload)
	if err != nil {
		return "", session{}, fmt.Errorf("sessionIDRandom: %w", err)
	}
	if byteCount != len(idPayload) {
		return "", session{}, errors.New("incomplete panel session ID randomness")
	}
	id := "csid_" + base64.RawURLEncoding.EncodeToString(idPayload)
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	accountSessionCount := 0
	for key, entry := range e.sessions {
		if entry.id == id {
			return "", session{}, errors.New("panel session ID collision")
		}
		if !now.Before(entry.expiresAt) {
			delete(e.sessions, key)
			continue
		}
		if entry.accountID == accountID {
			accountSessionCount++
		}
	}
	if len(e.sessions) >= maximumSessions || accountSessionCount >= maximumAccountSessions {
		return "", session{}, errCapacity
	}
	entry := session{id: id, accountID: accountID, revision: revision,
		createdAt: now, lastUsedAt: now, expiresAt: now.Add(sessionLifetime)}
	e.sessions[digest] = entry
	return token, entry, nil
}

func (e *Service) touchSession(digest [sha256.Size]byte, expected session) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, isFound := e.sessions[digest]
	if !isFound || entry.id != expected.id || !time.Now().Before(entry.expiresAt) {
		return false
	}
	entry.lastUsedAt = time.Now()
	e.sessions[digest] = entry
	return true
}

// RevokeAccount discards every panel session for the immutable account ID.
func (e *Service) RevokeAccount(accountID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for key, entry := range e.sessions {
		if entry.accountID == accountID {
			delete(e.sessions, key)
		}
	}
}

func (e *Service) revoke(digest [sha256.Size]byte) {
	e.mu.Lock()
	delete(e.sessions, digest)
	e.mu.Unlock()
}

func (e *Service) findSession(digest [sha256.Size]byte) (session, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, isFound := e.sessions[digest]
	if !isFound {
		return session{}, false
	}
	if !time.Now().Before(entry.expiresAt) {
		delete(e.sessions, digest)
		return session{}, false
	}
	return entry, true
}
