//go:build !scenario

package gameplay

type scenarioPeerReadiness struct{}

func (e *gameplayPeerSession) recordScenarioDungeonCommit(setupEpoch uint64) {}
