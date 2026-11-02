//go:build scenario

package main

import "github.com/darkspinnet/darkspin/server/scenario"

type debugWorldPickup struct {
	ObjectID           uint32             `json:"object_id"`
	Kind               string             `json:"kind"`
	Position           debugWorldPosition `json:"position"`
	Destination        debugWorldPosition `json:"destination"`
	AvailableInMS      int64              `json:"available_in_ms"`
	ExpiresInMS        int64              `json:"expires_in_ms"`
	IsCollectible      bool               `json:"is_collectible"`
	IsReserved         bool               `json:"is_reserved"`
	IsExpired          bool               `json:"is_expired"`
	IsServerRegistered bool               `json:"is_server_registered"`
	DistanceSquared    float64            `json:"distance_squared"`
	PickupRadius       float32            `json:"pickup_radius"`
}

func debugWorldPickupDTO(pickup scenario.WorldPickup) debugWorldPickup {
	return debugWorldPickup{ObjectID: pickup.ObjectID, Kind: pickup.Kind, Position: debugWorldPositionDTO(pickup.Position), Destination: debugWorldPositionDTO(pickup.Destination),
		AvailableInMS: pickup.AvailableInMS, ExpiresInMS: pickup.ExpiresInMS, IsCollectible: pickup.IsCollectible, IsReserved: pickup.IsReserved, IsExpired: pickup.IsExpired, IsServerRegistered: pickup.IsServerRegistered,
		DistanceSquared: pickup.DistanceSquared, PickupRadius: pickup.PickupRadius}
}
