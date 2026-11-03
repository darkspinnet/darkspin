package controlpanel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type AccountController interface {
	RenameAccount(context.Context, sporenet.RenameAccountRequest) (sporenet.ProfileSummary, error)
	ChangeAccountPassword(context.Context, sporenet.ChangeAccountPasswordRequest) error
	DeleteAccount(context.Context, sporenet.DeleteAccountRequest) error
}

type Moderator interface {
	ListUsers(context.Context, accountaccess.Actor, accountaccess.UserListRequest) (accountaccess.UserList, error)
	Moderate(context.Context, accountaccess.ModerationRequest) (accountaccess.ModerationResult, error)
	ListHistory(context.Context, accountaccess.Actor, accountaccess.HistoryRequest) (accountaccess.History, error)
	BeginKick(context.Context, accountaccess.KickRequest) (accountaccess.KickOperation, error)
	CompleteKick(context.Context, accountaccess.KickOutcomeRequest) error
}

// ConnectionControl disconnects gameplay through the runtime's existing owners.
// Ban failures remain enforced at admission and are retried by that owner.
type ConnectionControl interface {
	IsOnline(int64) bool
	DisconnectAccount(context.Context, int64) error
	EnforceBan(context.Context, int64) bool
}

func controlMethod(path string) string {
	switch path {
	case "/api/control/admin/diagnostics/list", "/api/control/admin/diagnostics/download", "/api/control/admin/diagnostics/delete":
		return http.MethodPost
	case "/api/control/server/activity", "/api/control/account/activity", "/api/control/admin/users/activity", "/api/control/admin/users/login-history":
		return http.MethodPost
	case "/api/control/admin/update":
		return http.MethodGet
	case "/api/control/admin/update/check":
		return http.MethodPost
	case "/api/control/account/sessions":
		return http.MethodGet
	case "/api/control/account/sessions/revoke", "/api/control/account/sessions/revoke-others":
		return http.MethodPost
	case "/api/control/admin/maintenance":
		return http.MethodGet
	case "/api/control/admin/maintenance/restart", "/api/control/admin/maintenance/update", "/api/control/admin/maintenance/mode", "/api/control/admin/maintenance/cancel":
		return http.MethodPost
	case "/api/control/server/identity", "/api/control/server/announcements", "/api/control/admin/health":
		return http.MethodGet
	case "/api/control/admin/identity":
		return http.MethodPatch
	case "/api/control/admin/announcements":
		return http.MethodPost
	case "/api/control/account":
		return "PATCH, DELETE"
	case "/api/control/account/password", "/api/control/admin/users/list", "/api/control/admin/history/list":
		return http.MethodPost
	}
	targetID, action := moderationPath(path)
	if targetID > 0 && (action == "ban" || action == "unban" || action == "kick" || action == "timeout" || action == "access") {
		return http.MethodPost
	}
	return ""
}

func (e *Service) supportedOperations() []string {
	operations := []string{"server.read", "account.sessions.read", "account.sessions.revoke"}
	if e.config.IsDedicated && e.config.LoginHistoryReviewer != nil {
		operations = append(operations, "admin.users.loginHistory")
	}
	if e.config.ActivityReader != nil {
		operations = append(operations, "server.activity.read", "server.activity.level.read", "server.activity.dna.read", "server.activity.progression.read")
	}
	if e.config.UserActivityReader != nil {
		operations = append(operations, "account.activity.read")
		if e.config.MetricsReviewer != nil {
			operations = append(operations, "admin.users.activity")
		}
	}
	if e.config.AccountController != nil {
		operations = append(operations, "account.rename", "account.password", "account.delete")
	}
	if e.config.Moderator != nil && e.config.ConnectionControl != nil {
		operations = append(operations, "admin.users.list", "admin.history.list", "admin.users.ban", "admin.users.unban", "admin.users.kick", "admin.users.timeout")
		accessEditor, isAccessSupported := e.config.Moderator.(accountaccess.AccessEditor)
		if isAccessSupported && accessEditor != nil {
			operations = append(operations, "admin.users.access")
		}
	}
	if e.config.Information != nil {
		operations = append(operations, "server.identity.read", "server.announcements.read", "admin.identity.write", "admin.announcements.write")
	}
	if e.config.HealthReader != nil {
		operations = append(operations, "admin.health.read")
	}
	if e.config.Diagnostic != nil {
		operations = append(operations, "admin.diagnostics.list", "admin.diagnostics.download", "admin.diagnostics.delete")
	}
	if e.config.MaintenanceController != nil {
		operations = append(operations, "admin.maintenance.read", "admin.maintenance.write")
	}
	if e.config.UpdateReader != nil {
		operations = append(operations, "admin.update.read")
		if e.config.UpdateReader.IsCheckSupported() {
			operations = append(operations, "admin.update.check")
		}
	}
	return operations
}

func moderationPath(path string) (int64, string) {
	parts := strings.Split(strings.TrimPrefix(path, "/api/control/admin/users/"), "/")
	if !strings.HasPrefix(path, "/api/control/admin/users/") || len(parts) != 2 {
		return 0, ""
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, ""
	}
	return id, parts[1]
}

func decodeControlRequest(writer http.ResponseWriter, req *http.Request, destination any) error {
	return decodeBoundedControlRequest(writer, req, destination, 4096)
}

func decodeBoundedControlRequest(writer http.ResponseWriter, req *http.Request, destination any, maximumSize int64) error {
	mediaType, parameters, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(parameters) > 1 {
		return errors.New("JSON required")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, req.Body, maximumSize))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(destination)
	if err != nil {
		return errors.New("invalid JSON command")
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON command")
	}
	return nil
}

func (e *Service) controlOperation(writer http.ResponseWriter, req *http.Request, entry session, access accountaccess.Access) {
	// Apply the administrator boundary before dispatching any admin route.
	if strings.HasPrefix(req.URL.Path, "/api/control/admin/") && !access.IsAdmin() {
		writeError(writer, http.StatusForbidden, "Administrator access is required.")
		return
	}
	switch req.URL.Path {
	case "/api/control/admin/diagnostics/list", "/api/control/admin/diagnostics/download", "/api/control/admin/diagnostics/delete":
		e.diagnosticOperation(writer, req, accountaccess.Actor{UserID: entry.accountID, Revision: entry.revision})
		return
	case "/api/control/admin/users/login-history":
		e.reviewLoginHistory(writer, req, accountaccess.Actor{UserID: entry.accountID, Revision: entry.revision})
		return
	case "/api/control/account/activity":
		e.accountActivity(writer, req, entry)
		return
	case "/api/control/admin/users/activity":
		e.reviewUserActivity(writer, req, accountaccess.Actor{UserID: entry.accountID, Revision: entry.revision})
		return
	case "/api/control/server/activity":
		e.serverActivity(writer, req)
		return
	case "/api/control/server/identity":
		e.serverIdentity(writer, req)
		return
	case "/api/control/server/announcements":
		e.announcements(writer, req)
		return
	}
	if strings.HasPrefix(req.URL.Path, "/api/control/admin/") {
		if e.config.Moderator == nil || e.config.ConnectionControl == nil {
			writeError(writer, http.StatusNotFound, "Administration is unavailable.")
			return
		}
		e.adminOperation(writer, req, accountaccess.Actor{UserID: entry.accountID, Revision: entry.revision})
		return
	}
	if e.config.AccountController == nil {
		writeError(writer, http.StatusNotFound, "Account changes are unavailable.")
		return
	}
	e.accountOperation(writer, req, entry)
}

func (e *Service) accountOperation(writer http.ResponseWriter, req *http.Request, entry session) {
	source, port, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil || port == "" {
		writeError(writer, http.StatusForbidden, "Invalid request source.")
		return
	}
	if !e.allowAttempt(source) {
		writer.Header().Set("Retry-After", "60")
		writeError(writer, http.StatusTooManyRequests, "Too many credential attempts. Try again in a minute.")
		return
	}
	command := struct {
		CurrentPassword string `json:"current_password"`
		DisplayName     string `json:"display_name,omitempty"`
		NewPassword     string `json:"new_password,omitempty"`
	}{}
	err = decodeControlRequest(writer, req, &command)
	if err != nil || command.CurrentPassword == "" {
		writeError(writer, http.StatusBadRequest, "Current password is required.")
		return
	}
	if req.URL.Path == "/api/control/account/password" {
		err = e.config.AccountController.ChangeAccountPassword(req.Context(), sporenet.ChangeAccountPasswordRequest{
			UserID: entry.accountID, Revision: entry.revision, CurrentPassword: command.CurrentPassword,
			NewPassword: command.NewPassword,
		})
		if err != nil {
			writeOperationError(writer, err)
			return
		}
		e.RevokeAccount(entry.accountID)
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Method == http.MethodDelete {
		err = e.config.AccountController.DeleteAccount(req.Context(), sporenet.DeleteAccountRequest{
			UserID: entry.accountID, Revision: entry.revision, CurrentPassword: command.CurrentPassword,
		})
		if err != nil {
			writeOperationError(writer, err)
			return
		}
		e.RevokeAccount(entry.accountID)
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	summary, err := e.config.AccountController.RenameAccount(req.Context(), sporenet.RenameAccountRequest{
		UserID: entry.accountID, Revision: entry.revision, CurrentPassword: command.CurrentPassword,
		DisplayName: command.DisplayName,
	})
	if err != nil {
		writeOperationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		DisplayName string `json:"display_name"`
	}{DisplayName: summary.Identity.DisplayName})
}

func writeOperationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sporenet.ErrUserActive):
		writeErrorCode(writer, http.StatusConflict, "target_online", "Close the game before changing this account.")
	case errors.Is(err, sporenet.ErrUserExists):
		writeErrorCode(writer, http.StatusConflict, "name_in_use", "That display name is already in use.")
	case errors.Is(err, sporenet.ErrPasswordIncorrect):
		writeErrorCode(writer, http.StatusUnauthorized, "invalid_password", "The current password is incorrect.")
	case errors.Is(err, sporenet.ErrUsernameRequired):
		writeErrorCode(writer, http.StatusBadRequest, "username_required", "Display name is required.")
	case errors.Is(err, sporenet.ErrUsernamePattern):
		writeErrorCode(writer, http.StatusBadRequest, "username_pattern", "Display names can only contain letters and numbers.")
	case errors.Is(err, sporenet.ErrUsernameTooLong):
		writeErrorCode(writer, http.StatusBadRequest, "username_too_long", "Display names cannot exceed 20 characters.")
	case errors.Is(err, sporenet.ErrPasswordRequired):
		writeErrorCode(writer, http.StatusBadRequest, "password_required", "Password is required.")
	case errors.Is(err, sporenet.ErrPasswordTooLong):
		writeErrorCode(writer, http.StatusBadRequest, "password_too_long", "Passwords cannot exceed 72 UTF-8 bytes.")
	case errors.Is(err, sporenet.ErrUserNotFound), errors.Is(err, accountaccess.ErrNotFound):
		writeErrorCode(writer, http.StatusNotFound, "not_found", "The account is no longer available.")
	case errors.Is(err, accountaccess.ErrAccessDenied):
		writeErrorCode(writer, http.StatusForbidden, "forbidden", "This operation is not permitted.")
	case errors.Is(err, accountaccess.ErrConflict):
		writeErrorCode(writer, http.StatusConflict, "conflict", "The account changed. Refresh before trying again.")
	case errors.Is(err, accountaccess.ErrInvalidRequest), errors.Is(err, sporenet.ErrInvalidUser):
		writeErrorCode(writer, http.StatusBadRequest, "invalid_request", "Check the supplied account details.")
	default:
		writeError(writer, http.StatusServiceUnavailable, "The operation could not be completed.")
	}
}

func writeErrorCode(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}{Code: code, Error: message})
}
