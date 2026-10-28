//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioWorldStateProvider interface {
	ScenarioWorldState(context.Context, int64, uint32) (scenario.WorldState, error)
}

func (e Lifecycle) ScenarioWorldState(ctx context.Context, userID int64, gameID uint32) (scenario.WorldState, error) {
	provider, isSupported := e.syncSnapshot.(scenarioWorldStateProvider)
	if !isSupported {
		return scenario.WorldState{UnavailableReason: "world observer is unavailable"}, nil
	}
	result, err := provider.ScenarioWorldState(ctx, userID, gameID)
	if err != nil {
		return result, fmt.Errorf("worldProvider: %w", err)
	}
	return result, nil
}

func (e *gameplaySessionRegistry) ScenarioWorldState(ctx context.Context, userID int64, gameID uint32) (scenario.WorldState, error) {
	result := scenario.WorldState{CapturedAt: time.Now().UTC()}
	if ctx == nil || userID <= 0 || gameID == 0 {
		return result, errors.New("world state requires context and an admitted user/game")
	}
	if e == nil {
		result.UnavailableReason = "gameplay registry is unavailable"
		return result, nil
	}
	queryContext, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mutex.TryRLock() {
		select {
		case <-queryContext.Done():
			return result, fmt.Errorf("worldRegistryLock: %w", queryContext.Err())
		case <-ticker.C:
		}
	}
	defer e.mutex.RUnlock()
	if e.isClosed {
		result.UnavailableReason = "gameplay registry is closed"
		return result, nil
	}
	var matched *gameplayPeerSession
	for endpoint, peer := range e.sessions {
		err := queryContext.Err()
		if err != nil {
			return result, fmt.Errorf("worldRegistryScan: %w", err)
		}
		if peer.binding.UserID != uint64(userID) || peer.binding.GameID != gameID {
			continue
		}
		if matched != nil {
			result.UnavailableReason = "multiple gameplay bindings match the owned user/game"
			return result, nil
		}
		key := gameplaySessionMemberKey(peer)
		if peer.generation == 0 || peer.transportGeneration == 0 || e.memberEndpoints[key] != endpoint || e.memberTransports[key] != peer.transportGeneration {
			result.UnavailableReason = "gameplay binding is not the current admitted transport"
			return result, nil
		}
		peerCopy := peer
		matched = &peerCopy
	}
	if matched == nil {
		result.UnavailableReason = "no admitted gameplay binding"
		return result, nil
	}
	result.GameID = gameID
	result.PeerGeneration = matched.generation
	result.TransportGeneration = matched.transportGeneration
	result.SessionStage = bugSessionStage(matched.stage)
	if matched.binding.Mode == game.ModeArena {
		return e.scenarioArenaWorld(*matched, result), nil
	}
	if !matched.dungeonSetup.IsCommitted() || matched.zone == nil {
		result.UnavailableReason = "world requires committed dungeon setup"
		return result, nil
	}
	world, err := matched.zone.ScenarioWorldState(queryContext, uint64(userID), matched.generation)
	if err != nil {
		return result, fmt.Errorf("worldZone: %w", err)
	}
	if world.Hero != nil && (world.Hero.ObjectID != matched.deployedObjectID || world.Hero.Index != matched.deployedCreatureIndex) {
		result.UnavailableReason = "zone hero does not match current deployed gameplay object"
		return result, nil
	}
	if world.Hero != nil && world.Hero.Index < uint32(len(matched.binding.Creatures)) {
		world.Hero.Noun = matched.binding.Creatures[world.Hero.Index].Noun
		world.Hero.PassiveKillStack = matched.passiveKillStack[world.Hero.Index]
		world.Hero.InputLockRemainingMS = abilityDurationMS(matched.heroInputLockRemaining(e.now()))
	}
	// The zone producer samples pickup availability and simulation expiry
	// against this same captured clock, after acquiring the pinned epoch.
	world.GameID = result.GameID
	world.PeerGeneration = result.PeerGeneration
	world.TransportGeneration = result.TransportGeneration
	world.SessionStage = result.SessionStage
	ability := e.scenarioAbilitySnapshot(*matched, world.CapturedAt)
	world.Ability = &ability
	for index := range world.Teleporters {
		teleporter := &world.Teleporters[index]
		if matched.zone.Security() != nil {
			objectID, isFound := matched.zone.Security().ScenarioAnchor(teleporter.MarkerID)
			if isFound {
				teleporter.ObjectID = objectID
			}
		}
		isActive, isPresented := matched.campaignTeleporterStates[teleporter.MarkerID]
		teleporter.IsActivationAvailable = isPresented
		teleporter.IsActive = isActive
	}
	if matched.zone.Security() != nil {
		for _, teleporter := range matched.zone.Security().ScenarioRoutes(matched.binding.Level) {
			if len(world.Teleporters) >= 128 {
				world.IsTeleporterTruncated = true
				break
			}
			isActive, isPresented := matched.securityTeleporterStates[teleporter.ObjectID]
			teleporter.IsActivationAvailable = isPresented
			teleporter.IsActive = isActive
			world.Teleporters = append(world.Teleporters, teleporter)
		}
		if matched.binding.Mode == game.ModeTutorial && len(world.Teleporters) < 128 {
			objectID, isFound := matched.zone.Security().ScenarioAnchor(tutorialBossTeleporterSourceID)
			world.Teleporters = append(world.Teleporters, scenario.WorldTeleporter{RouteIndex: -1, MarkerID: tutorialBossTeleporterSourceID, ObjectID: objectID,
				Source:      scenario.WorldPosition{X: tutorialBossTeleport.Source.X, Y: tutorialBossTeleport.Source.Y, Z: tutorialBossTeleport.Source.Z},
				Destination: scenario.WorldPosition{X: tutorialBossTeleport.Destination.X, Y: tutorialBossTeleport.Destination.Y, Z: tutorialBossTeleport.Destination.Z},
				IsBoss:      true, IsSecurity: true, IsActivationAvailable: isFound, IsActive: matched.isTutorialTeleporterActive})
		}
	}
	err = queryContext.Err()
	if err != nil {
		return result, fmt.Errorf("worldDeadline: %w", err)
	}
	return world, nil
}
