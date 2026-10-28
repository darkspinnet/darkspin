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
	"github.com/darkspinnet/darkspin/server/zone/ability"
)

func TestScenarioHeroRankFixtureLifetime(t *testing.T) {
	registry := newGameplaySessionRegistry(nil, time.Now, nil)
	for index, userID := range []uint64{1, 2} {
		member := gameplayPeerSession{}
		member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 11, UserID: userID, Team: uint16(index + 1)}
		member.binding.Creatures[0] = game.GameplayCreature{ID: 1, Noun: 7, HitPoint: 100, PowerPoint: 60}
		member.generation, member.transportGeneration, member.deployedObjectID = userID, userID, uint32(index+10)
		member.isArenaCombatActive = true
		member.stage.EnterDungeon()
		setupGeneration, isReserved := member.dungeonSetup.Reserve()
		if !isReserved || !member.dungeonSetup.Commit(setupGeneration) {
			t.Fatal("commit rank fixture setup")
		}
		var err error
		member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 30, ManaPoints: 15, IsAvailable: true}}, 0)
		if err != nil {
			t.Fatalf("squad: %v", err)
		}
		key := string(rune('a' + index))
		registry.sessions[key] = member
		memberKey := gameplaySessionMemberKey(member)
		registry.memberEndpoints[memberKey], registry.memberTransports[memberKey] = key, userID
	}
	registry.arenaMatches[11] = &arenaMatchState{isCombatActive: true}
	req := scenario.HeroResetRequest{UserID: 1, GameID: 11, PeerGeneration: 1, TransportGeneration: 1, ObjectID: 10,
		OwnedUserIDs: []int64{1, 2}, IsAbilityRankSet: true, AbilityRank: 5, ObservedNoun: 7, ObservedIndex: 0}
	before := registry.sessions["a"]
	beforeState := before.squad.State()
	for rank := int32(1); rank <= 8; rank++ {
		req.AbilityRank = rank
		receipt, err := registry.ScenarioResetHero(context.Background(), req, game.GameplayCreature{})
		if err != nil {
			t.Fatalf("set rank%d: %v", rank, err)
		}
		after := registry.sessions["a"]
		if !receipt.IsAbilityRankSet || receipt.AbilityRank != rank || receipt.IsQueued || receipt.RecipientCount != 2 ||
			after.scenarioHeroAbilityRank(2) != rank || after.squad != before.squad || after.squad.State() != beforeState || len(after.pendingPacketBatches) != 0 {
			t.Fatal("server rank fixture changed resources/lifetime/native queue or did not resolve requested rank")
		}
	}
	req.AbilityRank = 5
	override := registry.sessions["a"].scenarioAbility.rankOverride
	for _, name := range []string{"negative", "outOfRange", "flag", "noun", "slot", "transport", "ownership", "cooldown", "resource"} {
		rejected := req
		switch name {
		case "negative":
			rejected.AbilityRank = -1
		case "outOfRange":
			rejected.AbilityRank = 9
		case "flag":
			rejected.IsAbilityRankSet = false
		case "noun":
			rejected.ObservedNoun = 8
		case "slot":
			rejected.ObservedIndex = 1
		case "transport":
			rejected.TransportGeneration = 99
		case "ownership":
			rejected.OwnedUserIDs = []int64{1}
		case "cooldown":
			rejected.IsCooldownReset = true
		case "resource":
			amount := float32(1)
			rejected.HitPoint = &amount
		}
		receipt, err := registry.ScenarioResetHero(context.Background(), rejected, game.GameplayCreature{})
		if err == nil || receipt.IsAbilityRankSet || registry.sessions["a"].scenarioAbility.rankOverride != override {
			t.Fatalf("%s accepted rank fixture or mutated its previous owner", name)
		}
	}
	current := registry.sessions["a"]
	for _, name := range []string{"game", "peer", "transport", "object", "noun", "slot", "squad", "retirement"} {
		changed := current
		switch name {
		case "game":
			changed.binding.GameID = 12
		case "peer":
			changed.generation = 99
		case "transport":
			changed.transportGeneration = 99
		case "object":
			changed.deployedObjectID = 11
		case "noun":
			changed.binding.Creatures[0].Noun = 8
		case "slot":
			changed.deployedCreatureIndex = 1
		case "squad":
			changed.squad = &squad.Session{}
		case "retirement":
			changed.scenarioAbility.retirement = &scenarioHeroRetirement{}
		}
		if changed.scenarioHeroAbilityRank(2) != 2 {
			t.Fatalf("%s reused stale rank override", name)
		}
	}
	switched := current
	switched.clearEnemyHeroStatuses()
	if switched.scenarioHeroAbilityRank(2) != 2 {
		t.Fatal("ordinary switch cleanup retained override")
	}
	req.AbilityRank = 0
	receipt, err := registry.ScenarioResetHero(context.Background(), req, game.GameplayCreature{})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !receipt.IsAbilityRankSet || receipt.AbilityRank != 0 || registry.sessions["a"].scenarioHeroAbilityRank(2) != 2 {
		t.Fatal("rank0 did not clear fixture")
	}
	current.scenarioAbility.rankOverride = override
	isGameOver, err := current.squad.SetHitPoints(0, 0)
	if err != nil {
		t.Fatalf("death: %v", err)
	}
	if !isGameOver || current.scenarioHeroAbilityRank(2) != 2 {
		t.Fatal("defeated actor retained override")
	}
}

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
	health, power := float32(20), float32(8)
	req.HitPoint, req.ManaPoint = &health, &power
	receipt, err = registry.ScenarioResetHero(context.Background(), req, game.GameplayCreature{})
	if err != nil {
		t.Fatalf("deficit: %v", err)
	}
	after = registry.sessions["a"]
	if after.deployedHitPoint() != 20 || after.deployedManaPoint() != 8 || after.squad != before.squad ||
		receipt.HitPoint != 20 || receipt.ManaPoint != 8 {
		t.Fatal("resource deficit did not retain actor lifetime")
	}
	for _, invalidHealth := range []float32{0, -1, 101} {
		req.HitPoint = &invalidHealth
		receipt, err = registry.ScenarioResetHero(context.Background(), req, game.GameplayCreature{})
		if err == nil || receipt.IsQueued || registry.sessions["a"].deployedHitPoint() != 20 {
			t.Fatal("invalid resource request mutated living hero")
		}
	}
	req.HitPoint, req.ManaPoint = nil, nil
	deadline := time.Now().Add(time.Minute)
	after.heroInputLockedUntil = deadline
	reservation, isReserved := after.abilityCooldownSession().Reserve(ability.HeroAbilityCooldown(42), time.Now(), time.Minute)
	if !isReserved || reservation == (ability.CooldownReservation{}) {
		t.Fatal("cooldown reservation unavailable")
	}
	registry.sessions["a"] = after
	req.IsCooldownReset = true
	receipt, err = registry.ScenarioResetHero(context.Background(), req, game.GameplayCreature{})
	if err != nil {
		t.Fatalf("cooldown: %v", err)
	}
	after = registry.sessions["a"]
	if !receipt.IsCooldownReset || receipt.CooldownCount != 1 ||
		!after.abilityCooldownSession().IsReady(ability.HeroAbilityCooldown(42), time.Now()) ||
		after.heroInputLockedUntil != deadline || after.deployedHitPoint() != 20 || after.deployedManaPoint() != 8 {
		t.Fatal("cooldown reset changed recovery/resources or retained cooldown")
	}
	req.IsCooldownReset = false
	receipt, err = registry.ScenarioResetHero(context.Background(), req, game.GameplayCreature{})
	if err != nil {
		t.Fatalf("restoreBaseline: %v", err)
	}
	after = registry.sessions["a"]
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
	if err == nil || receipt.IsQueued {
		t.Fatal("replacement committed without retirement")
	}
	for _, sessionKey := range []string{"a", "b"} {
		member := registry.sessions[sessionKey]
		member.pendingPacketBatches = nil
		registry.sessions[sessionKey] = member
	}
	req.IsRetirement = true
	receipt, err = registry.ScenarioResetHero(context.Background(), req, replacement)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	retired := registry.sessions["a"]
	if !receipt.IsRetirement || !receipt.IsQueued || receipt.IsReplacement || receipt.RecipientCount != 2 ||
		retired.scenarioAbility.retirement == nil || retired.squad == previousSquad ||
		retired.binding.Creatures[0] != previousBinding.Creatures[0] ||
		!retired.heroInputLockedUntil.After(time.Now()) {
		t.Fatal("retirement did not preserve the old template and fence its actor")
	}
	for _, sessionKey := range []string{"a", "b"} {
		member := registry.sessions[sessionKey]
		deleteCount := 0
		rosterCount := 0
		for _, batch := range member.pendingPacketBatches {
			for _, packet := range batch.packets {
				if len(packet) == 0 {
					t.Fatal("empty retirement packet")
				}
				if raknet.PacketID(packet[0]) == raknet.ObjectCreate {
					t.Fatal("retirement recreated the object before the client fence")
				}
				if raknet.PacketID(packet[0]) == raknet.ObjectDelete {
					deleteCount++
				}
				if raknet.PacketID(packet[0]) == raknet.LabsPlayerUpdate {
					if deleteCount != 1 {
						t.Fatal("new appearance roster preceded old object deletion")
					}
					rosterCount++
				}
			}
		}
		if deleteCount != 1 || rosterCount != 1 {
			t.Fatal("retirement did not queue exactly one deletion and appearance roster to each owned participant")
		}
	}
	for _, rejected := range []scenario.HeroResetRequest{
		req,
		{UserID: 1, GameID: 11, PeerGeneration: 1, TransportGeneration: 1, ObjectID: 10, OwnedUserIDs: []int64{1, 2}},
		{UserID: 1, GameID: 11, PeerGeneration: 1, TransportGeneration: 1, ObjectID: 10, Noun: 8, OwnedUserIDs: []int64{1, 2}},
		{UserID: 1, GameID: 11, PeerGeneration: 1, TransportGeneration: 1, ObjectID: 10, IsCooldownReset: true, OwnedUserIDs: []int64{1, 2}},
		{UserID: 1, GameID: 11, PeerGeneration: 1, TransportGeneration: 1, ObjectID: 10, IsPlacement: true, Position: req.Position, OwnedUserIDs: []int64{1, 2}},
	} {
		rejectedReceipt, rejectedErr := registry.ScenarioResetHero(context.Background(), rejected, replacement)
		if rejectedErr == nil || rejectedReceipt.IsQueued || registry.sessions["a"].scenarioAbility.retirement != retired.scenarioAbility.retirement {
			t.Fatal("pending replacement accepted another mutation or lost its marker")
		}
	}
	req.IsRetirement = false
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	receipt, err = registry.ScenarioResetHero(canceledContext, req, replacement)
	if err == nil || receipt.IsQueued || registry.sessions["a"].scenarioAbility.retirement == nil {
		t.Fatal("canceled commit recreated the actor or cleared retirement")
	}
	retirementBatchID := retired.nextPendingPacketID
	receipt, err = registry.ScenarioResetHero(context.Background(), req, replacement)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	after = registry.sessions["a"]
	if after.scenarioAbility.retirement != nil || !after.heroInputLockedUntil.IsZero() {
		t.Fatal("successful commit retained retirement state or its input lock")
	}
	for _, batch := range after.pendingPacketBatches {
		if batch.id <= retirementBatchID {
			continue
		}
		for _, packet := range batch.packets {
			if len(packet) > 0 && raknet.PacketID(packet[0]) == raknet.ObjectDelete {
				t.Fatal("commit queued a second deletion after the client fence")
			}
		}
	}
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
