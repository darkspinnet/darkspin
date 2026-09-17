package callback

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

const (
	RandomUnlock      = "nTutorial_RandomUnlock.main"
	SoloSupportUnlock = "nTutorial_SoloSupportUnlock.main"
	SupportUnlock     = "nTutorial_SupportUnlock.main"
	OverdriveUnlock   = "nTutorial_OverdriveUnlock.main"
	OverdriveClient   = "nTutorial_OverdriveUnlockClient.main"
	LevelObjectDeath  = "nLevelObject.OnTreeDeath"
	CatalystUnlock    = "nTutorial_CatalystUnlock.main"
)

func IsClientOnly(callbackName string) bool {
	switch callbackName {
	case OverdriveClient, LevelObjectDeath:
		return true
	default:
		return false
	}
}

type Binding struct {
	MarkerID     uint32
	CallbackName string
}

type Program struct {
	Binding Binding
	Program sim.Program
}

type Programs struct {
	SoloSupportUnlock sim.Program
	SupportUnlock     sim.Program
	OverdriveUnlock   sim.Program
	CatalystUnlock    sim.Program
}

// PublicationBindings projects both halves of the authored director
// contract: a trigger may call Lua directly, or it may publish a named event
// consumed by one or more marker listeners.
func PublicationBindings(
	publication game.CampaignDirectorPublication,
) []Binding {
	callbacks := make([]Binding, 0, len(publication.Listeners)+1)
	seen := make(map[Binding]bool, len(publication.Listeners)+1)
	appendCallback := func(callback Binding) {
		if callback.MarkerID == 0 || callback.CallbackName == "" || seen[callback] {
			return
		}
		callbacks = append(callbacks, callback)
		seen[callback] = true
	}
	appendCallback(Binding{
		MarkerID: publication.TriggerMarkerID, CallbackName: publication.CallbackName,
	})
	for _, listener := range publication.Listeners {
		appendCallback(Binding{
			MarkerID: listener.MarkerID, CallbackName: listener.CallbackName,
		})
	}
	return callbacks
}

func NamedEventBindings(
	publication game.CampaignDirectorNamedEventPublication,
) []Binding {
	callbacks := make([]Binding, 0, len(publication.Listeners))
	seen := make(map[Binding]bool, len(publication.Listeners))
	for _, listener := range publication.Listeners {
		callback := Binding{
			MarkerID: listener.MarkerID, CallbackName: listener.CallbackName,
		}
		if callback.MarkerID == 0 || callback.CallbackName == "" || seen[callback] {
			continue
		}
		callbacks = append(callbacks, callback)
		seen[callback] = true
	}
	return callbacks
}

func HasCallback(
	publication game.CampaignDirectorPublication, callbackName string,
) bool {
	if callbackName == "" {
		return false
	}
	for _, callback := range PublicationBindings(publication) {
		if callback.CallbackName == callbackName {
			return true
		}
	}
	return false
}

// ProgramFor is the allowlist between authored campaign
// callbacks and pinned, constrained Lua programs. Unknown callbacks never
// receive a generic Lua environment.
func ProgramFor(
	callbackName string, program Programs,
) (sim.Program, bool) {
	switch callbackName {
	case SoloSupportUnlock:
		return program.SoloSupportUnlock, true
	case SupportUnlock:
		return program.SupportUnlock, true
	case OverdriveUnlock:
		return program.OverdriveUnlock, true
	case CatalystUnlock:
		return program.CatalystUnlock, true
	default:
		return sim.Program{}, false
	}
}

func PlanNamedEventPrograms(
	publication game.CampaignDirectorNamedEventPublication, program Programs,
) []Program {
	plans := make([]Program, 0, len(publication.Listeners))
	for _, callback := range NamedEventBindings(publication) {
		callbackProgram, isFound := ProgramFor(callback.CallbackName, program)
		if !isFound {
			continue
		}
		plans = append(plans, Program{
			Binding: callback, Program: callbackProgram,
		})
	}
	return plans
}
