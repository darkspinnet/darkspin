//go:build scenario

package scenario

// HeroResetRequest fences a local fixture mutation to the observed transport.
// A zero Noun restores resources unless IsPlacement requests fixture positioning.
type HeroResetRequest struct {
	UserID              int64
	GameID              uint32
	PeerGeneration      uint64
	TransportGeneration uint64
	ObjectID            uint32
	Noun                uint32
	Position            *WorldPosition
	IsPlacement         bool
	OwnedUserIDs        []int64
}

type HeroResetReceipt struct {
	GameID         uint32
	ObjectID       uint32
	Noun           uint32
	Index          uint32
	HitPoint       float32
	ManaPoint      float32
	RecipientCount int
	IsReplacement  bool
	IsPlacement    bool
	IsQueued       bool
}
