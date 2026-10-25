//go:build !scenario

package gameplay

import (
	"github.com/darkspinnet/darkspin/server/raknet"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	"time"
)

type scenarioAbilityCatalog struct{}
type scenarioAbilityState struct{}
type scenarioRideCast struct{}

func (e *gameplaySessionRegistry) configureScenarioAbility(program Programs) {}
func (e *gameplayPeerSession) prepareScenarioRide(req campaignCharacterAbilityRequest, ability zonecontent.HeroAbility, at time.Time, destination raknet.Vector3, targetObjectID uint32) *scenarioRideCast {
	return nil
}
func (e *gameplayPeerSession) arriveScenarioRide(cast *scenarioRideCast, at time.Time) {}
func (e *gameplayPeerSession) impactScenarioRide(cast *scenarioRideCast, at time.Time, damage float32, isDefeated bool) {
}
func (e *gameplayPeerSession) releaseScenarioRide(cast *scenarioRideCast, at time.Time)             {}
func (e *gameplayPeerSession) failScenarioRide(cast *scenarioRideCast, at time.Time, reason string) {}
func (e *gameplayPeerSession) cancelScenarioRide(reason string, at time.Time)                       {}
func (e campaignAbilityCommandRuntime) acceptScenarioRide(sessionKey string, cast *scenarioRideCast, at time.Time) {
}
func (e campaignAbilityCommandRuntime) failScenarioRide(sessionKey string, cast *scenarioRideCast, reason string) {
}
func (e campaignCharacterAbilityRequest) recordScenarioRideRejection(reason string) {}

func (e campaignAbilityCommandRuntime) completeScenarioRideArrival(sessionKey string, cast *scenarioRideCast, at time.Time) {
}

func (e *gameplayPeerSession) prepareScenarioBurst(req campaignCharacterAbilityRequest, abilityID uint32, at time.Time, destination raknet.Vector3, targetObjectID uint32, launchCount int) *scenarioRideCast {
	return nil
}
func (e *gameplayPeerSession) launchScenarioBurst(cast *scenarioRideCast, index int, at time.Time) {}
func (e *gameplayPeerSession) completeScenarioBurst(cast *scenarioRideCast, at time.Time)          {}
func (e campaignAbilityCommandRuntime) releaseScenarioBurst(sessionKey string, cast *scenarioRideCast, at time.Time) {
}
