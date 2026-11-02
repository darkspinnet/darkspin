package raknet103

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	interactraknet "github.com/darkspinnet/darkspin/server/zone/interact/raknet103"
	"github.com/darkspinnet/darkspin/server/zone/quest"
)

func SpawnAt(crystal quest.Crystal, at time.Time, sourceTime uint64) ([][]byte, error) {
	crystal.Flight = crystal.Flight.At(at)
	crystal.FlightElapsed = min(500*time.Millisecond, max(time.Duration(0), at.Sub(crystal.Flight.StartedAt)))
	create, err := raknet.MarshalApplication(raknet.EnemyObjectCreateMessage{
		ObjectID: crystal.ObjectID, Noun: crystal.Request.NounID,
		Position: raknet.Vector3(crystal.Flight.Position), Scale: 1,
		IsCollidable: true, MovementType: crystal.Flight.MovementType})
	if err != nil {
		return nil, fmt.Errorf("questCreate: %w", err)
	}
	interact, err := raknet.MarshalApplication(raknet.InteractableDataUpdateMessage{
		ObjectID: crystal.ObjectID, UsesAllowed: 1, Ability: util.HashID(quest.PickupAbility)})
	if err != nil {
		return nil, fmt.Errorf("questInteractable: %w", err)
	}
	packets := [][]byte{create, interact}
	if crystal.Flight.MovementType == 4 {
		lobPlan := crystal.Flight.Lob
		elapsed := uint64(crystal.FlightElapsed / time.Millisecond)
		startTime := uint64(0)
		if sourceTime >= elapsed {
			startTime = sourceTime - elapsed
		}
		lobPlan.StartTime = time.Duration(startTime) * time.Millisecond
		locomotion, locErr := interactraknet.CrystalLobLocomotion(sim.CrystalPickupRequest{
			Role: "questCrystal", Position: crystal.Flight.Source, Destination: crystal.Flight.Destination,
			Lob: lobPlan}, crystal.ObjectID)
		if locErr != nil {
			return nil, fmt.Errorf("questLocomotion: %w", locErr)
		}
		lob, marshalErr := raknet.MarshalApplication(locomotion)
		if marshalErr != nil {
			return nil, fmt.Errorf("questFlight: %w", marshalErr)
		}
		packets = append(packets, lob)
	}
	return packets, nil
}
