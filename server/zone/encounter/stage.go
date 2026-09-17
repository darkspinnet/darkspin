package encounter

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type StagePlan struct {
	Initial        int
	TerminalStages []int
}

type StageTransition struct {
	Stage      int
	IsAdvanced bool
	IsComplete bool
}

type stageState struct {
	stage          int
	terminalStages []int
	isPublished    bool
}

type StageSession struct {
	mu          sync.RWMutex
	statesByKey map[string]*stageState
}

type StageSnapshot struct {
	Key            string
	Stage          int
	TerminalStages []int
	IsPublished    bool
}

func NewStageSession() *StageSession {
	return &StageSession{statesByKey: make(map[string]*stageState)}
}

func (e *StageSession) Register(key string, plan StagePlan) error {
	if e == nil {
		return errors.New("encounter stage: nil session")
	}
	key = strings.ToLower(key)
	if key == "" || len(plan.TerminalStages) == 0 {
		return errors.New("encounter stage: invalid registration")
	}
	terminalStages := append([]int(nil), plan.TerminalStages...)
	slices.Sort(terminalStages)
	terminalStages = slices.Compact(terminalStages)
	if plan.Initial < 0 || terminalStages[len(terminalStages)-1] < plan.Initial {
		return errors.New("encounter stage: invalid terminal")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.statesByKey[key]
	if current != nil && current.stage == plan.Initial &&
		slices.Equal(current.terminalStages, terminalStages) {
		return nil
	}
	if current != nil {
		return errors.New("encounter stage: already registered")
	}
	e.statesByKey[key] = &stageState{
		stage: plan.Initial, terminalStages: terminalStages,
	}
	return nil
}

func (e *StageSession) Stage(key string) (int, bool) {
	if e == nil {
		return 0, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	state := e.statesByKey[strings.ToLower(key)]
	if state == nil {
		return 0, false
	}
	return state.stage, true
}

func (e *StageSession) IsPublished(key string) bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	state := e.statesByKey[strings.ToLower(key)]
	return state != nil && state.isPublished
}

func (e *StageSession) Snapshots() []StageSnapshot {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	snapshots := make([]StageSnapshot, 0, len(e.statesByKey))
	for key, state := range e.statesByKey {
		if state == nil {
			continue
		}
		snapshots = append(snapshots, StageSnapshot{
			Key: key, Stage: state.stage,
			TerminalStages: append([]int(nil), state.terminalStages...),
			IsPublished:    state.isPublished,
		})
	}
	e.mu.RUnlock()
	slices.SortFunc(snapshots, func(left StageSnapshot, right StageSnapshot) int {
		return strings.Compare(left.Key, right.Key)
	})
	return snapshots
}

func (e *StageSession) MarkPublished(key string) error {
	if e == nil {
		return errors.New("encounter publication: nil session")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.statesByKey[strings.ToLower(key)]
	if state == nil {
		return errors.New("encounter publication: not registered")
	}
	state.isPublished = true
	return nil
}

func (e *StageSession) Set(key string, stage int) (StageTransition, error) {
	if e == nil {
		return StageTransition{}, errors.New("encounter stage: nil session")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.statesByKey[strings.ToLower(key)]
	if state == nil {
		return StageTransition{}, errors.New("encounter stage: not registered")
	}
	if stage < 0 ||
		stage > state.terminalStages[len(state.terminalStages)-1] ||
		stage == state.stage {
		return StageTransition{}, fmt.Errorf("encounter stage: invalid stage %d", stage)
	}
	state.stage = stage
	return StageTransition{Stage: stage, IsAdvanced: true}, nil
}

func (e *StageSession) ObserveClear(
	key string, isStageClear bool,
) (StageTransition, error) {
	if e == nil {
		return StageTransition{}, errors.New("encounter stage: nil session")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.statesByKey[strings.ToLower(key)]
	if state == nil {
		return StageTransition{}, errors.New("encounter stage: not registered")
	}
	transition := StageTransition{Stage: state.stage}
	if !isStageClear {
		return transition, nil
	}
	if slices.Contains(state.terminalStages, state.stage) {
		transition.IsComplete = true
		return transition, nil
	}
	state.stage++
	transition.Stage = state.stage
	transition.IsAdvanced = true
	return transition, nil
}
