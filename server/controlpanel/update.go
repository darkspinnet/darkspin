package controlpanel

import (
	"context"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

type UpdateSnapshot struct {
	InstalledVersion  string
	InstalledBuildID  string
	BootstrapVersion  string
	BootstrapBuildID  string
	Channel           string
	CheckStatus       string
	CheckedAt         *time.Time
	CandidateVersion  string
	CandidateBuildID  string
	FailureCode       string
	IsCheckSupported  bool
	IsCheckInProgress bool
}

type UpdateReader interface {
	Snapshot(context.Context) (UpdateSnapshot, error)
	Check(context.Context, accountaccess.Actor) (UpdateSnapshot, error)
	IsCheckSupported() bool
}

type updateDTO struct {
	InstalledVersion  string     `json:"installed_version"`
	InstalledBuildID  string     `json:"installed_build_id"`
	BootstrapVersion  string     `json:"bootstrap_version"`
	BootstrapBuildID  string     `json:"bootstrap_build_id"`
	Channel           string     `json:"channel"`
	CheckStatus       string     `json:"check_status"`
	CheckedAt         *time.Time `json:"checked_at"`
	CandidateVersion  string     `json:"candidate_version"`
	CandidateBuildID  string     `json:"candidate_build_id"`
	FailureCode       string     `json:"failure_code"`
	IsCheckSupported  bool       `json:"is_check_supported"`
	IsCheckInProgress bool       `json:"is_check_in_progress"`
}

func updateResponse(snapshot UpdateSnapshot) updateDTO {
	return updateDTO{
		InstalledVersion: snapshot.InstalledVersion, InstalledBuildID: snapshot.InstalledBuildID,
		BootstrapVersion: snapshot.BootstrapVersion, BootstrapBuildID: snapshot.BootstrapBuildID,
		Channel: snapshot.Channel, CheckStatus: snapshot.CheckStatus, CheckedAt: snapshot.CheckedAt,
		CandidateVersion: snapshot.CandidateVersion, CandidateBuildID: snapshot.CandidateBuildID,
		FailureCode: snapshot.FailureCode, IsCheckSupported: snapshot.IsCheckSupported,
		IsCheckInProgress: snapshot.IsCheckInProgress,
	}
}

func (e *Service) updateOperation(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if e.config.UpdateReader == nil {
		writeErrorCode(writer, http.StatusNotFound, "update_unsupported", "Update information is unavailable.")
		return
	}
	if req.Method == http.MethodGet {
		snapshot, err := e.config.UpdateReader.Snapshot(req.Context())
		if err != nil {
			writeErrorCode(writer, http.StatusServiceUnavailable, "update_unavailable", "Update information is unavailable.")
			return
		}
		writeJSON(writer, http.StatusOK, updateResponse(snapshot))
		return
	}
	if !e.config.UpdateReader.IsCheckSupported() {
		writeErrorCode(writer, http.StatusNotFound, "update_check_unsupported", "Update checks are unavailable.")
		return
	}
	command := struct{}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "An empty JSON update check command is required.")
		return
	}
	snapshot, err := e.config.UpdateReader.Check(req.Context(), actor)
	if err != nil {
		writeOperationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, updateResponse(snapshot))
}
