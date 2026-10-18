//go:build !scenario

package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type scenarioPeerPreparation struct{}

func (e *gameplayPeerSession) recordScenarioPrepareQueued(packet []byte, queuedAt time.Time) {}

func (e *gameplayPeerSession) recordScenarioPrepareStatus(
	packet raknet.Packet, status raknet.PlayerStatus, receivedAt time.Time,
) {
}

func (e *gameplaySessionRegistry) recordScenarioPrepareResponse(
	packet raknet.Packet, peerSession gameplayPeerSession, setupPacket []byte,
) {
}

func (e *gameplaySessionRegistry) recordScenarioPrepareAccepted(
	packet raknet.Packet, peerSession gameplayPeerSession, status raknet.PlayerStatus,
) {
}
