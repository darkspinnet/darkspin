//go:build scenario

package scenario

import "time"

// WorldState observes server producers only. Publication never proves native
// visibility, and these samples never advance movement or gameplay.
type WorldState struct {
	CapturedAt              time.Time
	IsAvailable             bool
	UnavailableReason       string
	GameID                  uint32
	PeerGeneration          uint64
	TransportGeneration     uint64
	ZoneGeneration          uint64
	SessionStage            string
	Hero                    *WorldHero
	Arena                   *WorldArena
	Ability                 *AbilityState
	NPCs                    []WorldNPC
	NPCTotal                int
	IsNPCTruncated          bool
	Pickups                 []WorldPickup
	PickupTotal             int
	IsPickupTruncated       bool
	IsPickupAvailable       bool
	PickupUnavailableReason string
	Teleporters             []WorldTeleporter
	IsTeleporterTruncated   bool
	IsMissionComplete       bool
	IsZoneTerminal          bool
	IsVictoryAvailable      bool
	IsVictoryCommitted      bool
	IsObjectiveComplete     bool
	Boss                    *WorldBoss
}

// WorldArena copies match and squad producers; it does not attest native UI.
type WorldArena struct {
	Team           uint16
	WinningTeam    uint32
	IsCombatActive bool
	IsResultReady  bool
	Members        []WorldArenaMember
}

type WorldArenaMember struct {
	DamageDone      float32
	DamageTaken     float32
	HealingDone     float32
	HealingReceived float32
	SwapRemainingMS uint32
	UserID          uint64
	Team            uint16
	DeployedIndex   uint32
	Characters      []WorldHero
}

// WorldOutcome describes a generation-pinned personal result transaction,
// rather than the zone's terminal state or a native results-screen observation.
type WorldOutcome struct {
	IsAvailable        bool
	IsVictoryCommitted bool
}

type WorldPosition struct{ X, Y, Z float32 }

type WorldHero struct {
	ObjectID         uint32
	Noun             uint32
	Index            uint32
	Position         WorldPosition
	HitPoint         float32
	ManaPoint        float32
	MaximumHitPoint  float32
	MaximumManaPoint float32
	IsDefeated       bool
}

type WorldNPC struct {
	ObjectID             uint32
	Noun                 string
	Position             WorldPosition
	HitPoint             float32
	IsDead               bool
	IsPublished          bool
	IsHostile            bool
	IsHostilityAvailable bool
	IsFixture            bool
	IsBoss               bool
	DistanceSquared      float64
}

type WorldTeleporter struct {
	RouteIndex            int
	MarkerID              uint32
	DestinationMarkerID   uint32
	ObjectID              uint32
	Source                WorldPosition
	Destination           WorldPosition
	IsSecurity            bool
	IsBoss                bool
	IsTunnel              bool
	IsActivationAvailable bool
	IsActive              bool
}

type WorldBoss struct {
	Phase              uint8
	LeaderObjectID     uint32
	LeaderHitPoint     float32
	IsComplete         bool
	IsBeamOutCommitted bool
}
