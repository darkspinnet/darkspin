//go:build scenario

package scenario

// ControlledNPCIdentity cannot acquire arbitrary zone actors: SpawnRequestID
// must identify an admitted controlled spawn owned by this run and cohort.
type ControlledNPCIdentity struct {
	RunID               string
	UserID              int64
	GameID              uint32
	PeerGeneration      uint64
	TransportGeneration uint64
	ZoneGeneration      uint64
	SpawnRequestID      string
	ObjectID            uint32
}

type NPCControlRequest struct {
	Identity       ControlledNPCIdentity
	Operation      string
	IsAIEnabled    *bool
	Destination    *WorldPosition
	AbilityName    string
	TargetObjectID uint32
}

// Admission, normal action completion and response publication are distinct.
// None of these fields claim native rendering or physical input observation.
type NPCControlReceipt struct {
	RequestID            string
	Identity             ControlledNPCIdentity
	Operation            string
	State                string
	IsAccepted           bool
	IsCompleted          bool
	IsSettled            bool
	IsResponseCommitted  bool
	ActionGeneration     uint64
	Failure              string
	SupersededRequestID  string
	SupersededFailure    string
	RequestedDestination *WorldPosition
	ProjectedDestination *WorldPosition
}

type ControlledNPCState struct {
	Identity                    ControlledNPCIdentity
	IsAvailable                 bool
	UnavailableReason           string
	IsAIEnabled                 bool
	IsFixture                   bool
	IsRetirementReady           bool
	RetirementUnavailableReason string
	Position                    WorldPosition
	HitPoint                    float32
	IsDead                      bool
	SupportedAbilityNames       []string
	ActionGeneration            uint64
	LastCommand                 *NPCControlReceipt
}
