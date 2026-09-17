// Package readiness registers the server identity probe.
package readiness

import (
	"encoding/json"
	"net/http"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	recaphttp "github.com/darkspinnet/darkspin/server/http"
)

type response struct {
	BuildID       string `json:"build_id"`
	ServerVersion string `json:"server_version"`
	GameVersion   string `json:"game_version"`
}

// Register adds the loopback readiness endpoint to router.
func Register(
	router *recaphttp.Router, serverVersion string, gameVersion string,
) error {
	return router.Add(
		`/recap/server`,
		[]string{http.MethodGet},
		func(
			writer http.ResponseWriter, _ *http.Request, _ *recaphttp.URI,
		) {
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(response{
				BuildID:       buildinfo.ID,
				ServerVersion: serverVersion,
				GameVersion:   gameVersion,
			})
		},
	)
}
