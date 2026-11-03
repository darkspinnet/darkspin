package controlpanel

import (
	"context"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

type adminUserDTO struct {
	ID               int64  `json:"id"`
	LoginName        string `json:"login_name"`
	DisplayName      string `json:"display_name"`
	Level            int64  `json:"level"`
	Status           int64  `json:"status"`
	IPLimit          string `json:"ip_limit"`
	IsBanned         bool   `json:"is_banned"`
	IsTimedOut       bool   `json:"is_timed_out"`
	TimeoutUntilUnix int64  `json:"timeout_until_unix"`
	IsOnline         bool   `json:"is_online"`
}

type adminEventDTO struct {
	ID             int64  `json:"id"`
	ActorType      string `json:"actor_type"`
	ActorUserID    int64  `json:"actor_user_id"`
	TargetUserID   int64  `json:"target_user_id"`
	Action         string `json:"action"`
	OccurredAtUnix int64  `json:"occurred_at_unix"`
	Reason         string `json:"reason"`
	OperationID    string `json:"operation_id"`
	Outcome        string `json:"outcome"`
}

func (e *Service) adminOperation(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	switch req.URL.Path {
	case "/api/control/admin/update", "/api/control/admin/update/check":
		e.updateOperation(writer, req, actor)
	case "/api/control/admin/maintenance", "/api/control/admin/maintenance/restart", "/api/control/admin/maintenance/update", "/api/control/admin/maintenance/mode", "/api/control/admin/maintenance/cancel":
		e.maintenanceOperation(writer, req, actor)
	case "/api/control/admin/identity":
		e.setIdentity(writer, req, actor)
	case "/api/control/admin/announcements":
		e.announce(writer, req, actor)
	case "/api/control/admin/health":
		if e.config.HealthReader == nil {
			writeError(writer, http.StatusNotFound, "Health information is unavailable.")
			return
		}
		e.health(writer, req)
	case "/api/control/admin/users/list":
		e.listUsers(writer, req, actor)
	case "/api/control/admin/history/list":
		e.listHistory(writer, req, actor)
	default:
		e.moderateUser(writer, req, actor)
	}
}

func (e *Service) listUsers(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	command := struct {
		AfterUserID int64  `json:"after_user_id"`
		Limit       int    `json:"limit"`
		Search      string `json:"search"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid user search.")
		return
	}
	result, err := e.config.Moderator.ListUsers(req.Context(), actor, accountaccess.UserListRequest{
		AfterUserID: command.AfterUserID, Limit: command.Limit, Search: command.Search,
	})
	if err != nil {
		writeOperationError(writer, err)
		return
	}
	users := make([]adminUserDTO, 0, len(result.Users))
	for _, user := range result.Users {
		users = append(users, adminUserDTO{
			ID: user.UserID, LoginName: user.LoginName, DisplayName: user.DisplayName,
			Level: user.Level, Status: user.Status, IPLimit: user.IPLimit, IsBanned: user.IsBanned,
			IsTimedOut: user.IsTimedOut, TimeoutUntilUnix: user.TimeoutUntilUnix,
			IsOnline: e.config.ConnectionControl.IsOnline(user.UserID),
		})
	}
	writeJSON(writer, http.StatusOK, struct {
		Users      []adminUserDTO `json:"users"`
		NextUserID int64          `json:"next_user_id"`
	}{Users: users, NextUserID: result.NextUserID})
}

func (e *Service) listHistory(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	command := struct {
		BeforeEventID int64  `json:"before_event_id"`
		TargetUserID  int64  `json:"target_user_id"`
		ActorUserID   int64  `json:"actor_user_id"`
		ActorType     string `json:"actor_type"`
		AfterUnix     int64  `json:"after_unix"`
		BeforeUnix    int64  `json:"before_unix"`
		Action        string `json:"action"`
		Outcome       string `json:"outcome"`
		OperationID   string `json:"operation_id"`
		Limit         int    `json:"limit"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid history search.")
		return
	}
	result, err := e.config.Moderator.ListHistory(req.Context(), actor, accountaccess.HistoryRequest{
		BeforeEventID: command.BeforeEventID, TargetUserID: command.TargetUserID, Limit: command.Limit,
		ActorUserID: command.ActorUserID, ActorType: command.ActorType,
		AfterUnix: command.AfterUnix, BeforeUnix: command.BeforeUnix,
		Action: command.Action, Outcome: command.Outcome, OperationID: command.OperationID,
	})
	if err != nil {
		writeOperationError(writer, err)
		return
	}
	events := make([]adminEventDTO, 0, len(result.Events))
	for _, event := range result.Events {
		events = append(events, adminEventDTO{
			ID: event.ID, ActorType: event.ActorType, ActorUserID: event.ActorUserID,
			TargetUserID: event.TargetUserID, Action: event.Action, OccurredAtUnix: event.OccurredAtUnix,
			Reason: event.Reason, OperationID: event.OperationID, Outcome: event.Outcome,
		})
	}
	writeJSON(writer, http.StatusOK, struct {
		Events      []adminEventDTO `json:"events"`
		NextEventID int64           `json:"next_event_id"`
	}{Events: events, NextEventID: result.NextEventID})
}

func (e *Service) moderateUser(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	targetID, action := moderationPath(req.URL.Path)
	if action == "access" {
		e.changeUserAccess(writer, req, actor, targetID)
		return
	}
	command := struct {
		Reason          string `json:"reason"`
		DurationMinutes int64  `json:"duration_minutes"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "A moderation reason is required.")
		return
	}
	if action == "kick" {
		e.kickUser(writer, req, actor, targetID, command.Reason)
		return
	}
	result, err := e.config.Moderator.Moderate(req.Context(), accountaccess.ModerationRequest{
		Actor: actor, TargetUserID: targetID, Action: action, Reason: command.Reason,
		DurationMinutes: command.DurationMinutes,
	})
	if err != nil {
		writeOperationError(writer, err)
		return
	}
	e.RevokeAccount(targetID)
	isDisconnectPending := false
	if result.After.IsBlocked() {
		isDisconnectPending = e.config.ConnectionControl.EnforceBan(req.Context(), targetID)
	}
	writeJSON(writer, http.StatusOK, struct {
		IsBanned            bool  `json:"is_banned"`
		IsTimedOut          bool  `json:"is_timed_out"`
		TimeoutUntilUnix    int64 `json:"timeout_until_unix"`
		IsDisconnectPending bool  `json:"is_disconnect_pending"`
	}{IsBanned: result.After.IsBanned, IsTimedOut: result.After.IsTimedOut(), TimeoutUntilUnix: result.After.TimeoutUntilUnix, IsDisconnectPending: isDisconnectPending})
}

func (e *Service) kickUser(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor, targetID int64, reason string) {
	operation, err := e.config.Moderator.BeginKick(req.Context(), accountaccess.KickRequest{
		Actor: actor, TargetUserID: targetID, Reason: reason,
	})
	if err != nil {
		writeOperationError(writer, err)
		return
	}
	disconnectCtx, disconnectCancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
	disconnectErr := e.config.ConnectionControl.DisconnectAccount(disconnectCtx, targetID)
	disconnectCancel()
	completionCtx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
	defer cancel()
	err = e.config.Moderator.CompleteKick(completionCtx, accountaccess.KickOutcomeRequest{
		OperationID: operation.OperationID, IsCompleted: disconnectErr == nil,
	})
	if err != nil {
		writeErrorCode(writer, http.StatusServiceUnavailable, "outcome_unknown", "The kick was accepted but its outcome could not be recorded. Check administration history.")
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		IsDisconnectPending bool `json:"is_disconnect_pending"`
	}{IsDisconnectPending: disconnectErr != nil})
}
