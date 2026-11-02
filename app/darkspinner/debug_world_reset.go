//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

func debugHeroResetDTO(receipt scenario.HeroResetReceipt, isRigReady bool) map[string]any {
	return map[string]any{"game_id": receipt.GameID, "zone_generation": receipt.ZoneGeneration, "object_id": receipt.ObjectID, "noun": receipt.Noun,
		"index": receipt.Index, "hit_point": receipt.HitPoint, "maximum_hit_point": receipt.MaximumHitPoint, "mana_point": receipt.ManaPoint,
		"recipient_count": receipt.RecipientCount, "is_replacement": receipt.IsReplacement,
		"is_cooldown_reset": receipt.IsCooldownReset, "cooldown_count": receipt.CooldownCount,
		"is_retirement": receipt.IsRetirement, "is_rig_ready": isRigReady,
		"is_ability_rank_set": receipt.IsAbilityRankSet, "ability_rank": receipt.AbilityRank,
		"is_server_rank_fixture": receipt.IsAbilityRankSet,
		"is_placement":           receipt.IsPlacement, "is_queued": receipt.IsQueued, "is_native_observed": false}
}

// The host proves every world participant belongs to this supervisor before
// passing the allowlist to the epoch-checked gameplay fixture.
func resetDebugHero(ctx context.Context, host *debugHost, clientManager *debugClientManager, operation string, payload json.RawMessage) (scenario.HeroResetReceipt, error) {
	result := scenario.HeroResetReceipt{}
	var req struct {
		ClientID        string             `json:"client_id"`
		Noun            uint32             `json:"noun,omitempty"`
		Point           *controlWorldPoint `json:"point,omitempty"`
		HitPoint        *float32           `json:"hit_point,omitempty"`
		MaximumHitPoint *float32           `json:"maximum_hit_point,omitempty"`
		ManaPoint       *float32           `json:"mana_point,omitempty"`
		AbilityRank     *int32             `json:"ability_rank,omitempty"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("resetPayload: %w", err)
	}
	if (operation == "client.replace_hero") != (req.Noun != 0) {
		return result, errors.New("ReplaceHero requires an installed noun ID; RestoreHero accepts no noun")
	}
	if operation != "client.restore_hero" && (req.HitPoint != nil || req.MaximumHitPoint != nil || req.ManaPoint != nil) {
		return result, errors.New("resource fields require RestoreHero")
	}
	if operation == "client.set_ability_rank" {
		if req.AbilityRank == nil || *req.AbilityRank < 0 || *req.AbilityRank > 8 {
			return result, errors.New("SetAbilityRank requires an integer rank0 through8")
		}
	} else if req.AbilityRank != nil {
		return result, errors.New("ability_rank requires SetAbilityRank")
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
	if operation == "client.set_ability_rank" && world.Arena == nil {
		return result, errors.New("server ability rank fixtures currently require an owned arena")
	}
	participants, err := debugHeroResetParticipants(ctx, host, clientManager, world)
	if err != nil {
		return result, fmt.Errorf("resetParticipants: %w", err)
	}
	command := scenario.HeroResetRequest{UserID: owner.client.UserID, GameID: world.GameID,
		PeerGeneration: world.PeerGeneration, TransportGeneration: world.TransportGeneration,
		ZoneGeneration: world.ZoneGeneration, ObjectID: world.Hero.ObjectID, Noun: req.Noun,
		ObservedNoun: world.Hero.Noun, ObservedIndex: world.Hero.Index}
	command.HitPoint, command.MaximumHitPoint, command.ManaPoint = req.HitPoint, req.MaximumHitPoint, req.ManaPoint
	command.IsCooldownReset = operation == "client.reset_cooldowns"
	if operation == "client.set_ability_rank" {
		command.IsAbilityRankSet = true
		command.AbilityRank = *req.AbilityRank
		command.ObservedNoun = world.Hero.Noun
		command.ObservedIndex = world.Hero.Index
	}
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
	isOwnerParticipant := false
	for _, participant := range participants {
		if participant.owner.client.UserID == owner.client.UserID {
			isOwnerParticipant = true
		}
		err = recheckDebugWorldOwner(ctx, host, clientManager, participant.owner)
		if err != nil {
			return result, fmt.Errorf("resetRecheck: %w", err)
		}
		err = recheckDebugHeroResetEpoch(ctx, host, participant.owner, participant.epoch)
		if err != nil {
			return result, fmt.Errorf("resetEpoch: %w", err)
		}
		command.OwnedUserIDs = append(command.OwnedUserIDs, participant.owner.client.UserID)
	}
	if !isOwnerParticipant {
		return result, errors.New("hero fixture owner is absent from the observed world participants")
	}
	if req.Noun != 0 {
		witnesses, witnessErr := prepareDebugHeroRetirement(ctx, clientManager, participants)
		if witnessErr != nil {
			return result, fmt.Errorf("resetWitness: %w", witnessErr)
		}
		command.IsRetirement = true
		result, err = host.server.DebugResetHero(ctx, command)
		if err != nil {
			return result, fmt.Errorf("resetRetire: %w", err)
		}
		err = waitDebugHeroRetirement(ctx, host, clientManager, witnesses, command.ObjectID)
		if err != nil {
			return result, fmt.Errorf("resetRetireWait: %w", err)
		}
		command.IsRetirement = false
	}
	result, err = host.server.DebugResetHero(ctx, command)
	if err != nil {
		return result, fmt.Errorf("resetApply: %w", err)
	}
	if req.Noun != 0 {
		err = refreshDebugHeroReplacement(ctx, host, clientManager, owner, command)
		if err != nil {
			return result, fmt.Errorf("resetRefresh: %w", err)
		}
		err = waitDebugHeroReplacementRig(ctx, host, clientManager, owner, command)
		if err != nil {
			return result, fmt.Errorf("resetRig: %w", err)
		}
	}
	return result, nil
}

type debugHeroResetParticipant struct {
	owner debugWorldOwner
	epoch scenario.HeroResetRequest
}

func debugHeroResetParticipants(ctx context.Context, host *debugHost, clientManager *debugClientManager, world scenario.WorldState) ([]debugHeroResetParticipant, error) {
	if !world.IsAvailable || world.Hero == nil || world.Hero.IsDefeated || world.Hero.HitPoint <= 0 || world.IsZoneTerminal ||
		world.PeerGeneration == 0 || world.TransportGeneration == 0 {
		return nil, errors.New("hero fixtures require a committed living world")
	}
	expectedMembers := make(map[uint64]scenario.WorldCampaignMember)
	if world.Arena != nil {
		if !world.Arena.IsCombatActive || len(world.Arena.Members) < 2 {
			return nil, errors.New("hero fixtures require an active owned arena")
		}
		for _, member := range world.Arena.Members {
			if member.UserID == 0 || expectedMembers[member.UserID].UserID != 0 {
				return nil, errors.New("hero fixture arena participant identity is invalid or duplicated")
			}
			expectedMembers[member.UserID] = scenario.WorldCampaignMember{UserID: member.UserID, IsReady: true}
		}
	} else if world.Campaign != nil && world.ZoneGeneration != 0 {
		for _, member := range world.Campaign.Members {
			if !member.IsReady || member.UserID == 0 || member.PeerGeneration == 0 || member.TransportGeneration == 0 || member.ZoneGeneration == 0 {
				return nil, errors.New("hero fixture campaign participant is not committed and ready")
			}
			isDuplicate := expectedMembers[member.UserID].UserID != 0
			if isDuplicate {
				return nil, errors.New("hero fixture campaign participant identity is duplicated")
			}
			expectedMembers[member.UserID] = member
		}
	} else {
		return nil, errors.New("hero fixtures require an active arena or committed campaign")
	}
	if len(expectedMembers) == 0 {
		return nil, errors.New("hero fixture has no committed participants")
	}
	participants := make([]debugHeroResetParticipant, 0, len(expectedMembers))
	ownedUsers := make(map[uint64]bool)
	for _, client := range clientManager.List() {
		member, isExpected := expectedMembers[uint64(client.UserID)]
		if !client.IsRunning || !isExpected || ownedUsers[member.UserID] {
			continue
		}
		candidate, err := observeDebugWorldOwner(ctx, host, clientManager, client.ClientID)
		if err != nil {
			return nil, fmt.Errorf("participantOwner: %w", err)
		}
		observed, err := host.server.DebugWorldState(ctx, candidate.client.UserID, candidate.profile.GameID)
		if err != nil {
			return nil, fmt.Errorf("participantWorld: %w", err)
		}
		if candidate.profile.GameID != world.GameID || !observed.IsAvailable || observed.GameID != world.GameID || observed.Hero == nil ||
			observed.PeerGeneration == 0 || observed.TransportGeneration == 0 || observed.IsZoneTerminal {
			return nil, errors.New("hero fixture participant lost its committed game")
		}
		if world.Campaign != nil && (observed.Campaign == nil || observed.PeerGeneration != member.PeerGeneration ||
			observed.TransportGeneration != member.TransportGeneration || observed.ZoneGeneration != member.ZoneGeneration || observed.Hero.Index != member.DeployedIndex) {
			return nil, errors.New("hero fixture participant changed campaign epochs or actor")
		}
		if world.Arena != nil && (observed.Arena == nil || !observed.Arena.IsCombatActive) {
			return nil, errors.New("hero fixture participant left active arena combat")
		}
		participants = append(participants, debugHeroResetParticipant{owner: candidate,
			epoch: scenario.HeroResetRequest{GameID: observed.GameID, PeerGeneration: observed.PeerGeneration,
				TransportGeneration: observed.TransportGeneration, ZoneGeneration: observed.ZoneGeneration}})
		ownedUsers[member.UserID] = true
	}
	if len(participants) != len(expectedMembers) {
		return nil, errors.New("hero fixture requires every world participant to be owned by this supervisor")
	}
	return participants, nil
}

func recheckDebugHeroResetEpoch(ctx context.Context, host *debugHost, owner debugWorldOwner, epoch scenario.HeroResetRequest) error {
	world, err := host.server.DebugWorldState(ctx, owner.client.UserID, epoch.GameID)
	if err != nil {
		return fmt.Errorf("epochWorld: %w", err)
	}
	if !isDebugHeroResetEpochCurrent(world, epoch) {
		return errors.New("hero fixture participant changed game, peer, transport or zone")
	}
	return nil
}

func isDebugHeroResetEpochCurrent(world scenario.WorldState, epoch scenario.HeroResetRequest) bool {
	return world.GameID == epoch.GameID && world.PeerGeneration == epoch.PeerGeneration &&
		world.TransportGeneration == epoch.TransportGeneration && world.ZoneGeneration == epoch.ZoneGeneration && !world.IsZoneTerminal
}
