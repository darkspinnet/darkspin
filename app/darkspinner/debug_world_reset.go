//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

// The host proves every match participant belongs to this supervisor before
// passing the allowlist to the epoch-checked gameplay fixture.
func resetDebugHero(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (scenario.HeroResetReceipt, error) {
	result := scenario.HeroResetReceipt{}
	var req struct {
		ClientID string             `json:"client_id"`
		Noun     uint32             `json:"noun,omitempty"`
		Point    *controlWorldPoint `json:"point,omitempty"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("resetPayload: %w", err)
	}
	if (operation == "client.replace_hero") != (req.Noun != 0) {
		return result, errors.New("ReplaceHero requires an installed noun ID; RestoreHero accepts no noun")
	}
	if operation == "client.place_hero" {
		if req.Point == nil || !isDebugWorldPointFinite(*req.Point) {
			return result, errors.New("PlaceHero requires a finite point")
		}
	} else if req.Point != nil {
		return result, errors.New("fixture point requires PlaceHero")
	}
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("resetOwner: %w", err)
	}
	world, err := host.server.DebugWorldState(ctx, owner.client.UserID, owner.profile.GameID)
	if err != nil {
		return result, fmt.Errorf("resetWorld: %w", err)
	}
	if !world.IsAvailable || world.Hero == nil || world.Arena == nil || !world.Arena.IsCombatActive {
		return result, errors.New("hero fixtures require a committed active PvP arena")
	}
	owners := make([]debugWorldOwner, 0, len(world.Arena.Members))
	ownedUsers := make(map[uint64]bool)
	for _, client := range clientManager.List() {
		if !client.IsRunning {
			continue
		}
		for _, member := range world.Arena.Members {
			if uint64(client.UserID) != member.UserID || ownedUsers[member.UserID] {
				continue
			}
			candidate, candidateErr := observeDebugWorldOwner(ctx, host, clientManager, client.ClientID)
			if candidateErr != nil {
				return result, fmt.Errorf("resetParticipant: %w", candidateErr)
			}
			if candidate.profile.GameID != world.GameID {
				return result, errors.New("hero fixture participant changed games")
			}
			owners = append(owners, candidate)
			ownedUsers[member.UserID] = true
		}
	}
	if len(owners) != len(world.Arena.Members) || len(owners) < 2 {
		return result, errors.New("hero fixture requires every arena member to be owned by this supervisor")
	}
	command := scenario.HeroResetRequest{UserID: owner.client.UserID, GameID: world.GameID,
		PeerGeneration: world.PeerGeneration, TransportGeneration: world.TransportGeneration,
		ObjectID: world.Hero.ObjectID, Noun: req.Noun}
	if operation == "client.place_hero" {
		command.IsPlacement = true
		command.Position = &scenario.WorldPosition{X: float32(req.Point.X), Y: float32(req.Point.Y), Z: float32(req.Point.Z)}
	}
	if req.Noun != 0 {
		probe, native, nativeErr := observeDebugHeroNative(ctx, clientManager, owner)
		if nativeErr != nil {
			return result, fmt.Errorf("resetNative: %w", nativeErr)
		}
		if probe == nil || native.Position == nil || !isDebugWorldPointFinite(*native.Position) {
			return result, errors.New("hero replacement requires a finite observed native position")
		}
		command.Position = &scenario.WorldPosition{X: float32(native.Position.X), Y: float32(native.Position.Y), Z: float32(native.Position.Z)}
	}
	for _, participant := range owners {
		err = recheckDebugWorldOwner(ctx, host, clientManager, participant)
		if err != nil {
			return result, fmt.Errorf("resetRecheck: %w", err)
		}
		command.OwnedUserIDs = append(command.OwnedUserIDs, participant.client.UserID)
	}
	result, err = host.server.DebugResetHero(ctx, command)
	if err != nil {
		return result, fmt.Errorf("resetApply: %w", err)
	}
	return result, nil
}
