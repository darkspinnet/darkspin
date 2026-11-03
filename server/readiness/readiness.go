// Package readiness registers the server identity probe.
package readiness

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/darkspinnet/darkspin/server/admission"
	"github.com/darkspinnet/darkspin/server/buildinfo"
	recaphttp "github.com/darkspinnet/darkspin/server/http"
)

type response struct {
	BuildID           string `json:"build_id"`
	ServerVersion     string `json:"server_version"`
	GameVersion       string `json:"game_version"`
	IsMaintenance     bool   `json:"is_maintenance"`
	MaintenanceReason string `json:"maintenance_reason,omitempty"`
}

// Register adds the server identity endpoint without maintenance state.
func Register(
	router *recaphttp.Router, serverVersion string, gameVersion string,
) error {
	err := RegisterWithAdmission(router, serverVersion, gameVersion, nil)
	if err != nil {
		return fmt.Errorf("identityRegister: %w", err)
	}
	return nil
}

// RegisterWithAdmission exposes admission state before launcher authentication.
// Maintenance keeps HTTP 200: the server is reachable, but not accepting players.
func RegisterWithAdmission(
	router *recaphttp.Router, serverVersion string, gameVersion string,
	source admission.StateReader,
) error {
	err := router.Add(
		`/recap/server`,
		[]string{http.MethodGet},
		func(
			writer http.ResponseWriter, _ *http.Request, _ *recaphttp.URI,
		) {
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("Cache-Control", "no-store")
			identity := response{
				BuildID:       buildinfo.ID,
				ServerVersion: serverVersion,
				GameVersion:   gameVersion,
			}
			if source != nil {
				state := source.CurrentAdmission()
				identity.IsMaintenance = state.IsBlocked
				if state.IsBlocked {
					identity.MaintenanceReason = state.Reason
				}
			}
			encodeErr := json.NewEncoder(writer).Encode(identity)
			if encodeErr != nil {
				// The response may already be written; the caller can retry the probe.
				return
			}
		},
	)
	if err != nil {
		return fmt.Errorf("probeRegister: %w", err)
	}
	return nil
}
