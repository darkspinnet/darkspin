//go:build scenario

package scenario

// WorldPickup observes server-owned resource-orb registration and contact
// eligibility. It does not attest packet delivery, native visibility or healing.
type WorldPickup struct {
	ObjectID           uint32
	Kind               string
	Position           WorldPosition
	Destination        WorldPosition
	AvailableInMS      int64
	ExpiresInMS        int64
	IsCollectible      bool
	IsReserved         bool
	IsExpired          bool
	IsServerRegistered bool
	DistanceSquared    float64
	PickupRadius       float32
}

type WorldPickupSnapshot struct {
	IsAvailable       bool
	UnavailableReason string
	Pickups           []WorldPickup
	Total             int
	IsTruncated       bool
}
