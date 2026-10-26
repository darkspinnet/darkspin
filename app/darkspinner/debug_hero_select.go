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

type controlHeroSelection struct {
	RequestedSlot            uint32  `json:"requested_slot"`
	PreviousControlledHandle *uint32 `json:"previous_controlled_handle"`
	ObservedControlledHandle *uint32 `json:"observed_controlled_handle"`
	PreviousSlot             *uint32 `json:"previous_slot"`
	ObservedSlot             *uint32 `json:"observed_slot"`
	EligibilityCode          *uint32 `json:"eligibility_code"`
	IsQueryProcessed         bool    `json:"is_query_processed"`
}

type debugHeroSelectionRequest struct {
	ClientID  string  `json:"client_id"`
	Slot      *uint32 `json:"slot"`
	TimeoutMS uint32  `json:"timeout_ms,omitempty"`
}

type debugHeroSelectionResult struct {
	RequestedSlot           uint32          `json:"requested_slot"`
	InitialControlledHandle *uint32         `json:"initial_controlled_handle"`
	ControlledHandle        *uint32         `json:"controlled_handle"`
	IsAlreadySelected       bool            `json:"is_already_selected"`
	Action                  any             `json:"action"`
	Native                  any             `json:"native"`
	World                   debugWorldState `json:"world"`
}

// SelectHero dispatches one native input edge and observes its outcome without
// holding lifecycle locks. Errors and unknown dispatch outcomes are never replayed.
func selectDebugHero(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugHeroSelectionResult, error) {
	result := debugHeroSelectionResult{}
	if ctx == nil || host == nil || clientManager == nil {
		return result, errors.New("hero selection requires an owned host/client and context")
	}
	var req debugHeroSelectionRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("heroRequest: %w", err)
	}
	if req.ClientID == "" || req.Slot == nil || *req.Slot > 2 || req.TimeoutMS > 120000 {
		return result, errors.New("hero selection requires client_id, zero-based slot 0 through 2 and timeout_ms at most 120000")
	}
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 60000
	}
	result.RequestedSlot = *req.Slot
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	selectionMutex, err := clientManager.lockHeroSelection(waitContext, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("heroOwnerLock: %w", err)
	}
	defer selectionMutex.Unlock()
	owner, err := observeDebugWorldOwner(waitContext, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("heroOwner: %w", err)
	}
	if owner.profile.GameID == 0 {
		return result, errors.New("hero selection requires an admitted game")
	}
	worldPayload, err := json.Marshal(debugMissionRequest{ClientID: req.ClientID})
	if err != nil {
		return result, fmt.Errorf("heroWorldPayload: %w", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var initial debugWorldState
	var initialNative *controlGameplayState
	for {
		world, worldErr := readDebugHeroWorld(waitContext, host, clientManager, owner, initial, worldPayload)
		result.World = world
		if worldErr != nil {
			return result, fmt.Errorf("heroInitialWorld: %w", worldErr)
		}
		if world.IsAvailable && world.Hero != nil {
			if initial.PeerGeneration == 0 {
				initial = world
			}
			probe, native, nativeErr := observeDebugHeroNative(waitContext, clientManager, owner)
			result.Native = probe
			if nativeErr != nil && !isDebugHeroUnavailable(nativeErr) {
				return result, fmt.Errorf("heroInitialNative: %w", nativeErr)
			}
			if nativeErr == nil {
				initialNative = native
				break
			}
		}
		err = waitDebugHeroTick(waitContext, ticker)
		if err != nil {
			return result, fmt.Errorf("heroInitialWait: %w", err)
		}
	}
	result.InitialControlledHandle = initialNative.ControlledHandle
	result.ControlledHandle = initialNative.ControlledHandle
	if initial.Hero.Index == *req.Slot {
		current, currentErr := readDebugHeroWorld(waitContext, host, clientManager, owner, initial, worldPayload)
		result.World = current
		if currentErr != nil {
			return result, fmt.Errorf("heroAlreadyObserve: %w", currentErr)
		}
		if !current.IsAvailable || current.Hero == nil || current.Hero.Index != *req.Slot {
			return result, errors.New("requested hero changed while observing an already selected slot")
		}
		result.IsAlreadySelected = true
		return result, nil
	}
	for {
		before, err := readDebugHeroWorld(waitContext, host, clientManager, owner, initial, worldPayload)
		result.World = before
		if err != nil {
			return result, fmt.Errorf("heroBefore: %w", err)
		}
		if !before.IsAvailable || before.Hero == nil {
			return result, &control.ScenarioError{Code: control.NotAvailable, Context: "heroBefore", Message: "owned deployed hero became unavailable before native dispatch"}
		}
		if before.Hero.Index != initial.Hero.Index || before.Hero.ObjectID != initial.Hero.ObjectID {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "heroBeforeActor", Message: "active hero changed before native dispatch"}
		}
		result.Action, err = clientManager.SelectHero(waitContext, req.ClientID, *req.Slot)
		if err != nil {
			if isDebugHeroDispatchBlocked(result.Action, *req.Slot, initialNative) {
				err = waitDebugHeroTick(waitContext, ticker)
				if err != nil {
					return result, fmt.Errorf("heroEligibilityWait[%s]: %w", debugHeroRefusal(result.Action), err)
				}
				continue
			}
			return result, fmt.Errorf("heroNativeAction[%s]: %w", debugHeroRefusal(result.Action), err)
		}
		break
	}
	err = validateDebugHeroAction(result.Action, *req.Slot, initialNative)
	if err != nil {
		return result, fmt.Errorf("heroActionReceipt: %w", err)
	}
	for {
		world, worldErr := readDebugHeroWorld(waitContext, host, clientManager, owner, initial, worldPayload)
		result.World = world
		if worldErr != nil {
			return result, fmt.Errorf("heroOutcomeWorld: %w", worldErr)
		}
		probe, native, nativeErr := observeDebugHeroNative(waitContext, clientManager, owner)
		result.Native = probe
		if nativeErr != nil && !isDebugHeroUnavailable(nativeErr) {
			return result, fmt.Errorf("heroOutcomeNative: %w", nativeErr)
		}
		if nativeErr == nil {
			result.ControlledHandle = native.ControlledHandle
			if native.OwnerThreadID != initialNative.OwnerThreadID {
				return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "heroNativeOwner", Message: "native gameplay owner changed during hero selection"}
			}
			if world.IsAvailable && world.Hero != nil && world.Hero.Index == *req.Slot && native.FrameSequence > initialNative.FrameSequence && *native.ControlledHandle != *initialNative.ControlledHandle {
				err = recheckDebugWorldOwner(waitContext, host, clientManager, owner)
				if err != nil {
					return result, fmt.Errorf("heroFinalOwner: %w", err)
				}
				return result, nil
			}
		}
		err = waitDebugHeroTick(waitContext, ticker)
		if err != nil {
			return result, fmt.Errorf("heroOutcomeWait: %w", err)
		}
	}
}

func readDebugHeroWorld(ctx context.Context, host *debugHost, clientManager *debugClientManager, owner debugWorldOwner, initial debugWorldState, payload json.RawMessage) (debugWorldState, error) {
	err := recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return initial, fmt.Errorf("heroIdentity: %w", err)
	}
	world, err := observeDebugWorld(ctx, host, clientManager, payload)
	if err != nil {
		return world, fmt.Errorf("heroObserve: %w", err)
	}
	if world.ClientID != owner.client.ClientID || world.InstanceID != owner.client.InstanceID || world.PID != owner.client.ProcessID || world.UserID != owner.client.UserID || world.GameID != owner.profile.GameID {
		return world, &control.ScenarioError{Code: control.OwnershipLost, Context: "heroWorldOwner", Message: "hero selection process/account/game changed"}
	}
	if initial.PeerGeneration != 0 {
		isEpochChanged := world.PeerGeneration != 0 && world.PeerGeneration != initial.PeerGeneration || world.TransportGeneration != 0 && world.TransportGeneration != initial.TransportGeneration || world.ZoneGeneration != 0 && world.ZoneGeneration != initial.ZoneGeneration
		if isEpochChanged {
			return world, &control.ScenarioError{Code: control.OwnershipLost, Context: "heroWorldEpoch", Message: "hero selection peer/transport/zone changed"}
		}
	}
	if world.IsZoneTerminal {
		return world, errors.New("hero selection requires a nonterminal dungeon")
	}
	if world.Hero != nil && (world.Hero.IsDefeated || world.Hero.HitPoint <= 0) {
		return world, errors.New("hero selection requires the observed active hero alive")
	}
	isArena := owner.profile.IsArena && world.Arena != nil
	if world.IsAvailable && (world.PeerGeneration == 0 || world.TransportGeneration == 0 || !isArena && world.ZoneGeneration == 0) {
		return world, errors.New("available hero observation omitted required gameplay epochs")
	}
	return world, nil
}

func observeDebugHeroNative(ctx context.Context, clientManager *debugClientManager, owner debugWorldOwner) (any, *controlGameplayState, error) {
	probe, err := clientManager.InspectGameplay(ctx, owner.client.ClientID)
	if err != nil {
		return probe, nil, fmt.Errorf("heroNativeProbe: %w", err)
	}
	receipt, isReceipt := probe.(map[string]any)
	if !isReceipt {
		return probe, nil, errors.New("native gameplay receipt has an invalid shape")
	}
	if receipt["instance_id"] != owner.client.InstanceID || receipt["pid"] != owner.client.ProcessID {
		return probe, nil, &control.ScenarioError{Code: control.OwnershipLost, Context: "heroNativeProcess", Message: "native gameplay receipt belongs to a different process"}
	}
	native, isNative := receipt["gameplay_state"].(*controlGameplayState)
	if !isNative || native == nil {
		return probe, nil, errors.New("native gameplay receipt omitted its observation")
	}
	if receipt["is_processed"] != true || !native.IsAvailable || !native.IsWorldActive || native.ControlledHandle == nil || *native.ControlledHandle == 0 || native.OwnerThreadID == 0 || native.FrameSequence == 0 {
		return probe, native, &control.ScenarioError{Code: control.NotAvailable, Context: "heroNativeReady", Message: "fresh native gameplay owner and controlled handle are unavailable"}
	}
	return probe, native, nil
}

func validateDebugHeroAction(action any, slot uint32, initial *controlGameplayState) error {
	receipt, isReceipt := action.(map[string]any)
	if !isReceipt {
		return errors.New("native hero action returned no typed receipt; do not replay")
	}
	if receipt["is_processed"] != true || receipt["is_invocation_attempted"] != true || receipt["is_invocation_accepted"] != true {
		return errors.New("native hero action lacks explicit processing and dispatch acceptance; do not replay")
	}
	selection, isSelection := receipt["hero_selection"].(*controlHeroSelection)
	if !isSelection || selection == nil || !selection.IsQueryProcessed || selection.RequestedSlot != slot {
		return errors.New("native hero action omitted its correlated selection observation; do not replay")
	}
	if selection.PreviousControlledHandle == nil || *selection.PreviousControlledHandle != *initial.ControlledHandle {
		return &control.ScenarioError{Code: control.OwnershipLost, Context: "heroActionActor", Message: "native controlled actor changed before dispatch"}
	}
	if selection.EligibilityCode == nil || *selection.EligibilityCode != 0 {
		return errors.New("native hero selection eligibility did not permit dispatch; do not replay")
	}
	return nil
}

// Only a correlated native refusal before any input edge permits another
// admission check. Physical input and modal guards can refuse before native
// eligibility is sampled. Waiting preserves those guards, without replaying
// dispatched edges, missing receipts or unknown outcomes.
func isDebugHeroDispatchBlocked(action any, slot uint32, initial *controlGameplayState) bool {
	receipt, isReceipt := action.(map[string]any)
	if !isReceipt || receipt["status"] != "unavailable" ||
		receipt["is_invocation_attempted"] != false || receipt["is_invocation_accepted"] != false {
		return false
	}
	selection, isSelection := receipt["hero_selection"].(*controlHeroSelection)
	if !isSelection || selection == nil || selection.IsQueryProcessed || selection.RequestedSlot != slot {
		return false
	}
	if selection.PreviousSlot != nil && (*selection.PreviousSlot > 2 || *selection.PreviousSlot == slot) {
		return false
	}
	native, isNative := receipt["gameplay_state"].(*controlGameplayState)
	return isNative && native != nil && native.IsAvailable && native.IsWorldActive &&
		native.OwnerThreadID == initial.OwnerThreadID && native.ControlledHandle != nil &&
		selection.PreviousControlledHandle != nil && initial.ControlledHandle != nil &&
		*native.ControlledHandle == *initial.ControlledHandle &&
		*selection.PreviousControlledHandle == *initial.ControlledHandle
}

func debugHeroRefusal(action any) string {
	receipt, isReceipt := action.(map[string]any)
	if !isReceipt {
		return "receiptMissing"
	}
	selection, isSelection := receipt["hero_selection"].(*controlHeroSelection)
	if !isSelection || selection == nil {
		return fmt.Sprintf("status=%v,selectionMissing", receipt["status"])
	}
	eligibility := "notSampled"
	if selection.EligibilityCode != nil {
		eligibility = fmt.Sprint(*selection.EligibilityCode)
	}
	return fmt.Sprintf("status=%v,slot=%d,eligibility=%s,attempted=%v,accepted=%v,processed=%t",
		receipt["status"], selection.RequestedSlot, eligibility, receipt["is_invocation_attempted"],
		receipt["is_invocation_accepted"], selection.IsQueryProcessed)
}

func isDebugHeroUnavailable(err error) bool {
	var failure *control.ScenarioError
	return errors.As(err, &failure) && failure.Code == control.NotAvailable
}

func waitDebugHeroTick(ctx context.Context, ticker *time.Ticker) error {
	select {
	case <-ctx.Done():
		return &control.ScenarioError{Code: control.DeadlineExceeded, Context: "heroSelection", Message: "requested native/server hero selection was not observed before the deadline; native action is never replayed", Cause: ctx.Err()}
	case <-ticker.C:
		return nil
	}
}
