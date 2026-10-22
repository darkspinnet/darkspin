// Package effect owns transient campaign effect and modifier identities.
package effect

import "sync"

// AttachmentSlotCount is the build-103 per-object attachment capacity.
const AttachmentSlotCount = 16

// AttachmentPool allocates per-object attached-effect slots.
type AttachmentPool struct {
	mutex       sync.Mutex
	objectSlots map[uint32][AttachmentSlotCount]bool
	heldSlots   map[uint32][AttachmentSlotCount]bool
}

// NewAttachmentPool creates an empty attachment-slot pool.
func NewAttachmentPool() *AttachmentPool {
	return &AttachmentPool{
		objectSlots: make(map[uint32][AttachmentSlotCount]bool),
		heldSlots:   make(map[uint32][AttachmentSlotCount]bool),
	}
}

// Allocate reserves the first free slot for objectID.
func (p *AttachmentPool) Allocate(objectID uint32) (uint8, bool) {
	if p == nil || objectID == 0 {
		return 0, false
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	slots := p.objectSlots[objectID]
	for index, isAllocated := range slots {
		if isAllocated {
			continue
		}
		slots[index] = true
		p.objectSlots[objectID] = slots
		return uint8(index), true
	}
	return 0, false
}

// AllocateHeld atomically reserves an attachment and its delivery lifetime.
// Blanket actor cleanup cannot make it available before CommitRemoval.
func (e *AttachmentPool) AllocateHeld(objectID uint32) (uint8, bool) {
	if e == nil || objectID == 0 {
		return 0, false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	slots := e.objectSlots[objectID]
	for index, isAllocated := range slots {
		if isAllocated {
			continue
		}
		slots[index] = true
		e.objectSlots[objectID] = slots
		heldSlots := e.heldSlots[objectID]
		heldSlots[index] = true
		e.heldSlots[objectID] = heldSlots
		return uint8(index), true
	}
	return 0, false
}

// Release frees slot for objectID.
func (p *AttachmentPool) Release(objectID uint32, slot uint8) bool {
	if p == nil || objectID == 0 || slot >= AttachmentSlotCount {
		return false
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	slots, isFound := p.objectSlots[objectID]
	if !isFound || !slots[slot] {
		return false
	}
	if p.heldSlots[objectID][slot] {
		return false
	}
	slots[slot] = false
	p.objectSlots[objectID] = slots
	p.pruneObject(objectID)
	return true
}

// ReleaseObject frees unheld slots owned by objectID.
func (p *AttachmentPool) ReleaseObject(objectID uint32) {
	if p == nil || objectID == 0 {
		return
	}
	p.mutex.Lock()
	p.objectSlots[objectID] = p.heldSlots[objectID]
	p.pruneObject(objectID)
	p.mutex.Unlock()
}

// ReleaseObjectExcept retires abandoned action effects while keeping slots
// explicitly retained by a modifier or an uncommitted deletion lease.
func (p *AttachmentPool) ReleaseObjectExcept(objectID uint32, retainedSlots []uint8) {
	if p == nil || objectID == 0 {
		return
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	slots := p.objectSlots[objectID]
	retainedFlags := p.heldSlots[objectID]
	for _, slot := range retainedSlots {
		if slot < AttachmentSlotCount && slots[slot] {
			retainedFlags[slot] = true
		}
	}
	p.objectSlots[objectID] = retainedFlags
	p.pruneObject(objectID)
}

// HoldRemoval keeps a slot reserved until every deletion reaches transport commit.
func (e *AttachmentPool) HoldRemoval(objectID uint32, slot uint8) bool {
	if e == nil || objectID == 0 || slot >= AttachmentSlotCount {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if !e.objectSlots[objectID][slot] || e.heldSlots[objectID][slot] {
		return false
	}
	slots := e.heldSlots[objectID]
	slots[slot] = true
	e.heldSlots[objectID] = slots
	return true
}

// CommitRemoval releases a reservation after every recipient commits delivery.
func (e *AttachmentPool) CommitRemoval(objectID uint32, slot uint8) bool {
	if e == nil || objectID == 0 || slot >= AttachmentSlotCount {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	slots := e.heldSlots[objectID]
	if !slots[slot] {
		return false
	}
	slots[slot] = false
	e.heldSlots[objectID] = slots
	slots = e.objectSlots[objectID]
	slots[slot] = false
	e.objectSlots[objectID] = slots
	e.pruneObject(objectID)
	return true
}

// Called with the pool mutex held. Empty object records have no ownership.
func (e *AttachmentPool) pruneObject(objectID uint32) {
	isAllocated := false
	for _, isSlotAllocated := range e.objectSlots[objectID] {
		if isSlotAllocated {
			isAllocated = true
			break
		}
	}
	if !isAllocated {
		delete(e.objectSlots, objectID)
	}
	isHeld := false
	for _, isSlotHeld := range e.heldSlots[objectID] {
		if isSlotHeld {
			isHeld = true
			break
		}
	}
	if !isHeld {
		delete(e.heldSlots, objectID)
	}
}
