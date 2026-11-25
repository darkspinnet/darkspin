package controlpanel

import (
	"context"
	"errors"
	"net/http"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/matchmaking"
)

type Matchmaking interface {
	Read(context.Context) (matchmaking.Settings, error)
	SetAuthorized(context.Context, accountaccess.Actor, matchmaking.Settings) error
}

type matchmakingDTO struct {
	CampaignMatchmakingBotSize         uint32 `json:"campaign_matchmaking_bot_size"`
	IsCampaignBotSquadMirroringEnabled bool   `json:"is_campaign_bot_squad_mirroring_enabled"`
}

func matchmakingResponse(settings matchmaking.Settings) matchmakingDTO {
	return matchmakingDTO{CampaignMatchmakingBotSize: settings.CampaignMatchmakingBotSize,
		IsCampaignBotSquadMirroringEnabled: settings.IsCampaignBotSquadMirroringEnabled}
}

func (e *Service) matchmakingOperation(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if !e.config.IsDedicated || e.config.Matchmaking == nil {
		writeError(writer, http.StatusNotFound, "Matchmaking settings are unavailable.")
		return
	}
	if req.Method == http.MethodPatch {
		command := matchmakingDTO{}
		err := decodeControlRequest(writer, req, &command)
		if err != nil || command.CampaignMatchmakingBotSize < 1 || command.CampaignMatchmakingBotSize > 3 {
			writeError(writer, http.StatusBadRequest, "Choose between one and three matchmaking bots.")
			return
		}
		err = e.config.Matchmaking.SetAuthorized(req.Context(), actor, matchmaking.Settings{
			CampaignMatchmakingBotSize:         command.CampaignMatchmakingBotSize,
			IsCampaignBotSquadMirroringEnabled: command.IsCampaignBotSquadMirroringEnabled,
		})
		if err != nil {
			if errors.Is(err, matchmaking.ErrAuditIncomplete) {
				writeErrorCode(writer, http.StatusServiceUnavailable, "outcome_unknown", "The configuration was saved but its audit outcome could not be recorded. Reload before retrying.")
				return
			}
			writeOperationError(writer, err)
			return
		}
	}
	settings, err := e.config.Matchmaking.Read(req.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "Matchmaking settings could not be read.")
		return
	}
	writeJSON(writer, http.StatusOK, matchmakingResponse(settings))
}
