package sqlstore

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type mutationPositionPayload struct{ X, Y, Z float32 }
type mutationTimePayload struct {
	IsSet  bool
	Offset time.Duration
}

type mutationDamageProfilePayload struct {
	PrimaryAttribute                float32
	IsPrimaryAttributeFound         bool
	PrimaryAttributeBaseline        float32
	IsPrimaryAttributeBaselineFound bool
	BasicDefenseBoost               float32
	DefenseRatingPerPercent         float32
	PhysicalDefenseBoost            float32
	EnergyDefenseBoost              float32
	DamageBuff                      float32
	ScienceDamage                   [5]float32
	ProjectileDamage                float32
	EnergyDamageBuff                float32
	AreaDamage                      float32
	DamageOverTimeIncrease          float32
	DirectAttackDamagePercent       float32
	PhysicalDamage                  float32
	PhysicalAbilityDamageIncrease   float32
	EnergyDamage                    float32
	EnergyAbilityDamageIncrease     float32
	DirectAttackDamage              float32
}

type mutationDamagePayload struct {
	ObjectID             uint32
	Generation           uint64
	Faction              uint8
	Profile              mutationDamageProfilePayload
	CriticalRating       float32
	DifficultyMultiplier float32
}

type mutationBeamPayload struct {
	Faction          uint8
	TargetObjectID   uint32
	TargetGeneration uint64
	StartedAt        mutationTimePayload
	ChannelAt        mutationTimePayload
	MutateAt         mutationTimePayload
	ReleaseAt        mutationTimePayload
	IsActive         bool
	IsChannelActive  bool
	IsPromoted       bool
}

type mutationTransformPayload struct {
	SourceObjectID    uint32
	SourceGeneration  uint64
	CompleteAt        mutationTimePayload
	IsActive          bool
	IsFullHealPending bool
	HealAfter         mutationTimePayload
}

type mutationMovementPayload struct {
	TargetObjectID    uint32
	TargetGeneration  uint64
	Origin            mutationPositionPayload
	Destination       mutationPositionPayload
	Waypoint          mutationPositionPayload
	MovementSpeed     float32
	MovementSpeedBuff float32
	UpdatedAt         mutationTimePayload
	CheckAt           mutationTimePayload
	IsInitialized     bool
	IsFleeing         bool
	IsPaused          bool
	IsApproaching     bool
}

type mutationGrowthPayload struct {
	StackCount          uint32
	BaseMaximumHitPoint float32
	BaseGraphicsScale   float32
}

type mutationRetirementPayload struct {
	Generation           uint64
	BeamTargetObjectID   uint32
	BeamTargetGeneration uint64
	IsBeamPending        bool
	IsDeactivatePending  bool
}

type mutationDamageTargetPayload struct {
	ObjectID         uint32
	UserID           uint64
	PeerGeneration   uint64
	DeathRevision    uint64
	Damage           float32
	IsCritical       bool
	IsCommitted      bool
	IsComplete       bool
	PreviousHitPoint float32
	HitPoint         float32
}

type mutationDamageWorkPayload struct {
	Targets     [128]mutationDamageTargetPayload
	TargetCount uint32
	IsPrepared  bool
}

type mutationEventPayload struct {
	ObjectID          uint32
	Generation        uint64
	Sequence          uint64
	Kind              uint8
	At                mutationTimePayload
	TargetObjectID    uint32
	TargetGeneration  uint64
	Position          mutationPositionPayload
	Facing            mutationPositionPayload
	Waypoint          mutationPositionPayload
	MovementSpeed     float32
	MovementSpeedBuff float32
	DamageSource      mutationDamagePayload
	IsDamageClaimed   bool
	DamageWork        *mutationDamageWorkPayload `json:"DamageWork,omitempty"`
}

type mutationPayload struct {
	Generation          uint64
	Rank                uint32
	CurrentIndex        uint32
	PromotedObjectIDs   [3]uint32
	NextSlotAt          mutationTimePayload
	NextActionAt        mutationTimePayload
	NextAuraAt          mutationTimePayload
	AuraHitAt           mutationTimePayload
	AuraPosition        mutationPositionPayload
	AuraFacing          mutationPositionPayload
	DamageSource        mutationDamagePayload
	Beam                mutationBeamPayload
	Transform           mutationTransformPayload
	Movement            mutationMovementPayload
	Growth              mutationGrowthPayload
	Retirement          mutationRetirementPayload
	IsPassiveActive     bool
	IsAuraActive        bool
	IsDeactivatePending bool
	NextSequence        uint64
	Events              [8]mutationEventPayload
	EventCount          uint32
}

func encodeMutationTime(at, capturedAt time.Time) mutationTimePayload {
	if at.IsZero() {
		return mutationTimePayload{}
	}
	return mutationTimePayload{IsSet: true, Offset: at.Sub(capturedAt)}
}

func decodeMutationTime(clock mutationTimePayload, capturedAt time.Time) time.Time {
	if !clock.IsSet {
		return time.Time{}
	}
	return capturedAt.Add(clock.Offset)
}

func encodeMutationDamage(source zonenpc.MutationDamageSource) mutationDamagePayload {
	return mutationDamagePayload{ObjectID: source.ObjectID, Generation: source.Generation,
		Faction: uint8(source.Faction), Profile: mutationDamageProfilePayload(source.Profile),
		CriticalRating: source.CriticalRating, DifficultyMultiplier: source.DifficultyMultiplier}
}

func decodeMutationDamage(source mutationDamagePayload) zonenpc.MutationDamageSource {
	return zonenpc.MutationDamageSource{ObjectID: source.ObjectID, Generation: source.Generation,
		Faction: zonenpc.Faction(source.Faction), Profile: game.DamageProfile(source.Profile),
		CriticalRating: source.CriticalRating, DifficultyMultiplier: source.DifficultyMultiplier}
}

func encodeMutationDamageWork(work *zonenpc.MutationDamageWork) *mutationDamageWorkPayload {
	if work == nil {
		return nil
	}
	encoded := &mutationDamageWorkPayload{TargetCount: work.TargetCount, IsPrepared: work.IsPrepared}
	for index, target := range work.Targets {
		encoded.Targets[index] = mutationDamageTargetPayload(target)
	}
	return encoded
}

func decodeMutationDamageWork(encoded *mutationDamageWorkPayload) *zonenpc.MutationDamageWork {
	if encoded == nil {
		return nil
	}
	work := &zonenpc.MutationDamageWork{TargetCount: encoded.TargetCount, IsPrepared: encoded.IsPrepared}
	for index, target := range encoded.Targets {
		work.Targets[index] = zonenpc.MutationDamageTarget(target)
	}
	return work
}

func encodeMutation(state zonenpc.MutationState, capturedAt time.Time) *mutationPayload {
	if state == (zonenpc.MutationState{}) {
		return nil
	}
	encoded := &mutationPayload{
		Generation: state.Generation, Rank: state.Rank, CurrentIndex: state.CurrentIndex,
		PromotedObjectIDs: state.PromotedObjectIDs,
		NextSlotAt:        encodeMutationTime(state.NextSlotAt, capturedAt),
		NextActionAt:      encodeMutationTime(state.NextActionAt, capturedAt),
		NextAuraAt:        encodeMutationTime(state.NextAuraAt, capturedAt),
		AuraHitAt:         encodeMutationTime(state.AuraHitAt, capturedAt),
		AuraPosition:      mutationPositionPayload(state.AuraPosition), AuraFacing: mutationPositionPayload(state.AuraFacing),
		DamageSource: encodeMutationDamage(state.DamageSource),
		Growth:       mutationGrowthPayload(state.Growth),
		Retirement:   mutationRetirementPayload(state.Retirement),
		Beam: mutationBeamPayload{Faction: uint8(state.Beam.Faction),
			TargetObjectID: state.Beam.TargetObjectID, TargetGeneration: state.Beam.TargetGeneration,
			StartedAt: encodeMutationTime(state.Beam.StartedAt, capturedAt),
			ChannelAt: encodeMutationTime(state.Beam.ChannelAt, capturedAt),
			MutateAt:  encodeMutationTime(state.Beam.MutateAt, capturedAt),
			ReleaseAt: encodeMutationTime(state.Beam.ReleaseAt, capturedAt),
			IsActive:  state.Beam.IsActive, IsChannelActive: state.Beam.IsChannelActive, IsPromoted: state.Beam.IsPromoted},
		Transform: mutationTransformPayload{SourceObjectID: state.Transform.SourceObjectID,
			SourceGeneration: state.Transform.SourceGeneration, CompleteAt: encodeMutationTime(state.Transform.CompleteAt, capturedAt),
			IsActive: state.Transform.IsActive, IsFullHealPending: state.Transform.IsFullHealPending,
			HealAfter: encodeMutationTime(state.Transform.HealAfter, capturedAt)},
		Movement: mutationMovementPayload{Origin: mutationPositionPayload(state.Movement.Origin),
			TargetObjectID: state.Movement.TargetObjectID, TargetGeneration: state.Movement.TargetGeneration, IsApproaching: state.Movement.IsApproaching,
			Waypoint: mutationPositionPayload(state.Movement.Waypoint), MovementSpeed: state.Movement.MovementSpeed, MovementSpeedBuff: state.Movement.MovementSpeedBuff,
			Destination: mutationPositionPayload(state.Movement.Destination), UpdatedAt: encodeMutationTime(state.Movement.UpdatedAt, capturedAt),
			CheckAt: encodeMutationTime(state.Movement.CheckAt, capturedAt), IsInitialized: state.Movement.IsInitialized, IsFleeing: state.Movement.IsFleeing, IsPaused: state.Movement.IsPaused},
		IsPassiveActive: state.IsPassiveActive, IsAuraActive: state.IsAuraActive, IsDeactivatePending: state.IsDeactivatePending,
		NextSequence: state.NextSequence, EventCount: state.EventCount,
	}
	for index, event := range state.Events {
		encoded.Events[index] = mutationEventPayload{ObjectID: event.ID.ObjectID,
			Generation: event.ID.Generation, Sequence: event.ID.Sequence, Kind: uint8(event.Kind),
			At: encodeMutationTime(event.At, capturedAt), TargetObjectID: event.TargetObjectID, TargetGeneration: event.TargetGeneration,
			Position: mutationPositionPayload(event.Position), Facing: mutationPositionPayload(event.Facing),
			Waypoint: mutationPositionPayload(event.Waypoint), MovementSpeed: event.MovementSpeed, MovementSpeedBuff: event.MovementSpeedBuff,
			DamageSource: encodeMutationDamage(event.DamageSource), IsDamageClaimed: event.IsDamageClaimed,
			DamageWork: encodeMutationDamageWork(event.DamageWork)}
	}
	return encoded
}

func decodeMutation(encoded *mutationPayload, capturedAt time.Time) zonenpc.MutationState {
	if encoded == nil {
		return zonenpc.MutationState{}
	}
	state := zonenpc.MutationState{
		Generation: encoded.Generation, Rank: encoded.Rank, CurrentIndex: encoded.CurrentIndex,
		PromotedObjectIDs: encoded.PromotedObjectIDs,
		NextSlotAt:        decodeMutationTime(encoded.NextSlotAt, capturedAt),
		NextActionAt:      decodeMutationTime(encoded.NextActionAt, capturedAt),
		NextAuraAt:        decodeMutationTime(encoded.NextAuraAt, capturedAt),
		AuraHitAt:         decodeMutationTime(encoded.AuraHitAt, capturedAt),
		AuraPosition:      game.Vec3(encoded.AuraPosition), AuraFacing: game.Vec3(encoded.AuraFacing),
		DamageSource: decodeMutationDamage(encoded.DamageSource),
		Growth:       zonenpc.MutationGrowthState(encoded.Growth),
		Retirement:   zonenpc.MutationRetirementState(encoded.Retirement),
		Beam: zonenpc.MutationBeamState{Faction: zonenpc.Faction(encoded.Beam.Faction),
			TargetObjectID: encoded.Beam.TargetObjectID, TargetGeneration: encoded.Beam.TargetGeneration,
			StartedAt: decodeMutationTime(encoded.Beam.StartedAt, capturedAt),
			ChannelAt: decodeMutationTime(encoded.Beam.ChannelAt, capturedAt),
			MutateAt:  decodeMutationTime(encoded.Beam.MutateAt, capturedAt),
			ReleaseAt: decodeMutationTime(encoded.Beam.ReleaseAt, capturedAt),
			IsActive:  encoded.Beam.IsActive, IsChannelActive: encoded.Beam.IsChannelActive, IsPromoted: encoded.Beam.IsPromoted},
		Transform: zonenpc.MutationTransformState{SourceObjectID: encoded.Transform.SourceObjectID,
			SourceGeneration: encoded.Transform.SourceGeneration, CompleteAt: decodeMutationTime(encoded.Transform.CompleteAt, capturedAt),
			IsActive: encoded.Transform.IsActive, IsFullHealPending: encoded.Transform.IsFullHealPending,
			HealAfter: decodeMutationTime(encoded.Transform.HealAfter, capturedAt)},
		Movement: zonenpc.MutationMovementState{Origin: game.Vec3(encoded.Movement.Origin),
			TargetObjectID: encoded.Movement.TargetObjectID, TargetGeneration: encoded.Movement.TargetGeneration, IsApproaching: encoded.Movement.IsApproaching,
			Waypoint: game.Vec3(encoded.Movement.Waypoint), MovementSpeed: encoded.Movement.MovementSpeed, MovementSpeedBuff: encoded.Movement.MovementSpeedBuff,
			Destination: game.Vec3(encoded.Movement.Destination), UpdatedAt: decodeMutationTime(encoded.Movement.UpdatedAt, capturedAt),
			CheckAt: decodeMutationTime(encoded.Movement.CheckAt, capturedAt), IsInitialized: encoded.Movement.IsInitialized, IsFleeing: encoded.Movement.IsFleeing, IsPaused: encoded.Movement.IsPaused},
		IsPassiveActive: encoded.IsPassiveActive, IsAuraActive: encoded.IsAuraActive, IsDeactivatePending: encoded.IsDeactivatePending,
		NextSequence: encoded.NextSequence, EventCount: encoded.EventCount,
	}
	for index, event := range encoded.Events {
		state.Events[index] = zonenpc.MutationEvent{ID: zonenpc.MutationEventID{ObjectID: event.ObjectID,
			Generation: event.Generation, Sequence: event.Sequence}, Kind: zonenpc.MutationEventKind(event.Kind),
			At: decodeMutationTime(event.At, capturedAt), TargetObjectID: event.TargetObjectID, TargetGeneration: event.TargetGeneration,
			Position: game.Vec3(event.Position), Facing: game.Vec3(event.Facing),
			Waypoint: game.Vec3(event.Waypoint), MovementSpeed: event.MovementSpeed, MovementSpeedBuff: event.MovementSpeedBuff,
			DamageSource: decodeMutationDamage(event.DamageSource), IsDamageClaimed: event.IsDamageClaimed,
			DamageWork: decodeMutationDamageWork(event.DamageWork)}
	}
	return state
}
