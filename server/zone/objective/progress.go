package objective

import (
	"errors"
	"fmt"
	"sync"

	"github.com/darkspinnet/darkspin/server/sim"
)

type Progress struct {
	mu                sync.RWMutex
	npcObjectIDs      map[uint32]struct{}
	defeatedObjectIDs map[uint32]struct{}
}

func NewProgress(npcObjectIDs []uint32) (*Progress, error) {
	progress := &Progress{
		npcObjectIDs:      make(map[uint32]struct{}, len(npcObjectIDs)),
		defeatedObjectIDs: make(map[uint32]struct{}, len(npcObjectIDs)),
	}
	for index, objectID := range npcObjectIDs {
		if objectID == 0 {
			return nil, fmt.Errorf("npcID[%d]: zero", index)
		}
		if _, isFound := progress.npcObjectIDs[objectID]; isFound {
			return nil, fmt.Errorf("npcID[%d]: duplicate %d", index, objectID)
		}
		progress.npcObjectIDs[objectID] = struct{}{}
	}
	return progress, nil
}

// Register adds newly admitted, completion-relevant NPCs to the shared
// objective population. Dynamic summons and fixtures can be omitted by the
// zone before they reach this boundary.
func (p *Progress) Register(npcObjectIDs []uint32) error {
	if p == nil {
		return errors.New("nil objective progress")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for index, objectID := range npcObjectIDs {
		if objectID == 0 {
			return fmt.Errorf("npcID[%d]: zero", index)
		}
		p.npcObjectIDs[objectID] = struct{}{}
	}
	return nil
}

// Restore rebuilds completion accounting from durable NPC state without
// replaying packaged objective callbacks whose token state was checkpointed.
func (p *Progress) Restore(
	npcObjectIDs []uint32, defeatedObjectIDs []uint32,
) error {
	if p == nil {
		return errors.New("nil objective progress")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.npcObjectIDs)
	clear(p.defeatedObjectIDs)
	for index, objectID := range npcObjectIDs {
		if objectID == 0 {
			return fmt.Errorf("npcID[%d]: zero", index)
		}
		p.npcObjectIDs[objectID] = struct{}{}
	}
	for index, objectID := range defeatedObjectIDs {
		if _, isFound := p.npcObjectIDs[objectID]; !isFound {
			return fmt.Errorf("defeatedNPC[%d]: unknown %d", index, objectID)
		}
		p.defeatedObjectIDs[objectID] = struct{}{}
	}
	return nil
}

func (p *Progress) RecordDeath(
	objectID uint32,
) ([]sim.LuaObjectiveEvent, bool) {
	if p == nil || objectID == 0 {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, isFound := p.npcObjectIDs[objectID]; !isFound {
		return nil, false
	}
	if _, isFound := p.defeatedObjectIDs[objectID]; isFound {
		return nil, false
	}
	p.defeatedObjectIDs[objectID] = struct{}{}
	killPercent := float64(len(p.defeatedObjectIDs)) /
		float64(len(p.npcObjectIDs))
	events := []sim.LuaObjectiveEvent{{
		Kind:        sim.LuaObjectiveEventDeath,
		ObjectID:    objectID,
		KillPercent: killPercent,
	}}
	return events, true
}

func (p *Progress) KillPercent() float64 {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.npcObjectIDs) == 0 {
		return 0
	}
	return float64(len(p.defeatedObjectIDs)) / float64(len(p.npcObjectIDs))
}

func (p *Progress) NPCObjectIDs() []uint32 {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	objectID := make([]uint32, 0, len(p.npcObjectIDs))
	for current := range p.npcObjectIDs {
		objectID = append(objectID, current)
	}
	return objectID
}
