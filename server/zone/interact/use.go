package interact

import (
	"errors"
	"sort"
	"sync"
)

type UseSession struct {
	mu     sync.Mutex
	limits map[uint32]int32
	counts map[uint32]int32
}

type UseSnapshot struct {
	ObjectID uint32
	Limit    int32
	Count    int32
}

func NewUseSession() *UseSession {
	return &UseSession{
		limits: make(map[uint32]int32),
		counts: make(map[uint32]int32),
	}
}

func (e *UseSession) Register(objectID uint32, limit int32) error {
	if e == nil || objectID == 0 || limit == 0 || limit < -1 {
		return errors.New("interactable registration invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.limits[objectID]; isFound {
		return errors.New("interactable already registered")
	}
	e.limits[objectID] = limit
	return nil
}

func (e *UseSession) Use(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	limit, isFound := e.limits[objectID]
	if !isFound || (limit >= 0 && e.counts[objectID] >= limit) {
		return false
	}
	e.counts[objectID]++
	return true
}

func (e *UseSession) Restore(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.counts[objectID] <= 0 {
		return false
	}
	e.counts[objectID]--
	return true
}

func (e *UseSession) Snapshots() []UseSnapshot {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	snapshots := make([]UseSnapshot, 0, len(e.limits))
	for objectID, limit := range e.limits {
		snapshots = append(snapshots, UseSnapshot{
			ObjectID: objectID, Limit: limit, Count: e.counts[objectID],
		})
	}
	e.mu.Unlock()
	sort.Slice(snapshots, func(left int, right int) bool {
		return snapshots[left].ObjectID < snapshots[right].ObjectID
	})
	return snapshots
}
