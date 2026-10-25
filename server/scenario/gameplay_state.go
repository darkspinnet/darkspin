//go:build scenario

package scenario

// GameplayState is a small lifecycle observation, independent of character,
// resource, and diagnostic dumps. Availability describes the observation source;
// an available source can correctly report no active gameplay session.
type GameplayState struct {
	IsAvailable          bool
	UnavailableReason    string
	IsGameplayActive     bool
	GameID               uint32
	SessionStage         string
	MissionLabel         string
	MissionAsset         string
	MissionDifficulty    uint32
	IsDungeonSetupFinal  bool
	IsArena              bool
	IsArenaSquadAccepted bool
	IsArenaCombatActive  bool
}
