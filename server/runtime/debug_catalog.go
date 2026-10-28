//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugNPCCatalogProvider interface {
	ScenarioNPCCatalog(context.Context, int64, uint32) (scenario.NPCCatalog, error)
}

func (e *Server) DebugNPCCatalog(ctx context.Context, userID int64, gameID uint32) (scenario.NPCCatalog, error) {
	if e == nil || ctx == nil || userID <= 0 || gameID == 0 {
		return scenario.NPCCatalog{}, errors.New("NPC catalog requires an owned server, context and admitted user/game")
	}
	provider, isSupported := e.bugContext.(debugNPCCatalogProvider)
	if !isSupported {
		return scenario.NPCCatalog{}, errors.New("NPC catalog provider unavailable")
	}
	catalog, err := provider.ScenarioNPCCatalog(ctx, userID, gameID)
	if err != nil {
		return catalog, fmt.Errorf("catalogRead: %w", err)
	}
	return catalog, nil
}
