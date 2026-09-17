package sim

import (
	"errors"
	"fmt"
	"reflect"
)

const SemanticTraceVersion = 4

type SemanticTraceRole struct {
	Role         string  `json:"role"`
	AllocationID uint32  `json:"allocation_id"`
	AssetName    string  `json:"asset_name,omitempty"`
	PositionX    float32 `json:"position_x,omitempty"`
	PositionY    float32 `json:"position_y,omitempty"`
	PositionZ    float32 `json:"position_z,omitempty"`
	FacingX      float32 `json:"facing_x,omitempty"`
	FacingY      float32 `json:"facing_y,omitempty"`
	FacingZ      float32 `json:"facing_z,omitempty"`
}

type SemanticTraceCollision struct {
	QueryID           string  `json:"query_id"`
	ShapeKind         string  `json:"shape_kind"`
	ActorRole         string  `json:"actor_role,omitempty"`
	TargetRole        string  `json:"target_role,omitempty"`
	StartX            float32 `json:"start_x,omitempty"`
	StartY            float32 `json:"start_y,omitempty"`
	StartZ            float32 `json:"start_z,omitempty"`
	EndX              float32 `json:"end_x,omitempty"`
	EndY              float32 `json:"end_y,omitempty"`
	EndZ              float32 `json:"end_z,omitempty"`
	Radius            float32 `json:"radius,omitempty"`
	HitX              float32 `json:"hit_x,omitempty"`
	HitY              float32 `json:"hit_y,omitempty"`
	HitZ              float32 `json:"hit_z,omitempty"`
	HitAtMicroseconds int64   `json:"hit_at_microseconds,omitempty"`
	IsHit             bool    `json:"is_hit"`
}

type SemanticTraceTargetChange struct {
	AtMicroseconds int64  `json:"at_microseconds"`
	ActorRole      string `json:"actor_role"`
	TargetRole     string `json:"target_role"`
	IsValid        bool   `json:"is_valid"`
}

type SemanticTracePhaseTransition struct {
	AtMicroseconds int64  `json:"at_microseconds"`
	From           string `json:"from"`
	To             string `json:"to"`
	Reason         string `json:"reason,omitempty"`
}

// SemanticTraceInputs records deterministic non-event inputs needed to replay
// or compare a simulation trace.
type SemanticTraceInputs struct {
	ClockRevision     string                         `json:"clock_revision,omitempty"`
	RandomSeed        uint64                         `json:"random_seed"`
	IsRandomSeedKnown bool                           `json:"is_random_seed_known"`
	RandomDraws       []uint64                       `json:"random_draws,omitempty"`
	CollisionRevision string                         `json:"collision_revision"`
	Roles             []SemanticTraceRole            `json:"roles,omitempty"`
	Collisions        []SemanticTraceCollision       `json:"collisions,omitempty"`
	TargetChanges     []SemanticTraceTargetChange    `json:"target_changes,omitempty"`
	PhaseTransitions  []SemanticTracePhaseTransition `json:"phase_transitions,omitempty"`
}

// SemanticTrace is the versioned, protocol-independent fixture format.
type SemanticTrace struct {
	Version int                  `json:"version"`
	Inputs  SemanticTraceInputs  `json:"inputs"`
	Events  []SemanticTraceEvent `json:"events"`
}

// SemanticTraceEvent is one exact event on the session-relative clock.
type SemanticTraceEvent struct {
	Sequence       uint64                  `json:"sequence"`
	EventEpoch     uint64                  `json:"event_epoch,omitempty"`
	EventSequence  uint64                  `json:"event_sequence,omitempty"`
	AtMicroseconds int64                   `json:"at_microseconds"`
	Phase          string                  `json:"phase"`
	Domain         string                  `json:"domain"`
	Intent         SemanticTraceIntent     `json:"intent"`
	Provenance     SemanticTraceProvenance `json:"provenance"`
	Scope          SemanticTraceScope      `json:"scope"`
}

// SemanticTraceIntent is a closed JSON DTO. Fields are interpreted according
// to Kind and remain packet/storage independent.
type SemanticTraceIntent struct {
	Kind                    string  `json:"kind"`
	Role                    string  `json:"role,omitempty"`
	TargetRole              string  `json:"target_role,omitempty"`
	InitiatorRole           string  `json:"initiator_role,omitempty"`
	AssetName               string  `json:"asset_name,omitempty"`
	ActionKind              string  `json:"action_kind,omitempty"`
	ConditionKind           string  `json:"condition_kind,omitempty"`
	Identifier              uint32  `json:"identifier,omitempty"`
	Count                   uint32  `json:"count,omitempty"`
	Amount                  int64   `json:"amount,omitempty"`
	DurationMicroseconds    int64   `json:"duration_microseconds,omitempty"`
	Radius                  float32 `json:"radius,omitempty"`
	Speed                   float32 `json:"speed,omitempty"`
	Acceleration            float32 `json:"acceleration,omitempty"`
	Distance                float32 `json:"distance,omitempty"`
	RangeIncrease           float32 `json:"range_increase,omitempty"`
	Scalar                  float32 `json:"scalar,omitempty"`
	IntegerChange           int32   `json:"integer_change,omitempty"`
	PositionX               float32 `json:"position_x,omitempty"`
	PositionY               float32 `json:"position_y,omitempty"`
	PositionZ               float32 `json:"position_z,omitempty"`
	DirectionX              float32 `json:"direction_x,omitempty"`
	DirectionY              float32 `json:"direction_y,omitempty"`
	DirectionZ              float32 `json:"direction_z,omitempty"`
	AngularVelocityX        float32 `json:"angular_velocity_x,omitempty"`
	AngularVelocityY        float32 `json:"angular_velocity_y,omitempty"`
	AngularVelocityZ        float32 `json:"angular_velocity_z,omitempty"`
	FacingX                 float32 `json:"facing_x,omitempty"`
	FacingY                 float32 `json:"facing_y,omitempty"`
	FacingZ                 float32 `json:"facing_z,omitempty"`
	Slot                    uint8   `json:"slot,omitempty"`
	PlayerIndex             uint8   `json:"player_index,omitempty"`
	TokenIndex              uint8   `json:"token_index,omitempty"`
	Flags                   [2]bool `json:"flags,omitempty"`
	IsEnabled               bool    `json:"is_enabled,omitempty"`
	IsKilling               bool    `json:"is_killing,omitempty"`
	IsCritical              bool    `json:"is_critical,omitempty"`
	IsValid                 bool    `json:"is_valid,omitempty"`
	IsExpected              bool    `json:"is_expected,omitempty"`
	IsHoming                bool    `json:"is_homing,omitempty"`
	IsPiercing              bool    `json:"is_piercing,omitempty"`
	IsOrientationRecomputed bool    `json:"is_orientation_recomputed,omitempty"`
	IsDirect                bool    `json:"is_direct,omitempty"`
	IsTurn                  bool    `json:"is_turn,omitempty"`
	IsLoot                  bool    `json:"is_loot,omitempty"`
	IsCrystal               bool    `json:"is_crystal,omitempty"`
	SpawnEffectID           uint32  `json:"spawn_effect_id,omitempty"`
	SpawnAbilityID          uint32  `json:"spawn_ability_id,omitempty"`
	BurrowModifierID        uint32  `json:"burrow_modifier_id,omitempty"`
	DamageMinimum           float32 `json:"damage_minimum,omitempty"`
	DamageMaximum           float32 `json:"damage_maximum,omitempty"`
	DamageCoefficient       float32 `json:"damage_coefficient,omitempty"`
	HealingMinimum          float32 `json:"healing_minimum,omitempty"`
	HealingMaximum          float32 `json:"healing_maximum,omitempty"`
	HealingCoefficient      float32 `json:"healing_coefficient,omitempty"`
	RootModifierID          uint32  `json:"root_modifier_id,omitempty"`
	RootChance              float32 `json:"root_chance,omitempty"`
}

// SemanticTraceProvenance identifies recovered Lua/native evidence.
type SemanticTraceProvenance struct {
	LuaChunkID        int64   `json:"lua_chunk_id,omitempty"`
	BytecodeSHA256    string  `json:"bytecode_sha256,omitempty"`
	FunctionName      string  `json:"function_name,omitempty"`
	InstructionOffset *uint32 `json:"instruction_offset,omitempty"`
	NativeAddress     *uint32 `json:"native_address,omitempty"`
	Confidence        string  `json:"confidence"`
}

// SemanticTraceScope records the exact cancellation generation attached to an
// event. IsKnown is false only for externally constructed legacy events.
type SemanticTraceScope struct {
	IsKnown           bool   `json:"is_known"`
	SessionGeneration uint64 `json:"session_generation,omitempty"`
	PhaseGeneration   uint64 `json:"phase_generation,omitempty"`
	Role              string `json:"role,omitempty"`
	RoleGeneration    uint64 `json:"role_generation,omitempty"`
}

// BuildSemanticTrace converts typed simulator events into the stable fixture
// representation.
func BuildSemanticTrace(events []Event, inputs SemanticTraceInputs) (SemanticTrace, error) {
	err := validateSemanticTraceInputs(inputs)
	if err != nil {
		return SemanticTrace{}, fmt.Errorf("inputsValidate: %w", err)
	}
	trace := SemanticTrace{
		Version: SemanticTraceVersion,
		Inputs:  cloneSemanticTraceInputs(inputs),
		Events:  make([]SemanticTraceEvent, 0, len(events)),
	}
	normalizer := newScopeNormalizer()
	for index, event := range events {
		traceEvent, err := buildSemanticTraceEvent(event, normalizer)
		if err != nil {
			return SemanticTrace{}, fmt.Errorf("eventBuild[%d]: %w", index, err)
		}
		trace.Events = append(trace.Events, traceEvent)
	}
	return trace, nil
}

func cloneSemanticTraceInputs(inputs SemanticTraceInputs) SemanticTraceInputs {
	cloned := inputs
	cloned.RandomDraws = append([]uint64(nil), inputs.RandomDraws...)
	cloned.Roles = append([]SemanticTraceRole(nil), inputs.Roles...)
	cloned.Collisions = append([]SemanticTraceCollision(nil), inputs.Collisions...)
	cloned.TargetChanges = append([]SemanticTraceTargetChange(nil), inputs.TargetChanges...)
	cloned.PhaseTransitions = append([]SemanticTracePhaseTransition(nil), inputs.PhaseTransitions...)
	return cloned
}

func validateSemanticTraceInputs(inputs SemanticTraceInputs) error {
	roleSet := make(map[string]struct{}, len(inputs.Roles))
	allocationSet := make(map[uint32]struct{}, len(inputs.Roles))
	for index, role := range inputs.Roles {
		if role.Role == "" {
			return fmt.Errorf("emptyRole[%d]", index)
		}
		if role.AllocationID == 0 {
			return fmt.Errorf("zeroAllocation[%d]", index)
		}
		if _, isFound := roleSet[role.Role]; isFound {
			return fmt.Errorf("duplicateRole[%d]: %s", index, role.Role)
		}
		if _, isFound := allocationSet[role.AllocationID]; isFound {
			return fmt.Errorf("duplicateAllocation[%d]: %d", index, role.AllocationID)
		}
		roleSet[role.Role] = struct{}{}
		allocationSet[role.AllocationID] = struct{}{}
	}
	querySet := make(map[string]struct{}, len(inputs.Collisions))
	for index, collision := range inputs.Collisions {
		if collision.QueryID == "" {
			return fmt.Errorf("emptyCollision[%d]", index)
		}
		if _, isFound := querySet[collision.QueryID]; isFound {
			return fmt.Errorf("duplicateCollision[%d]: %s", index, collision.QueryID)
		}
		querySet[collision.QueryID] = struct{}{}
	}
	previousTargetTime := int64(-1)
	for index, change := range inputs.TargetChanges {
		if change.AtMicroseconds < 0 || change.AtMicroseconds < previousTargetTime {
			return fmt.Errorf("targetTime[%d]: %d", index, change.AtMicroseconds)
		}
		if change.ActorRole == "" || change.TargetRole == "" {
			return fmt.Errorf("targetRole[%d]", index)
		}
		previousTargetTime = change.AtMicroseconds
	}
	previousPhaseTime := int64(-1)
	for index, transition := range inputs.PhaseTransitions {
		if transition.AtMicroseconds < 0 || transition.AtMicroseconds < previousPhaseTime {
			return fmt.Errorf("phaseTime[%d]: %d", index, transition.AtMicroseconds)
		}
		if transition.To == "" {
			return fmt.Errorf("phaseTarget[%d]", index)
		}
		previousPhaseTime = transition.AtMicroseconds
	}
	return nil
}

// CompareSemanticTrace reports the first protocol-independent parity mismatch.
func CompareSemanticTrace(expected, actual SemanticTrace) error {
	if expected.Version != actual.Version {
		return fmt.Errorf("versionMismatch: got %d, want %d", actual.Version, expected.Version)
	}
	err := ValidateSemanticTrace(expected)
	if err != nil {
		return fmt.Errorf("expectedValidate: %w", err)
	}
	err = ValidateSemanticTrace(actual)
	if err != nil {
		return fmt.Errorf("actualValidate: %w", err)
	}
	if !reflect.DeepEqual(expected.Inputs, actual.Inputs) {
		return fmt.Errorf("inputMismatch: got %#v, want %#v", actual.Inputs, expected.Inputs)
	}
	if len(expected.Events) != len(actual.Events) {
		return fmt.Errorf("eventCount: got %d, want %d", len(actual.Events), len(expected.Events))
	}
	for index := range expected.Events {
		if reflect.DeepEqual(expected.Events[index], actual.Events[index]) {
			continue
		}
		return fmt.Errorf("eventMismatch[%d]: got %#v, want %#v",
			index, actual.Events[index], expected.Events[index])
	}
	return nil
}

// ValidateSemanticTrace rejects fixtures that cannot be replayed or compared
// deterministically, including JSON-decoded fixtures not built in this process.
func ValidateSemanticTrace(trace SemanticTrace) error {
	if trace.Version != SemanticTraceVersion {
		return fmt.Errorf("version: got %d, want %d", trace.Version, SemanticTraceVersion)
	}
	err := validateSemanticTraceInputs(trace.Inputs)
	if err != nil {
		return fmt.Errorf("inputs: %w", err)
	}
	previousTime := int64(-1)
	previousSequence := uint64(0)
	for index, event := range trace.Events {
		if event.AtMicroseconds < 0 || event.AtMicroseconds < previousTime {
			return fmt.Errorf("eventTime[%d]: %d", index, event.AtMicroseconds)
		}
		if event.Intent.Kind == "" {
			return fmt.Errorf("eventIntent[%d]", index)
		}
		if event.Domain != "control" && event.Domain != "authority" && event.Domain != "presentation" {
			return fmt.Errorf("eventDomain[%d]: %s", index, event.Domain)
		}
		if event.Sequence != 0 && event.Sequence <= previousSequence {
			return fmt.Errorf("eventSequence[%d]: %d", index, event.Sequence)
		}
		if (event.EventEpoch == 0) != (event.EventSequence == 0) {
			return fmt.Errorf("eventKey[%d]: %d/%d", index, event.EventEpoch, event.EventSequence)
		}
		if event.Scope.IsKnown && event.Scope.SessionGeneration == 0 {
			return fmt.Errorf("eventScope[%d]", index)
		}
		previousTime = event.AtMicroseconds
		if event.Sequence != 0 {
			previousSequence = event.Sequence
		}
	}
	return nil
}

func buildSemanticTraceEvent(event Event, normalizer *scopeNormalizer) (SemanticTraceEvent, error) {
	if event.Intent == nil {
		return SemanticTraceEvent{}, errors.New("nil intent")
	}
	intent, err := buildSemanticTraceIntent(event.Intent)
	if err != nil {
		return SemanticTraceEvent{}, fmt.Errorf("intentBuild: %w", err)
	}
	return SemanticTraceEvent{
		Sequence:       event.Sequence,
		EventEpoch:     normalizer.normalizeEventEpoch(event.ID.Epoch),
		EventSequence:  event.ID.Sequence,
		AtMicroseconds: event.At.Microseconds(),
		Phase:          string(event.Phase),
		Domain:         domainName(DomainOf(event.Intent)),
		Intent:         intent,
		Provenance:     buildSemanticTraceProvenance(event.Provenance),
		Scope:          normalizer.normalize(event.Scope),
	}, nil
}

func buildSemanticTraceIntent(intent Intent) (SemanticTraceIntent, error) {
	switch typedIntent := intent.(type) {
	case WaitIntent:
		return SemanticTraceIntent{Kind: "wait", DurationMicroseconds: typedIntent.Duration.Microseconds()}, nil
	case ConditionalWaitIntent:
		return SemanticTraceIntent{
			Kind: "conditional_wait", ConditionKind: typedIntent.ConditionKind,
			Role: string(typedIntent.Role), TargetRole: string(typedIntent.TargetRole),
			DurationMicroseconds: typedIntent.Timeout.Microseconds(), IsExpected: typedIntent.IsExpected,
		}, nil
	case SpawnIntent:
		return SemanticTraceIntent{Kind: "spawn", Role: string(typedIntent.Role), AssetName: typedIntent.NounName}, nil
	case CompanionSpawnIntent:
		return SemanticTraceIntent{
			Kind: "companion_spawn", Role: string(typedIntent.Role),
			InitiatorRole: string(typedIntent.OwnerRole), AssetName: typedIntent.NounName,
			Radius: typedIntent.SpawnRadius, Distance: typedIntent.OwnerDistance,
			SpawnEffectID: typedIntent.SpawnEffectID, SpawnAbilityID: typedIntent.SpawnAbilityID,
			BurrowModifierID: typedIntent.BurrowModifierID,
		}, nil
	case DespawnIntent:
		return SemanticTraceIntent{Kind: "despawn", Role: string(typedIntent.Role)}, nil
	case VisibilityIntent:
		return SemanticTraceIntent{Kind: "visibility", Role: string(typedIntent.Role), IsEnabled: typedIntent.IsVisible}, nil
	case AnimationIntent:
		return SemanticTraceIntent{Kind: "animation", Role: string(typedIntent.Role), AssetName: typedIntent.AnimationName}, nil
	case AnimationResetIntent:
		return SemanticTraceIntent{Kind: "animation_reset", Role: string(typedIntent.Role)}, nil
	case GraphicsStateIntent:
		return SemanticTraceIntent{
			Kind: "graphics_state", Role: string(typedIntent.Role), Identifier: typedIntent.State,
		}, nil
	case PhysicsStateIntent:
		collisionKind := typedIntent.CollisionKind
		if collisionKind == "" {
			collisionKind = CollisionStatePhysics
		}
		return SemanticTraceIntent{
			Kind: "physics_state", Role: string(typedIntent.Role),
			ActionKind: string(collisionKind), IsEnabled: typedIntent.IsCollisionEnabled,
		}, nil
	case InteractableUseIntent:
		return SemanticTraceIntent{
			Kind: "interactable_use", Role: string(typedIntent.Role), Count: typedIntent.UseCount,
		}, nil
	case LootDropIntent:
		return SemanticTraceIntent{
			Kind: "loot_drop", Role: string(typedIntent.SourceRole),
			TargetRole: string(typedIntent.PlayerRole), IsLoot: typedIntent.IsLoot,
			IsCrystal: typedIntent.IsCrystal,
		}, nil
	case EffectIntent:
		return SemanticTraceIntent{
			Kind: "effect", Role: string(typedIntent.Role), AssetName: typedIntent.EffectName,
			InitiatorRole: string(typedIntent.ActorRole), Slot: typedIntent.Slot,
			IsEnabled: !typedIntent.IsStopped, FacingX: typedIntent.Facing.X,
			FacingY: typedIntent.Facing.Y, FacingZ: typedIntent.Facing.Z,
			IsCritical: typedIntent.IsCritical,
		}, nil
	case PositionedEffectIntent:
		return SemanticTraceIntent{
			Kind: "positioned_effect", AssetName: typedIntent.EffectName,
			PositionX: typedIntent.Position.X, PositionY: typedIntent.Position.Y,
			PositionZ: typedIntent.Position.Z, FacingX: typedIntent.Facing.X,
			FacingY: typedIntent.Facing.Y, FacingZ: typedIntent.Facing.Z,
		}, nil
	case CinematicIntent:
		return SemanticTraceIntent{
			Kind: "cinematic", Role: string(typedIntent.FocusRole),
			DurationMicroseconds: typedIntent.Duration.Microseconds(), Radius: typedIntent.Radius,
		}, nil
	case DialogueIntent:
		return SemanticTraceIntent{Kind: "dialogue", Role: string(typedIntent.Role), Identifier: typedIntent.DialogueID}, nil
	case ClientEventIntent:
		return SemanticTraceIntent{
			Kind: "client_event", ActionKind: string(typedIntent.EventKind), Identifier: typedIntent.EventID,
		}, nil
	case ObjectiveIntent:
		return SemanticTraceIntent{Kind: "objective", Identifier: typedIntent.ObjectiveID, IsEnabled: typedIntent.IsActive}, nil
	case ObjectiveDataIntent:
		return SemanticTraceIntent{
			Kind: "objective_data", Identifier: typedIntent.ObjectiveID,
			PlayerIndex: typedIntent.PlayerIndex, TokenIndex: typedIntent.TokenIndex,
			IntegerChange: typedIntent.Integer, Flags: typedIntent.Flags,
		}, nil
	case UnlockIntent:
		return SemanticTraceIntent{
			Kind: "unlock", Role: string(typedIntent.PlayerRole), ActionKind: string(typedIntent.UnlockKind),
			Count: typedIntent.Count,
		}, nil
	case CrystalDropIntent:
		return SemanticTraceIntent{Kind: "drop_crystals", Role: string(typedIntent.PlayerRole)}, nil
	case CrystalPickupIntent:
		return SemanticTraceIntent{
			Kind: "pickup_crystal", Role: string(typedIntent.PlayerRole),
			InitiatorRole: string(typedIntent.AgentRole), TargetRole: string(typedIntent.TargetRole),
		}, nil
	case MovementIntent:
		return SemanticTraceIntent{
			Kind: "movement", Role: string(typedIntent.Role), TargetRole: string(typedIntent.Target),
			Speed: typedIntent.Speed,
		}, nil
	case TeleportIntent:
		return SemanticTraceIntent{
			Kind: "teleport", Role: string(typedIntent.Role),
			PositionX: typedIntent.Destination.X, PositionY: typedIntent.Destination.Y,
			PositionZ: typedIntent.Destination.Z,
		}, nil
	case TriggerVolumeIntent:
		return SemanticTraceIntent{
			Kind: "trigger_volume", Role: string(typedIntent.Role), Radius: typedIntent.Radius,
			PositionX: typedIntent.Center.X, PositionY: typedIntent.Center.Y,
			PositionZ: typedIntent.Center.Z,
		}, nil
	case DestroyTriggerVolumeIntent:
		return SemanticTraceIntent{Kind: "destroy_trigger_volume", Role: string(typedIntent.Role)}, nil
	case LocomotionStopIntent:
		return SemanticTraceIntent{
			Kind: "locomotion_stop", Role: string(typedIntent.Role),
			FacingX: typedIntent.Facing.X, FacingY: typedIntent.Facing.Y, FacingZ: typedIntent.Facing.Z,
			PositionX: typedIntent.TargetPosition.X, PositionY: typedIntent.TargetPosition.Y,
			PositionZ: typedIntent.TargetPosition.Z, IsTurn: typedIntent.IsTurn,
		}, nil
	case AttributeModifierIntent:
		return SemanticTraceIntent{
			Kind: "attribute_modifier", Role: string(typedIntent.Role),
			ActionKind: string(typedIntent.AttributeKind), Scalar: typedIntent.Amount,
		}, nil
	case ModifierRequestIntent:
		return SemanticTraceIntent{
			Kind: "modifier_request", Role: string(typedIntent.TargetRole),
			InitiatorRole: string(typedIntent.InitiatorRole), AssetName: typedIntent.ModifierGUID,
			Count: typedIntent.Rank, PositionX: typedIntent.Destination.X,
			PositionY: typedIntent.Destination.Y, PositionZ: typedIntent.Destination.Z,
		}, nil
	case CombatIntent:
		return SemanticTraceIntent{
			Kind: "combat", Role: string(typedIntent.ActorRole), TargetRole: string(typedIntent.TargetRole),
			AssetName: typedIntent.AbilityName,
		}, nil
	case TargetValidationIntent:
		return SemanticTraceIntent{
			Kind: "target_validation", Role: string(typedIntent.ActorRole),
			TargetRole: string(typedIntent.TargetRole), ActionKind: typedIntent.Stage,
			IsValid: typedIntent.IsValid,
		}, nil
	case CooldownIntent:
		return SemanticTraceIntent{
			Kind: "cooldown", Role: string(typedIntent.Role), AssetName: typedIntent.AbilityName,
			DurationMicroseconds: typedIntent.Duration.Microseconds(),
		}, nil
	case ResourceChangeIntent:
		return SemanticTraceIntent{
			Kind: "resource_change", Role: string(typedIntent.Role), Scalar: typedIntent.Delta,
		}, nil
	case AreaPulseIntent:
		return SemanticTraceIntent{
			Kind: "area_pulse", Role: string(typedIntent.Role), InitiatorRole: string(typedIntent.ActorRole),
			PositionX: typedIntent.Position.X, PositionY: typedIntent.Position.Y,
			PositionZ: typedIntent.Position.Z, Radius: typedIntent.Radius, Count: typedIntent.Tick,
			DamageMinimum: typedIntent.DamageMinimum, DamageMaximum: typedIntent.DamageMaximum,
			DamageCoefficient: typedIntent.DamageCoefficient, HealingMinimum: typedIntent.HealingMinimum,
			HealingMaximum: typedIntent.HealingMaximum, HealingCoefficient: typedIntent.HealingCoefficient,
			RootModifierID: typedIntent.RootModifierID, RootChance: typedIntent.RootChance,
		}, nil
	case AbilityReleaseIntent:
		return SemanticTraceIntent{
			Kind: "ability_release", Role: string(typedIntent.Role), AssetName: typedIntent.AbilityName,
		}, nil
	case ProjectileLaunchIntent:
		return SemanticTraceIntent{
			Kind: "projectile_launch", Role: string(typedIntent.Role),
			InitiatorRole: string(typedIntent.ActorRole), TargetRole: string(typedIntent.TargetRole),
			AssetName: typedIntent.NounName, PositionX: typedIntent.Position.X,
			PositionY: typedIntent.Position.Y, PositionZ: typedIntent.Position.Z,
			DirectionX: typedIntent.Direction.X, DirectionY: typedIntent.Direction.Y,
			DirectionZ:       typedIntent.Direction.Z,
			AngularVelocityX: typedIntent.AngularVelocity.X,
			AngularVelocityY: typedIntent.AngularVelocity.Y,
			AngularVelocityZ: typedIntent.AngularVelocity.Z, Speed: typedIntent.Speed,
			Acceleration: typedIntent.Acceleration, Distance: typedIntent.Distance,
			RangeIncrease: typedIntent.RangeIncrease, IsHoming: typedIntent.IsHoming,
			IsPiercing:              typedIntent.IsPiercing,
			IsOrientationRecomputed: typedIntent.IsOrientationRecomputed,
		}, nil
	case ProjectileImpactIntent:
		return SemanticTraceIntent{
			Kind: "projectile_impact", Role: string(typedIntent.Role),
			TargetRole: string(typedIntent.TargetRole), PositionX: typedIntent.Position.X,
			PositionY: typedIntent.Position.Y, PositionZ: typedIntent.Position.Z,
			FacingX: typedIntent.Facing.X, FacingY: typedIntent.Facing.Y,
			FacingZ: typedIntent.Facing.Z, IsDirect: typedIntent.IsDirect,
		}, nil
	case ProjectileMotionIntent:
		intent := SemanticTraceIntent{
			Kind: "projectile_motion", Role: string(typedIntent.Role),
			TargetRole: string(typedIntent.TargetRole),
			DirectionX: typedIntent.Direction.X, DirectionY: typedIntent.Direction.Y,
			DirectionZ:       typedIntent.Direction.Z,
			AngularVelocityX: typedIntent.AngularVelocity.X,
			AngularVelocityY: typedIntent.AngularVelocity.Y,
			AngularVelocityZ: typedIntent.AngularVelocity.Z, Speed: typedIntent.Speed,
			Acceleration: typedIntent.Acceleration, Distance: typedIntent.Distance,
			RangeIncrease:        typedIntent.RangeIncrease,
			DurationMicroseconds: typedIntent.HomingDelay.Microseconds(),
			IsHoming:             typedIntent.IsHoming,
		}
		if typedIntent.ExpectedGeometryCollision != nil {
			intent.PositionX = typedIntent.ExpectedGeometryCollision.X
			intent.PositionY = typedIntent.ExpectedGeometryCollision.Y
			intent.PositionZ = typedIntent.ExpectedGeometryCollision.Z
			intent.IsEnabled = true
		}
		return intent, nil
	case DamageIntent:
		return SemanticTraceIntent{
			Kind: "damage", Role: string(typedIntent.ActorRole), TargetRole: string(typedIntent.TargetRole),
			Scalar: typedIntent.DeltaHealth, IntegerChange: typedIntent.IntegerHitPointChange,
			IsKilling: typedIntent.IsKilling, IsCritical: typedIntent.IsCritical,
		}, nil
	case HitPointIntent:
		return SemanticTraceIntent{
			Kind: "hit_points", Role: string(typedIntent.Role), Scalar: typedIntent.HitPoints,
		}, nil
	case DeathStateIntent:
		return SemanticTraceIntent{
			Kind: "death_state", Role: string(typedIntent.Role), ActionKind: string(typedIntent.Action),
		}, nil
	case RewardIntent:
		return SemanticTraceIntent{
			Kind: "reward", Role: string(typedIntent.PlayerRole), ActionKind: typedIntent.RewardKind,
			Amount: typedIntent.Amount,
		}, nil
	case SequenceCompleteIntent:
		return SemanticTraceIntent{Kind: "sequence_complete", Role: string(typedIntent.Role)}, nil
	default:
		return SemanticTraceIntent{}, fmt.Errorf("intentUnsupported: %T", intent)
	}
}

func buildSemanticTraceProvenance(provenance Provenance) SemanticTraceProvenance {
	traceProvenance := SemanticTraceProvenance{
		LuaChunkID: provenance.LuaChunkID, BytecodeSHA256: provenance.BytecodeSHA256,
		FunctionName: provenance.FunctionName, Confidence: confidenceName(provenance.Confidence),
	}
	if provenance.IsInstructionOffsetKnown {
		offset := provenance.InstructionOffset
		traceProvenance.InstructionOffset = &offset
	}
	if provenance.NativeAddress != 0 {
		address := provenance.NativeAddress
		traceProvenance.NativeAddress = &address
	}
	return traceProvenance
}

type scopeNormalizer struct {
	sessions    map[uint64]uint64
	phases      map[uint64]uint64
	roles       map[Role]map[uint64]uint64
	eventEpochs map[uint64]uint64
	nextSession uint64
	nextPhase   uint64
	nextEpoch   uint64
}

func newScopeNormalizer() *scopeNormalizer {
	return &scopeNormalizer{
		sessions: make(map[uint64]uint64), phases: make(map[uint64]uint64),
		roles: make(map[Role]map[uint64]uint64), eventEpochs: make(map[uint64]uint64),
	}
}

func (n *scopeNormalizer) normalizeEventEpoch(epoch uint64) uint64 {
	if epoch == 0 {
		return 0
	}
	normalized, isFound := n.eventEpochs[epoch]
	if isFound {
		return normalized
	}
	n.nextEpoch++
	normalized = n.nextEpoch
	n.eventEpochs[epoch] = normalized
	return normalized
}

func (n *scopeNormalizer) normalize(scope CancelScope) SemanticTraceScope {
	if scope.sessionGeneration == 0 {
		return SemanticTraceScope{}
	}
	sessionGeneration, isFound := n.sessions[scope.sessionGeneration]
	if !isFound {
		n.nextSession++
		sessionGeneration = n.nextSession
		n.sessions[scope.sessionGeneration] = sessionGeneration
	}
	phaseGeneration, isFound := n.phases[scope.phaseGeneration]
	if !isFound {
		n.nextPhase++
		phaseGeneration = n.nextPhase
		n.phases[scope.phaseGeneration] = phaseGeneration
	}
	roleGeneration := uint64(0)
	if scope.role != "" {
		generation := n.roles[scope.role]
		if generation == nil {
			generation = make(map[uint64]uint64)
			n.roles[scope.role] = generation
		}
		roleGeneration, isFound = generation[scope.roleGeneration]
		if !isFound {
			roleGeneration = uint64(len(generation) + 1)
			generation[scope.roleGeneration] = roleGeneration
		}
	}
	return SemanticTraceScope{
		IsKnown: true, SessionGeneration: sessionGeneration, PhaseGeneration: phaseGeneration,
		Role: string(scope.role), RoleGeneration: roleGeneration,
	}
}

func domainName(domain Domain) string {
	switch domain {
	case DomainControl:
		return "control"
	case DomainAuthority:
		return "authority"
	case DomainPresentation:
		return "presentation"
	default:
		return fmt.Sprintf("domain_%d", domain)
	}
}

func confidenceName(confidence Confidence) string {
	switch confidence {
	case ConfidenceUnknown:
		return "unknown"
	case ConfidenceInferred:
		return "inferred"
	case ConfidenceBytecode:
		return "bytecode"
	case ConfidenceNative:
		return "native"
	case ConfidenceLive:
		return "live"
	default:
		return fmt.Sprintf("confidence_%d", confidence)
	}
}
