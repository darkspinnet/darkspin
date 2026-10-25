//go:build scenario

package scenario

import "time"

// AbilityState currently supports only the authored LightningRogueActive
// (Ride the Lightning) path. Admission and landing never attest native input.
type AbilityState struct {
	CapturedAt           time.Time
	IsAvailable          bool
	UnavailableReason    string
	IsSupported          bool
	UserID               uint64
	GameID               uint32
	PeerGeneration       uint64
	TransportGeneration  uint64
	ZoneGeneration       uint64
	ActorObjectID        uint32
	CreatureIndex        uint32
	CreatureNoun         uint32
	AbilityID            uint32
	AbilityAsset         string
	AbilityName          string
	ManaPoint            float32
	ManaCost             float32
	IsManaCostAvailable  bool
	CooldownRemainingMS  int64
	ReleaseRemainingMS   int64
	IsGateStateAvailable bool
	// LastRevision is the last allocated supported-cast sequence within this
	// peer, including preparation and casts belonging to a previous hero actor.
	LastRevision uint64
	Phase        string
	Cast         *AbilityCast
	Rejection    *AbilityRejection
}

type AbilityCast struct {
	Revision           uint64
	ActorObjectID      uint32
	CreatureIndex      uint32
	AbilityID          uint32
	AbilityAsset       string
	InputSyncStamp     uint32
	RequestStamp       uint8
	Phase              string
	IsAccepted         bool
	IsArrived          bool
	IsArrivalProcessed bool
	IsImpactCommitted  bool
	IsReleased         bool
	IsFailed           bool
	AcceptedAt         time.Time
	ArrivedAt          time.Time
	ArrivalProcessedAt time.Time
	ImpactAt           time.Time
	ReleasedAt         time.Time
	FailedAt           time.Time
	FailureReason      string
	TargetObjectID     uint32
	Destination        WorldPosition
	Damage             float32
	IsTargetDefeated   bool
}

// Rejection is separate from the last cast, so a refused later request cannot
// erase the completion or failure of a previously accepted cast.
type AbilityRejection struct {
	Revision       uint64
	ActorObjectID  uint32
	AbilityID      uint32
	InputSyncStamp uint32
	RequestStamp   uint8
	At             time.Time
	Reason         string
}
