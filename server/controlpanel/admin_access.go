package controlpanel

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

func (e *Service) changeUserAccess(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor, targetID int64) {
	accessEditor, isSupported := e.config.Moderator.(accountaccess.AccessEditor)
	if !isSupported {
		writeError(writer, http.StatusNotFound, "Account access editing is unavailable.")
		return
	}
	command := struct {
		Status  *int64  `json:"status"`
		IPLimit *string `json:"ip_limit"`
		Reason  string  `json:"reason"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil || command.Status == nil || command.IPLimit == nil {
		writeError(writer, http.StatusBadRequest, "Status, IP limit, and an administration reason are required.")
		return
	}
	change, err := accessEditor.ChangeAccess(req.Context(), accountaccess.ChangeAccessRequest{
		Actor: actor, TargetUserID: targetID, Status: *command.Status,
		IPLimit: *command.IPLimit, Reason: command.Reason,
	})
	if err != nil {
		if errors.Is(err, accountaccess.ErrAccessProtected) {
			writeErrorCode(writer, http.StatusForbidden, "access_protected", "You cannot edit your own access or exceed your administrator status.")
			return
		}
		writeOperationError(writer, err)
		return
	}
	// The committed credential revision invalidates launch credentials and
	// admission; revoke panel sessions and retire existing gameplay as well.
	e.RevokeAccount(targetID)
	disconnectCtx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
	disconnectErr := e.config.ConnectionControl.DisconnectAccount(disconnectCtx, targetID)
	cancel()
	writeJSON(writer, http.StatusOK, struct {
		Status              int64  `json:"status"`
		IPLimit             string `json:"ip_limit"`
		IsDisconnectPending bool   `json:"is_disconnect_pending"`
	}{Status: change.After.Status, IPLimit: change.After.IPLimit, IsDisconnectPending: disconnectErr != nil})
}
