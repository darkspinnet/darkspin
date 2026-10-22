package npc

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

type MutationEventKind uint8

const (
	MutationEventActivate MutationEventKind = iota + 1
	MutationEventBeamStart
	MutationEventBeamChannel
	MutationEventBeamStop
	MutationEventTransformStart
	MutationEventTransformComplete
	MutationEventAuraPulse
	MutationEventAuraHit
	MutationEventDeactivate
	MutationEventMovement
	MutationEventFullHeal
)

type MutationEventID struct {
	ObjectID   uint32
	Generation uint64
	Sequence   uint64
}

type MutationDamageSource struct {
	ObjectID             uint32
	Generation           uint64
	Faction              Faction
	Profile              game.DamageProfile
	CriticalRating       float32
	DifficultyMultiplier float32
}

type MutationEvent struct {
	ID                MutationEventID
	Kind              MutationEventKind
	At                time.Time
	TargetObjectID    uint32
	TargetGeneration  uint64
	Position          game.Vec3
	Facing            game.Vec3
	Waypoint          game.Vec3
	MovementSpeed     float32
	MovementSpeedBuff float32
	DamageSource      MutationDamageSource
	IsDamageClaimed   bool
	// Published work is immutable; updates replace it with a fresh copy.
	DamageWork *MutationDamageWork
}

type MutationBeamState struct {
	Faction          Faction
	TargetObjectID   uint32
	TargetGeneration uint64
	StartedAt        time.Time
	ChannelAt        time.Time
	MutateAt         time.Time
	ReleaseAt        time.Time
	IsActive         bool
	IsChannelActive  bool
	IsPromoted       bool
}

type MutationTransformState struct {
	SourceObjectID    uint32
	SourceGeneration  uint64
	CompleteAt        time.Time
	IsActive          bool
	IsFullHealPending bool
	HealAfter         time.Time
}

type MutationRetirementState struct {
	Generation           uint64
	BeamTargetObjectID   uint32
	BeamTargetGeneration uint64
	IsBeamPending        bool
	IsDeactivatePending  bool
}

// MutationState is copied with the actor snapshot. Its bounded outbox retains
// semantic publications until the world presentation owner accepts them.
type MutationState struct {
	Generation          uint64
	Rank                uint32
	CurrentIndex        uint32
	PromotedObjectIDs   [3]uint32
	NextSlotAt          time.Time
	NextActionAt        time.Time
	NextAuraAt          time.Time
	AuraHitAt           time.Time
	AuraPosition        game.Vec3
	AuraFacing          game.Vec3
	DamageSource        MutationDamageSource
	Beam                MutationBeamState
	Transform           MutationTransformState
	Growth              MutationGrowthState
	Movement            MutationMovementState
	Retirement          MutationRetirementState
	IsPassiveActive     bool
	IsAuraActive        bool
	IsDeactivatePending bool
	NextSequence        uint64
	Events              [8]MutationEvent
	EventCount          uint32
}

type MutationAdvanceRequest struct {
	At                time.Time
	ControlledTargets []Target
	Targets           []Target
}

func appendMutationEvent(actor *Snapshot, event MutationEvent) bool {
	if actor == nil {
		return false
	}
	return appendMutationEventGeneration(actor, event, actor.Mutation.Generation)
}

func appendMutationEventGeneration(actor *Snapshot, event MutationEvent, generation uint64) bool {
	if actor == nil || actor.Mutation.EventCount >= uint32(len(actor.Mutation.Events)) {
		return false
	}
	actor.Mutation.NextSequence++
	if actor.Mutation.NextSequence == 0 {
		actor.Mutation.NextSequence++
	}
	event.ID = MutationEventID{ObjectID: actor.Plan.ObjectID, Generation: generation, Sequence: actor.Mutation.NextSequence}
	actor.Mutation.Events[actor.Mutation.EventCount] = event
	actor.Mutation.EventCount++
	return true
}

func (e *Session) mutationEventsLocked() []MutationEvent {
	events := make([]MutationEvent, 0)
	for _, objectID := range e.objectIDs {
		actor := e.npcs[objectID]
		for index := uint32(0); index < actor.Mutation.EventCount; index++ {
			events = append(events, actor.Mutation.Events[index])
		}
	}
	return events
}

func (e *Session) MutationEvents() []MutationEvent {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.mutationEventsLocked()
}

func (e *Session) CommitMutationEvent(id MutationEventID) bool {
	if e == nil || id.ObjectID == 0 || id.Sequence == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[id.ObjectID]
	if !isFound {
		return false
	}
	for index := uint32(0); index < actor.Mutation.EventCount; index++ {
		if actor.Mutation.Events[index].ID != id {
			continue
		}
		copy(actor.Mutation.Events[index:], actor.Mutation.Events[index+1:actor.Mutation.EventCount])
		actor.Mutation.EventCount--
		actor.Mutation.Events[actor.Mutation.EventCount] = MutationEvent{}
		e.npcs[id.ObjectID] = actor
		return true
	}
	return false
}

func (e *Session) ClaimMutationDamage(id MutationEventID) bool {
	if e == nil || id.ObjectID == 0 || id.Sequence == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[id.ObjectID]
	if !isFound {
		return false
	}
	for index := uint32(0); index < actor.Mutation.EventCount; index++ {
		event := &actor.Mutation.Events[index]
		if event.ID != id || event.Kind != MutationEventAuraHit || event.IsDamageClaimed {
			continue
		}
		event.IsDamageClaimed = true
		e.npcs[id.ObjectID] = actor
		return true
	}
	return false
}
