package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

const (
	desktopCodeLifetime = 30 * time.Second
	launchTokenLifetime = 2 * time.Minute
	maximumRequestSize  = 4 * 1024
)

type desktopGrant struct {
	identity  string
	expiresAt time.Time
}

// LocalBroker substitutes loopback identity approval for OAuth in development.
type LocalBroker struct {
	issuer         *JWTIssuer
	host           string
	accountManager accountManager
	logger         *log.Logger
	now            func() time.Time
	mu             sync.Mutex
	grants         map[string]desktopGrant
}

type accountManager interface {
	RegisterDesktop(context.Context, sporenet.Registration) (*sporenet.User, error)
	Authenticate(context.Context, string, string) (sporenet.UserIdentity, error)
	DeleteProfile(context.Context, string) error
}

// NewLocalBroker creates a broker restricted to the exact loopback HTTP host.
func NewLocalBroker(issuer *JWTIssuer, host string, logger *log.Logger) (*LocalBroker, error) {
	return newBroker(issuer, host, nil, logger)
}

// NewAccountBroker creates a desktop broker that preserves password-free
// loopback authorization while requiring account credentials from LAN peers.
func NewAccountBroker(
	issuer *JWTIssuer, host string, accountManager accountManager, logger *log.Logger,
) (*LocalBroker, error) {
	if accountManager == nil {
		return nil, errors.New("create account broker: nil account manager")
	}
	return newBroker(issuer, host, accountManager, logger)
}

func newBroker(
	issuer *JWTIssuer, host string, accountManager accountManager, logger *log.Logger,
) (*LocalBroker, error) {
	if issuer == nil {
		return nil, errors.New("create local broker: nil JWT issuer")
	}
	if strings.TrimSpace(host) == "" {
		return nil, errors.New("create local broker: host is empty")
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &LocalBroker{
		issuer: issuer, host: host, accountManager: accountManager,
		logger: logger, now: time.Now, grants: make(map[string]desktopGrant),
	}, nil
}

// Handler returns the local desktop authentication protocol handler.
func (b *LocalBroker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/desktop/register", b.register)
	mux.HandleFunc("/api/desktop/start", b.start)
	mux.HandleFunc("/api/desktop/exchange", b.exchange)
	mux.HandleFunc("/api/desktop/delete", b.delete)
	return mux
}

func (b *LocalBroker) delete(writer http.ResponseWriter, request *http.Request) {
	_, isAllowed := b.requestAccess(request)
	if !isAllowed || b.accountManager == nil {
		writeAuthError(writer, http.StatusForbidden, "local network request required")
		return
	}
	if request.Method != http.MethodPost {
		writeAuthError(writer, http.StatusMethodNotAllowed, "POST required")
		return
	}
	payload := struct {
		Account  string `json:"account"`
		Password string `json:"password"`
	}{}
	err := decodeAuthRequest(request, &payload)
	if err != nil {
		writeAuthError(writer, http.StatusBadRequest, "invalid request")
		return
	}
	identity := strings.TrimSpace(payload.Account)
	if identity == "" || payload.Password == "" || len(identity) > 320 {
		writeAuthError(writer, http.StatusBadRequest, "account and password are required")
		return
	}
	profile, err := b.accountManager.Authenticate(request.Context(), identity, payload.Password)
	if err != nil || profile.LoginName == "" {
		writeAuthError(writer, http.StatusUnauthorized, "invalid account or password")
		return
	}
	err = b.accountManager.DeleteProfile(request.Context(), profile.LoginName)
	if err != nil {
		writeAuthError(writer, http.StatusConflict, err.Error())
		return
	}
	writeAuthJSON(writer, http.StatusOK, map[string]bool{"deleted": true})
}

func (b *LocalBroker) start(writer http.ResponseWriter, request *http.Request) {
	identity := ""
	result := "rejected"
	defer func() {
		b.logger.Printf(
			"auth_login_attempt remote_ip=%q account=%q result=%q",
			remoteIP(request.RemoteAddr), identity, result,
		)
	}()
	isLoopback, isAllowed := b.requestAccess(request)
	if !isAllowed {
		writeAuthError(writer, http.StatusForbidden, "local network request required")
		return
	}
	if request.Method != http.MethodPost {
		writeAuthError(writer, http.StatusMethodNotAllowed, "POST required")
		return
	}
	payload := struct {
		Account  string `json:"account"`
		Password string `json:"password"`
	}{}
	err := decodeAuthRequest(request, &payload)
	if err != nil {
		writeAuthError(writer, http.StatusBadRequest, "invalid request")
		return
	}
	identity = strings.TrimSpace(payload.Account)
	if identity == "" || len(identity) > 320 {
		writeAuthError(writer, http.StatusBadRequest, "invalid account")
		return
	}
	profile := sporenet.UserIdentity{LoginName: identity, DisplayName: identity}
	if !isLoopback {
		if payload.Password == "" {
			writeAuthError(writer, http.StatusUnauthorized, "password is required")
			return
		}
		profile, err = b.accountManager.Authenticate(request.Context(), identity, payload.Password)
		if err != nil {
			writeAuthError(writer, http.StatusUnauthorized, "invalid account or password")
			return
		}
	}
	code, err := randomDesktopCode()
	if err != nil {
		writeAuthError(writer, http.StatusInternalServerError, "code generation failed")
		return
	}
	b.mu.Lock()
	b.grants[code] = desktopGrant{identity: identity, expiresAt: b.now().Add(desktopCodeLifetime)}
	b.mu.Unlock()
	result = "approved"
	writeAuthJSON(writer, http.StatusOK, map[string]any{
		"code": code, "profile": desktopProfile(profile),
	})
}

func (b *LocalBroker) register(writer http.ResponseWriter, request *http.Request) {
	isLoopback, isAllowed := b.requestAccess(request)
	if !isAllowed {
		writeAuthError(writer, http.StatusForbidden, "local network request required")
		return
	}
	if request.Method != http.MethodPost {
		writeAuthError(writer, http.StatusMethodNotAllowed, "POST required")
		return
	}
	payload := struct {
		Account           string `json:"account"`
		DisplayName       string `json:"display_name"`
		Password          string `json:"password"`
		AvatarID          uint32 `json:"avatar_id"`
		IsTutorialSkipped bool   `json:"is_tutorial_skipped"`
	}{}
	err := decodeAuthRequest(request, &payload)
	if err != nil {
		writeAuthError(writer, http.StatusBadRequest, "invalid request")
		return
	}
	if !isLoopback && payload.Password == "" {
		writeAuthError(writer, http.StatusBadRequest, "password is required")
		return
	}
	identity := strings.TrimSpace(payload.Account)
	displayName := strings.TrimSpace(payload.DisplayName)
	if identity == "" || displayName == "" || len(identity) > 320 || len(displayName) > 320 {
		writeAuthError(writer, http.StatusBadRequest, "invalid account")
		return
	}
	user, err := b.accountManager.RegisterDesktop(request.Context(), sporenet.Registration{
		DisplayName: displayName, LoginName: identity, Password: payload.Password,
		AvatarID: payload.AvatarID, IsTutorialSkipped: payload.IsTutorialSkipped,
	})
	if err != nil {
		writeAuthError(writer, http.StatusConflict, err.Error())
		return
	}
	view := user.View()
	writeAuthJSON(writer, http.StatusCreated, map[string]any{"profile": desktopProfile(sporenet.UserIdentity{
		LoginName: view.LoginName, DisplayName: view.DisplayName,
		AvatarID: view.Account.AvatarID, Level: view.Account.Level,
		XP: view.Account.XP, ChainProgression: view.Account.ChainProgression,
	})})
}

func (b *LocalBroker) exchange(writer http.ResponseWriter, request *http.Request) {
	_, isAllowed := b.requestAccess(request)
	if !isAllowed {
		writeAuthError(writer, http.StatusForbidden, "local network request required")
		return
	}
	if request.Method != http.MethodPost {
		writeAuthError(writer, http.StatusMethodNotAllowed, "POST required")
		return
	}
	payload := struct {
		Code string `json:"code"`
	}{}
	err := decodeAuthRequest(request, &payload)
	if err != nil {
		writeAuthError(writer, http.StatusBadRequest, "invalid request")
		return
	}
	b.mu.Lock()
	grant, isFound := b.grants[payload.Code]
	delete(b.grants, payload.Code)
	b.mu.Unlock()
	if !isFound || !b.now().Before(grant.expiresAt) {
		writeAuthError(writer, http.StatusUnauthorized, "invalid or expired code")
		return
	}
	token, err := b.issuer.Issue(grant.identity, launchTokenLifetime)
	if err != nil {
		writeAuthError(writer, http.StatusInternalServerError, "token generation failed")
		return
	}
	writeAuthJSON(writer, http.StatusOK, map[string]string{"launch_token": token})
}

func (b *LocalBroker) requestAccess(request *http.Request) (bool, bool) {
	if request.Header.Get("Origin") != "" {
		return false, false
	}
	address, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false, false
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return false, false
	}
	if ip.IsLoopback() {
		_, expectedPort, expectedErr := net.SplitHostPort(b.host)
		requestHost, requestPort, requestErr := net.SplitHostPort(request.Host)
		requestIP := net.ParseIP(requestHost)
		return true, expectedErr == nil && requestErr == nil && requestPort == expectedPort &&
			(strings.EqualFold(requestHost, "localhost") || requestIP != nil && requestIP.IsLoopback())
	}
	return false, ip.IsPrivate() && b.accountManager != nil
}

func desktopProfile(identity sporenet.UserIdentity) map[string]any {
	return map[string]any{
		"login_name": identity.LoginName, "display_name": identity.DisplayName,
		"avatar_id": identity.AvatarID, "crogenitor_level": identity.Level,
		"cumulative_xp":             identity.XP,
		"highest_campaign_unlocked": identity.ChainProgression + 1,
	}
}

func remoteIP(remoteAddress string) string {
	address, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return address
	}
	return remoteAddress
}

func decodeAuthRequest(request *http.Request, target any) error {
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		return errors.New("content type is not JSON")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, request.Body, maximumRequestSize))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if err != nil {
		return fmt.Errorf("jsonDecode: %w", err)
	}
	return nil
}

func randomDesktopCode() (string, error) {
	contents := make([]byte, 32)
	_, err := rand.Read(contents)
	if err != nil {
		return "", fmt.Errorf("randomRead: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(contents), nil
}

func writeAuthError(writer http.ResponseWriter, status int, message string) {
	writeAuthJSON(writer, status, map[string]string{"error": message})
}

func writeAuthJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
