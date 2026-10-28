package scenario

// NPCCatalog is the installed director's combat noun inventory for one
// admitted campaign zone. It describes server support, not native playback.
type NPCCatalog struct {
	GameID         uint32
	ZoneGeneration uint64
	Level          string
	Difficulty     uint32
	Entries        []NPCCatalogEntry
}

type NPCCatalogEntry struct {
	Noun               string
	HitPoint           float32
	Rank               int32
	AbilityName        string
	Family             string
	TargetKind         string
	Range              float32
	MinimumRange       float32
	IsActionRecovered  bool
	IsControlSupported bool
	IsFinalBoss        bool
}
