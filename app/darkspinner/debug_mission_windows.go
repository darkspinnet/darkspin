//go:build scenario && windows

package main

import (
	"context"
	"fmt"
)

func (e *debugClientManager) InspectBeamOut(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.beam_out_state", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("beamInspect: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) BeamOut(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.beam_out", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("beamOut: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) OpenCampaign(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.campaign", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("campaignOpen: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) PreviousMission(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.previous", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("missionPrevious: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) NextMission(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.next", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("missionNext: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) StartMission(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.start", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("missionStart: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) ContinueMission(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.continue", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("missionContinue: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) InspectDeployment(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.deployment_state", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("deploymentInspect: %w", err)
	}
	return result, nil
}
