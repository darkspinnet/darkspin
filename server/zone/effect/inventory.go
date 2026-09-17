package effect

import (
	"errors"
	"sort"
	"sync"
	"time"
)

type ModifierKind uint8

const (
	ModifierKindBuff ModifierKind = iota + 1
	ModifierKindDebuff
)

type Modifier struct {
	InstanceID                uint32
	GUID                      uint32
	SourceObjectID            uint32
	TargetObjectID            uint32
	Rank                      uint32
	Duration                  time.Duration
	Kind                      ModifierKind
	IsChannel                 bool
	IsHaste                   bool
	InitiatorObject           uint32
	StackCount                uint32
	DamageBuff                float32
	EnergyDamageBuff          float32
	EnergyDamageTakenIncrease float32
	HealingReduction          float32
	AttackSpeed               float32
	CooldownReduction         float32
	MovementSpeedBuff         float32
}

func (e *Inventory) Update(modifier Modifier) error {
	if e == nil || modifier.InstanceID == 0 || modifier.GUID == 0 ||
		modifier.SourceObjectID == 0 || modifier.TargetObjectID == 0 ||
		modifier.Rank == 0 || modifier.Duration <= 0 ||
		modifier.Kind != ModifierKindBuff && modifier.Kind != ModifierKindDebuff {
		return errors.New("modifier inventory entry invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.modifiers[modifier.InstanceID]; !isFound {
		return errors.New("modifier inventory entry missing")
	}
	e.modifiers[modifier.InstanceID] = modifier
	return nil
}

type Inventory struct {
	mu        sync.RWMutex
	modifiers map[uint32]Modifier
}

func NewInventory() *Inventory {
	return &Inventory{modifiers: make(map[uint32]Modifier)}
}

func (e *Inventory) Put(modifier Modifier) error {
	if e == nil || modifier.InstanceID == 0 || modifier.GUID == 0 ||
		modifier.SourceObjectID == 0 || modifier.TargetObjectID == 0 ||
		modifier.Rank == 0 || modifier.Duration <= 0 ||
		modifier.Kind != ModifierKindBuff && modifier.Kind != ModifierKindDebuff {
		return errors.New("modifier inventory entry invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isFound := e.modifiers[modifier.InstanceID]; isFound {
		return errors.New("modifier inventory entry exists")
	}
	e.modifiers[modifier.InstanceID] = modifier
	return nil
}

func (e *Inventory) Remove(instanceID uint32) (Modifier, bool) {
	if e == nil || instanceID == 0 {
		return Modifier{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	modifier, isFound := e.modifiers[instanceID]
	if isFound {
		delete(e.modifiers, instanceID)
	}
	return modifier, isFound
}

func (e *Inventory) Snapshot() []Modifier {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	modifier := make([]Modifier, 0, len(e.modifiers))
	for _, current := range e.modifiers {
		modifier = append(modifier, current)
	}
	sort.Slice(modifier, func(left int, right int) bool {
		return modifier[left].InstanceID < modifier[right].InstanceID
	})
	return modifier
}
