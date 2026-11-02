package zone

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
)

// IsHordeTriggerEnabled gates the whole optional Mutation Agent encounter,
// including its opening wave and barriers, on completed campaign progression.
// Authored boss-pit waves and the initial campaign's scripted boss prerequisites
// remain independent of that optional encounter policy.
func (e *Zone) IsHordeTriggerEnabled(publication game.CampaignDirectorPublication) bool {
	if e == nil || !zonehorde.IsTrigger(publication) {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.info.Horde == nil {
		return false
	}
	if e.info.Horde.Policy().IsMutationAgentEnabled ||
		strings.EqualFold(e.info.DirectorDefinition.Level, game.InitialChainLevel) {
		return true
	}
	return zoneboss.IsFinalAuthoredHordeCompletion(e.info.DirectorDefinition, zonehorde.Completion{
		MarkerSetName: publication.MarkerSetName, TriggerMarkerID: publication.TriggerMarkerID,
		EventName: "horde complete",
	})
}
