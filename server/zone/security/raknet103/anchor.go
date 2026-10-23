package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
)

// Anchor retains the authored teleporter's render model. Native 103
// sub_5057B0 silently skips force-attached effects without a render component;
// SweepingBeamMarker's null model cannot own these persistent effect slots.
func Anchor(objectID uint32, teleport zonesecurity.Teleport) ([]byte, error) {
	return AnchorWithRotation(objectID, teleport, raknet.Vector3{})
}

// AnchorWithRotation preserves the authored placement of campaign portals.
func AnchorWithRotation(objectID uint32, teleport zonesecurity.Teleport, rotation raknet.Vector3) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("security anchor object invalid")
	}
	nounName := "SecurityTeleporter.Noun"
	if teleport.IsBoss {
		nounName = "BossSecurityTeleporter.Noun"
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID(nounName),
		PositionX: teleport.Source.X, PositionY: teleport.Source.Y, PositionZ: teleport.Source.Z,
		Rotation: rotation,
		Scale:    1, IsCollisionEnabled: false,
	})
	if err != nil {
		return nil, fmt.Errorf("anchorMarshal: %w", err)
	}
	return packet, nil
}
