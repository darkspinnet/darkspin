//go:build scenario && !windows

package main

import (
	"context"
	"errors"
)

func (e *debugClientManager) EditorHierarchy(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("native editor hierarchy requires Windows")
}

func (e *debugClientManager) OpenCreatureEditor(ctx context.Context, clientID string, heroNoun uint32, isImport bool) (any, error) {
	return nil, errors.New("Creature Editor requires Windows")
}

func (e *debugClientManager) InspectBeamOut(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned beam out readiness requires Windows")
}
func (e *debugClientManager) BeamOut(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned beam out requires Windows")
}

func (e *debugClientManager) InspectWindow(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned window inspection requires Windows")
}

func (e *debugClientManager) InspectMap(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned mission selection inspection requires Windows")
}

func (e *debugClientManager) CancelEditor(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned Editor cancellation requires Windows")
}

func (e *debugClientManager) StartMission(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned mission actions require Windows")
}

func (e *debugClientManager) OpenCampaign(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned campaign selection requires Windows")
}

func (e *debugClientManager) PreviousMission(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned mission selection requires Windows")
}

func (e *debugClientManager) NextMission(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned mission selection requires Windows")
}

func (e *debugClientManager) ContinueMission(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned mission actions require Windows")
}

func (e *debugClientManager) InspectDeployment(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned deployment inspection requires Windows")
}

func (e *debugClientManager) ReadyWindow(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned window responsiveness requires Windows")
}

func (e *debugClientManager) SetScene(ctx context.Context, clientID, scene string) (any, error) {
	return nil, errors.New("owned scene requests require Windows")
}

func (e *debugClientManager) ReadyScene(ctx context.Context, clientID, scene string) (any, error) {
	return nil, errors.New("owned scene readiness requires Windows")
}

func (e *debugClientManager) ClickWindow(ctx context.Context, req debugWindowClickRequest) (any, error) {
	return nil, errors.New("owned window input requires Windows")
}

func (e *debugClientManager) PressWindow(ctx context.Context, req debugWindowPressRequest) (any, error) {
	return nil, errors.New("owned window input requires Windows")
}

func (e *debugClientManager) TypeWindow(ctx context.Context, req debugWindowTextRequest) (any, error) {
	return nil, errors.New("owned window text requires Windows")
}

func (e *debugClientManager) ConfigureCinematic(ctx context.Context, req debugCinematicRequest) (any, error) {
	return nil, errors.New("owned cinematic configuration requires Windows")
}
