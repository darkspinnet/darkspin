//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugWorldPathProvider interface {
	ScenarioWorldPath(context.Context, scenario.WorldPathRequest) (scenario.WorldPath, error)
}

func (e *Server) DebugWorldPath(ctx context.Context, req scenario.WorldPathRequest) (scenario.WorldPath, error) {
	if e == nil || ctx == nil {
		return scenario.WorldPath{}, errors.New("world navigation service unavailable")
	}
	provider, isSupported := e.bugContext.(debugWorldPathProvider)
	if !isSupported {
		return scenario.WorldPath{}, errors.New("world navigation provider unavailable")
	}
	result, err := provider.ScenarioWorldPath(ctx, req)
	if err != nil {
		return result, fmt.Errorf("worldPath: %w", err)
	}
	return result, nil
}
