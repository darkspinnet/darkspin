package gameplay

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// These authored casts select their own range/target admission. Do not make
// their availability depend on the ordinary attack's pursuit stop distance.
func (e campaignNPCActionRuntime) producePriorityNPCAbility(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, timestamp uint64,
) ([][]byte, bool, error) {
	if len(zonenpc.SupportSpells(source.Plan)) != 0 {
		return e.produceNPCSupport(packet, sessionKey, generation, source.Plan.ObjectID, timestamp)
	}
	if zonenpc.IsNashiraNoun(source.Plan.NounName) {
		packets, isHandled, err := e.produceNashiraPanic(packet, sessionKey, generation, source.Plan.ObjectID, timestamp)
		if err != nil {
			return nil, false, fmt.Errorf("priorityPanic: %w", err)
		}
		if isHandled {
			return packets, true, nil
		}
		return e.produceNashiraCurse(packet, sessionKey, generation, source.Plan.ObjectID, timestamp)
	}
	if strings.EqualFold(source.Plan.NounName, "CryosBoss_3.Noun") {
		return e.produceMerakStorm(packet, sessionKey, generation, source.Plan.ObjectID, timestamp)
	}
	return nil, false, nil
}
