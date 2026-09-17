// Package effect owns transient campaign effect and modifier identities.
package effect

import "sync"

// AttachmentSlotCount is the build-103 per-object attachment capacity.
const AttachmentSlotCount = 16

// AttachmentPool allocates per-object attached-effect slots.
type AttachmentPool struct {
	mutex       sync.Mutex
	objectSlots map[uint32][AttachmentSlotCount]bool
}

// NewAttachmentPool creates an empty attachment-slot pool.
func NewAttachmentPool() *AttachmentPool {
	return &AttachmentPool{
		objectSlots: make(map[uint32][AttachmentSlotCount]bool),
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
	slots[slot] = false
	p.objectSlots[objectID] = slots
	return true
}

// ReleaseObject frees every slot owned by objectID.
func (p *AttachmentPool) ReleaseObject(objectID uint32) {
	if p == nil || objectID == 0 {
		return
	}
	p.mutex.Lock()
	delete(p.objectSlots, objectID)
	p.mutex.Unlock()
}
