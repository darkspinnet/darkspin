//go:build scenario

package gameplay

import (
	"context"
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/squad"
)

// Exercise the real registry operation: a stale or foreign request must not
// restore resources or enqueue packets. A valid restore keeps actor lifetime.
func TestScenarioHeroRestoreOwnershipAndLifetime(t *testing.T) {
	registry := newGameplaySessionRegistry(nil, time.Now, nil)
	registry.configureScenarioHeroPools(newModifierPool(), newAttachedEffectPool())
	for index, userID := range []uint64{1, 2} {
		member := gameplayPeerSession{}
		member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 11, UserID: userID, Team: uint16(index + 1)}
		member.binding.Creatures[0] = game.GameplayCreature{ID: 1, Noun: 7, HitPoint: 100, PowerPoint: 60}
		member.deployedObjectID = uint32(index + 10)
		member.generation, member.transportGeneration = userID, userID
		member.isArenaCombatActive = true
		member.stage.EnterDungeon()
		setupGeneration, isReserved := member.dungeonSetup.Reserve()
		if !isReserved || !member.dungeonSetup.Commit(setupGeneration) {
			t.Fatal("commit fixture setup")
		}
		var err error
		member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 30, ManaPoints: 15, IsAvailable: true}}, 0)
		if err != nil {
			t.Fatalf("squad: %v", err)
		}
		key := string(rune('a' + index))
		registry.sessions[key] = member
		memberKey := gameplaySessionMemberKey(member)
		registry.memberEndpoints[memberKey] = key
		registry.memberTransports[memberKey] = userID
	}
	originalStats := raknet.ArenaPlayerResult{PlayerID: 1, DamageTaken: 70, DamageDealt: 25, HealingDealt: 9, HealingReceived: 3}
	registry.arenaMatches[11] = &arenaMatchState{isCombatActive: true,
		result: arenaResultState{players: map[uint64]raknet.ArenaPlayerResult{1: originalStats}}}
	req := scenario.HeroResetRequest{UserID: 1, GameID: 11, PeerGeneration: 1, TransportGeneration: 1, ObjectID: 10, OwnedUserIDs: []int64{1, 2}}
	for _, rejected := range []scenario.HeroResetRequest{
		{UserID: 1, GameID: 11, PeerGeneration: 1, TransportGeneration: 99, ObjectID: 10, OwnedUserIDs: []int64{1, 2}},
		{UserID: 1, GameID: 11, PeerGeneration: 1, TransportGeneration: 1, ObjectID: 10, OwnedUserIDs: []int64{1}},
	} {
		receipt, err := registry.ScenarioResetHero(context.Background(), rejected, game.GameplayCreature{})
		if err == nil || receipt.IsQueued {
			t.Fatal("accepted stale or incomplete ownership")
		}
		member := registry.sessions["a"]
		if member.deployedHitPoint() != 30 || len(member.pendingPacketBatches) != 0 {
			t.Fatal("rejected restore mutated resources or queued presentation")
		}
	}
	before := registry.sessions["a"]
	receipt, err := registry.ScenarioResetHero(context.Background(), req, game.GameplayCreature{})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	after := registry.sessions["a"]
	if !receipt.IsQueued || receipt.RecipientCount != 2 || after.squad != before.squad ||
		after.deployedHitPoint() != 100 || after.deployedManaPoint() != 60 {
		t.Fatalf("restore did not preserve lifetime and restore resources: %+v", receipt)
	}
	enemy := registry.sessions["b"]
	if enemy.deployedHitPoint() != 30 || enemy.deployedManaPoint() != 15 || len(enemy.pendingPacketBatches) == 0 {
		t.Fatal("restore changed opponent resources or failed to replicate to it")
	}
	previousSquad := after.squad
	previousBinding := after.binding
	req.IsPlacement = true
	req.Position = &scenario.WorldPosition{X: 8, Y: 9, Z: 5}
	receipt, err = registry.ScenarioResetHero(context.Background(), req, game.GameplayCreature{})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	placed := registry.sessions["a"]
	if !receipt.IsPlacement || placed.squad != previousSquad || placed.deployedHitPoint() != 100 ||
		placed.deployedManaPoint() != 60 || placed.playerPosition.X != 8 || placed.playerPosition.Y != 9 {
		t.Fatal("fixture positioning changed actor lifetime or resources")
	}
	req.IsPlacement = false
	req.Noun = 9
	req.Position = &scenario.WorldPosition{X: 15, Y: 20, Z: 5}
	replacement := game.GameplayCreature{ID: 10, Noun: 9, HitPoint: 200, PowerPoint: 90}
	receipt, err = registry.ScenarioResetHero(context.Background(), req, replacement)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	after = registry.sessions["a"]
	if !receipt.IsReplacement || after.squad == previousSquad || after.binding.Creatures[0] != replacement ||
		after.deployedHitPoint() != 200 || after.deployedManaPoint() != 90 ||
		after.playerPosition.X != 15 || after.playerPosition.Y != 20 || after.playerPosition.Z != 5 {
		t.Fatal("replacement failed to fence the old actor and install template resources at observed position")
	}
	if previousBinding.Creatures[0].Noun != 7 || previousSquad.State().Characters[0].HitPoints != 100 {
		t.Fatal("replacement mutated retained old binding or squad")
	}
	if registry.arenaMatches[11].result.players[1] != originalStats {
		t.Fatal("fixture altered damage or healing statistics")
	}
}
