package gameplay

import zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"

// The modifier retains its companion contribution through every retirement path.
type companionSilenceControl struct {
	companion  *zonecompanion.Session
	objectID   uint32
	instanceID uint32
}

func (e *companionSilenceControl) clear() {
	if e == nil {
		return
	}
	e.companion.ClearSilenceModifier(e.objectID, e.instanceID)
}
