package gameplay

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

func arenaBurstTestMember(t *testing.T, userID uint64, team uint16, objectID uint32, health float32) gameplayPeerSession {
	t.Helper()
	member := gameplayPeerSession{}
	member.generation, member.transportGeneration, member.deployedObjectID = 1, 1, objectID
	member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 1, UserID: userID, Team: team}
	member.binding.Creatures[0] = game.GameplayCreature{Noun: objectID, HitPoint: 100, PowerPoint: 50}
	member.stage.EnterDungeon()
	member.isArenaCombatActive = true
	var err error
	member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: health, ManaPoints: 50, IsAvailable: true}}, 0)
	if err != nil {
		t.Fatalf("squad: %v", err)
	}
	err = member.teleportPlayer(time.Now(), raknet.Vector3{X: 100 + float32(objectID), Y: 100, Z: 1})
	if err != nil {
		t.Fatalf("position: %v", err)
	}
	return member
}

func TestBurstRetargetUsesArenaEnemiesAndVisitedObjects(t *testing.T) {
	for _, team := range []uint16{1, 2} {
		registry := newGameplaySessionRegistry(nil, time.Now, nil)
		source := arenaBurstTestMember(t, 1, team, 1, 100)
		friend := arenaBurstTestMember(t, 2, team, 4, 100)
		enemy := arenaBurstTestMember(t, 3, 3-team, 7, 100)
		registry.sessions["source"], registry.sessions["friend"], registry.sessions["enemy"] = source, friend, enemy
		registry.arenaMatches[1] = &arenaMatchState{isCombatActive: true, random: sim.NewSimulatorRandom(104729)}
		schedule := heroBurstSchedule{runtime: campaignAbilityCommandRuntime{registry: registry, now: time.Now},
			sourcePosition: game.Vec3(source.playerPosition), definition: sim.AbilityDefinition{Distance: 50, ShotHitChance: 1},
			shotTargetObjectIDs: make(map[int]uint32)}
		targetID, position, err := schedule.selectRetargetShot(source)
		if err != nil {
			t.Fatalf("retarget: %v", err)
		}
		if targetID != 7 || position != enemy.playerPosition {
			t.Fatalf("team=%d selected target=%d position=%v", team, targetID, position)
		}
		if registry.arenaMatches[1].random.DrawCount() != 1 {
			t.Fatal("singleton retarget consumed a candidate-selection draw in addition to its hit roll")
		}
		schedule.shotTargetObjectIDs[0] = targetID
		targetID, position, err = schedule.selectRetargetShot(source)
		if err != nil {
			t.Fatalf("visited: %v", err)
		}
		if targetID != 0 {
			t.Fatalf("visited enemy was reused, team=%d target=%d position=%v", team, targetID, position)
		}
	}
}

func TestBurstLifeStealUsesCurrentArenaCapacityAndCastingHero(t *testing.T) {
	fixtures := []struct {
		name        string
		health      float32
		damage      float32
		capacity    float32
		isSwapped   bool
		wantHealing float32
	}{
		{name: "clamped", health: 90, damage: 40, wantHealing: 10},
		{name: "temporary capacity", health: 120, damage: 40, capacity: 25, wantHealing: 5},
		{name: "full", health: 100, damage: 40},
		{name: "blocked", health: 90},
		{name: "swapped", health: 90, damage: 40, isSwapped: true},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			member := arenaBurstTestMember(t, 1, 1, 1, fixture.health)
			member.maximumHitPoints[0], member.maximumManaPoints[0] = 999, 888
			member.arenaHitPointIncreases[0] = fixture.capacity
			if fixture.isSwapped {
				member.deployedObjectID = 2
			}
			schedule := heroBurstSchedule{sourceObjectID: 1, creatureIndex: 0, definition: sim.AbilityDefinition{LifeSteal: 0.5}}
			packets, healed, err := schedule.applyLifeSteal(&member, fixture.damage)
			if err != nil {
				t.Fatalf("heal: %v", err)
			}
			if healed != fixture.wantHealing || member.deployedHitPoint() != fixture.health+fixture.wantHealing {
				t.Fatalf("healing=%g health=%g", healed, member.deployedHitPoint())
			}
			if (len(packets) > 0) != (healed > 0) {
				t.Fatal("healing packets disagree with actual restoration")
			}
		})
	}
}

func TestBurstFailureRefundsOnlyBeforeLaunch(t *testing.T) {
	for _, isLaunched := range []bool{false, true} {
		name := "before"
		if isLaunched {
			name = "after"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			registry := newGameplaySessionRegistry(nil, func() time.Time { return now }, nil)
			member := gameplayPeerSession{}
			member.generation, member.deployedObjectID = 1, 1
			member.binding.Mode = game.ModeArena
			member.binding.GameID, member.binding.UserID, member.binding.Team = 1, 1, 1
			member.binding.Creatures[0] = game.GameplayCreature{Noun: 7, HitPoint: 100, PowerPoint: 50,
				PassiveAbility: util.HashID("SoulRavagerPassive"), PassiveMaximumStack: 5,
				PassiveKillDamageIncrease: 0.05}
			var err error
			member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, ManaPoints: 30, IsAvailable: true}}, 0)
			if err != nil {
				t.Fatalf("squad: %v", err)
			}
			definition := sim.AbilityDefinition{Name: "burst", AnimationName: "cast", ProjectileNoun: "projectile",
				ImpactEffectName: "impact", HitDelays: []time.Duration{0}, ReleaseDelay: time.Second,
				Cooldown: 10 * time.Second, Speed: 20, Distance: 10}
			run, packets, err := abilityraknet.NewBurstRun(abilityraknet.BurstInput{Ability: definition, ActorObjectID: 1,
				TargetObjectID: 4, ProjectileObjectIDs: []uint32{1000}, ActorFacing: sim.Position{X: 1},
				TargetPosition: sim.Position{X: 10}, Damage: 5, TargetHitPoint: 100, StartedAt: now})
			if err != nil {
				t.Fatalf("burst: %v", err)
			}
			if len(packets) == 0 {
				t.Fatal("missing admission presentation")
			}
			if isLaunched {
				packets, err = run.Advance(context.Background(), 0)
				if err != nil {
					t.Fatalf("launch: %v", err)
				}
				if len(packets) == 0 {
					t.Fatal("missing launch presentation")
				}
			}
			cooldownKey := zoneability.HeroAbilityCooldown(7)
			cooldown, isReserved := member.abilityCooldownSession().Reserve(cooldownKey, now, 10*time.Second)
			if !isReserved {
				t.Fatal("cooldown not reserved")
			}
			release, isReserved := member.abilityReleaseSession().Reserve(now, time.Second)
			if !isReserved {
				t.Fatal("release not reserved")
			}
			member.heroBurstAttacks = map[uint32]*abilityraknet.BurstRun{1000: run}
			registry.sessions["source"] = member
			registry.arenaMatches[1] = &arenaMatchState{isCombatActive: true}
			runtime := campaignAbilityCommandRuntime{registry: registry, now: func() time.Time { return now }, logger: log.New(io.Discard, "", 0)}
			schedule := heroBurstSchedule{runtime: runtime, sessionKey: "source", generation: 1, run: run,
				firstProjectileObjectID: 1000, sourceObjectID: 1, previousManaPoint: 50, passiveKillStack: 2,
				cooldownReservation: cooldown, releaseReservation: release}
			schedule.fail(errors.New("injected publication failure"))
			current := registry.sessions["source"]
			wantPower := float32(50)
			wantStack := uint32(2)
			if isLaunched {
				wantPower = 30
				wantStack = 0
			}
			if current.deployedManaPoint() != wantPower || current.passiveKillStack[0] != wantStack ||
				current.abilityCooldownSession().IsReady(cooldownKey, now) == isLaunched {
				t.Fatal("burst failure refunded a launched activation or retained an unlaunched reservation")
			}
		})
	}
}
