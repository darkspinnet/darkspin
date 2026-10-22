package objective

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/sim"
)

// DropRequest remains in the objective outbox until the world feature accepts
// ownership. Projection or scheduling failures cannot consume a Lua drop.
type DropRequest struct {
	ID                    uint64
	ObjectiveID           uint32
	NounID                uint32
	SourceObjectID        uint32
	SourcePosition        sim.Position
	SourceFootprintRadius float32
}

type Checkpoint struct {
	Objectives []sim.ObjectiveSnapshot
	Runtimes   []sim.LuaObjectiveSnapshot
	Drops      []DropRequest
	Events     []sim.LuaObjectiveEvent
	NextDropID uint64
}

func (e *Session) questCandidate() (*sim.LuaObjectiveRuntime, error) {
	runtime := e.runtimes[LootCrystalsID]
	if runtime == nil {
		return nil, errors.New("quest crystal objective unavailable")
	}
	snapshot, err := runtime.SnapshotState()
	if err != nil {
		return nil, fmt.Errorf("questSnapshot: %w", err)
	}
	candidate, err := runtime.RestoredState(snapshot)
	if err != nil {
		return nil, fmt.Errorf("questCandidate: %w", err)
	}
	return candidate, nil
}

// The caller holds the session lock. Only the candidate can mutate while the
// authored callback and every emitted operation are being validated.
func (e *Session) applyQuestEvent(ctx context.Context, event sim.LuaObjectiveEvent) ([]Update, error) {
	if event.Kind != sim.LuaObjectiveEventDeath {
		return nil, nil
	}
	err := validateQuestEvent(event)
	if err != nil {
		return nil, fmt.Errorf("questEventValidate: %w", err)
	}
	for _, pending := range e.pendingEvents {
		if pending == event {
			updates, retryErr := e.retryQuestEvents(ctx)
			if retryErr != nil {
				return updates, fmt.Errorf("questReplay: %w", retryErr)
			}
			return updates, nil
		}
	}
	// Progress may already have deduplicated this death. Retain its immutable
	// source context before attempting a callback that can reject or exhaust.
	e.pendingEvents = append(e.pendingEvents, event)
	updates, err := e.retryQuestEvents(ctx)
	if err != nil {
		return updates, fmt.Errorf("questEnqueue: %w", err)
	}
	return updates, nil
}

// RetryDrops retries only journaled quest callbacks, never generic death
// accounting or another objective's already accepted contribution.
func (e *Session) RetryDrops(ctx context.Context) ([]Update, error) {
	if ctx == nil {
		return nil, errors.New("quest retry context unavailable")
	}
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	updates, err := e.retryQuestEvents(ctx)
	if err != nil {
		return updates, fmt.Errorf("questDrain: %w", err)
	}
	return updates, nil
}

func (e *Session) retryQuestEvents(ctx context.Context) ([]Update, error) {
	updates := make([]Update, 0)
	for len(e.pendingEvents) != 0 {
		current, err := e.stageQuestEvent(ctx, e.pendingEvents[0])
		if err != nil {
			return updates, fmt.Errorf("questRetry: %w", err)
		}
		updates = append(updates, current...)
		copy(e.pendingEvents, e.pendingEvents[1:])
		e.pendingEvents[len(e.pendingEvents)-1] = sim.LuaObjectiveEvent{}
		e.pendingEvents = e.pendingEvents[:len(e.pendingEvents)-1]
	}
	return updates, nil
}

func (e *Session) stageQuestEvent(ctx context.Context, event sim.LuaObjectiveEvent) ([]Update, error) {
	candidate, err := e.questCandidate()
	if err != nil {
		return nil, fmt.Errorf("questEventPrepare: %w", err)
	}
	program, err := candidate.HandleEvent(event)
	if err != nil {
		return nil, fmt.Errorf("questEventCall: %w", err)
	}
	updates, err := e.commitQuestProgram(ctx, candidate, program, event.TargetObjectID)
	if err != nil {
		return nil, fmt.Errorf("questEventCommit: %w", err)
	}
	return updates, nil
}

func (e *Session) invokeQuestCallback(ctx context.Context, callbackName string) ([]Update, error) {
	if callbackName != "CrystalFound" {
		return nil, fmt.Errorf("quest callback unsupported: %s", callbackName)
	}
	candidate, err := e.questCandidate()
	if err != nil {
		return nil, fmt.Errorf("questPickupPrepare: %w", err)
	}
	program, err := candidate.Invoke(callbackName)
	if err != nil {
		return nil, fmt.Errorf("questPickupCall: %w", err)
	}
	updates, err := e.commitQuestProgram(ctx, candidate, program, 0)
	if err != nil {
		return nil, fmt.Errorf("questPickupCommit: %w", err)
	}
	return updates, nil
}

func (e *Session) commitQuestProgram(
	ctx context.Context, candidate *sim.LuaObjectiveRuntime,
	program sim.Program, sourceObjectID uint32,
) ([]Update, error) {
	nextDropID := e.nextDropID
	if nextDropID == 0 {
		nextDropID = 1
	}
	dataProgram := program
	dataProgram.Steps = nil
	drops := make([]DropRequest, 0, 1)
	for stepIndex, step := range program.Steps {
		emit, isEmit := step.(sim.EmitStep)
		if !isEmit {
			return nil, fmt.Errorf("quest step unsupported[%d]: %T", stepIndex, step)
		}
		switch intent := emit.Intent.(type) {
		case sim.ObjectiveDataIntent:
			if intent.ObjectiveID != LootCrystalsID {
				return nil, errors.New("quest token objective mismatch")
			}
			dataProgram.Steps = append(dataProgram.Steps, emit)
		case sim.ObjectiveDropIntent:
			if sourceObjectID == 0 || intent.SourceObjectID != sourceObjectID || len(drops) != 0 ||
				nextDropID == math.MaxUint64 || len(e.pendingDrops) >= 8 {
				return nil, errors.New("quest drop batch invalid")
			}
			drop := DropRequest{
				ID: nextDropID, ObjectiveID: intent.ObjectiveID, NounID: intent.NounID,
				SourceObjectID: intent.SourceObjectID, SourcePosition: intent.SourcePosition,
				SourceFootprintRadius: intent.SourceFootprintRadius,
			}
			err := validateDropRequest(drop)
			if err != nil {
				return nil, fmt.Errorf("questDrop[%d]: %w", stepIndex, err)
			}
			drops = append(drops, drop)
			nextDropID++
		default:
			return nil, fmt.Errorf("quest intent unsupported[%d]: %T", stepIndex, emit.Intent)
		}
	}
	snapshots := e.state.Snapshot()
	seeds := make([]sim.ObjectiveSeed, 0, len(snapshots))
	for _, snapshot := range snapshots {
		seeds = append(seeds, sim.ObjectiveSeed{ObjectiveID: snapshot.ObjectiveID})
	}
	state, err := sim.NewObjectiveState(seeds)
	if err != nil {
		return nil, fmt.Errorf("questSeed: %w", err)
	}
	err = state.Restore(snapshots)
	if err != nil {
		return nil, fmt.Errorf("questProjection: %w", err)
	}
	updates, err := applyProgram(ctx, state, LootCrystalsID, dataProgram)
	if err != nil {
		return nil, fmt.Errorf("questData: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("questContext: %w", err)
	}
	err = e.state.Restore(state.Snapshot())
	if err != nil {
		return nil, fmt.Errorf("questCommit: %w", err)
	}
	// Nothing can reject after this point: private threshold/count, UI and
	// pending world ownership advance together under the same session lock.
	e.runtimes[LootCrystalsID] = candidate
	e.pendingDrops = append(e.pendingDrops, drops...)
	e.nextDropID = nextDropID
	return updates, nil
}
