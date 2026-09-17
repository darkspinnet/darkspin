package game

import (
	"sort"
	"sync"
)

type AttributeType uint8

const (
	AttributeStrength AttributeType = iota
	AttributeDexterity
	AttributeMind
	AttributeMaxHealthIncrease
	AttributeMaxHealth
	AttributeMaxMana
	AttributeDamageReduction
	AttributePhysicalDefense
	AttributePhysicalDamageReduction
	AttributeEnergyDefense
	AttributeCriticalRating
	AttributeBodyScale AttributeType = 113
)

type AttributeValue struct {
	Index uint8
	Value float32
}

// Attributes tracks sparse values plus reflection changes.
type Attributes struct {
	mu                  sync.RWMutex
	values              map[uint8]float32
	changedAttributes   map[uint8]struct{}
	erasedAttributes    map[uint8]struct{}
	minimumWeaponDamage float32
	maximumWeaponDamage float32
}

func NewAttributes() *Attributes {
	return &Attributes{values: make(map[uint8]float32), changedAttributes: make(map[uint8]struct{}), erasedAttributes: make(map[uint8]struct{})}
}

func (a *Attributes) Value(attribute AttributeType) float32 {
	a.mu.RLock()
	value := a.values[uint8(attribute)]
	a.mu.RUnlock()
	return value
}

func (a *Attributes) Set(attribute AttributeType, value float32) {
	index := uint8(attribute)
	a.mu.Lock()
	if value == 0 {
		delete(a.values, index)
		a.erasedAttributes[index] = struct{}{}
	} else {
		a.values[index] = value
		delete(a.erasedAttributes, index)
	}
	a.changedAttributes[index] = struct{}{}
	a.mu.Unlock()
}

func (a *Attributes) Values() []AttributeValue {
	a.mu.RLock()
	values := make([]AttributeValue, 0, len(a.values))
	for index, value := range a.values {
		values = append(values, AttributeValue{Index: index, Value: value})
	}
	a.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return values[i].Index < values[j].Index })
	return values
}

func (a *Attributes) SetWeaponDamage(minimum, maximum float32) {
	a.mu.Lock()
	a.minimumWeaponDamage = minimum
	a.maximumWeaponDamage = maximum
	a.changedAttributes[111] = struct{}{}
	a.changedAttributes[112] = struct{}{}
	a.mu.Unlock()
}

func (a *Attributes) WeaponDamage() (float32, float32) {
	a.mu.RLock()
	minimum, maximum := a.minimumWeaponDamage, a.maximumWeaponDamage
	a.mu.RUnlock()
	return minimum, maximum
}

func (a *Attributes) ResetReflection() {
	a.mu.Lock()
	clear(a.changedAttributes)
	clear(a.erasedAttributes)
	a.mu.Unlock()
}
