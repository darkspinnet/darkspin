package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// Use the installed client's existing localized horde banners. These transient
// events travel with the accepted encounter transition, never a rejoin baseline.
func marshalCampaignHordeAlert(clientEventID uint32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ClientEventMessage{
		ClientEventID: clientEventID,
	})
	if err != nil {
		return nil, fmt.Errorf("hordeAlert: %w", err)
	}
	return packet, nil
}
