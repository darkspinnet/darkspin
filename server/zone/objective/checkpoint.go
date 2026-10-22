package objective

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
)

// Checkpoint captures the UI projection and authored private data under one
// session lock, so one event cannot fall between the two snapshots.
func (e *Session) Checkpoint() ([]sim.ObjectiveSnapshot, []sim.LuaObjectiveSnapshot, error) {
	if e == nil {
		return nil, nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runtimes[LootCrystalsID] != nil {
		return nil, nil, errors.New("quest checkpoint requires outbox state")
	}
	objectives, runtimes, err := e.checkpoint()
	if err != nil {
		return nil, nil, fmt.Errorf("checkpointState: %w", err)
	}
	return objectives, runtimes, nil
}

func (e *Session) checkpoint() ([]sim.ObjectiveSnapshot, []sim.LuaObjectiveSnapshot, error) {
	if e.state == nil {
		return nil, nil, nil
	}
	objectives := e.state.Snapshot()
	runtimes := make([]sim.LuaObjectiveSnapshot, 0, len(objectives))
	for _, objective := range objectives {
		runtime := e.runtimes[objective.ObjectiveID]
		if runtime == nil {
			return nil, nil, fmt.Errorf("objective runtime missing: %#x", objective.ObjectiveID)
		}
		snapshot, err := runtime.SnapshotState()
		if err != nil {
			return nil, nil, fmt.Errorf("checkpointRuntime[%#x]: %w", objective.ObjectiveID, err)
		}
		err = ValidateCheckpointRuntime(snapshot)
		if err != nil {
			return nil, nil, fmt.Errorf("checkpointSchema: %w", err)
		}
		runtimes = append(runtimes, snapshot)
	}
	return objectives, runtimes, nil
}

func (e *Session) RestoreCheckpoint(
	objectives []sim.ObjectiveSnapshot, runtimes []sim.LuaObjectiveSnapshot,
) error {
	if e == nil {
		if len(objectives) == 0 && len(runtimes) == 0 {
			return nil
		}
		return errors.New("objective restore unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runtimes[LootCrystalsID] != nil {
		return errors.New("quest restore requires outbox state")
	}
	err := e.restoreCheckpoint(objectives, runtimes)
	if err != nil {
		return fmt.Errorf("checkpointRestore: %w", err)
	}
	return nil
}

func (e *Session) restoreCheckpoint(
	objectives []sim.ObjectiveSnapshot, snapshots []sim.LuaObjectiveSnapshot,
) error {
	if len(objectives) != len(e.runtimes) || len(snapshots) != len(e.runtimes) {
		return errors.New("objective checkpoint count mismatch")
	}
	if e.state == nil {
		if len(objectives) == 0 {
			return nil
		}
		return errors.New("objective checkpoint unavailable")
	}
	seeds := make([]sim.ObjectiveSeed, 0, len(e.initials))
	for _, initial := range e.initials {
		seeds = append(seeds, sim.ObjectiveSeed{ObjectiveID: initial.ObjectiveID})
	}
	state, err := sim.NewObjectiveState(seeds)
	if err != nil {
		return fmt.Errorf("restoreSeed: %w", err)
	}
	objectives = append([]sim.ObjectiveSnapshot(nil), objectives...)
	for index := range objectives {
		if !isPlayerObjective(objectives[index].ObjectiveID) {
			continue
		}
		for _, initial := range e.initials {
			if initial.ObjectiveID != objectives[index].ObjectiveID {
				continue
			}
			for playerIndex, medal := range objectives[index].State {
				if medal == 0 {
					objectives[index].Token[playerIndex] = initial.Token[playerIndex]
				}
			}
		}
	}
	err = state.Restore(objectives)
	if err != nil {
		return fmt.Errorf("restoreProjection: %w", err)
	}
	runtimes := make(map[uint32]*sim.LuaObjectiveRuntime, len(snapshots))
	for _, snapshot := range snapshots {
		err = ValidateCheckpointRuntime(snapshot)
		if err != nil {
			return fmt.Errorf("restoreSchema: %w", err)
		}
		runtime := e.runtimes[snapshot.ObjectiveID]
		if runtime == nil || runtimes[snapshot.ObjectiveID] != nil {
			return fmt.Errorf("objective checkpoint identity: %#x", snapshot.ObjectiveID)
		}
		candidate, restoreErr := runtime.RestoredState(snapshot)
		if restoreErr != nil {
			return fmt.Errorf("restoreRuntime[%#x]: %w", snapshot.ObjectiveID, restoreErr)
		}
		runtimes[snapshot.ObjectiveID] = candidate
	}
	// All work above is isolated. A rejected later record cannot leave an
	// earlier runtime or UI record partially restored.
	err = e.state.Restore(state.Snapshot())
	if err != nil {
		return fmt.Errorf("restoreCommit: %w", err)
	}
	e.runtimes = runtimes
	return nil
}

// ValidateCheckpointRuntime admits only the reviewed private-state schemas.
// Bytecode changes require an explicit review of state and closure ownership.
func ValidateCheckpointRuntime(snapshot sim.LuaObjectiveSnapshot) error {
	err := validateRuntimeIdentity(snapshot)
	if err != nil {
		return fmt.Errorf("checkpointIdentity: %w", err)
	}
	err = sim.ValidateLuaObjectiveSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("checkpointPrivate: %w", err)
	}
	if snapshot.ObjectiveID == sameTypeID {
		err = validateSameTypeState(snapshot)
		if err != nil {
			return fmt.Errorf("checkpointPlayer: %w", err)
		}
	}
	return nil
}

func validateRuntimeIdentity(snapshot sim.LuaObjectiveSnapshot) error {
	var chunkID int64
	var sha256 string
	switch snapshot.ObjectiveID {
	case ObeliskID:
		chunkID = 5
		sha256 = "e168a19cf64bddaff0b483b7d1b2546011d14cc4a72b5528331105c4b2133047"
	case defeatAllMonstersID:
		chunkID = 384
		sha256 = "fda2e11e8da8f72c7415c061ee35b98d62a2d8ebbc71d0ce2ebaaf583a96f70a"
	case hugeDamageID:
		chunkID = 708
		sha256 = "0031aa46c8b68375d6b8976a0655a5c41bc8e1dbef70d1bb4703e30fa1877bfc"
	case finishLevelQuicklyID:
		chunkID = 754
		sha256 = "cb9020a40265832803e62418ca1141fff829373d57b2f3ef8b644ee77bdc875c"
	case damageOftenID:
		chunkID = 902
		sha256 = "8441e30aa93c5e61cb09cb2817bfc40a6a4fca856aea24645c673dd3d6721db4"
	case healthObeliskID:
		chunkID = 163
		sha256 = "ce065a01ba48775543cbbcb24807465398c2a3a8a1a94af8866c6f2dde99beda"
	case stayAliveID:
		chunkID = 506
		sha256 = "1547e9e5dcda912fc166202042e9d09ac04c54b73fc727e2de37ff7834d8a4ae"
	case sameTypeID:
		chunkID = 849
		sha256 = "8f15ed5adb0e0de1becb91218d36b19f515224eebe9e5dc8fbf2c971564ecec0"
	case LootCrystalsID:
		chunkID = 631
		sha256 = "bb8cf84a351727e30a37596b334850862e9bb040df8cff0137e279585ab9741a"
	default:
		return fmt.Errorf("objective checkpoint schema unsupported: %#x", snapshot.ObjectiveID)
	}
	if snapshot.ChunkID != chunkID || snapshot.SHA256 != sha256 {
		return fmt.Errorf("objective checkpoint chunk mismatch: %d", snapshot.ChunkID)
	}
	return nil
}

func (e *Session) leavePlayer(playerIndex uint8) error {
	if playerIndex >= 4 {
		return fmt.Errorf("objective player index: %d", playerIndex)
	}
	objectives, runtimes, err := e.checkpoint()
	if err != nil {
		return fmt.Errorf("leaveCheckpoint: %w", err)
	}
	for index, runtime := range runtimes {
		runtimes[index], err = WithoutPlayer(runtime, playerIndex)
		if err != nil {
			return fmt.Errorf("leavePrivate[%#x]: %w", runtime.ObjectiveID, err)
		}
	}
	for index := range objectives {
		objectives[index].State[playerIndex] = 0
		if !isPlayerObjective(objectives[index].ObjectiveID) {
			// Group progress and the shared elapsed clock remain available to
			// a replacement member; they are not an individual contribution.
			continue
		}
		for _, initial := range e.initials {
			if initial.ObjectiveID == objectives[index].ObjectiveID {
				objectives[index].Token[playerIndex] = initial.Token[playerIndex]
				break
			}
		}
	}
	err = e.restoreCheckpoint(objectives, runtimes)
	if err != nil {
		return fmt.Errorf("leaveRestore: %w", err)
	}
	return nil
}

// WithoutPlayer resets reviewed per-player private state. Shared counters and
// thresholds survive permanent departure; retained reconnects never call it.
func WithoutPlayer(snapshot sim.LuaObjectiveSnapshot, playerIndex uint8) (sim.LuaObjectiveSnapshot, error) {
	if playerIndex >= 4 {
		return sim.LuaObjectiveSnapshot{}, fmt.Errorf("objective player index: %d", playerIndex)
	}
	switch snapshot.ObjectiveID {
	case finishLevelQuicklyID, defeatAllMonstersID, ObeliskID, healthObeliskID, LootCrystalsID:
		return snapshot.Clone(), nil
	case sameTypeID:
		reset, err := resetSameTypePlayer(snapshot, playerIndex)
		if err != nil {
			return sim.LuaObjectiveSnapshot{}, fmt.Errorf("leaveSameType: %w", err)
		}
		return reset, nil
	case hugeDamageID, damageOftenID, stayAliveID:
		if len(snapshot.PrivateTables) == 0 {
			return sim.LuaObjectiveSnapshot{}, errors.New("objective private root unavailable")
		}
		snapshot = snapshot.Clone()
		fields := make([]sim.LuaObjectiveField, 0, len(snapshot.PrivateTables[0].Fields))
		for _, field := range snapshot.PrivateTables[0].Fields {
			if field.Key.Kind == "number" && field.Key.Number == float64(playerIndex) {
				continue
			}
			fields = append(fields, field)
		}
		snapshot.PrivateTables[0].Fields = fields
		return snapshot, nil
	default:
		return sim.LuaObjectiveSnapshot{}, fmt.Errorf("objective player reset unsupported: %#x", snapshot.ObjectiveID)
	}
}
