//go:build scenario

package tcp

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

// ScenarioObservation forwards to the actual Blaze server served by this
// multiplexer, including deployments where no standalone Blaze port exists.
func (e *SharedServer) ScenarioObservation(
	ctx context.Context, userID int64, kind scenario.StepKind,
) (scenario.Observation, error) {
	if e == nil || e.blaze == nil {
		return scenario.Observation{
			Outcome: scenario.Inconclusive,
			Detail:  "shared TCP Blaze observation provider is unavailable",
		}, nil
	}
	observation, err := e.blaze.ScenarioObservation(ctx, userID, kind)
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("sharedObservation: %w", err)
	}
	return observation, nil
}
