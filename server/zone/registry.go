package zone

import (
	"errors"
	"fmt"
	"sync"
)

type Registry struct {
	mu             sync.RWMutex
	zones          map[uint64]*Zone
	nextGeneration uint64
}

func NewRegistry() *Registry {
	return &Registry{zones: make(map[uint64]*Zone)}
}

func (r *Registry) Resolve(
	id uint64, member Member, info ZoneInfo,
) (*Zone, bool, error) {
	if r == nil {
		return nil, false, errors.New("nil campaign zone registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, isFound := r.zones[id]
	if isFound {
		err := current.Join(member)
		if err != nil {
			return nil, false, fmt.Errorf("instanceJoin: %w", err)
		}
		return current, false, nil
	}
	r.nextGeneration++
	if r.nextGeneration == 0 {
		r.nextGeneration++
	}
	current, err := New(id, r.nextGeneration, info)
	if err != nil {
		return nil, false, fmt.Errorf("instanceCreate: %w", err)
	}
	current.registry = r
	err = current.Join(member)
	if err != nil {
		return nil, false, fmt.Errorf("instanceJoin: %w", err)
	}
	r.zones[id] = current
	return current, true, nil
}

func (r *Registry) Get(id uint64) (*Zone, bool) {
	if r == nil || id == 0 {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	zone, isFound := r.zones[id]
	return zone, isFound
}

// Discard removes and stops one live zone regardless of retained membership.
// Launcher Start Fresh uses this boundary before deleting its durable
// checkpoint so an in-memory rejoin cannot resurrect the retired mission.
func (r *Registry) Discard(id uint64) bool {
	if r == nil || id == 0 {
		return false
	}
	r.mu.Lock()
	current, isFound := r.zones[id]
	if isFound {
		delete(r.zones, id)
	}
	r.mu.Unlock()
	if !isFound {
		return false
	}
	current.Discard()
	return true
}

func (r *Registry) retire(expected *Zone) {
	if r == nil || expected == nil {
		return
	}
	snapshot := expected.Snapshot()
	if snapshot.ID == 0 || len(snapshot.Members) != 0 {
		return
	}
	r.mu.Lock()
	if r.zones[snapshot.ID] == expected {
		delete(r.zones, snapshot.ID)
	}
	r.mu.Unlock()
}
