package gameplay

import "log"

// A charge replaces the client's ordinary movement attributes. Retire that
// override before another action can cancel its scheduled impact restoration.
// Every caller holds the registry lock, just like animation retirement.
func (e *heroChargeRun) resetChargeSpeedLocked() [][]byte {
	e.mutex.Lock()
	if !e.isSpeedActive {
		e.mutex.Unlock()
		return nil
	}
	e.isSpeedActive = false
	e.mutex.Unlock()
	if e.registry != nil {
		isCurrent := false
		for _, member := range e.registry.sessions {
			if member.zone == e.zone && member.binding.UserID == e.userID &&
				member.generation == e.generation && member.heroCharge == e &&
				member.deployedObjectID == e.ownerObjectID {
				isCurrent = true
				break
			}
		}
		if !isCurrent {
			return nil
		}
	}
	packet, err := heroChargeSpeedPacket(e.ownerObjectID, zonePlayerMoveSpeed)
	if err != nil {
		log.Printf("RakNet charge speed retirement failed owner=%d: %v", e.ownerObjectID, err)
		return nil
	}
	packets := [][]byte{packet}
	if e.registry != nil {
		e.registry.queueChargePacketsLocked(e.zone, packets)
		return nil
	}
	return packets
}
