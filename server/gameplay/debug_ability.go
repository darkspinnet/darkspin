//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

type scenarioAbilityStateProvider interface {
	ScenarioAbilityState(context.Context, int64, uint32) (scenario.AbilityState, error)
}

func (e Lifecycle) ScenarioAbilityState(ctx context.Context, userID int64, gameID uint32) (scenario.AbilityState, error) {
	provider, isSupported := e.syncSnapshot.(scenarioAbilityStateProvider)
	if !isSupported {
		return scenario.AbilityState{UnavailableReason: "ability observer is unavailable"}, nil
	}
	result, err := provider.ScenarioAbilityState(ctx, userID, gameID)
	if err != nil {
		return result, fmt.Errorf("abilityProvider: %w", err)
	}
	return result, nil
}

func (e *gameplaySessionRegistry) ScenarioAbilityState(ctx context.Context, userID int64, gameID uint32) (scenario.AbilityState, error) {
	result := scenario.AbilityState{CapturedAt: time.Now().UTC()}
	if ctx == nil || userID <= 0 || gameID == 0 {
		return result, errors.New("ability state requires context and owned admitted user/game")
	}
	err := ctx.Err()
	if err != nil {
		return result, fmt.Errorf("abilityContext: %w", err)
	}
	if e == nil || !e.mutex.TryRLock() {
		result.UnavailableReason = "gameplay ability registry is unavailable or busy"
		return result, nil
	}
	defer e.mutex.RUnlock()
	if e.isClosed {
		result.UnavailableReason = "gameplay ability registry is closed"
		return result, nil
	}
	var matched *gameplayPeerSession
	for endpoint, peer := range e.sessions {
		if peer.binding.UserID != uint64(userID) || peer.binding.GameID != gameID {
			continue
		}
		if matched != nil {
			result.UnavailableReason = "multiple gameplay bindings match owned ability actor"
			return result, nil
		}
		key := gameplaySessionMemberKey(peer)
		if peer.generation == 0 || peer.transportGeneration == 0 || e.memberEndpoints[key] != endpoint || e.memberTransports[key] != peer.transportGeneration {
			result.UnavailableReason = "ability gameplay transport is no longer admitted"
			return result, nil
		}
		copyPeer := peer
		matched = &copyPeer
	}
	if matched == nil {
		result.UnavailableReason = "no admitted ability gameplay binding"
		return result, nil
	}
	return e.scenarioAbilitySnapshot(*matched, e.now()), nil
}

// Caller holds the gameplay registry lock. Local producer locks never wait,
// and this operation neither creates a cooldown gate nor mutates an ability.
func (e *gameplaySessionRegistry) scenarioAbilitySnapshot(peer gameplayPeerSession, now time.Time) scenario.AbilityState {
	result := scenario.AbilityState{CapturedAt: now.UTC(), UserID: peer.binding.UserID, GameID: peer.binding.GameID, PeerGeneration: peer.generation, TransportGeneration: peer.transportGeneration, ActorObjectID: peer.deployedObjectID, CreatureIndex: peer.deployedCreatureIndex, Phase: "idle", LastRevision: peer.scenarioAbility.nextRevision}
	if peer.zone == nil || !peer.stage.IsDungeon() || !peer.dungeonSetup.IsCommitted() || peer.deployedCreatureIndex >= uint32(len(peer.binding.Creatures)) {
		result.UnavailableReason = "ability requires an admitted deployed dungeon hero"
		return result
	}
	creature := peer.binding.Creatures[peer.deployedCreatureIndex]
	result.CreatureNoun = creature.Noun
	ability, isFound := e.abilitiesByNoun[creature.Noun]
	result.AbilityID = ability.ID
	result.AbilityAsset = ability.AssetName
	isRide := ability.AssetName == "LightningRogueActive" && ability.Definition.Kind == sim.AbilityKindTeleportStrike
	isBurst := ability.AssetName == "MissileTempestActive" && ability.Definition.Kind == sim.AbilityKindProjectileBurst
	result.IsSupported = isFound && ability.ID != 0 && (isRide || isBurst)
	if isRide {
		result.AbilityName = "Ride the Lightning"
	} else if isBurst {
		result.AbilityName = "Rocket Barrage"
	}
	zoneGeneration, actor, isActor := peer.zone.ScenarioAbilityActor(peer.binding.UserID, peer.generation)
	if !isActor || actor.ObjectID != peer.deployedObjectID || actor.CreatureIndex != peer.deployedCreatureIndex {
		result.UnavailableReason = "current ability hero/zone producer is unavailable or busy"
		return result
	}
	result.ZoneGeneration = zoneGeneration
	result.ManaPoint = actor.ManaPoint
	result.IsAvailable = true
	if !result.IsSupported {
		return result
	}
	if peer.scenarioAbility.isCaptureUnavailable {
		result.IsAvailable = false
		result.UnavailableReason = "ability admission epoch could not be sampled"
		return result
	}
	isOverdrive := peer.binding.IsOverdriveUnlocked && peer.overdriveObjectID == peer.deployedObjectID && actor.HitPoint > 0 && !now.Before(peer.overdriveStartsAt) && now.Before(peer.overdriveExpiresAt)
	manaCost, costErr := game.ResolveAbilityManaCost(ability.Definition.ManaCost, creature.DamageProfile.PrimaryAttribute, ability.Definition.ManaCoefficient, isOverdrive)
	if costErr != nil {
		// Projection is optional evidence; an unavailable cost must not turn
		// into a zero-cost readiness assertion.
		result.IsManaCostAvailable = false
	} else {
		result.ManaCost = manaCost
		result.IsManaCostAvailable = true
	}
	// A missing lazy gate has never admitted a reservation and is empty.
	cooldown, release := time.Duration(0), time.Duration(0)
	isCooldownAvailable, isReleaseAvailable := true, true
	if peer.abilityCooldown != nil {
		cooldown, isCooldownAvailable = peer.abilityCooldown.ScenarioRemaining(zoneability.HeroAbilityCooldown(ability.ID), now)
	}
	if peer.abilityRelease != nil {
		release, isReleaseAvailable = peer.abilityRelease.ScenarioRemaining(now)
	}
	result.CooldownRemainingMS = abilityDurationMS(cooldown)
	result.ReleaseRemainingMS = abilityDurationMS(release)
	result.IsGateStateAvailable = isCooldownAvailable && isReleaseAvailable
	last := peer.scenarioAbility.last
	if peer.isScenarioRideCurrent(last) && last.zoneGeneration == zoneGeneration {
		if !last.mutex.TryLock() {
			result.IsAvailable = false
			result.UnavailableReason = "ability cast producer is busy"
			return result
		}
		cast := last.state
		last.mutex.Unlock()
		cast.Phase = scenarioRidePhase(cast)
		result.Cast = &cast
		result.Phase = cast.Phase
		if !cast.IsAccepted && !cast.IsFailed {
			result.IsAvailable = false
			result.UnavailableReason = "ability admission is preparing"
		}
	}
	rejection := peer.scenarioAbility.rejection
	if rejection != nil && rejection.ActorObjectID == peer.deployedObjectID && rejection.AbilityID == ability.ID {
		copyRejection := *rejection
		result.Rejection = &copyRejection
	}
	return result
}

func abilityDurationMS(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64((duration-1)/time.Millisecond) + 1
}
