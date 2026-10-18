package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

func marshalTimeLapseImpact(result zoneability.AreaResult) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset:    util.HashID(result.Definition.HitEffectName),
		ObjectID: result.Damage.ObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("timeLapseEffect: %w", err)
	}
	return packet, nil
}
