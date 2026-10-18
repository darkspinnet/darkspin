package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	interactraknet "github.com/darkspinnet/darkspin/server/zone/interact/raknet103"
)

func (e gameplayPeerSession) pickupFlight(nounName string, source sim.Position, destination sim.Position, lob sim.CrystalLob) sim.DropFlight {
	return sim.DropFlight{
		Source: source, Destination: destination, Lob: lob,
		StartedAt: time.Now(), Position: source, MovementType: 4,
		IsProjectilePresent: e.zone.DirectorDefinition().IsProjectileNoun(nounName),
	}
}

func marshalPickupFlight(objectID uint32, flight sim.DropFlight, isRejoin bool) ([][]byte, error) {
	if flight.StartedAt.IsZero() || flight.IsProjectilePresent {
		return nil, nil
	}
	position := raknet.Vector3(flight.Position)
	movement := raknet.Vector3(flight.Movement)
	movementType := flight.MovementType
	transform, err := raknet.MarshalApplication(raknet.ObjectUpdateContractMessage{
		ObjectID: objectID, Object: raknet.ObjectReflection{
			Position: &position, LinearVelocity: &movement, MovementType: &movementType,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("flightTransform: %w", err)
	}
	if !isRejoin || movementType != 4 {
		return [][]byte{transform}, nil
	}
	message, err := interactraknet.CrystalLobLocomotion(sim.CrystalPickupRequest{
		Role: "pickup", Position: flight.Source, Destination: flight.Destination, Lob: flight.Lob,
	}, objectID)
	if err != nil {
		return nil, fmt.Errorf("flightLob: %w", err)
	}
	lob, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("flightMarshal: %w", err)
	}
	return [][]byte{transform, lob}, nil
}
