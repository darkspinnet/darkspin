package effect

import (
	"errors"
	"fmt"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
)

const (
	modifierPoolCapacity     = 2048
	nounModifierGeneration   = uint16(0xfffe)
	nounAffixGenerationStart = nounModifierGeneration - game.MaxCampaignNPCAffixCount
)

type modifierSlot struct {
	generation  uint16
	isAllocated bool
}

// ModifierPool reproduces build 103's 32-bit component-216 handle shape.
type ModifierPool struct {
	mutex          sync.Mutex
	slot           [modifierPoolCapacity]modifierSlot
	freeSlots      []uint16
	nextGeneration uint16
}

// NewModifierPool creates a pool with every handle slot available.
func NewModifierPool() *ModifierPool {
	freeSlots := make([]uint16, modifierPoolCapacity)
	for index := range freeSlots {
		freeSlots[modifierPoolCapacity-1-index] = uint16(index)
	}
	return &ModifierPool{freeSlots: freeSlots}
}

// Allocate reserves a modifier handle.
func (p *ModifierPool) Allocate() (uint32, error) {
	if p == nil {
		return 0, errors.New("nil modifier pool")
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if len(p.freeSlots) == 0 {
		return 0, errors.New("modifier pool exhausted")
	}
	last := len(p.freeSlots) - 1
	slot := p.freeSlots[last]
	p.freeSlots = p.freeSlots[:last]
	p.nextGeneration++
	for p.nextGeneration == 0 ||
		(p.nextGeneration >= nounAffixGenerationStart && p.nextGeneration <= nounModifierGeneration) {
		p.nextGeneration++
	}
	p.slot[slot] = modifierSlot{
		generation: p.nextGeneration, isAllocated: true,
	}
	return uint32(p.nextGeneration)<<16 | uint32(slot), nil
}

// NounModifierInstanceID returns the stable handle used by permanent modifiers
// constructed with an NPC noun rather than allocated by an ability runtime.
func NounModifierInstanceID(objectID uint32) uint32 {
	return uint32(nounModifierGeneration)<<16 | uint32(uint16(objectID))
}

// NounAffixModifierInstanceID gives each authored affix a distinct permanent
// handle, stable across replay and disjoint from elite and ability modifiers.
func NounAffixModifierInstanceID(objectID uint32, affixIndex int) (uint32, error) {
	if affixIndex < 0 || affixIndex >= game.MaxCampaignNPCAffixCount {
		return 0, errors.New("noun affix index invalid")
	}
	generation := nounAffixGenerationStart + uint16(affixIndex)
	return uint32(generation)<<16 | uint32(uint16(objectID)), nil
}

// Release returns a live modifier handle to the pool.
func (p *ModifierPool) Release(instanceID uint32) error {
	if p == nil {
		return errors.New("nil modifier pool")
	}
	slot := uint16(instanceID)
	generation := uint16(instanceID >> 16)
	if int(slot) >= len(p.slot) || generation == 0 {
		return fmt.Errorf("instance ID invalid: %#x", instanceID)
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	state := p.slot[slot]
	if !state.isAllocated || state.generation != generation {
		return fmt.Errorf("instance ID stale: %#x", instanceID)
	}
	p.slot[slot].isAllocated = false
	p.freeSlots = append(p.freeSlots, slot)
	return nil
}
