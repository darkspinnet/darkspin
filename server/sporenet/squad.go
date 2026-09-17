package sporenet

import (
	"strconv"
)

// Squad is a three-creature PVE or PVP deck.
type Squad struct {
	Name        string
	Category    string
	ID          uint32
	Slot        uint32
	IsLocked    bool
	CreatureIDs [3]uint32
}

// NewSquad returns an unlocked default slot.
func NewSquad(slot uint32) Squad {
	return Squad{ID: slot, Slot: slot, Name: "Slot " + strconv.FormatUint(uint64(slot), 10)}
}
