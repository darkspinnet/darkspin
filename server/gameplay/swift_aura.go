package gameplay

import (
	"fmt"

	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e gameplayPendingRuntime) pollSwiftAurasLocked(member *gameplayPeerSession) ([][]byte, error) {
	changes := member.zone.NPCs().RefreshSwift()
	packets, err := npcraknet.SwiftChanges(changes)
	if err != nil {
		return nil, fmt.Errorf("swiftChanges: %w", err)
	}
	if len(packets) == 0 {
		return nil, nil
	}
	for sessionKey, ally := range e.registry.sessions {
		if ally.zone != member.zone || ally.binding.UserID == member.binding.UserID {
			continue
		}
		ally.queuePackets(packets)
		e.registry.sessions[sessionKey] = ally
	}
	return packets, nil
}
