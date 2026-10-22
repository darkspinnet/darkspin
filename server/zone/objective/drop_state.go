package objective

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

// PendingDrops retains acceptance order without exposing the session's outbox.
func (e *Session) PendingDrops() []DropRequest {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]DropRequest(nil), e.pendingDrops...)
}

// AckDrop is idempotent for every previously issued request. The world owner
// calls it only after retaining the identical request and its world identity.
func (e *Session) AckDrop(id uint64) error {
	if e == nil {
		return errors.New("objective drop acknowledgement unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	nextDropID := e.nextDropID
	if nextDropID == 0 {
		nextDropID = 1
	}
	if id == 0 || id >= nextDropID {
		return errors.New("objective drop acknowledgement identity invalid")
	}
	for index, drop := range e.pendingDrops {
		if drop.ID != id {
			continue
		}
		copy(e.pendingDrops[index:], e.pendingDrops[index+1:])
		e.pendingDrops[len(e.pendingDrops)-1] = DropRequest{}
		e.pendingDrops = e.pendingDrops[:len(e.pendingDrops)-1]
		return nil
	}
	return nil
}

// CheckpointState captures the outbox and its authored/UI state under the same
// lock, so an accepted callback cannot be split across checkpoint records.
func (e *Session) CheckpointState() (Checkpoint, error) {
	if e == nil {
		return Checkpoint{NextDropID: 1}, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	objectives, runtimes, err := e.checkpoint()
	if err != nil {
		return Checkpoint{}, fmt.Errorf("dropCheckpoint: %w", err)
	}
	nextDropID := e.nextDropID
	if nextDropID == 0 {
		nextDropID = 1
	}
	return Checkpoint{
		Objectives: objectives, Runtimes: runtimes,
		Drops: append([]DropRequest(nil), e.pendingDrops...), NextDropID: nextDropID,
		Events: append([]sim.LuaObjectiveEvent(nil), e.pendingEvents...),
	}, nil
}

// RestoreState validates the complete candidate before committing any live
// runtime, UI projection, pending request or request identity highwater.
func (e *Session) RestoreState(req Checkpoint) error {
	if e == nil {
		if len(req.Objectives) == 0 && len(req.Runtimes) == 0 &&
			len(req.Drops) == 0 && len(req.Events) == 0 && req.NextDropID <= 1 {
			return nil
		}
		return errors.New("objective drop restore unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	err := ValidateDropState(req)
	if err != nil {
		return fmt.Errorf("dropState: %w", err)
	}
	nextDropID := req.NextDropID
	if nextDropID == 0 {
		nextDropID = 1
	}
	drops := append([]DropRequest(nil), req.Drops...)
	events := append([]sim.LuaObjectiveEvent(nil), req.Events...)
	err = e.restoreCheckpoint(req.Objectives, req.Runtimes)
	if err != nil {
		return fmt.Errorf("dropRuntime: %w", err)
	}
	e.pendingDrops = drops
	e.pendingEvents = events
	e.nextDropID = nextDropID
	return nil
}

// ValidateDropState lets the checkpoint adapter reject an invalid outbox or
// retry journal before offering a saved mission for resume.
func ValidateDropState(req Checkpoint) error {
	nextDropID := req.NextDropID
	if nextDropID == 0 {
		if len(req.Drops) != 0 {
			return errors.New("objective drop highwater missing")
		}
		nextDropID = 1
	}
	isQuestRuntimePresent := false
	for _, runtime := range req.Runtimes {
		if runtime.ObjectiveID == LootCrystalsID {
			isQuestRuntimePresent = true
			break
		}
	}
	if (len(req.Drops) != 0 || len(req.Events) != 0) && !isQuestRuntimePresent {
		return errors.New("objective drop runtime missing")
	}
	if len(req.Drops) > 8 {
		return errors.New("objective pending drop count invalid")
	}
	previousDropID := uint64(0)
	for index, drop := range req.Drops {
		if drop.ID <= previousDropID || drop.ID >= nextDropID {
			return fmt.Errorf("objective drop identity invalid: %d", index)
		}
		err := validateDropRequest(drop)
		if err != nil {
			return fmt.Errorf("dropRestore[%d]: %w", index, err)
		}
		previousDropID = drop.ID
	}
	eventsByContext := make(map[sim.LuaObjectiveEvent]struct{}, len(req.Events))
	for index, event := range req.Events {
		err := validateQuestEvent(event)
		if err != nil {
			return fmt.Errorf("eventRestore[%d]: %w", index, err)
		}
		_, isDuplicate := eventsByContext[event]
		if isDuplicate {
			return fmt.Errorf("objective event repeated: %d", index)
		}
		eventsByContext[event] = struct{}{}
	}
	return nil
}

func validateQuestEvent(event sim.LuaObjectiveEvent) error {
	if event.Kind != sim.LuaObjectiveEventDeath || event.ObjectID == 0 ||
		event.TargetObjectID == 0 || event.PlayerIndex >= 4 ||
		math.IsNaN(event.KillPercent) || math.IsInf(event.KillPercent, 0) ||
		event.KillPercent < 0 || event.KillPercent > 1 ||
		math.IsNaN(event.Damage) || math.IsInf(event.Damage, 0) || event.Damage < 0 ||
		math.IsNaN(event.Healing) || math.IsInf(event.Healing, 0) || event.Healing < 0 {
		return errors.New("objective quest event invalid")
	}
	err := validateDropRequest(DropRequest{
		ID: 1, ObjectiveID: LootCrystalsID, NounID: util.HashID("loot_questCrystal.Noun"),
		SourceObjectID: event.TargetObjectID, SourcePosition: event.TargetPosition,
		SourceFootprintRadius: event.TargetFootprintRadius,
	})
	if err != nil {
		return fmt.Errorf("eventSource: %w", err)
	}
	return nil
}

func validateDropRequest(req DropRequest) error {
	if req.ID == 0 || req.ObjectiveID != LootCrystalsID ||
		req.NounID != util.HashID("loot_questCrystal.Noun") || req.SourceObjectID == 0 ||
		req.SourceFootprintRadius < 0 {
		return errors.New("objective drop request invalid")
	}
	sourceScalars := [...]float32{
		req.SourcePosition.X, req.SourcePosition.Y, req.SourcePosition.Z,
		req.SourceFootprintRadius,
	}
	for _, scalar := range sourceScalars {
		if math.IsNaN(float64(scalar)) || math.IsInf(float64(scalar), 0) {
			return errors.New("objective drop source invalid")
		}
	}
	return nil
}
