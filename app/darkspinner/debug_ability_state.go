//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/control"
)

type debugAbilityCast struct {
	Revision            uint64             `json:"revision"`
	ActorObjectID       uint32             `json:"actor_object_id"`
	CreatureIndex       uint32             `json:"creature_index"`
	AbilityID           uint32             `json:"ability_id"`
	AbilityAsset        string             `json:"ability_asset"`
	InputSyncStamp      uint32             `json:"input_sync_stamp"`
	RequestStamp        uint8              `json:"request_stamp"`
	Phase               string             `json:"phase"`
	IsAccepted          bool               `json:"is_accepted"`
	IsArrived           bool               `json:"is_arrived"`
	IsArrivalProcessed  bool               `json:"is_arrival_processed"`
	IsImpactCommitted   bool               `json:"is_impact_committed"`
	IsReleased          bool               `json:"is_released"`
	IsFailed            bool               `json:"is_failed"`
	AcceptedAt          time.Time          `json:"accepted_at"`
	ArrivedAt           time.Time          `json:"arrived_at"`
	ArrivalProcessedAt  time.Time          `json:"arrival_processed_at"`
	ImpactAt            time.Time          `json:"impact_at"`
	ReleasedAt          time.Time          `json:"released_at"`
	FailedAt            time.Time          `json:"failed_at"`
	FailureReason       string             `json:"failure_reason,omitempty"`
	TargetObjectID      uint32             `json:"target_object_id"`
	Destination         debugWorldPosition `json:"destination"`
	Damage              float32            `json:"damage"`
	IsTargetDefeated    bool               `json:"is_target_defeated"`
	ExpectedLaunchCount int                `json:"expected_launch_count"`
	LaunchCount         int                `json:"launch_count"`
	LastLaunchAt        time.Time          `json:"last_launch_at"`
	IsBurstCompleted    bool               `json:"is_burst_completed"`
	BurstCompletedAt    time.Time          `json:"burst_completed_at"`
}

type debugAbilityRejection struct {
	Revision       uint64    `json:"revision"`
	ActorObjectID  uint32    `json:"actor_object_id"`
	AbilityID      uint32    `json:"ability_id"`
	InputSyncStamp uint32    `json:"input_sync_stamp"`
	RequestStamp   uint8     `json:"request_stamp"`
	At             time.Time `json:"at"`
	Reason         string    `json:"reason"`
}

type debugAbilityState struct {
	IsArena              bool                   `json:"is_arena"`
	CapturedAt           time.Time              `json:"captured_at"`
	IsAvailable          bool                   `json:"is_available"`
	UnavailableReason    string                 `json:"unavailable_reason,omitempty"`
	IsSupported          bool                   `json:"is_supported"`
	UserID               uint64                 `json:"user_id"`
	GameID               uint32                 `json:"game_id"`
	PeerGeneration       uint64                 `json:"peer_generation"`
	TransportGeneration  uint64                 `json:"transport_generation"`
	ZoneGeneration       uint64                 `json:"zone_generation"`
	ActorObjectID        uint32                 `json:"actor_object_id"`
	CreatureIndex        uint32                 `json:"creature_index"`
	CreatureNoun         uint32                 `json:"creature_noun"`
	AbilityID            uint32                 `json:"ability_id"`
	AbilityAsset         string                 `json:"ability_asset"`
	AbilityName          string                 `json:"ability_name"`
	ManaPoint            float32                `json:"mana_point"`
	ManaCost             float32                `json:"mana_cost"`
	IsManaCostAvailable  bool                   `json:"is_mana_cost_available"`
	CooldownRemainingMS  int64                  `json:"cooldown_remaining_ms"`
	ReleaseRemainingMS   int64                  `json:"release_remaining_ms"`
	IsGateStateAvailable bool                   `json:"is_gate_state_available"`
	LastRevision         uint64                 `json:"last_revision"`
	LastDispatch         *debugAbilityDispatch  `json:"last_dispatch"`
	Phase                string                 `json:"phase"`
	Cast                 *debugAbilityCast      `json:"cast"`
	Rejection            *debugAbilityRejection `json:"rejection"`
}

type debugAbilityDispatch struct {
	Revision       uint64    `json:"revision"`
	ActorObjectID  uint32    `json:"actor_object_id"`
	AbilityID      uint32    `json:"ability_id"`
	AbilityAsset   string    `json:"ability_asset"`
	Slot           string    `json:"slot"`
	TargetObjectID uint32    `json:"target_object_id"`
	At             time.Time `json:"at"`
}

type debugAbilityObservation struct {
	ClientID   string            `json:"client_id"`
	InstanceID string            `json:"instance_id"`
	PID        uint32            `json:"pid"`
	Source     string            `json:"source"`
	State      debugAbilityState `json:"state"`
}

func debugAbilityStateDTO(state scenario.AbilityState) debugAbilityState {
	result := debugAbilityState{CapturedAt: state.CapturedAt, IsAvailable: state.IsAvailable, UnavailableReason: state.UnavailableReason, IsSupported: state.IsSupported,
		UserID: state.UserID, GameID: state.GameID, PeerGeneration: state.PeerGeneration, TransportGeneration: state.TransportGeneration, ZoneGeneration: state.ZoneGeneration,
		ActorObjectID: state.ActorObjectID, CreatureIndex: state.CreatureIndex, CreatureNoun: state.CreatureNoun, AbilityID: state.AbilityID, AbilityAsset: state.AbilityAsset, AbilityName: state.AbilityName,
		ManaPoint: state.ManaPoint, ManaCost: state.ManaCost, IsManaCostAvailable: state.IsManaCostAvailable, CooldownRemainingMS: state.CooldownRemainingMS, ReleaseRemainingMS: state.ReleaseRemainingMS,
		IsGateStateAvailable: state.IsGateStateAvailable, LastRevision: state.LastRevision, Phase: state.Phase}
	result.IsArena = state.IsArena
	if state.LastDispatch != nil {
		dispatch := state.LastDispatch
		result.LastDispatch = &debugAbilityDispatch{Revision: dispatch.Revision, ActorObjectID: dispatch.ActorObjectID,
			AbilityID: dispatch.AbilityID, AbilityAsset: dispatch.AbilityAsset, Slot: dispatch.Slot,
			TargetObjectID: dispatch.TargetObjectID, At: dispatch.At}
	}
	if state.Cast != nil {
		cast := state.Cast
		result.Cast = &debugAbilityCast{Revision: cast.Revision, ActorObjectID: cast.ActorObjectID, CreatureIndex: cast.CreatureIndex, AbilityID: cast.AbilityID, AbilityAsset: cast.AbilityAsset,
			InputSyncStamp: cast.InputSyncStamp, RequestStamp: cast.RequestStamp, Phase: cast.Phase, IsAccepted: cast.IsAccepted, IsArrived: cast.IsArrived, IsArrivalProcessed: cast.IsArrivalProcessed, IsImpactCommitted: cast.IsImpactCommitted, IsReleased: cast.IsReleased, IsFailed: cast.IsFailed,
			AcceptedAt: cast.AcceptedAt, ArrivedAt: cast.ArrivedAt, ArrivalProcessedAt: cast.ArrivalProcessedAt, ImpactAt: cast.ImpactAt, ReleasedAt: cast.ReleasedAt, FailedAt: cast.FailedAt, FailureReason: cast.FailureReason,
			TargetObjectID: cast.TargetObjectID, Destination: debugWorldPositionDTO(cast.Destination), Damage: cast.Damage, IsTargetDefeated: cast.IsTargetDefeated,
			ExpectedLaunchCount: cast.ExpectedLaunchCount, LaunchCount: cast.LaunchCount, LastLaunchAt: cast.LastLaunchAt, IsBurstCompleted: cast.IsBurstCompleted, BurstCompletedAt: cast.BurstCompletedAt}
	}
	if state.Rejection != nil {
		rejection := state.Rejection
		result.Rejection = &debugAbilityRejection{Revision: rejection.Revision, ActorObjectID: rejection.ActorObjectID, AbilityID: rejection.AbilityID, InputSyncStamp: rejection.InputSyncStamp, RequestStamp: rejection.RequestStamp, At: rejection.At, Reason: rejection.Reason}
	}
	return result
}

func observeDebugAbility(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugAbilityObservation, error) {
	var req struct {
		ClientID string `json:"client_id"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return debugAbilityObservation{}, fmt.Errorf("abilityRequest: %w", err)
	}
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return debugAbilityObservation{}, fmt.Errorf("abilityOwner: %w", err)
	}
	result, err := readDebugAbility(ctx, host, clientManager, owner, nil)
	if err != nil {
		return result, fmt.Errorf("abilityRead: %w", err)
	}
	return result, nil
}

func readDebugAbility(ctx context.Context, host *debugHost, clientManager *debugClientManager, owner debugWorldOwner, initial *debugAbilityState) (debugAbilityObservation, error) {
	result := debugAbilityObservation{ClientID: owner.client.ClientID, InstanceID: owner.client.InstanceID, PID: owner.client.ProcessID, Source: "server_supported_ability"}
	err := recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return result, fmt.Errorf("abilityIdentity: %w", err)
	}
	if owner.profile.GameID == 0 {
		result.State.UnavailableReason = "ability observation requires an admitted game"
		return result, nil
	}
	state, err := host.server.DebugAbilityState(ctx, owner.client.UserID, owner.profile.GameID)
	result.State = debugAbilityStateDTO(state)
	if err != nil {
		return result, fmt.Errorf("abilitySnapshot: %w", err)
	}
	if state.IsAvailable && (state.UserID != uint64(owner.client.UserID) || state.GameID != owner.profile.GameID || state.PeerGeneration == 0 || state.TransportGeneration == 0 || !state.IsArena && state.ZoneGeneration == 0) {
		return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "abilityBinding", Message: "ability observer omitted or changed the owned admitted binding"}
	}
	if initial != nil {
		isEpochChanged := state.PeerGeneration != 0 && state.PeerGeneration != initial.PeerGeneration || state.TransportGeneration != 0 && state.TransportGeneration != initial.TransportGeneration || state.ZoneGeneration != 0 && state.ZoneGeneration != initial.ZoneGeneration
		if isEpochChanged || state.IsAvailable && (state.IsArena != initial.IsArena || state.ActorObjectID != initial.ActorObjectID || state.CreatureIndex != initial.CreatureIndex || state.CreatureNoun != initial.CreatureNoun || state.AbilityID != initial.AbilityID) {
			return result, &control.ScenarioError{Code: control.OwnershipLost, Context: "abilityActor", Message: "owned ability actor, binding epoch or resolved ability changed"}
		}
	}
	err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return result, fmt.Errorf("abilityIdentityAfter: %w", err)
	}
	return result, nil
}
