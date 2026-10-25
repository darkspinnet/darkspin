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

type debugAbilityCastRequest struct {
	ClientID  string             `json:"client_id"`
	Point     *controlWorldPoint `json:"point"`
	TimeoutMS uint32             `json:"timeout_ms,omitempty"`
}

type debugAbilityCastResult struct {
	Before                        debugAbilityObservation `json:"before"`
	Observation                   debugAbilityObservation `json:"observation"`
	Action                        any                     `json:"action"`
	Native                        any                     `json:"native"`
	IsCompleted                   bool                    `json:"is_completed"`
	IsNativeServerStampCorrelated bool                    `json:"is_native_server_stamp_correlated"`
}

// The supported slice correlates one next server revision under pinned actor
// epochs and the native resolved ability. Native/server request-stamp equality
// is unavailable, and is never inferred from mana loss or local result codes.
func castDebugAbility(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugAbilityCastResult, error) {
	result := debugAbilityCastResult{}
	if ctx == nil || host == nil || clientManager == nil {
		return result, errors.New("ability cast requires owned host/client and context")
	}
	var req debugAbilityCastRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("castPayload: %w", err)
	}
	if req.ClientID == "" || req.Point == nil || !isDebugWorldPointFinite(*req.Point) || req.TimeoutMS > 120000 {
		return result, errors.New("ability cast requires client_id, a finite point and timeout_ms at most 120000")
	}
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 60000
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	castMutex, err := clientManager.lockAbilityCast(waitContext, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("castOwnerLock: %w", err)
	}
	defer castMutex.Unlock()
	owner, err := observeDebugWorldOwner(waitContext, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("castOwner: %w", err)
	}
	initial, err := readDebugAbility(waitContext, host, clientManager, owner, nil)
	result.Before = initial
	result.Observation = initial
	if err != nil {
		return result, fmt.Errorf("castInitial: %w", err)
	}
	err = validateDebugAbilityReady(initial.State)
	if err != nil {
		return result, fmt.Errorf("castSupportedGate: %w", err)
	}
	if initial.State.LastRevision == ^uint64(0) {
		return result, errors.New("ability revision space is exhausted")
	}
	worldPayload, err := json.Marshal(struct {
		ClientID string `json:"client_id"`
	}{ClientID: req.ClientID})
	if err != nil {
		return result, fmt.Errorf("castWorldPayload: %w", err)
	}
	world, err := observeDebugWorld(waitContext, host, clientManager, worldPayload)
	if err != nil {
		return result, fmt.Errorf("castWorld: %w", err)
	}
	if !world.IsAvailable || world.Hero == nil || world.Hero.IsDefeated || world.Hero.HitPoint <= 0 || world.Hero.ObjectID != initial.State.ActorObjectID || world.Hero.Index != initial.State.CreatureIndex || world.PeerGeneration != initial.State.PeerGeneration || world.TransportGeneration != initial.State.TransportGeneration || world.ZoneGeneration != initial.State.ZoneGeneration {
		return result, &control.ScenarioError{Code: control.NotAvailable, Context: "castActor", Message: "supported ability actor is not the current live owned dungeon hero"}
	}
	probe, native, err := observeDebugHeroNative(waitContext, clientManager, owner)
	result.Native = probe
	if err != nil {
		return result, fmt.Errorf("castNativeActor: %w", err)
	}
	before, err := readDebugAbility(waitContext, host, clientManager, owner, &initial.State)
	result.Before = before
	result.Observation = before
	if err != nil {
		return result, fmt.Errorf("castBefore: %w", err)
	}
	if before.State.LastRevision != initial.State.LastRevision || debugAbilityRejectionRevision(before.State) != debugAbilityRejectionRevision(initial.State) {
		return result, errors.New("ability cast or rejection changed before native dispatch; no action attempted")
	}
	err = validateDebugAbilityReady(before.State)
	if err != nil {
		return result, fmt.Errorf("castBeforeGate: %w", err)
	}
	result.Action, err = clientManager.CastActive(waitContext, req.ClientID, *req.Point, initial.State.AbilityID)
	if err != nil {
		return result, fmt.Errorf("castNativeAction: %w", err)
	}
	err = validateDebugActiveCast(result.Action, initial.State.AbilityID, native)
	if err != nil {
		return result, fmt.Errorf("castNativeReceipt: %w", err)
	}
	nextRevision := initial.State.LastRevision + 1
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		observed, observeErr := readDebugAbility(waitContext, host, clientManager, owner, &initial.State)
		result.Observation = observed
		if observeErr != nil {
			return result, fmt.Errorf("castOutcome: %w", observeErr)
		}
		probe, currentNative, nativeErr := observeDebugHeroNative(waitContext, clientManager, owner)
		result.Native = probe
		if nativeErr != nil && !isDebugHeroUnavailable(nativeErr) {
			return result, fmt.Errorf("castOutcomeNative: %w", nativeErr)
		}
		if nativeErr == nil && (*currentNative.ControlledHandle != *native.ControlledHandle || currentNative.OwnerThreadID != native.OwnerThreadID) {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "castNativeActor", Message: "native controlled actor or gameplay owner changed during cast observation"}
		}
		state := observed.State
		if state.IsAvailable {
			if debugAbilityRejectionRevision(state) > debugAbilityRejectionRevision(initial.State) {
				return result, &control.ScenarioError{Code: control.ErrorCode("server_refused"), Context: "castRejection", Message: state.Rejection.Reason}
			}
			if state.LastRevision > nextRevision {
				return result, &control.ScenarioError{Code: control.Conflict, Context: "castRevision", Message: "multiple server casts crossed the observation window; correlation is ambiguous"}
			}
			if state.LastRevision < initial.State.LastRevision {
				return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "castRevision", Message: "server cast revision moved backwards"}
			}
			if state.LastRevision == nextRevision {
				cast := state.Cast
				if cast == nil || cast.Revision != nextRevision || cast.ActorObjectID != initial.State.ActorObjectID || cast.CreatureIndex != initial.State.CreatureIndex || cast.AbilityID != initial.State.AbilityID {
					return result, errors.New("next server cast did not match the supported actor and resolved ability")
				}
				if cast.IsFailed {
					return result, &control.ScenarioError{Code: control.NotAvailable, Context: "castFailed", Message: cast.FailureReason}
				}
				if isDebugAbilityCastComplete(cast) && nativeErr == nil && currentNative.FrameSequence > native.FrameSequence {
					err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
					if err != nil {
						return result, fmt.Errorf("castFinalOwner: %w", err)
					}
					result.IsCompleted = true
					return result, nil
				}
			}
		}
		select {
		case <-waitContext.Done():
			return result, &control.ScenarioError{Code: control.DeadlineExceeded, Context: "castTerminal", Message: "supported server cast completion and release were not observed before the deadline; native action is not replayed", Cause: waitContext.Err()}
		case <-ticker.C:
		}
	}
}

func isDebugAbilityCastComplete(cast *debugAbilityCast) bool {
	if cast == nil || !cast.IsAccepted || !cast.IsReleased || cast.IsFailed {
		return false
	}
	switch cast.AbilityAsset {
	case "LightningRogueActive":
		return cast.IsArrived && cast.IsArrivalProcessed
	case "MissileTempestActive":
		return cast.IsBurstCompleted && cast.ExpectedLaunchCount > 0 && cast.LaunchCount == cast.ExpectedLaunchCount
	default:
		return false
	}
}

func validateDebugAbilityReady(state debugAbilityState) error {
	if !state.IsAvailable {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "abilityState", Message: state.UnavailableReason}
	}
	if !state.IsSupported || state.AbilityID == 0 || state.AbilityAsset != "LightningRogueActive" && state.AbilityAsset != "MissileTempestActive" {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "abilitySupported", Message: "independent cast verification supports Ride the Lightning and Rocket Barrage"}
	}
	if !state.IsManaCostAvailable || !state.IsGateStateAvailable {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "abilityMetadata", Message: "supported ability mana/cooldown/release metadata is unavailable; no native action attempted"}
	}
	if state.ManaPoint < state.ManaCost || state.CooldownRemainingMS > 0 || state.ReleaseRemainingMS > 0 {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "abilityGate", Message: "supported ability lacks mana or its cooldown/release gate is active; no native action attempted"}
	}
	if state.Cast != nil && !state.Cast.IsFailed && !isDebugAbilityCastComplete(state.Cast) {
		return &control.ScenarioError{Code: control.NotAvailable, Context: "abilityPending", Message: "a supported server cast is already in progress; no native action attempted"}
	}
	return nil
}

func debugAbilityRejectionRevision(state debugAbilityState) uint64 {
	if state.Rejection == nil {
		return 0
	}
	return state.Rejection.Revision
}

func validateDebugActiveCast(action any, abilityID uint32, initial *controlGameplayState) error {
	receipt, isReceipt := action.(map[string]any)
	if !isReceipt {
		return errors.New("active native action returned no typed receipt; do not replay")
	}
	active, isActive := receipt["active_cast"].(*controlActiveCast)
	if !isActive || active == nil {
		return errors.New("active native action omitted its cast receipt; do not replay")
	}
	if active.NativeResultCode != nil && *active.NativeResultCode < 0 {
		return &control.ScenarioError{Code: control.ErrorCode("native_refused"), Context: "activeNative", Message: fmt.Sprintf("native active ability refused with code %d; do not replay", *active.NativeResultCode)}
	}
	if receipt["is_processed"] != true || receipt["is_invocation_attempted"] != true || receipt["is_invocation_accepted"] != true || !active.IsQueryProcessed || !active.IsNativeRequestAttempted || active.ActionIndex != 2 || active.NativeResultCode == nil || *active.NativeResultCode < 1 || *active.NativeResultCode > 3 {
		return errors.New("native active ability did not report complete local admission; do not replay")
	}
	if active.AbilityID == nil || *active.AbilityID != abilityID {
		return errors.New("native resolved ability differs from the independently supported server ability")
	}
	native, isNative := receipt["gameplay_state"].(*controlGameplayState)
	if !isNative || native == nil || native.ControlledHandle == nil || *native.ControlledHandle != *initial.ControlledHandle || native.OwnerThreadID != initial.OwnerThreadID {
		return &control.ScenarioError{Code: control.OwnershipLost, Context: "activeNativeActor", Message: "native cast receipt omitted or changed the controlled actor"}
	}
	return nil
}
