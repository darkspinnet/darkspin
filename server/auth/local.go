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
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

const (
	desktopCodeLifetime = 30 * time.Second
	launchTokenLifetime = 2 * time.Minute
	maximumRequestSize  = 4 * 1024
)

type desktopGrant struct {
	identity      string
	accountID     int64
	expiresAt     time.Time
	clientVersion string
	revision      int64
}

// LocalBroker substitutes loopback identity approval for OAuth in development.
type LocalBroker struct {
	issuer         *JWTIssuer
	host           string
	accountManager accountManager
	isDedicated    bool
	logger         *log.Logger
	now            func() time.Time
	mu             sync.Mutex
	grants         map[string]desktopGrant
	attempts       map[string]credentialAttempt
}

type accountManager interface {
	RegisterDesktop(context.Context, sporenet.Registration) (*sporenet.User, error)
	Authenticate(context.Context, string, string) (sporenet.UserIdentity, error)
	ResolveDesktop(context.Context, string) (sporenet.UserIdentity, error)
	DeleteProfile(context.Context, string) error
}

type sessionAdmissionAuthorizer interface {
	AuthorizeSession(context.Context, int64) error
}

type credentialAccountManager interface {
	AuthenticateCredential(context.Context, string, string) (sporenet.UserIdentity, int64, error)
	CredentialRevision(context.Context, int64) (int64, error)
	AuthorizeCredential(context.Context, int64, int64) error
}

type accountDeleter interface {
	AuthenticateCredential(context.Context, string, string) (sporenet.UserIdentity, int64, error)
	DeleteAccount(context.Context, sporenet.DeleteAccountRequest) error
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

// NewDedicatedBroker requires account credentials from every desktop client,
// including connections arriving through a local container bridge or proxy.
func NewDedicatedBroker(
	issuer *JWTIssuer, host string, accountManager accountManager, logger *log.Logger,
) (*LocalBroker, error) {
	broker, err := NewAccountBroker(issuer, host, accountManager, logger)
	if err != nil {
		return nil, fmt.Errorf("dedicatedBroker: %w", err)
	}
	broker.isDedicated = true
	return broker, nil
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
	mux.HandleFunc("/api/desktop/profile", b.profile)
	mux.HandleFunc("/api/desktop/exchange", b.exchange)
	mux.HandleFunc("/api/desktop/delete", b.delete)
	return b.compatibleRequests(mux)
}

func (b *LocalBroker) profile(writer http.ResponseWriter, request *http.Request) {
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
	err = validateDesktopLogin(identity, payload.Password, true)
	if err != nil {
		writeAuthError(writer, http.StatusBadRequest, err.Error())
		return
	}
	profile, err := b.accountManager.Authenticate(request.Context(), identity, payload.Password)
	if err != nil || profile.LoginName == "" {
		b.writeDesktopAuthenticationError(writer, err)
		return
	}
	writeAuthJSON(writer, http.StatusOK, map[string]any{"profile": desktopProfile(profile)})
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
	err = validateDesktopLogin(identity, payload.Password, true)
	if err != nil {
		writeAuthError(writer, http.StatusBadRequest, err.Error())
		return
	}
	deleter, isSupported := b.accountManager.(accountDeleter)
	if !isSupported {
		writeAuthError(writer, http.StatusServiceUnavailable, "account deletion unavailable")
		return
	}
	profile, revision, err := deleter.AuthenticateCredential(request.Context(), identity, payload.Password)
	if err != nil || profile.ID <= 0 || revision <= 0 {
		b.writeDesktopAuthenticationError(writer, err)
		return
	}
	err = deleter.DeleteAccount(request.Context(), sporenet.DeleteAccountRequest{
		UserID: profile.ID, Revision: revision, CurrentPassword: payload.Password,
	})
	if err != nil {
		if errors.Is(err, sporenet.ErrUserActive) {
			writeAuthError(writer, http.StatusConflict, "Close the game before changing this account.")
			return
		}
		writeAuthError(writer, http.StatusConflict, "Account deletion could not be completed. Sign in again.")
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
	err = validateDesktopLogin(identity, payload.Password, !isLoopback)
	if err != nil {
		writeAuthError(writer, http.StatusBadRequest, err.Error())
		return
	}
	profile := sporenet.UserIdentity{LoginName: identity, DisplayName: identity}
	revision := int64(0)
	credentialManager, isCredentialSupported := b.accountManager.(credentialAccountManager)
	if !isLoopback {
		if isCredentialSupported {
			profile, revision, err = credentialManager.AuthenticateCredential(request.Context(), identity, payload.Password)
		} else {
			profile, err = b.accountManager.Authenticate(request.Context(), identity, payload.Password)
		}
		if err != nil {
			b.writeDesktopAuthenticationError(writer, err)
			return
		}
	} else if b.accountManager != nil {
		profile, err = b.accountManager.ResolveDesktop(request.Context(), identity)
		if err != nil {
			b.writeDesktopAuthenticationError(writer, err)
			return
		}
	}
	if isCredentialSupported && revision == 0 {
		revision, err = credentialManager.CredentialRevision(request.Context(), profile.ID)
		if err != nil {
			b.writeDesktopAuthenticationError(writer, err)
			return
		}
	}
	code, err := randomDesktopCode()
	if err != nil {
		writeAuthError(writer, http.StatusInternalServerError, "code generation failed")
		return
	}
	b.mu.Lock()
	for pendingCode, grant := range b.grants {
		if !b.now().Before(grant.expiresAt) {
			delete(b.grants, pendingCode)
		}
	}
	if len(b.grants) >= 4096 {
		b.mu.Unlock()
		writeAuthError(writer, http.StatusTooManyRequests, "too many pending launch grants")
		return
	}
	b.grants[code] = desktopGrant{
		identity: profile.LoginName, accountID: profile.ID, expiresAt: b.now().Add(desktopCodeLifetime),
		clientVersion: strings.TrimSpace(request.Header.Get(ClientVersionHeader)),
		revision:      revision,
	}
	b.mu.Unlock()
	result = "approved"
	writeAuthJSON(writer, http.StatusOK, map[string]any{
		"code": code, "profile": desktopProfile(profile),
	})
}

func (b *LocalBroker) register(writer http.ResponseWriter, request *http.Request) {
	isLoopback, isAllowed := b.requestAccess(request)
	if !isAllowed || b.accountManager == nil {
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
	identity := strings.TrimSpace(payload.Account)
	displayName := strings.TrimSpace(payload.DisplayName)
	err = validateDesktopLogin(identity, payload.Password, !isLoopback)
	if err != nil {
		writeAuthError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if displayName == "" {
		writeAuthError(writer, http.StatusBadRequest, "Display name is required.")
		return
	}
	if len(displayName) > 320 {
		writeAuthError(writer, http.StatusBadRequest, "Display name is too long (maximum 320 UTF-8 bytes).")
		return
	}
	user, err := b.accountManager.RegisterDesktop(request.Context(), sporenet.Registration{
		DisplayName: displayName, LoginName: identity, Password: payload.Password,
		AvatarID: payload.AvatarID, IsTutorialSkipped: payload.IsTutorialSkipped,
	})
	if err != nil {
		writeDesktopRegistrationError(writer, err)
		return
	}
	view := user.View()
	writeAuthJSON(writer, http.StatusCreated, map[string]any{"profile": desktopProfile(sporenet.UserIdentity{
		LoginName: view.LoginName, DisplayName: view.DisplayName,
		CreateDT: view.CreateDT,
		AvatarID: view.Account.AvatarID, Level: view.Account.Level,
		XP: view.Account.XP, ChainProgression: view.Account.ChainProgression,
	})})
}

func (b *LocalBroker) exchange(writer http.ResponseWriter, request *http.Request) {
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
	if !isLoopback || grant.clientVersion != "" {
		err = buildinfo.CheckCompatibility(grant.clientVersion, buildinfo.Version)
		if err != nil {
			writeAuthError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	credentialManager, isCredentialSupported := b.accountManager.(credentialAccountManager)
	if isCredentialSupported {
		err = credentialManager.AuthorizeCredential(request.Context(), grant.accountID, grant.revision)
		if err != nil {
			writeAuthError(writer, http.StatusUnauthorized, "Account credentials changed. Sign in again.")
			return
		}
	}
	authorizer, isSupported := b.accountManager.(sessionAdmissionAuthorizer)
	if isSupported {
		err = authorizer.AuthorizeSession(request.Context(), grant.accountID)
		if err != nil {
			writeAuthError(writer, http.StatusForbidden, "Account access is unavailable or denied.")
			return
		}
	}
	token := ""
	if isCredentialSupported {
		token, err = b.issuer.IssueForCredential(grant.identity, grant.accountID, grant.revision, launchTokenLifetime, grant.clientVersion)
	} else if b.accountManager != nil {
		token, err = b.issuer.IssueForAccount(grant.identity, grant.accountID, launchTokenLifetime, grant.clientVersion)
	} else {
		token, err = b.issuer.IssueForVersion(grant.identity, launchTokenLifetime, grant.clientVersion)
	}
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
	if b.isDedicated {
		return false, b.accountManager != nil && (ip.IsGlobalUnicast() || ip.IsLoopback())
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
	profile := map[string]any{
		"login_name": identity.LoginName, "display_name": identity.DisplayName,
		"avatar_id": identity.AvatarID, "crogenitor_level": identity.Level,
		"cumulative_xp":             identity.XP,
		"highest_campaign_unlocked": identity.ChainProgression + 1,
	}
	if !identity.CreateDT.IsZero() {
		profile["create_dt"] = identity.CreateDT.UTC().Format(time.RFC3339Nano)
	}
	return profile
}

func remoteIP(remoteAddress string) string {
	address, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return address
	}
	return remoteAddress
}

func decodeAuthRequest(request *http.Request, target any) error {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(parameters) > 1 {
		return errors.New("content type is not JSON")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, request.Body, maximumRequestSize))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(target)
	if err != nil {
		return fmt.Errorf("jsonDecode: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON request")
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
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
