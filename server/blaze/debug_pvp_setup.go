//go:build scenario

package blaze

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

// All recipients, including the host, take the unsolicited user-group path.
// Do not convert the host to a create callback or fabricate matchmaking IDs.
func queueDebugPVPSetup(req *Request, instance *game.Instance, sessionsByUsers map[int64]*Session) error {
	players := instance.Players()
	// Match the production PvP roster projection: UID and HSES identify
	// accepted Blaze sessions, not persistent account IDs.
	playerSessionIDsByUser := make(map[int64]uint32, len(players))
	for _, player := range players {
		session := sessionsByUsers[player.Account.ID]
		if session == nil || session.ID == 0 {
			return errors.New("PvP setup requires the pinned participant's Blaze session")
		}
		playerSessionIDsByUser[player.Account.ID] = session.ID
	}
	for _, player := range players {
		session := sessionsByUsers[player.Account.ID]
		if session == nil || player.CurrentPlaygroupID() == 0 {
			return errors.New("PvP setup requires the pinned participant's playgroup")
		}
		hostNetwork, err := sessionGameHostNetwork(session, instance.Info.HostNetwork)
		if err != nil {
			return fmt.Errorf("pvpHostNetwork: %w", err)
		}
		fields, err := gameSetupFields(instance, player, gameSetupContextIndirectJoin,
			0, player.CurrentPlaygroupID(), hostNetwork, playerSessionIDsByUser)
		if err != nil {
			return fmt.Errorf("pvpSetupFields: %w", err)
		}
		err = req.QueueSessionNotification(session, GameManagerComponentID, 0x14, fields)
		if err != nil {
			return fmt.Errorf("pvpSetupQueue: %w", err)
		}
	}
	return nil
}
