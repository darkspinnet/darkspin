//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
)

type controlBeamOutState struct {
	IsAvailable bool `json:"is_available"`
	IsReady     bool `json:"is_ready"`
	IsRequested bool `json:"is_requested"`
}

type debugBeamOutResult struct {
	Action             any             `json:"action"`
	Readiness          any             `json:"readiness,omitempty"`
	World              debugWorldState `json:"world"`
	IsAlreadyCommitted bool            `json:"is_already_committed"`
	IsAlreadyRequested bool            `json:"is_already_requested"`
}

func beamOutDebugMission(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugBeamOutResult, error) {
	result := debugBeamOutResult{}
	if ctx == nil {
		return result, errors.New("beam out requires a context")
	}
	var req debugMissionRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("beamRequest: %w", err)
	}
	if req.ClientID == "" || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return result, errors.New("beam out requires client_id and timeout_ms from 1 to 120000")
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	owner, err := observeDebugWorldOwner(waitContext, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("beamOwner: %w", err)
	}
	if owner.profile.GameID == 0 {
		return result, errors.New("beam out requires an admitted game")
	}
	worldPayload, err := json.Marshal(struct {
		ClientID string `json:"client_id"`
	}{ClientID: req.ClientID})
	if err != nil {
		return result, fmt.Errorf("beamWorldPayload: %w", err)
	}
	initial, err := observeDebugWorld(waitContext, host, clientManager, worldPayload)
	result.World = initial
	if err != nil {
		return result, fmt.Errorf("beamWorld: %w", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for initial.PeerGeneration == 0 || initial.TransportGeneration == 0 || initial.ZoneGeneration == 0 {
		if initial.GameID != owner.profile.GameID {
			return result, debugMissionActionOwnership("owned game changed before beam out world admission")
		}
		if initial.IsAvailable || initial.IsVictoryAvailable {
			return result, errors.New("beam out observation claims availability without complete world epochs")
		}
		select {
		case <-waitContext.Done():
			return result, debugBeamContext("beamEpochWait", waitContext.Err())
		case <-ticker.C:
		}
		err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
		if err != nil {
			return result, fmt.Errorf("beamEpochOwner: %w", err)
		}
		initial, err = observeDebugWorld(waitContext, host, clientManager, worldPayload)
		result.World = initial
		if err != nil {
			return result, fmt.Errorf("beamEpochObserve: %w", err)
		}
	}
	for {
		world, worldErr := readDebugBeamWorld(waitContext, host, clientManager, owner, initial, worldPayload)
		result.World = world
		if worldErr != nil {
			return result, fmt.Errorf("beamReadyWorld: %w", worldErr)
		}
		if world.IsVictoryAvailable && world.IsVictoryCommitted {
			result.IsAlreadyCommitted = true
			break
		}
		probe, probeErr := clientManager.InspectBeamOut(waitContext, req.ClientID)
		if probe != nil {
			result.Readiness = probe
		}
		if probeErr != nil {
			var failure *control.ScenarioError
			if !errors.As(probeErr, &failure) || failure.Code != control.NotAvailable {
				return result, fmt.Errorf("beamReadyProbe: %w", probeErr)
			}
		} else {
			receipt, isReceipt := probe.(map[string]any)
			if !isReceipt {
				return result, errors.New("beam out readiness receipt has an invalid shape")
			}
			readiness, isReadiness := receipt["beam_out_state"].(*controlBeamOutState)
			if !isReadiness || readiness == nil {
				return result, errors.New("beam out readiness receipt is missing native observations")
			}
			if readiness.IsAvailable && readiness.IsRequested {
				result.IsAlreadyRequested = true
				break
			}
			if readiness.IsAvailable && readiness.IsReady {
				before, beforeErr := readDebugBeamWorld(waitContext, host, clientManager, owner, initial, worldPayload)
				result.World = before
				if beforeErr != nil {
					return result, fmt.Errorf("beamBeforeAction: %w", beforeErr)
				}
				if before.IsVictoryAvailable && before.IsVictoryCommitted {
					result.IsAlreadyCommitted = true
					break
				}
				err = validateDebugBeamWorld(before)
				if err != nil {
					if !isDebugBeamUnavailable(err) {
						return result, fmt.Errorf("beamBeforeEligibility: %w", err)
					}
					select {
					case <-waitContext.Done():
						return result, debugBeamContext("beamReadyWait", waitContext.Err())
					case <-ticker.C:
					}
					continue
				}
				result.Action, err = clientManager.BeamOut(waitContext, req.ClientID)
				if err != nil {
					return result, fmt.Errorf("beamNativeAction: %w", err)
				}
				if result.Action == nil {
					return result, errors.New("beam out returned no native receipt; do not replay")
				}
				break
			}
		}
		err = validateDebugBeamWorld(world)
		if err != nil && !isDebugBeamUnavailable(err) {
			return result, fmt.Errorf("beamEligibility: %w", err)
		}
		select {
		case <-waitContext.Done():
			return result, debugBeamContext("beamReadyWait", waitContext.Err())
		case <-ticker.C:
		}
	}
	for {
		world, worldErr := readDebugBeamWorld(waitContext, host, clientManager, owner, initial, worldPayload)
		result.World = world
		if worldErr != nil {
			return result, fmt.Errorf("beamCommitWorld: %w", worldErr)
		}
		if world.IsVictoryAvailable && world.IsVictoryCommitted {
			return result, nil
		}
		if world.IsZoneTerminal && world.IsVictoryAvailable {
			return result, errors.New("zone retired without the owned member's committed victory")
		}
		select {
		case <-waitContext.Done():
			return result, debugBeamContext("beamCommitWait", waitContext.Err())
		case <-ticker.C:
		}
	}
}

func readDebugBeamWorld(ctx context.Context, host *debugHost, clientManager *debugClientManager, owner debugWorldOwner, initial debugWorldState, payload json.RawMessage) (debugWorldState, error) {
	err := recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return initial, fmt.Errorf("beamIdentity: %w", err)
	}
	world, err := observeDebugWorld(ctx, host, clientManager, payload)
	if err != nil {
		return world, fmt.Errorf("beamObserve: %w", err)
	}
	isEpochChanged := world.PeerGeneration != 0 && world.PeerGeneration != initial.PeerGeneration || world.TransportGeneration != 0 && world.TransportGeneration != initial.TransportGeneration || world.ZoneGeneration != 0 && world.ZoneGeneration != initial.ZoneGeneration
	if world.ClientID != initial.ClientID || world.InstanceID != initial.InstanceID || world.PID != initial.PID || world.UserID != initial.UserID || world.GameID != initial.GameID || isEpochChanged {
		return world, &control.ScenarioError{Code: control.OwnershipLost, Context: "beamWorldEpoch", Message: "owned beam out process/account/game/peer/zone changed"}
	}
	if (world.IsAvailable || world.IsVictoryAvailable) && (world.PeerGeneration == 0 || world.TransportGeneration == 0 || world.ZoneGeneration == 0) {
		return world, errors.New("beam out observation claims availability without complete world epochs")
	}
	err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return world, fmt.Errorf("beamIdentityAfter: %w", err)
	}
	return world, nil
}

func validateDebugBeamWorld(world debugWorldState) error {
	if world.Hero != nil && (world.Hero.IsDefeated || world.Hero.HitPoint <= 0) {
		return errors.New("beam out requires the current owned hero alive")
	}
	if !world.IsAvailable {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "beamWorldReady", Message: world.UnavailableReason}
	}
	if world.Boss == nil {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "beamBoss", Message: "boss completion observation is temporarily unavailable"}
	}
	if !world.Boss.IsComplete || world.Boss.LeaderObjectID == 0 {
		return errors.New("beam out requires observed completed boss with a nonzero leader identity")
	}
	if world.Hero == nil {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "beamHero", Message: "owned hero observation is temporarily unavailable"}
	}
	return nil
}

func isDebugBeamUnavailable(err error) bool {
	var failure *control.ScenarioError
	return errors.As(err, &failure) && failure.Code == control.NotAvailable
}

func debugBeamContext(contextTag string, err error) error {
	code := control.DeadlineExceeded
	if errors.Is(err, context.Canceled) {
		code = control.ErrorCode("cancelled")
	}
	return &control.ScenarioError{Code: code, Context: contextTag, Message: "beam out readiness or personal victory commitment was not observed within the requested lifetime", Cause: err}
}
