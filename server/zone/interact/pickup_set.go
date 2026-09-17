package interact

// PickupSet owns the lifecycle of pickup object identities independently from
// contact geometry, reward persistence, and wire presentation.
type PickupSet struct {
	registeredObjectIDs map[uint32]bool
	reservedObjectIDs   map[uint32]bool
	collectedObjectIDs  map[uint32]bool
}

func NewPickupSet() *PickupSet {
	return &PickupSet{
		registeredObjectIDs: make(map[uint32]bool),
		reservedObjectIDs:   make(map[uint32]bool),
		collectedObjectIDs:  make(map[uint32]bool),
	}
}

func (s *PickupSet) Register(objectID uint32) bool {
	if s == nil || objectID == 0 || s.registeredObjectIDs[objectID] ||
		s.reservedObjectIDs[objectID] || s.collectedObjectIDs[objectID] {
		return false
	}
	s.registeredObjectIDs[objectID] = true
	return true
}

func (s *PickupSet) IsAvailable(objectID uint32) bool {
	return s != nil && s.registeredObjectIDs[objectID] &&
		!s.reservedObjectIDs[objectID] && !s.collectedObjectIDs[objectID]
}

func (s *PickupSet) IsReserved(objectID uint32) bool {
	return s != nil && s.reservedObjectIDs[objectID]
}

func (s *PickupSet) IsCollected(objectID uint32) bool {
	return s != nil && s.collectedObjectIDs[objectID]
}

func (s *PickupSet) Reserve(objectID uint32) bool {
	if !s.IsAvailable(objectID) {
		return false
	}
	delete(s.registeredObjectIDs, objectID)
	s.reservedObjectIDs[objectID] = true
	return true
}

func (s *PickupSet) Release(objectID uint32) bool {
	if s == nil || !s.reservedObjectIDs[objectID] {
		return false
	}
	delete(s.reservedObjectIDs, objectID)
	s.registeredObjectIDs[objectID] = true
	return true
}

func (s *PickupSet) Commit(objectID uint32) bool {
	if s == nil || !s.reservedObjectIDs[objectID] {
		return false
	}
	delete(s.reservedObjectIDs, objectID)
	s.collectedObjectIDs[objectID] = true
	return true
}

// Collect commits an already-admitted scripted contact without introducing a
// second reservation boundary.
func (s *PickupSet) Collect(objectID uint32) bool {
	if !s.IsAvailable(objectID) {
		return false
	}
	delete(s.registeredObjectIDs, objectID)
	s.collectedObjectIDs[objectID] = true
	return true
}

func (s *PickupSet) Restore(objectID uint32) bool {
	if s == nil || !s.collectedObjectIDs[objectID] {
		return false
	}
	delete(s.collectedObjectIDs, objectID)
	s.registeredObjectIDs[objectID] = true
	return true
}

func (s *PickupSet) Remove(objectID uint32) bool {
	if s == nil || objectID == 0 {
		return false
	}
	isFound := s.registeredObjectIDs[objectID] || s.reservedObjectIDs[objectID] ||
		s.collectedObjectIDs[objectID]
	delete(s.registeredObjectIDs, objectID)
	delete(s.reservedObjectIDs, objectID)
	delete(s.collectedObjectIDs, objectID)
	return isFound
}
