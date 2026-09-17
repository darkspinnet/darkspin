package sim

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const ObjectiveAllPlayers = uint8(255)
const objectivePlayerCount = 4
const objectiveTokenCount = 3

// ObjectiveSeed declares one objective and its initial per-player state.
type ObjectiveSeed struct {
	ObjectiveID uint32
	State       [objectivePlayerCount]uint8
}

// ObjectiveSnapshot is protocol-independent transient match state.
type ObjectiveSnapshot struct {
	ObjectiveID uint32
	State       [objectivePlayerCount]uint8
	Token       [objectivePlayerCount][objectiveTokenCount]int32
}

// ObjectiveMutation records one accepted authored token write. The token array
// already reflects kAllPlayers expansion; this retains the original selector
// and opaque flags for later presentation-policy recovery.
type ObjectiveMutation struct {
	ObjectiveDataIntent
}

type objectiveRecord struct {
	snapshot ObjectiveSnapshot
}

// ObjectiveState owns the live, non-persistent objective array for one match.
type ObjectiveState struct {
	mu           sync.RWMutex
	objectiveIDs []uint32
	records      map[uint32]*objectiveRecord
	mutations    []ObjectiveMutation
}

func NewObjectiveState(seeds []ObjectiveSeed) (*ObjectiveState, error) {
	state := &ObjectiveState{
		objectiveIDs: make([]uint32, 0, len(seeds)), records: make(map[uint32]*objectiveRecord, len(seeds)),
	}
	for index, seed := range seeds {
		if seed.ObjectiveID == 0 {
			return nil, fmt.Errorf("seedID[%d]: zero", index)
		}
		if _, isDuplicate := state.records[seed.ObjectiveID]; isDuplicate {
			return nil, fmt.Errorf("seedDuplicate[%d]: %#x", index, seed.ObjectiveID)
		}
		state.objectiveIDs = append(state.objectiveIDs, seed.ObjectiveID)
		state.records[seed.ObjectiveID] = &objectiveRecord{snapshot: ObjectiveSnapshot{
			ObjectiveID: seed.ObjectiveID, State: seed.State,
		}}
	}
	return state, nil
}

func (state *ObjectiveState) SetObjective(
	ctx context.Context, _ EventMeta, intent ObjectiveIntent,
) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("objectiveContext: %w", err)
	}
	if state == nil {
		return errors.New("nil objective state")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	record := state.records[intent.ObjectiveID]
	if record == nil {
		return fmt.Errorf("objectiveUnknown: %#x", intent.ObjectiveID)
	}
	objectiveState := uint8(0)
	if intent.IsActive {
		objectiveState = 1
	}
	for index := range record.snapshot.State {
		record.snapshot.State[index] = objectiveState
	}
	return nil
}

func (state *ObjectiveState) SetObjectiveData(
	ctx context.Context, _ EventMeta, intent ObjectiveDataIntent,
) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("objectiveDataContext: %w", err)
	}
	if state == nil {
		return errors.New("nil objective state")
	}
	if intent.TokenIndex >= objectiveTokenCount {
		return fmt.Errorf("objectiveToken: %d", intent.TokenIndex)
	}
	if intent.PlayerIndex != ObjectiveAllPlayers && intent.PlayerIndex >= objectivePlayerCount {
		return fmt.Errorf("objectivePlayer: %d", intent.PlayerIndex)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	record := state.records[intent.ObjectiveID]
	if record == nil {
		return fmt.Errorf("objectiveUnknown: %#x", intent.ObjectiveID)
	}
	if intent.PlayerIndex == ObjectiveAllPlayers {
		for playerIndex := range record.snapshot.Token {
			record.snapshot.Token[playerIndex][intent.TokenIndex] = intent.Integer
		}
	} else {
		record.snapshot.Token[intent.PlayerIndex][intent.TokenIndex] = intent.Integer
	}
	state.mutations = append(state.mutations, ObjectiveMutation{ObjectiveDataIntent: intent})
	return nil
}

func (state *ObjectiveState) ActivatePlayer(
	objectiveID uint32, playerIndex uint8,
) error {
	if state == nil {
		return errors.New("nil objective state")
	}
	if playerIndex >= objectivePlayerCount {
		return fmt.Errorf("objectivePlayer: %d", playerIndex)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	record := state.records[objectiveID]
	if record == nil {
		return fmt.Errorf("objectiveUnknown: %#x", objectiveID)
	}
	record.snapshot.State[playerIndex] = 1
	return nil
}

func (state *ObjectiveState) DeactivatePlayer(
	objectiveID uint32, playerIndex uint8,
) error {
	if state == nil {
		return errors.New("nil objective state")
	}
	if playerIndex >= objectivePlayerCount {
		return fmt.Errorf("objectivePlayer: %d", playerIndex)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	record := state.records[objectiveID]
	if record == nil {
		return fmt.Errorf("objectiveUnknown: %#x", objectiveID)
	}
	record.snapshot.State[playerIndex] = 0
	record.snapshot.Token[playerIndex] = [objectiveTokenCount]int32{}
	return nil
}

// SetMedal commits one evaluated objective result for an active player.
func (state *ObjectiveState) SetMedal(
	objectiveID uint32, playerIndex uint8, medal uint8,
) error {
	if state == nil {
		return errors.New("nil objective state")
	}
	if playerIndex >= objectivePlayerCount {
		return fmt.Errorf("objectivePlayer: %d", playerIndex)
	}
	if medal < 1 || medal > 4 {
		return fmt.Errorf("objectiveMedal: %d", medal)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	record := state.records[objectiveID]
	if record == nil {
		return fmt.Errorf("objectiveUnknown: %#x", objectiveID)
	}
	if record.snapshot.State[playerIndex] == 0 {
		return errors.New("objective player inactive")
	}
	record.snapshot.State[playerIndex] = medal
	return nil
}

func (state *ObjectiveState) Snapshot() []ObjectiveSnapshot {
	if state == nil {
		return nil
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	snapshot := make([]ObjectiveSnapshot, 0, len(state.objectiveIDs))
	for _, objectiveID := range state.objectiveIDs {
		snapshot = append(snapshot, state.records[objectiveID].snapshot)
	}
	return snapshot
}

// Restore replaces objective values for the same authored objective set. It
// deliberately clears mutation history because presentation resumes from one
// baseline rather than replaying prior events.
func (state *ObjectiveState) Restore(snapshots []ObjectiveSnapshot) error {
	if state == nil {
		return errors.New("nil objective state")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(snapshots) != len(state.objectiveIDs) {
		return errors.New("objective restore count mismatch")
	}
	restored := make(map[uint32]ObjectiveSnapshot, len(snapshots))
	for index, snapshot := range snapshots {
		if snapshot.ObjectiveID == 0 || restored[snapshot.ObjectiveID].ObjectiveID != 0 {
			return fmt.Errorf("objectiveRestore[%d]: invalid", index)
		}
		restored[snapshot.ObjectiveID] = snapshot
	}
	for _, objectiveID := range state.objectiveIDs {
		snapshot, isFound := restored[objectiveID]
		if !isFound {
			return fmt.Errorf("objectiveRestoreMissing: %#x", objectiveID)
		}
		state.records[objectiveID].snapshot = snapshot
	}
	state.mutations = nil
	return nil
}

func (state *ObjectiveState) Mutations() []ObjectiveMutation {
	if state == nil {
		return nil
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return append([]ObjectiveMutation(nil), state.mutations...)
}
