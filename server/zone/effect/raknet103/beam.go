package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

type BeamRequest struct {
	AssetID        uint32
	SourceObjectID uint32
	TargetPoint    game.Vec3
}

// Beam preserves Notify's objectId and targetPoint arguments. Position is
// the effect origin and cannot substitute for the beam's target point.
func Beam(req BeamRequest) ([]byte, error) {
	if req.AssetID == 0 || req.SourceObjectID == 0 {
		return nil, errors.New("beam identity invalid")
	}
	endpoint := raknet.Vector3{X: req.TargetPoint.X, Y: req.TargetPoint.Y, Z: req.TargetPoint.Z}
	packet, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{
		Asset: &req.AssetID, ObjectID: &req.SourceObjectID, TargetPoint: &endpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("beamMarshal: %w", err)
	}
	return packet, nil
}
