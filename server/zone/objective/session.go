package objective

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

type completionResult struct {
	objectiveID uint32
	playerIndex uint8
	medal       uint8
	program     sim.Program
}

type Session struct {
	mu       sync.Mutex
	state    *sim.ObjectiveState
	runtimes map[uint32]*sim.LuaObjectiveRuntime
}

// Update is one immutable, protocol-independent objective projection.
type Update struct {
	ObjectiveID uint32
	PlayerIndex uint8
	Medal       uint8
	Token       [3]uint32
}

func NewSessionState(
	state *sim.ObjectiveState,
	runtimes map[uint32]*sim.LuaObjectiveRuntime,
) (*Session, error) {
	if state == nil {
		return nil, errors.New("objective state unavailable")
	}
	if len(runtimes) == 0 {
		return nil, errors.New("objective runtime unavailable")
	}
	return &Session{state: state, runtimes: runtimes}, nil
}

func NewSession(
	ctx context.Context, inputs []sim.LuaObjectiveInput,
	objects []game.CampaignScriptObject, playerIndex uint8,
	modes ...game.Mode,
) (*Session, error) {
	if len(modes) != 0 && modes[0] == game.ModeTutorial {
		interactableCount := LootInteractableCount(objects)
		if interactableCount == 0 {
			return &Session{}, nil
		}
		state, runtimes, err := NewObelisk(
			ctx, inputs, interactableCount, playerIndex,
		)
		if err != nil {
			return nil, fmt.Errorf("sessionTutorial: %w", err)
		}
		return &Session{state: state, runtimes: runtimes}, nil
	}
	state, runtimes, err := NewLevel(ctx, inputs, objects, playerIndex)
	if err != nil {
		return nil, fmt.Errorf("sessionLevel: %w", err)
	}
	return &Session{state: state, runtimes: runtimes}, nil
}

func (s *Session) Join(playerIndex uint8) error {
	if s == nil {
		return errors.New("objective join: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		return nil
	}
	for index, objective := range s.state.Snapshot() {
		err := s.state.ActivatePlayer(objective.ObjectiveID, playerIndex)
		if err != nil {
			return fmt.Errorf("objectiveJoin[%d]: %w", index, err)
		}
	}
	return nil
}

// Leave removes one permanently departed co-op slot. Transport disconnects do
// not call this method, so a member's retained rejoin window keeps its progress.
func (s *Session) Leave(playerIndex uint8) error {
	if s == nil {
		return errors.New("objective leave: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		return nil
	}
	for index, objective := range s.state.Snapshot() {
		err := s.state.DeactivatePlayer(objective.ObjectiveID, playerIndex)
		if err != nil {
			return fmt.Errorf("objectiveLeave[%d]: %w", index, err)
		}
	}
	return nil
}

func (s *Session) State() *sim.ObjectiveState {
	if s == nil {
		return nil
	}
	return s.state
}

// Snapshot returns one immutable copy of the shared objective projection.
func (s *Session) Snapshot() []sim.ObjectiveSnapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		return nil
	}
	return s.state.Snapshot()
}

func (s *Session) Restore(snapshots []sim.ObjectiveSnapshot) error {
	if s == nil || s.state == nil {
		if len(snapshots) == 0 {
			return nil
		}
		return errors.New("objective restore unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.state.Restore(snapshots)
	if err != nil {
		return fmt.Errorf("objectiveRestore: %w", err)
	}
	return nil
}

func (s *Session) Apply(
	ctx context.Context, objectiveID uint32, event sim.LuaObjectiveEvent,
) ([]Update, error) {
	if ctx == nil {
		return nil, errors.New("objective apply: nil context")
	}
	if s == nil || s.state == nil || len(s.runtimes) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return ApplyState(ctx, s.state, s.runtimes, objectiveID, event)
}

// ApplyEvent routes one semantic zone event through every selected packaged
// objective that registered an event callback. Timer- and status-only
// objectives retain their independent progression paths.
func (s *Session) ApplyEvent(
	ctx context.Context, event sim.LuaObjectiveEvent,
) ([]Update, error) {
	if ctx == nil {
		return nil, errors.New("objective event: nil context")
	}
	if s == nil || s.state == nil || len(s.runtimes) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	objectiveIDs := make([]uint32, 0, len(s.runtimes))
	for objectiveID := range s.runtimes {
		objectiveIDs = append(objectiveIDs, objectiveID)
	}
	sort.Slice(objectiveIDs, func(left int, right int) bool {
		return objectiveIDs[left] < objectiveIDs[right]
	})
	updates := make([]Update, 0)
	for _, objectiveID := range objectiveIDs {
		objectiveRuntime := s.runtimes[objectiveID]
		if objectiveRuntime == nil || !objectiveRuntime.IsEventSubscriber() {
			continue
		}
		current, err := ApplyState(ctx, s.state, s.runtimes, objectiveID, event)
		if err != nil {
			return nil, fmt.Errorf("objectiveApply[%#x]: %w", objectiveID, err)
		}
		updates = append(updates, current...)
	}
	return updates, nil
}

// Invoke calls one proven custom callback on a selected packaged objective.
func (s *Session) Invoke(
	ctx context.Context, objectiveID uint32, callbackName string,
) ([]Update, error) {
	if ctx == nil {
		return nil, errors.New("objective invoke: nil context")
	}
	if s == nil || s.state == nil || len(s.runtimes) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	objectiveRuntime := s.runtimes[objectiveID]
	if objectiveRuntime == nil {
		return nil, nil
	}
	program, err := objectiveRuntime.Invoke(callbackName)
	if err != nil {
		return nil, fmt.Errorf("objectiveInvoke[%#x]: %w", objectiveID, err)
	}
	updates, err := applyProgram(ctx, s.state, objectiveID, program)
	if err != nil {
		return nil, fmt.Errorf("objectiveInvokeApply[%#x]: %w", objectiveID, err)
	}
	return updates, nil
}

// UpdateElapsed projects the live campaign duration through token zero of the
// packaged FinishLevelQuickly objective. Its ObjectiveStatus callback still
// owns the final medal; this keeps the Objectives Log's time presentation in
// step with the shared zone clock while the campaign is active.
func (s *Session) UpdateElapsed(
	ctx context.Context, elapsed time.Duration,
) ([]Update, error) {
	if ctx == nil {
		return nil, errors.New("objective elapsed: nil context")
	}
	if s == nil || s.state == nil {
		return nil, nil
	}
	if elapsed < 0 {
		elapsed = 0
	}
	elapsedSeconds := int64(elapsed / time.Second)
	if elapsedSeconds > int64(math.MaxInt32) {
		elapsedSeconds = int64(math.MaxInt32)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var finishObjective sim.ObjectiveSnapshot
	isFinishObjectiveFound := false
	for _, objective := range s.state.Snapshot() {
		if objective.ObjectiveID != finishLevelQuicklyID {
			continue
		}
		finishObjective = objective
		isFinishObjectiveFound = true
		break
	}
	if !isFinishObjectiveFound {
		return nil, nil
	}
	elapsedInteger := int32(elapsedSeconds)
	isChanged := false
	for playerIndex, medal := range finishObjective.State {
		if medal != 0 && finishObjective.Token[playerIndex][0] != elapsedInteger {
			isChanged = true
			break
		}
	}
	if !isChanged {
		return nil, nil
	}
	intent := sim.ObjectiveDataIntent{
		ObjectiveID: finishLevelQuicklyID,
		PlayerIndex: sim.ObjectiveAllPlayers,
		TokenIndex:  0,
		Integer:     elapsedInteger,
	}
	err := s.state.SetObjectiveData(ctx, sim.EventMeta{}, intent)
	if err != nil {
		return nil, fmt.Errorf("objectiveElapsedData: %w", err)
	}
	updates, err := Updates(s.state, intent)
	if err != nil {
		return nil, fmt.Errorf("objectiveElapsedUpdate: %w", err)
	}
	return updates, nil
}

// Complete evaluates every selected objective once for each active player.
// Each valid result commits independently so one optional callback cannot
// suppress the other authored medals.
func (s *Session) Complete(
	ctx context.Context, completionTime time.Duration, killPercent float64,
) ([]Update, error) {
	if ctx == nil {
		return nil, errors.New("objective complete: nil context")
	}
	if s == nil || s.state == nil || len(s.runtimes) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshots := s.state.Snapshot()
	results := make([]completionResult, 0, len(snapshots)*4)
	completionErrors := make([]error, 0)
	for _, snapshot := range snapshots {
		objectiveRuntime := s.runtimes[snapshot.ObjectiveID]
		if objectiveRuntime == nil {
			completionErrors = append(completionErrors, fmt.Errorf(
				"objectiveRuntime[%#x]: unavailable", snapshot.ObjectiveID,
			))
			continue
		}
		for playerIndex, state := range snapshot.State {
			if state == 0 {
				continue
			}
			result, err := objectiveRuntime.EvaluateStatus(sim.LuaObjectiveStatusInput{
				PlayerIndex: uint8(playerIndex), CompletionTime: completionTime,
				KillPercent: killPercent,
			})
			if err != nil {
				completionErrors = append(completionErrors, fmt.Errorf(
					"objectiveStatus[%#x:%d]: %w",
					snapshot.ObjectiveID, playerIndex, err,
				))
				continue
			}
			medal, err := objectiveMedal(result.Status)
			if err != nil {
				completionErrors = append(completionErrors, fmt.Errorf(
					"objectiveMedal[%#x:%d]: %w",
					snapshot.ObjectiveID, playerIndex, err,
				))
				continue
			}
			results = append(results, completionResult{
				objectiveID: snapshot.ObjectiveID, playerIndex: uint8(playerIndex),
				medal: medal, program: result.Program,
			})
		}
	}
	updates := make([]Update, 0, len(results))
	for resultIndex, result := range results {
		isResultValid := true
		intents := make([]sim.ObjectiveDataIntent, 0, len(result.program.Steps))
		for stepIndex, step := range result.program.Steps {
			emit, isEmit := step.(sim.EmitStep)
			if !isEmit {
				completionErrors = append(completionErrors, fmt.Errorf(
					"objectiveCompleteStep[%d:%d]: %T",
					resultIndex, stepIndex, step,
				))
				isResultValid = false
				break
			}
			intent, isObjectiveData := emit.Intent.(sim.ObjectiveDataIntent)
			if !isObjectiveData || intent.ObjectiveID != result.objectiveID {
				completionErrors = append(completionErrors, fmt.Errorf(
					"objectiveCompleteIntent[%d:%d]: %T",
					resultIndex, stepIndex, emit.Intent,
				))
				isResultValid = false
				break
			}
			intents = append(intents, intent)
		}
		if !isResultValid {
			continue
		}
		for stepIndex, intent := range intents {
			err := s.state.SetObjectiveData(ctx, sim.EventMeta{}, intent)
			if err != nil {
				completionErrors = append(completionErrors, fmt.Errorf(
					"objectiveCompleteData[%d:%d]: %w",
					resultIndex, stepIndex, err,
				))
				isResultValid = false
				break
			}
		}
		if !isResultValid {
			continue
		}
		err := s.state.SetMedal(
			result.objectiveID, result.playerIndex, result.medal,
		)
		if err != nil {
			completionErrors = append(completionErrors, fmt.Errorf(
				"objectiveCompleteMedal[%d]: %w", resultIndex, err,
			))
			continue
		}
		current, err := Updates(s.state, sim.ObjectiveDataIntent{
			ObjectiveID: result.objectiveID, PlayerIndex: result.playerIndex,
		})
		if err != nil {
			completionErrors = append(completionErrors, fmt.Errorf(
				"objectiveCompleteUpdate[%d]: %w", resultIndex, err,
			))
			continue
		}
		updates = append(updates, current...)
	}
	return updates, errors.Join(completionErrors...)
}

func objectiveMedal(status sim.LuaObjectiveStatus) (uint8, error) {
	switch status {
	case sim.LuaObjectiveStatusFailed:
		return 1, nil
	case sim.LuaObjectiveStatusBronze:
		return 2, nil
	case sim.LuaObjectiveStatusSilver:
		return 3, nil
	case sim.LuaObjectiveStatusGold:
		return 4, nil
	default:
		return 0, fmt.Errorf("unknown status %q", status)
	}
}

func ApplyState(
	ctx context.Context,
	state *sim.ObjectiveState,
	runtimes map[uint32]*sim.LuaObjectiveRuntime,
	objectiveID uint32,
	event sim.LuaObjectiveEvent,
) ([]Update, error) {
	if ctx == nil {
		return nil, errors.New("objective apply: nil context")
	}
	if state == nil || len(runtimes) == 0 {
		return nil, nil
	}
	objectiveRuntime := runtimes[objectiveID]
	if objectiveRuntime == nil {
		return nil, fmt.Errorf("objectiveRuntime: %#x", objectiveID)
	}
	program, err := objectiveRuntime.HandleEvent(event)
	if err != nil {
		return nil, fmt.Errorf("objectiveEvent: %w", err)
	}
	updates, err := applyProgram(ctx, state, objectiveID, program)
	if err != nil {
		return nil, fmt.Errorf("objectiveProgram: %w", err)
	}
	return updates, nil
}

func applyProgram(
	ctx context.Context, state *sim.ObjectiveState,
	objectiveID uint32, program sim.Program,
) ([]Update, error) {
	updates := make([]Update, 0, len(program.Steps))
	for stepIndex, step := range program.Steps {
		emit, isEmit := step.(sim.EmitStep)
		if !isEmit {
			return nil, fmt.Errorf("objectiveStep[%d]: %T", stepIndex, step)
		}
		intent, isObjectiveData := emit.Intent.(sim.ObjectiveDataIntent)
		if !isObjectiveData || intent.ObjectiveID != objectiveID {
			return nil, fmt.Errorf("objectiveIntent[%d]: %T", stepIndex, emit.Intent)
		}
		err := state.SetObjectiveData(ctx, sim.EventMeta{}, intent)
		if err != nil {
			return nil, fmt.Errorf("objectiveData[%d]: %w", stepIndex, err)
		}
		current, err := Updates(state, intent)
		if err != nil {
			return nil, fmt.Errorf("objectiveUpdate[%d]: %w", stepIndex, err)
		}
		updates = append(updates, current...)
	}
	return updates, nil
}

func Updates(
	state *sim.ObjectiveState, intent sim.ObjectiveDataIntent,
) ([]Update, error) {
	if state == nil {
		return nil, errors.New("nil objective state")
	}
	var objective sim.ObjectiveSnapshot
	isFound := false
	for _, snapshot := range state.Snapshot() {
		if snapshot.ObjectiveID == intent.ObjectiveID {
			objective = snapshot
			isFound = true
			break
		}
	}
	if !isFound {
		return nil, fmt.Errorf("objectiveSnapshot: %#x", intent.ObjectiveID)
	}
	playerIndex := make([]uint8, 0, len(objective.State))
	if intent.PlayerIndex != sim.ObjectiveAllPlayers {
		playerIndex = append(playerIndex, intent.PlayerIndex)
	} else {
		for index, medal := range objective.State {
			if medal != 0 {
				playerIndex = append(playerIndex, uint8(index))
			}
		}
	}
	updates := make([]Update, 0, len(playerIndex))
	for _, index := range playerIndex {
		token := [3]uint32{}
		for tokenIndex, integer := range objective.Token[index] {
			token[tokenIndex] = uint32(integer)
		}
		updates = append(updates, Update{
			ObjectiveID: intent.ObjectiveID, PlayerIndex: index,
			Medal: objective.State[index], Token: token,
		})
	}
	return updates, nil
}
