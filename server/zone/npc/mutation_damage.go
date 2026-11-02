package npc

import (
	"errors"
	"fmt"
	"math"
)

const MutationDamageTargetLimit = 128

// MutationDamageTarget retains the selected roll and authority receipt. A
// publication retry must never turn a committed target back into a new hit.
type MutationDamageTarget struct {
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

type MutationDamageWork struct {
	Targets     [MutationDamageTargetLimit]MutationDamageTarget
	TargetCount uint32
	IsPrepared  bool
}

func (e MutationDamageWork) IsComplete() bool {
	if !e.IsPrepared {
		return false
	}
	for index := uint32(0); index < e.TargetCount; index++ {
		if !e.Targets[index].IsComplete {
			return false
		}
	}
	return true
}

func (e *Session) PrepareMutationDamage(id MutationEventID, targets []MutationDamageTarget) (MutationDamageWork, error) {
	if e == nil || len(targets) > MutationDamageTargetLimit {
		return MutationDamageWork{}, errors.New("mutation damage targets invalid")
	}
	work := MutationDamageWork{TargetCount: uint32(len(targets)), IsPrepared: true}
	previousID := uint32(0)
	for index, target := range targets {
		if target.ObjectID <= previousID || target.UserID == 0 || target.PeerGeneration == 0 ||
			math.IsNaN(float64(target.Damage)) || math.IsInf(float64(target.Damage), 0) || target.Damage < 0 ||
			target.IsCommitted || target.IsComplete {
			return MutationDamageWork{}, fmt.Errorf("mutation damage target invalid: %d", index)
		}
		work.Targets[index] = target
		previousID = target.ObjectID
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[id.ObjectID]
	if !isFound {
		return MutationDamageWork{}, errors.New("mutation damage source unavailable")
	}
	for index := uint32(0); index < actor.Mutation.EventCount; index++ {
		event := &actor.Mutation.Events[index]
		if event.ID != id || event.Kind != MutationEventAuraHit {
			continue
		}
		if event.DamageWork != nil && event.DamageWork.IsPrepared {
			return *event.DamageWork, nil
		}
		event.DamageWork = &work
		event.IsDamageClaimed = true
		e.npcs[id.ObjectID] = actor
		return work, nil
	}
	return MutationDamageWork{}, errors.New("mutation damage event unavailable")
}

func (e *Session) MutationDamageWork(id MutationEventID) (MutationDamageWork, bool) {
	if e == nil {
		return MutationDamageWork{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, isFound := e.npcs[id.ObjectID]
	if !isFound {
		return MutationDamageWork{}, false
	}
	for index := uint32(0); index < actor.Mutation.EventCount; index++ {
		event := actor.Mutation.Events[index]
		if event.ID == id && event.Kind == MutationEventAuraHit {
			if event.DamageWork == nil {
				return MutationDamageWork{}, true
			}
			return *event.DamageWork, true
		}
	}
	return MutationDamageWork{}, false
}

func (e *Session) UpdateMutationDamageTarget(id MutationEventID, target MutationDamageTarget) error {
	if e == nil {
		return errors.New("mutation damage session unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[id.ObjectID]
	if !isFound {
		return errors.New("mutation damage source unavailable")
	}
	for eventIndex := uint32(0); eventIndex < actor.Mutation.EventCount; eventIndex++ {
		event := &actor.Mutation.Events[eventIndex]
		if event.ID != id || event.Kind != MutationEventAuraHit {
			continue
		}
		if event.DamageWork == nil {
			return errors.New("mutation damage work unavailable")
		}
		work := *event.DamageWork
		for index := uint32(0); index < work.TargetCount; index++ {
			previous := work.Targets[index]
			if previous.ObjectID != target.ObjectID {
				continue
			}
			if previous.UserID != target.UserID || previous.PeerGeneration != target.PeerGeneration ||
				previous.DeathRevision != target.DeathRevision ||
				previous.Damage != target.Damage || previous.IsCritical != target.IsCritical ||
				(previous.IsCommitted && !target.IsCommitted) || (previous.IsComplete && !target.IsComplete) {
				return errors.New("mutation damage target changed")
			}
			work.Targets[index] = target
			event.DamageWork = &work
			e.npcs[id.ObjectID] = actor
			return nil
		}
		return errors.New("mutation damage target unavailable")
	}
	return errors.New("mutation damage event unavailable")
}
