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

// IsLockedFor includes account unlocks when evaluating legacy squad records
// whose stored lock flag may still be false for unpurchased slots.
func (e Squad) IsLockedFor(account Account) bool {
	if e.IsLocked || e.Slot == 0 {
		return true
	}
	switch e.Category {
	case "", "pve":
		return e.Slot > max(uint32(1), account.UnlockPVEDecks)
	case "pvp":
		return e.Slot > account.UnlockPVPDecks
	default:
		return true
	}
}
