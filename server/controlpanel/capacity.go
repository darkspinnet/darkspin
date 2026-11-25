package controlpanel

import (
	"context"
	"errors"
	"net/http"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/admissionconfig"
)

type Capacity interface {
	Read(context.Context) (admissionconfig.Settings, error)
	SetAuthorized(context.Context, accountaccess.Actor, admissionconfig.Settings) error
}

type capacityDTO struct {
	WorldPlayerLimit uint32 `json:"world_player_limit"`
}

func (e *Service) capacityOperation(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if e.config.Capacity == nil {
		writeError(writer, http.StatusNotFound, "Server capacity settings are unavailable.")
		return
	}
	if req.Method == http.MethodPatch {
		command := capacityDTO{}
		err := decodeControlRequest(writer, req, &command)
		if err != nil || command.WorldPlayerLimit == 0 {
			writeError(writer, http.StatusBadRequest, "Choose a positive whole-number player limit.")
			return
		}
		err = e.config.Capacity.SetAuthorized(req.Context(), actor, admissionconfig.Settings{
			WorldPlayerLimit: command.WorldPlayerLimit,
		})
		if err != nil {
			if errors.Is(err, admissionconfig.ErrAuditIncomplete) {
				writeErrorCode(writer, http.StatusServiceUnavailable, "outcome_unknown", "The configuration was saved but its audit outcome could not be recorded. Reload before retrying.")
				return
			}
			writeOperationError(writer, err)
			return
		}
	}
	settings, err := e.config.Capacity.Read(req.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "Server capacity settings could not be read.")
		return
	}
	writeJSON(writer, http.StatusOK, capacityDTO{WorldPlayerLimit: settings.WorldPlayerLimit})
}
