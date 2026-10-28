//go:build scenario

package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
)

func (e *gameplayPeerSession) prepareScenarioBurst(req campaignCharacterAbilityRequest, abilityID uint32, at time.Time, destination raknet.Vector3, targetObjectID uint32, launchCount int) *scenarioRideCast {
	if e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) || launchCount <= 0 {
		return nil
	}
	kit := req.runtime.program.HeroKits[e.binding.Creatures[e.deployedCreatureIndex].Noun]
	var ability zonecontent.HeroAbility
	for _, candidate := range kit.Abilities {
		if candidate.ID == abilityID && candidate.Definition.Kind == sim.AbilityKindProjectileBurst {
			ability = candidate
			break
		}
	}
	if ability.ID == 0 {
		return nil
	}
	cast := e.prepareScenarioRide(req, ability, at, destination, targetObjectID)
	if cast != nil {
		cast.state.ExpectedLaunchCount = launchCount
	}
	return cast
}

// The burst simulator has advanced through this prepared launch index. This
// includes a same-deadline batch, whose final producer advances every shot.
func (e *gameplayPeerSession) launchScenarioBurst(cast *scenarioRideCast, index int, at time.Time) {
	if !e.isScenarioRideCurrent(cast) {
		return
	}
	cast.mutex.Lock()
	defer cast.mutex.Unlock()
	cast.state.LaunchCount = max(cast.state.LaunchCount, index+1)
	cast.state.LastLaunchAt = at.UTC()
}

// Completion is observed only after the real finish producer finds no live
// projectile snapshots. An elapsed cast duration alone is not completion.
func (e *gameplayPeerSession) completeScenarioBurst(cast *scenarioRideCast, at time.Time) {
	if !e.isScenarioRideCurrent(cast) {
		return
	}
	cast.mutex.Lock()
	defer cast.mutex.Unlock()
	if cast.state.IsFailed {
		return
	}
	if cast.state.LaunchCount != cast.state.ExpectedLaunchCount || !cast.state.IsReleased {
		cast.state.IsFailed = true
		cast.state.FailedAt = at.UTC()
		cast.state.FailureReason = "burst finished without every launch and release"
		return
	}
	cast.state.IsBurstCompleted = true
	cast.state.BurstCompletedAt = at.UTC()
}

func (e campaignAbilityCommandRuntime) releaseScenarioBurst(sessionKey string, cast *scenarioRideCast, at time.Time) {
	if cast == nil {
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peer, isFound := e.registry.sessions[sessionKey]
	if !isFound {
		return
	}
	peer.releaseScenarioRide(cast, at)
}
