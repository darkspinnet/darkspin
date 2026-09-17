package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

func AbilityCount(slot uint16, abilityCount uint32) ([]byte, error) {
	publication, err := zoneunlock.NewAbilityPublication(slot, abilityCount)
	if err != nil {
		return nil, fmt.Errorf("abilityPublication: %w", err)
	}
	packet, err := raknet.MarshalApplication(raknet.LabsPlayerAbilityCountMessage{
		Slot:         uint8(publication.Slot),
		AbilityCount: publication.AbilityCount,
	})
	if err != nil {
		return nil, fmt.Errorf("abilityMarshal: %w", err)
	}
	return packet, nil
}
