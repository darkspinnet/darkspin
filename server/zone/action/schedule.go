package action

import (
	"errors"
	"sort"
	"sync"
)

type ScheduleKind uint8

const (
	ScheduleInteractable ScheduleKind = iota + 1
	ScheduleCrystal
	ScheduleEquipment
)

type Stopper interface {
	Stop()
}

type Cleaner interface {
	Cleanup()
}

type scheduleKey struct {
	kind     ScheduleKind
	objectID uint32
}

type schedule struct {
	run     Stopper
	cancel  func()
	cleaner Cleaner
}

// ScheduleSession owns connection-local scheduled action cleanup. World
// admission and pickup state remain owned by the zone feature that created the
// action.
type ScheduleSession struct {
	mu        sync.Mutex
	schedules map[scheduleKey]schedule
}

type ScheduleSnapshot struct {
	Kind              ScheduleKind
	ObjectID          uint32
	IsRunAttached     bool
	IsCleanerAttached bool
}

func NewScheduleSession() *ScheduleSession {
	return &ScheduleSession{schedules: make(map[scheduleKey]schedule)}
}

func (e *ScheduleSession) Add(
	kind ScheduleKind,
	objectID uint32,
	run Stopper,
	cancel func(),
	cleaner Cleaner,
) error {
	if e == nil || kind == 0 || objectID == 0 || cancel == nil {
		return errors.New("invalid action schedule")
	}
	key := scheduleKey{kind: kind, objectID: objectID}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.schedules[key]; isFound {
		return errors.New("duplicate action schedule")
	}
	e.schedules[key] = schedule{run: run, cancel: cancel, cleaner: cleaner}
	return nil
}

func (e *ScheduleSession) Has(kind ScheduleKind, objectID uint32) bool {
	if e == nil || kind == 0 || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, isFound := e.schedules[scheduleKey{kind: kind, objectID: objectID}]
	return isFound
}

func (e *ScheduleSession) Remove(
	kind ScheduleKind,
	objectID uint32,
	run Stopper,
) bool {
	if e == nil || kind == 0 || objectID == 0 {
		return false
	}
	key := scheduleKey{kind: kind, objectID: objectID}
	e.mu.Lock()
	defer e.mu.Unlock()
	current, isFound := e.schedules[key]
	if !isFound || (run != nil && current.run != run) {
		return false
	}
	delete(e.schedules, key)
	return true
}

func (e *ScheduleSession) StopAll() {
	if e == nil {
		return
	}
	e.mu.Lock()
	schedule := make([]schedule, 0, len(e.schedules))
	for key, current := range e.schedules {
		schedule = append(schedule, current)
		delete(e.schedules, key)
	}
	e.mu.Unlock()
	for _, current := range schedule {
		if current.cancel != nil {
			current.cancel()
		}
		if current.run != nil {
			current.run.Stop()
		}
		if current.cleaner != nil {
			current.cleaner.Cleanup()
		}
	}
}

func (e *ScheduleSession) Snapshots() []ScheduleSnapshot {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	snapshots := make([]ScheduleSnapshot, 0, len(e.schedules))
	for key, current := range e.schedules {
		snapshots = append(snapshots, ScheduleSnapshot{
			Kind: key.kind, ObjectID: key.objectID,
			IsRunAttached: current.run != nil, IsCleanerAttached: current.cleaner != nil,
		})
	}
	e.mu.Unlock()
	sort.Slice(snapshots, func(left int, right int) bool {
		if snapshots[left].Kind != snapshots[right].Kind {
			return snapshots[left].Kind < snapshots[right].Kind
		}
		return snapshots[left].ObjectID < snapshots[right].ObjectID
	})
	return snapshots
}
