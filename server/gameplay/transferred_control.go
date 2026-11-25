package gameplay

import (
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The transferred modifier owns its recipient contribution even when generic
// dispel, replacement, death, or transport cleanup retires the run.
type transferredControl struct {
	npc        *zonenpc.Session
	objectID   uint32
	instanceID uint32
	modifierID uint32
}

func (e *transferredControl) clear() {
	if e == nil {
		return
	}
	clearFieldMedicNPCStatus(e.npc, e.objectID, e.instanceID, e.modifierID, time.Time{})
}
