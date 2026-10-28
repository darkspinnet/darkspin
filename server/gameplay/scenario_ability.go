//go:build scenario

package gameplay

import (
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

type scenarioAbilityCatalog struct {
	abilitiesByNoun      map[uint32]zonecontent.HeroAbility
	scenarioModifierPool *modifierPool
	scenarioEffectPool   *attachedEffectPool
}
type scenarioAbilityState struct {
	isCaptureUnavailable bool
	nextRevision         uint64
	nextRejection        uint64
	last                 *scenarioRideCast
	rejection            *scenario.AbilityRejection
	retirement           *scenarioHeroRetirement
	rankOverride         *scenarioHeroRankOverride
}
type scenarioRideCast struct {
	mutex               sync.Mutex
	state               scenario.AbilityCast
	zone                *zone.Zone
	squad               *squad.Session
	userID              uint64
	gameID              uint32
	peerGeneration      uint64
	transportGeneration uint64
	zoneGeneration      uint64
}

func (e *gameplaySessionRegistry) configureScenarioAbility(program Programs) {
	e.abilitiesByNoun = make(map[uint32]zonecontent.HeroAbility)
	for noun, kit := range program.HeroKits {
		ability, isFound := kit.Abilities[zonecontent.HeroAbilitySpecialTwo]
		if isFound {
			e.abilitiesByNoun[noun] = ability
		}
	}
}

// Called while the ordinary gameplay registry owns the resource transaction.
// Preparation is not acceptance; scheduled callbacks may run before admission
// returns and therefore retain this exact preparing cast object. Publishing
// preparation never sets acceptance or makes the actor available for a cast.
func (e *gameplayPeerSession) prepareScenarioRide(req campaignCharacterAbilityRequest, ability zonecontent.HeroAbility, at time.Time, destination raknet.Vector3, targetObjectID uint32) *scenarioRideCast {
	if ability.AssetName != "LightningRogueActive" && ability.Definition.Kind != sim.AbilityKindProjectileBurst {
		return nil
	}
	zoneGeneration, actor, isActor := e.scenarioAbilityActor()
	if !isActor || actor.ObjectID != e.deployedObjectID || actor.CreatureIndex != e.deployedCreatureIndex {
		e.scenarioAbility.isCaptureUnavailable = true
		return nil
	}
	e.scenarioAbility.isCaptureUnavailable = false
	e.scenarioAbility.nextRevision++
	cast := &scenarioRideCast{zone: e.zone, squad: e.squad, userID: e.binding.UserID, gameID: e.binding.GameID, peerGeneration: e.generation, transportGeneration: e.transportGeneration, zoneGeneration: zoneGeneration,
		state: scenario.AbilityCast{Revision: e.scenarioAbility.nextRevision, ActorObjectID: e.deployedObjectID, CreatureIndex: e.deployedCreatureIndex, AbilityID: ability.ID, AbilityAsset: ability.AssetName, InputSyncStamp: req.command.Common.InputSyncStamp, RequestStamp: req.command.Common.Unknown[0], Phase: "preparing", TargetObjectID: targetObjectID, Destination: scenario.WorldPosition{X: destination.X, Y: destination.Y, Z: destination.Z}}}
	e.scenarioAbility.last = cast
	return cast
}

func (e *gameplayPeerSession) isScenarioRideCurrent(cast *scenarioRideCast) bool {
	return cast != nil && e.zone == cast.zone && e.squad == cast.squad && e.binding.UserID == cast.userID && e.binding.GameID == cast.gameID && e.generation == cast.peerGeneration && e.transportGeneration == cast.transportGeneration && e.deployedObjectID == cast.state.ActorObjectID && e.deployedCreatureIndex == cast.state.CreatureIndex
}

func (e gameplayPeerSession) scenarioAbilityActor() (uint64, zonehero.Actor, bool) {
	if e.binding.Mode == game.ModeArena {
		if e.squad == nil || !e.isArenaCombatActive || e.deployedObjectID == 0 {
			return 0, zonehero.Actor{}, false
		}
		return 0, zonehero.Actor{ObjectID: e.deployedObjectID, CreatureIndex: e.deployedCreatureIndex,
			HitPoint: e.deployedHitPoint(), ManaPoint: e.deployedManaPoint()}, true
	}
	if e.zone == nil {
		return 0, zonehero.Actor{}, false
	}
	return e.zone.ScenarioAbilityActor(e.binding.UserID, e.generation)
}

func (e *gameplayPeerSession) publishScenarioRide(cast *scenarioRideCast) {
	if !e.isScenarioRideCurrent(cast) {
		return
	}
	last := e.scenarioAbility.last
	if last == nil || last.state.Revision <= cast.state.Revision {
		e.scenarioAbility.last = cast
	}
}

func (e *gameplayPeerSession) arriveScenarioRide(cast *scenarioRideCast, at time.Time) {
	if !e.isScenarioRideCurrent(cast) {
		return
	}
	cast.mutex.Lock()
	defer cast.mutex.Unlock()
	cast.state.IsArrived = true
	cast.state.ArrivedAt = at.UTC()
}
func (e *gameplayPeerSession) impactScenarioRide(cast *scenarioRideCast, at time.Time, damage float32, isDefeated bool) {
	if !e.isScenarioRideCurrent(cast) {
		return
	}
	cast.mutex.Lock()
	defer cast.mutex.Unlock()
	cast.state.IsImpactCommitted = true
	cast.state.ImpactAt = at.UTC()
	cast.state.Damage = damage
	cast.state.IsTargetDefeated = isDefeated
}
func (e *gameplayPeerSession) releaseScenarioRide(cast *scenarioRideCast, at time.Time) {
	if !e.isScenarioRideCurrent(cast) {
		return
	}
	cast.mutex.Lock()
	defer cast.mutex.Unlock()
	cast.state.IsReleased = true
	cast.state.ReleasedAt = at.UTC()
}
func (e *gameplayPeerSession) failScenarioRide(cast *scenarioRideCast, at time.Time, reason string) {
	if !e.isScenarioRideCurrent(cast) {
		return
	}
	cast.mutex.Lock()
	cast.state.IsFailed = true
	cast.state.FailedAt = at.UTC()
	cast.state.FailureReason = reason
	cast.mutex.Unlock()
	e.publishScenarioRide(cast)
}
func (e *gameplayPeerSession) cancelScenarioRide(reason string, at time.Time) {
	cast := e.scenarioAbility.last
	if !e.isScenarioRideCurrent(cast) {
		return
	}
	// Teardown also owns detached copies outside the registry lock. Protect
	// just this cast's scalars; never reach back into the registry from it.
	cast.mutex.Lock()
	defer cast.mutex.Unlock()
	isCompleted := cast.state.IsReleased && cast.state.IsArrived && cast.state.IsArrivalProcessed
	if cast.state.ExpectedLaunchCount > 0 {
		isCompleted = cast.state.IsReleased && cast.state.IsBurstCompleted
	}
	if isCompleted || cast.state.IsFailed {
		return
	}
	cast.state.IsFailed = true
	cast.state.FailedAt = at.UTC()
	cast.state.FailureReason = reason
}

func (e campaignAbilityCommandRuntime) acceptScenarioRide(sessionKey string, cast *scenarioRideCast, at time.Time) {
	if cast == nil {
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peer, isFound := e.registry.sessions[sessionKey]
	if !isFound || !peer.isScenarioRideCurrent(cast) {
		return
	}
	cast.mutex.Lock()
	cast.state.IsAccepted = true
	cast.state.AcceptedAt = at.UTC()
	cast.mutex.Unlock()
	peer.publishScenarioRide(cast)
	e.registry.sessions[sessionKey] = peer
}

func (e campaignAbilityCommandRuntime) failScenarioRide(sessionKey string, cast *scenarioRideCast, reason string) {
	if cast == nil {
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peer, isFound := e.registry.sessions[sessionKey]
	if !isFound {
		return
	}
	peer.failScenarioRide(cast, e.now(), reason)
	e.registry.sessions[sessionKey] = peer
}

// This boundary is later than teleport commitment: the optional impact
// producer has returned. It does not imply any packet was delivered.
func (e campaignAbilityCommandRuntime) completeScenarioRideArrival(sessionKey string, cast *scenarioRideCast, at time.Time) {
	if cast == nil {
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peer, isFound := e.registry.sessions[sessionKey]
	if !isFound || !peer.isScenarioRideCurrent(cast) {
		return
	}
	cast.mutex.Lock()
	defer cast.mutex.Unlock()
	cast.state.IsArrivalProcessed = true
	cast.state.ArrivalProcessedAt = at.UTC()
}

func (e campaignCharacterAbilityRequest) recordScenarioRideRejection(reason string) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	key := e.packet.Address.String()
	peer, isFound := e.runtime.registry.sessions[key]
	if !isFound || peer.generation != e.commandSession.generation || peer.transportGeneration != e.commandSession.transportGeneration || peer.zone != e.commandSession.zone || peer.binding.GameID != e.commandSession.binding.GameID || peer.deployedObjectID != e.command.Common.ObjectID || e.command.Ability == nil || e.command.Ability.Index != 2 || peer.deployedCreatureIndex >= uint32(len(peer.binding.Creatures)) {
		return
	}
	ability := e.runtime.registry.abilitiesByNoun[peer.binding.Creatures[peer.deployedCreatureIndex].Noun]
	if ability.AssetName != "LightningRogueActive" && ability.AssetName != "MissileTempestActive" {
		return
	}
	peer.scenarioAbility.nextRejection++
	peer.scenarioAbility.rejection = &scenario.AbilityRejection{Revision: peer.scenarioAbility.nextRejection, ActorObjectID: peer.deployedObjectID, AbilityID: ability.ID, InputSyncStamp: e.command.Common.InputSyncStamp, RequestStamp: e.command.Common.Unknown[0], At: e.runtime.now().UTC(), Reason: reason}
	e.runtime.registry.sessions[key] = peer
}

func scenarioRidePhase(cast scenario.AbilityCast) string {
	if cast.IsFailed {
		return "failed"
	}
	if cast.IsBurstCompleted {
		return "completed"
	}
	if cast.IsReleased {
		return "released"
	}
	if cast.IsImpactCommitted {
		return "impact"
	}
	if cast.IsArrived {
		return "arrived"
	}
	if cast.LaunchCount > 0 {
		return "launching"
	}
	if cast.IsAccepted {
		return "accepted"
	}
	return "preparing"
}
