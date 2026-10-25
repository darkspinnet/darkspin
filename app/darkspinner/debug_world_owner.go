//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario/control"
)

type debugWorldOwner struct {
	client  debugClientState
	profile debugProfileState
}

func observeDebugWorldOwner(ctx context.Context, host *debugHost, clientManager *debugClientManager, clientID string) (debugWorldOwner, error) {
	owner := debugWorldOwner{}
	if ctx == nil || host == nil || clientManager == nil || clientID == "" {
		return owner, errors.New("world operation requires an owned server and client")
	}
	client, err := clientManager.State(clientID)
	if err != nil {
		return owner, fmt.Errorf("ownerClient: %w", err)
	}
	owner.client = client
	if !client.IsRunning || client.Status == "liveness_unavailable" || client.ProfileName == "" || client.UserID <= 0 {
		return owner, errors.New("world operation requires a live owned launch account")
	}
	profile, err := host.ProfileState(ctx, client.ProfileName)
	if err != nil {
		return owner, fmt.Errorf("ownerProfile: %w", err)
	}
	owner.profile = profile
	if profile.UserID != client.UserID || !profile.IsAuthenticated || profile.SessionID == "" {
		return owner, &control.ScenarioError{Code: control.OwnershipLost, Context: "worldAccount", Message: "world operation requires the owned client's accepted launch account"}
	}
	return owner, nil
}

func recheckDebugWorldOwner(ctx context.Context, host *debugHost, clientManager *debugClientManager, owner debugWorldOwner) error {
	current, err := observeDebugWorldOwner(ctx, host, clientManager, owner.client.ClientID)
	if err != nil {
		return fmt.Errorf("ownerRecheck: %w", err)
	}
	if current.client.InstanceID != owner.client.InstanceID || current.client.ProcessID != owner.client.ProcessID || current.client.ProfileName != owner.client.ProfileName || current.client.UserID != owner.client.UserID || current.profile.UserID != owner.profile.UserID || current.profile.SessionID != owner.profile.SessionID || current.profile.GameID != owner.profile.GameID {
		return &control.ScenarioError{Code: control.OwnershipLost, Context: "worldIdentity", Message: "owned process, accepted login or game changed during world operation"}
	}
	return nil
}
