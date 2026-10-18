package security

import "errors"

// AnchorObjectID returns the shared presentation owner for an authored source.
// Marker IDs identify definitions, rather than replicated client objects.
func (e *Session) AnchorObjectID(sourceID uint32) (uint32, bool) {
	if e == nil || sourceID == 0 {
		return 0, false
	}
	e.mutex.RLock()
	objectID, isFound := e.anchorsBySourceID[sourceID]
	e.mutex.RUnlock()
	return objectID, isFound
}

// BindAnchor retains one zone-allocated object per source across peer baselines.
// Racing reservations may consume an unused ID, but never replace a live owner.
func (e *Session) BindAnchor(sourceID, objectID uint32) (uint32, error) {
	if e == nil || sourceID == 0 || objectID == 0 {
		return 0, errors.New("security anchor invalid")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	retainedObjectID, isFound := e.anchorsBySourceID[sourceID]
	if isFound {
		return retainedObjectID, nil
	}
	if e.anchorsBySourceID == nil {
		e.anchorsBySourceID = make(map[uint32]uint32)
	}
	e.anchorsBySourceID[sourceID] = objectID
	return objectID, nil
}
