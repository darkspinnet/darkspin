package sim

import (
	"container/heap"
	"errors"
	"fmt"
	"time"
)

// TaskID identifies a pending deterministic continuation. Zero is invalid.
type TaskID uint64

// CancelScope ties a continuation to the current session, phase, and optional
// object lifetime.
type CancelScope struct {
	sessionGeneration uint64
	phaseGeneration   uint64
	role              Role
	roleGeneration    uint64
}

type continuation func(*Simulator) error

type scheduledTask struct {
	id       TaskID
	deadline time.Duration
	sequence uint64
	scope    CancelScope
	run      continuation
	index    int
}

type taskHeap []*scheduledTask

func (h taskHeap) Len() int { return len(h) }

func (h taskHeap) Less(left, right int) bool {
	if h[left].deadline != h[right].deadline {
		return h[left].deadline < h[right].deadline
	}
	return h[left].sequence < h[right].sequence
}

func (h taskHeap) Swap(left, right int) {
	h[left], h[right] = h[right], h[left]
	h[left].index = left
	h[right].index = right
}

func (h *taskHeap) Push(item any) {
	task := item.(*scheduledTask)
	task.index = len(*h)
	*h = append(*h, task)
}

func (h *taskHeap) Pop() any {
	old := *h
	last := len(old) - 1
	task := old[last]
	old[last] = nil
	task.index = -1
	*h = old[:last]
	return task
}

// Simulator owns one session's monotonic clock, scheduled continuations, role
// lifetimes, and semantic event trace. It starts no goroutines.
type Simulator struct {
	now               time.Duration
	phase             Phase
	sessionGeneration uint64
	phaseGeneration   uint64
	roleGenerations   map[Role]uint64
	tasks             taskHeap
	tasksByID         map[TaskID]*scheduledTask
	events            []Event
	nextEventSequence uint64
	nextTaskID        TaskID
	nextSequence      uint64
	isStopped         bool
}

// New creates a deterministic session at time zero.
func New(initialPhase Phase) *Simulator {
	simulator := &Simulator{
		phase: initialPhase, sessionGeneration: 1, phaseGeneration: 1,
		roleGenerations: make(map[Role]uint64), tasksByID: make(map[TaskID]*scheduledTask),
	}
	heap.Init(&simulator.tasks)
	return simulator
}

func (s *Simulator) Now() time.Duration { return s.now }

func (s *Simulator) Phase() Phase { return s.phase }

func (s *Simulator) Events() []Event { return append([]Event(nil), s.events...) }

func (s *Simulator) PendingTaskCount() int { return len(s.tasksByID) }

// Scope captures the current phase and optional role lifetime.
func (s *Simulator) Scope(role Role) CancelScope {
	return CancelScope{
		sessionGeneration: s.sessionGeneration,
		phaseGeneration:   s.phaseGeneration,
		role:              role,
		roleGeneration:    s.roleGenerations[role],
	}
}

// ActivateRole begins a new lifetime for a stable role.
func (s *Simulator) ActivateRole(role Role) CancelScope {
	if role != "" {
		s.roleGenerations[role]++
	}
	return s.Scope(role)
}

// InvalidateRole cancels every continuation tied to the role's current
// lifetime.
func (s *Simulator) InvalidateRole(role Role) {
	if role == "" {
		return
	}
	s.roleGenerations[role]++
}

// Emit records a typed semantic intent at the current simulation time.
func (s *Simulator) Emit(intent Intent, provenance Provenance) error {
	err := s.EmitScoped(intent, provenance, s.Scope(""))
	if err != nil {
		return fmt.Errorf("emitScoped: %w", err)
	}
	return nil
}

// EmitScoped records a typed semantic intent with its cancellation scope.
func (s *Simulator) EmitScoped(intent Intent, provenance Provenance, scope CancelScope) error {
	if intent == nil {
		return errors.New("nil intent")
	}
	if s.isStopped {
		return errors.New("simulator stopped")
	}
	if scope.sessionGeneration != s.sessionGeneration || scope.phaseGeneration != s.phaseGeneration {
		return errors.New("inactive scope")
	}
	if scope.role != "" && scope.roleGeneration != s.roleGenerations[scope.role] {
		return errors.New("inactive role scope")
	}
	s.nextEventSequence++
	s.events = append(s.events, Event{
		ID:       EventID{Epoch: s.sessionGeneration, Sequence: s.nextEventSequence},
		Sequence: s.nextEventSequence, At: s.now, Phase: s.phase,
		Intent: intent, Provenance: provenance, Scope: scope,
	})
	return nil
}

// Schedule queues a continuation relative to the current simulation time.
func (s *Simulator) Schedule(delay time.Duration, scope CancelScope, run continuation) (TaskID, error) {
	if delay < 0 {
		return 0, errors.New("negative delay")
	}
	if run == nil {
		return 0, errors.New("nil continuation")
	}
	if s.isStopped {
		return 0, errors.New("simulator stopped")
	}
	s.nextTaskID++
	if s.nextTaskID == 0 {
		s.nextTaskID++
	}
	s.nextSequence++
	task := &scheduledTask{
		id: s.nextTaskID, deadline: s.now + delay, sequence: s.nextSequence,
		scope: scope, run: run,
	}
	s.tasksByID[task.id] = task
	heap.Push(&s.tasks, task)
	return task.id, nil
}

// Cancel removes one pending continuation.
func (s *Simulator) Cancel(id TaskID) {
	task := s.tasksByID[id]
	if task == nil {
		return
	}
	delete(s.tasksByID, id)
	heap.Remove(&s.tasks, task.index)
}

// AdvanceBy advances the deterministic clock and runs due continuations.
func (s *Simulator) AdvanceBy(duration time.Duration) error {
	if duration < 0 {
		return errors.New("negative advance")
	}
	err := s.AdvanceTo(s.now + duration)
	if err != nil {
		return fmt.Errorf("advanceTo: %w", err)
	}
	return nil
}

// AdvanceTo runs valid continuations in stable deadline/insertion order.
func (s *Simulator) AdvanceTo(deadline time.Duration) error {
	if deadline < s.now {
		return fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, s.now)
	}
	if s.isStopped {
		return errors.New("simulator stopped")
	}
	for len(s.tasks) > 0 && s.tasks[0].deadline <= deadline {
		task := heap.Pop(&s.tasks).(*scheduledTask)
		delete(s.tasksByID, task.id)
		s.now = task.deadline
		if !s.isScopeActive(task.scope) {
			continue
		}
		err := task.run(s)
		if err != nil {
			return fmt.Errorf("taskRun[%d]: %w", task.id, err)
		}
	}
	s.now = deadline
	return nil
}

// Reset creates a fresh session epoch and discards all old continuations,
// roles, and trace events.
func (s *Simulator) Reset(initialPhase Phase) {
	s.now = 0
	s.phase = initialPhase
	s.sessionGeneration++
	s.phaseGeneration++
	s.roleGenerations = make(map[Role]uint64)
	s.tasks = nil
	heap.Init(&s.tasks)
	s.tasksByID = make(map[TaskID]*scheduledTask)
	s.events = nil
	s.nextEventSequence = 0
	s.isStopped = false
}

// Stop permanently discards pending work until Reset starts a new epoch.
func (s *Simulator) Stop() {
	s.isStopped = true
	s.tasks = nil
	s.tasksByID = make(map[TaskID]*scheduledTask)
}

func (s *Simulator) transition(phase Phase) {
	s.phase = phase
	s.phaseGeneration++
}

func (s *Simulator) isScopeActive(scope CancelScope) bool {
	if scope.sessionGeneration != s.sessionGeneration || scope.phaseGeneration != s.phaseGeneration {
		return false
	}
	if scope.role == "" {
		return true
	}
	return scope.roleGeneration == s.roleGenerations[scope.role]
}
