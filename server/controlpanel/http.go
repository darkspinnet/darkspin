package controlpanel

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/auth"
	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type profileDTO struct {
	LoginName   string `json:"login_name"`
	DisplayName string `json:"display_name"`
	AvatarID    uint32 `json:"avatar_id"`
}

type capabilityDTO struct {
	IsAdmin bool `json:"is_admin"`
}

type sessionDTO struct {
	AccountID    int64         `json:"account_id"`
	Token        string        `json:"token,omitempty"`
	ExpiresAt    time.Time     `json:"expires_at"`
	Profile      profileDTO    `json:"profile"`
	Capabilities capabilityDTO `json:"capabilities"`
}

type serverDTO struct {
	Name                string                `json:"name"`
	Version             string                `json:"version"`
	StartedAt           time.Time             `json:"started_at"`
	OnlineAccountCount  int                   `json:"online_account_count"`
	SupportedOperations []string              `json:"supported_operations"`
	Description         string                `json:"description"`
	Welcome             string                `json:"welcome"`
	Rules               string                `json:"rules"`
	ContactURL          string                `json:"contact_url"`
	HealthStatus        string                `json:"health_status"`
	Maintenance         *publicMaintenanceDTO `json:"maintenance,omitempty"`
}

// Handler exposes only the implemented, authenticated panel operations.
func (e *Service) Handler() http.Handler {
	return http.HandlerFunc(e.serveHTTP)
}

func (e *Service) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	request = request.WithContext(accountaccess.WithPeer(request.Context(), request.RemoteAddr))
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if !e.isRequestAllowed(request) {
		writeError(writer, http.StatusForbidden, "Panel requests must come from the launcher.")
		return
	}
	err := buildinfo.CheckCompatibility(request.Header.Get(auth.ClientVersionHeader), buildinfo.Version)
	if err != nil {
		writeError(writer, http.StatusConflict, "A launcher with the same major and minor version as this server is required.")
		return
	}
	if request.URL.RawQuery != "" {
		writeError(writer, http.StatusBadRequest, "Query parameters are not accepted.")
		return
	}
	if request.URL.Path == "/api/control/session" && request.Method == http.MethodPost {
		e.signIn(writer, request)
		return
	}
	isSessionRoute := request.URL.Path == "/api/control/session"
	isReadRoute := request.URL.Path == "/api/control/server"
	operationMethod := controlMethod(request.URL.Path)
	if !isSessionRoute && !isReadRoute && operationMethod == "" {
		writeError(writer, http.StatusNotFound, "Panel operation not found.")
		return
	}
	isAllowedMethod := isReadRoute && request.Method == http.MethodGet ||
		isSessionRoute && (request.Method == http.MethodGet || request.Method == http.MethodDelete) ||
		operationMethod != "" && (operationMethod == request.Method ||
			operationMethod == "PATCH, DELETE" && (request.Method == http.MethodPatch || request.Method == http.MethodDelete))
	if !isAllowedMethod {
		writer.Header().Set("Allow", "GET")
		if isSessionRoute {
			writer.Header().Set("Allow", "GET, POST, DELETE")
		}
		if operationMethod != "" {
			writer.Header().Set("Allow", operationMethod)
		}
		writeError(writer, http.StatusMethodNotAllowed, "Method not supported.")
		return
	}
	e.authorizedRequest(writer, request)
}

func (e *Service) signIn(writer http.ResponseWriter, request *http.Request) {
	source, port, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || port == "" {
		writeError(writer, http.StatusForbidden, "Invalid request source.")
		return
	}
	if !e.allowAttempt(source) {
		writer.Header().Set("Retry-After", "60")
		writeError(writer, http.StatusTooManyRequests, "Too many sign-in attempts. Try again in a minute.")
		return
	}
	req := struct {
		Account  string `json:"account"`
		Password string `json:"password"`
	}{}
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(parameters) > 1 {
		writeError(writer, http.StatusBadRequest, "A JSON sign-in request is required.")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid sign-in request.")
		return
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "Invalid sign-in request.")
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" || len(req.Account) > 320 || req.Password == "" || len(req.Password) > 1024 {
		writeError(writer, http.StatusBadRequest, "Account and password are required.")
		return
	}
	identity := sporenet.UserIdentity{}
	revision := int64(0)
	credentialManager, isCredentialSupported := e.config.Authenticator.(credentialAuthenticator)
	if isCredentialSupported {
		identity, revision, err = credentialManager.AuthenticateCredential(request.Context(), req.Account, req.Password)
	} else {
		identity, err = e.config.Authenticator.Authenticate(request.Context(), req.Account, req.Password)
	}
	if errors.Is(err, accountaccess.ErrAccessDenied) {
		writeError(writer, http.StatusForbidden, "This account cannot access the server.")
		return
	}
	if err != nil || identity.ID <= 0 || identity.LoginName == "" {
		writeError(writer, http.StatusUnauthorized, "Account or password is incorrect.")
		return
	}
	access, err := e.config.AccessReader.Read(request.Context(), identity.ID)
	if err != nil || access.UserID != identity.ID || access.Revision <= 0 {
		writeError(writer, http.StatusServiceUnavailable, "Account access is unavailable.")
		return
	}
	if access.IsBlocked() {
		writeError(writer, http.StatusForbidden, "This account cannot access the server.")
		return
	}
	err = accountaccess.CheckPeer(request.Context(), access)
	if err != nil {
		writeError(writer, http.StatusForbidden, "This account cannot access the server.")
		return
	}
	if isCredentialSupported && (revision <= 0 || access.Revision != revision) {
		writeError(writer, http.StatusUnauthorized, "Account credentials changed. Sign in again.")
		return
	}
	token, entry, err := e.createSession(identity.ID, access.Revision)
	if errors.Is(err, errCapacity) {
		writeError(writer, http.StatusTooManyRequests, "Panel session limit reached. Sign out another session or wait for it to expire.")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "Panel session could not be created.")
		return
	}
	writeJSON(writer, http.StatusCreated, sessionResponse(token, entry, identity, access.IsAdmin()))
}

func (e *Service) authorizedRequest(writer http.ResponseWriter, request *http.Request) {
	fields := strings.Fields(request.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") ||
		len(fields[1]) != 47 || !strings.HasPrefix(fields[1], "cps_") {
		writeError(writer, http.StatusUnauthorized, "Sign in to this server again.")
		return
	}
	digest := sha256.Sum256([]byte(fields[1]))
	entry, isFound := e.findSession(digest)
	if !isFound {
		writeError(writer, http.StatusUnauthorized, "Sign in to this server again.")
		return
	}
	access, err := e.config.AccessReader.Read(request.Context(), entry.accountID)
	if errors.Is(err, accountaccess.ErrNotFound) {
		e.revoke(digest)
		writeError(writer, http.StatusUnauthorized, "Account is unavailable. Sign in again.")
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "Account access is unavailable.")
		return
	}
	if access.IsBlocked() {
		e.RevokeAccount(entry.accountID)
		writeError(writer, http.StatusForbidden, "This account cannot access the server.")
		return
	}
	if access.UserID != entry.accountID || access.Revision <= 0 || access.Revision != entry.revision {
		e.revoke(digest)
		writeError(writer, http.StatusUnauthorized, "Account access changed. Sign in again.")
		return
	}
	err = accountaccess.CheckPeer(request.Context(), access)
	if err != nil {
		writeError(writer, http.StatusForbidden, "This account cannot access the server.")
		return
	}
	identity, err := e.config.IdentityReader.Identity(request.Context(), entry.accountID)
	if err != nil || identity.ID != entry.accountID || identity.LoginName == "" {
		e.revoke(digest)
		writeError(writer, http.StatusUnauthorized, "Account is unavailable. Sign in again.")
		return
	}
	if !e.touchSession(digest, entry) {
		writeError(writer, http.StatusUnauthorized, "Sign in to this server again.")
		return
	}
	switch request.URL.Path {
	case "/api/control/account/sessions", "/api/control/account/sessions/revoke", "/api/control/account/sessions/revoke-others":
		e.accountSessions(writer, request, digest, entry, identity)
	case "/api/control/session":
		if request.Method == http.MethodDelete {
			e.revoke(digest)
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(writer, http.StatusOK, sessionResponse("", entry, identity, access.IsAdmin()))
	case "/api/control/server":
		e.serverSummary(writer, request)
	default:
		e.controlOperation(writer, request, entry, access)
	}
}

func (e *Service) serverSummary(writer http.ResponseWriter, request *http.Request) {
	summary, err := e.config.Observer.ServerSummary(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "Server summary is unavailable.")
		return
	}
	operations := e.supportedOperations()
	var maintenanceState *publicMaintenanceDTO
	if e.config.MaintenanceController != nil {
		snapshot, snapshotErr := e.config.MaintenanceController.Snapshot(request.Context())
		if snapshotErr != nil {
			writeError(writer, http.StatusServiceUnavailable, "Maintenance status is unavailable.")
			return
		}
		state := publicMaintenanceResponse(snapshot.State)
		maintenanceState = &state
		if snapshot.IsRestartSupported {
			operations = append(operations, "admin.maintenance.restart")
		}
		if snapshot.IsUpdateSupported {
			operations = append(operations, "admin.maintenance.update")
		}
	}
	writeJSON(writer, http.StatusOK, serverDTO{
		Name: summary.Name, Version: summary.Version, StartedAt: summary.StartedAt.UTC(),
		OnlineAccountCount:  summary.OnlineAccountCount,
		SupportedOperations: operations, Maintenance: maintenanceState,
		Description: summary.Description, Welcome: summary.Welcome, Rules: summary.Rules,
		ContactURL: summary.ContactURL, HealthStatus: summary.HealthStatus,
	})
}

func sessionResponse(token string, entry session, identity sporenet.UserIdentity, isAdmin bool) sessionDTO {
	return sessionDTO{AccountID: entry.accountID, Token: token, ExpiresAt: entry.expiresAt.UTC(), Profile: profileDTO{
		LoginName: identity.LoginName, DisplayName: identity.DisplayName, AvatarID: identity.AvatarID,
	}, Capabilities: capabilityDTO{IsAdmin: isAdmin}}
}

func (e *Service) isRequestAllowed(request *http.Request) bool {
	if request.Header.Get("Origin") != "" {
		return false
	}
	address, port, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || port == "" {
		return false
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return false
	}
	if e.config.IsDedicated {
		return ip.IsGlobalUnicast() || ip.IsLoopback()
	}
	if !ip.IsLoopback() {
		return ip.IsPrivate()
	}
	expectedHost, expectedPort, expectedErr := net.SplitHostPort(e.config.Host)
	requestHost, requestPort, requestErr := net.SplitHostPort(request.Host)
	requestIP := net.ParseIP(requestHost)
	return expectedErr == nil && expectedHost != "" && requestErr == nil &&
		requestPort == expectedPort && (strings.EqualFold(requestHost, "localhost") ||
		requestIP != nil && requestIP.IsLoopback())
}

func writeError(writer http.ResponseWriter, status int, message string) {
	code := "request_failed"
	switch status {
	case http.StatusBadRequest:
		code = "invalid_request"
	case http.StatusUnauthorized:
		code = "unauthorized"
	case http.StatusForbidden:
		code = "forbidden"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusMethodNotAllowed:
		code = "method_not_allowed"
	case http.StatusConflict:
		code = "version_mismatch"
	case http.StatusTooManyRequests:
		code = "rate_limited"
	case http.StatusServiceUnavailable:
		code = "unavailable"
	}
	writeJSON(writer, status, struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}{Code: code, Error: message})
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	err := json.NewEncoder(writer).Encode(payload)
	if err != nil {
		// The response has already started; a failed/disconnected writer cannot
		// receive a replacement error. Never log a response containing tokens.
		return
	}
}
