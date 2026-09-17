package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
)

// Refresh both player-owned crystal records and occupied HUD slots. This is
// presentation only: it neither grants a catalyst nor changes slot ownership.
func marshalCrystalRepair(session gameplayPeerSession) ([][]byte, error) {
	packets, err := marshalGameplayCrystalState(session)
	if err != nil {
		return nil, fmt.Errorf("crystalState: %w", err)
	}
	for index, slot := range session.crystalInventory.Slots {
		if !slot.IsOccupied {
			continue
		}
		packet, err := raknet.MarshalApplication(raknet.CrystalAcquiredMessage{
			Slot: int32(index), NounAsset: slot.NounAsset,
			CrystalColor: sim.CrystalColorForNoun(slot.NounName, slot.CrystalType),
			CrystalLevel: slot.CrystalLevel,
		})
		if err != nil {
			return nil, fmt.Errorf("crystalSlot[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
