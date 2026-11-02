package zone

// HumanParticipantCount retains disconnected humans without counting socketless
// combat actors as reward recipients or loading participants.
func (e *Zone) HumanParticipantCount() uint32 {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.humanParticipantCount()
}

func (e *Zone) humanParticipantCount() uint32 {
	count := uint32(0)
	for _, member := range e.members {
		if !member.IsServerBot {
			count++
		}
	}
	return count
}
