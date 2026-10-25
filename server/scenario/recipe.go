//go:build scenario

package scenario

type FocusedRecipeRequest struct {
	RunID                         string
	UserID                        int64
	RunSeed                       uint64
	IsAmbientPopulationSuppressed bool
}

// This recipe retains ordinary installed scene loading and fixed scene actors.
// RunSeed controls server gameplay RNG, not an unobserved native RNG owner.
type FocusedRecipe struct {
	RunID                         string
	UserID                        int64
	RunSeed                       uint64
	IsAmbientPopulationSuppressed bool
}

type SpawnRequest struct {
	RunID          string
	UserID         int64
	GameID         uint32
	PeerGeneration uint64
	ZoneGeneration uint64
	NounName       string
	IsAIEnabled    *bool
}

// IsPublished is the owning response's server send commitment, not native
// rendering acknowledgment. All handles are qualified by peer and zone epochs.
type SpawnReceipt struct {
	RequestID           string
	RunID               string
	UserID              int64
	State               string
	GameID              uint32
	PeerGeneration      uint64
	TransportGeneration uint64
	ZoneGeneration      uint64
	ObjectID            uint32
	NounName            string
	IsAIEnabled         bool
	IsAdmitted          bool
	IsPublished         bool
	IsNativeObserved    bool
	Failure             string
}
