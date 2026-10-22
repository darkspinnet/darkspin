package zone

// NPCParticipantCount uses the same admitted party membership as enemy health
// and shield scaling; a transport reconnect does not create another participant.
func (e *Zone) NPCParticipantCount() uint32 {
	if e == nil {
		return 1
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return max(uint32(1), uint32(len(e.members)))
}
