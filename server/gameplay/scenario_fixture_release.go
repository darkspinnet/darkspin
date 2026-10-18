//go:build !scenario

package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type scenarioPeerFixture struct{}

func (e *gameplayPeerSession) recordScenarioFixtureSelection(director game.CampaignDirector) {}

func (e *gameplayPeerSession) recordScenarioFixtureTakeover(
	director game.CampaignDirector, markers []game.CampaignDirectorMarker,
	deletedObjectIDs []uint32, plans []zonenpc.SpawnPlan,
) {
}

func (e *gameplayPeerSession) recordScenarioFixtureAdmission() {}

func (e *gameplayPeerSession) recordScenarioFixtureCommit() {}
