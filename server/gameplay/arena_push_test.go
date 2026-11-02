package gameplay

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type arenaPushTestTimer struct {
	arenaStatusTestTimer
	attempt     int
	failAt      int
	isNilCancel bool
}

func (e *arenaPushTestTimer) Schedule(delay time.Duration, execute func()) (func(), error) {
	e.attempt++
	if e.attempt == e.failAt {
		if e.isNilCancel {
			return nil, nil
		}
		return nil, errors.New("push timer failure")
	}
	cancel, err := e.arenaStatusTestTimer.Schedule(delay, execute)
	if err != nil {
		return nil, fmt.Errorf("pushSchedule: %w", err)
	}
	return cancel, nil
}

type arenaPushTestFixture struct {
	targets arenaAbilityTargets
	timer   *arenaPushTestTimer
	plan    zoneability.AreaPlan
	results []zoneability.AreaResult
}

func newArenaPushTestFixture(t *testing.T) arenaPushTestFixture {
	t.Helper()
	timer := &arenaPushTestTimer{}
	registry := newGameplaySessionRegistry(timer, time.Now, nil)
	for index, key := range []string{"source", "target", "ally", "foreign"} {
		member := gameplayPeerSession{}
		member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7, UserID: uint64(index + 1), Team: 2}
		if key == "source" || key == "ally" {
			member.binding.Team = 1
		}
		if key == "foreign" {
			member.binding.GameID = 8
		}
		member.binding.Creatures[0] = game.GameplayCreature{Noun: 8, HitPoint: 100, MaximumHitPoint: 100}
		member.generation, member.transportGeneration, member.deployedObjectID = uint64(index+1), uint64(index+2), uint32(index+10)
		member.isArenaCombatActive = true
		member.stage.EnterDungeon()
		var err error
		member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, IsAvailable: true}}, 0)
		if err != nil {
			t.Fatalf("squad: %v", err)
		}
		member.playerPosition = raknet.Vector3{X: 2}
		registry.sessions[key] = member
	}
	runtime := campaignAbilityCommandRuntime{registry: registry, modifierPool: newModifierPool(), now: time.Now}
	return arenaPushTestFixture{
		targets: arenaAbilityTargets{runtime: runtime, source: registry.sessions["source"], timestamp: 100}, timer: timer,
		plan:    zoneability.AreaPlan{SourceObjectID: 10, Center: game.Vec3{}},
		results: []zoneability.AreaResult{{Damage: zonenpc.DamageResult{ObjectID: 11, Damage: 5}}},
	}
}

func (e *arenaPushTestFixture) apply() ([][]byte, error) {
	e.targets.runtime.registry.mutex.Lock()
	defer e.targets.runtime.registry.mutex.Unlock()
	packets, err := e.targets.applyCometPushesLocked(e.plan, e.results)
	if err != nil {
		return packets, fmt.Errorf("pushApply: %w", err)
	}
	return packets, nil
}

func (e arenaPushTestFixture) run(t *testing.T) *arenaHitPushRun {
	t.Helper()
	member := e.targets.runtime.registry.sessions["target"]
	for _, run := range member.arenaHitPushes {
		return run
	}
	t.Fatal("recipient push owner missing")
	return nil
}

func TestArenaHitPushLifetime(t *testing.T) {
	fixture := newArenaPushTestFixture(t)
	packets, err := fixture.apply()
	if err != nil || len(packets) < 2 {
		t.Fatalf("apply packets=%d: %v", len(packets), err)
	}
	run := fixture.run(t)
	registry := fixture.targets.runtime.registry
	member := registry.sessions["target"]
	if len(fixture.timer.jobs) != 2 || member.playerPosition.X < 5.99 || member.playerPosition.X > 6.01 || !run.ownsLanding(member) {
		t.Fatal("push did not commit its destination and two recipient timers")
	}
	// The target's reaction must survive caster departure.
	delete(registry.sessions, "source")
	fixture.timer.jobs[0]()
	member = registry.sessions["target"]
	if !member.heroKnockbackLanding.isLanded || run.isRetired || len(member.pendingPacketBatches) != 1 {
		t.Fatal("landing failed to reset animation while retaining the outro")
	}
	if len(registry.sessions["ally"].pendingPacketBatches) != 1 || len(registry.sessions["foreign"].pendingPacketBatches) != 0 {
		t.Fatal("landing presentation crossed or missed its realm")
	}
	fixture.timer.jobs[1]()
	member = registry.sessions["target"]
	if !run.isRetired || len(member.arenaHitPushes) != 0 || member.heroKnockbackLanding.objectID != 0 || len(member.pendingPacketBatches) != 2 {
		t.Fatal("outro failed to retire the owner and native modifier")
	}
	err = fixture.targets.runtime.modifierPool.Release(run.instanceID)
	if err == nil {
		t.Fatal("outro retained the modifier lease")
	}
}

func TestArenaHitPushReplacementAndCancellation(t *testing.T) {
	fixture := newArenaPushTestFixture(t)
	packets, err := fixture.apply()
	if err != nil || len(packets) == 0 {
		t.Fatalf("first push: %v", err)
	}
	old := fixture.run(t)
	packets, err = fixture.apply()
	if err != nil || len(packets) == 0 {
		t.Fatalf("replacement push: %v", err)
	}
	current := fixture.run(t)
	if current == old || !old.isRetired || !fixture.timer.cancellations[0] || !fixture.timer.cancellations[1] {
		t.Fatal("reapplication retained the old owner or its timers")
	}
	fixture.timer.jobs[0]()
	fixture.timer.jobs[1]()
	registry := fixture.targets.runtime.registry
	member := registry.sessions["target"]
	if current.isRetired || !current.ownsLanding(member) || len(member.pendingPacketBatches) != 0 {
		t.Fatal("stale callback changed the replacement reaction")
	}
	registry.mutex.Lock()
	member.retireArenaHitStatuses()
	registry.sessions["target"] = member
	registry.mutex.Unlock()
	if !current.isRetired || !fixture.timer.cancellations[2] || !fixture.timer.cancellations[3] || member.heroKnockbackLanding.objectID != 0 {
		t.Fatal("recipient cleanup retained timers or motion ownership")
	}
	count := len(member.pendingPacketBatches)
	fixture.timer.jobs[2]()
	fixture.timer.jobs[3]()
	if len(registry.sessions["target"].pendingPacketBatches) != count {
		t.Fatal("canceled callbacks published against the retired recipient")
	}
	err = fixture.targets.runtime.modifierPool.Release(current.instanceID)
	if err == nil {
		t.Fatal("recipient cleanup leaked its modifier lease")
	}
}

func TestArenaHitPushIdentityFence(t *testing.T) {
	for _, name := range []string{"game", "object", "peer", "transport", "slot", "noun", "squad"} {
		t.Run(name, func(t *testing.T) {
			fixture := newArenaPushTestFixture(t)
			packets, err := fixture.apply()
			if err != nil || len(packets) == 0 {
				t.Fatalf("apply: %v", err)
			}
			run := fixture.run(t)
			registry := fixture.targets.runtime.registry
			member := registry.sessions["target"]
			switch name {
			case "game":
				member.binding.GameID++
			case "object":
				member.deployedObjectID++
			case "peer":
				member.generation++
			case "transport":
				member.transportGeneration++
			case "slot":
				member.deployedCreatureIndex++
			case "noun":
				member.binding.Creatures[0].Noun++
			case "squad":
				member.squad = &squad.Session{}
			}
			registry.sessions["target"] = member
			fixture.timer.jobs[0]()
			fixture.timer.jobs[1]()
			if !run.isRetired || len(registry.sessions["target"].pendingPacketBatches) != 0 || len(registry.sessions["ally"].pendingPacketBatches) != 0 {
				t.Fatal("stale identity received old landing or modifier deletion")
			}
			err = fixture.targets.runtime.modifierPool.Release(run.instanceID)
			if err == nil {
				t.Fatal("stale recipient leaked old modifier lease")
			}
		})
	}
}

func TestArenaHitPushTimerPreparationFailure(t *testing.T) {
	for _, isNilCancel := range []bool{false, true} {
		for _, attempt := range []int{1, 2} {
			t.Run(fmt.Sprintf("attempt%d/nil%t", attempt, isNilCancel), func(t *testing.T) {
				fixture := newArenaPushTestFixture(t)
				fixture.timer.failAt, fixture.timer.isNilCancel = attempt, isNilCancel
				packets, err := fixture.apply()
				if err == nil || len(packets) != 0 {
					t.Fatalf("failed preparation published packets=%d err=%v", len(packets), err)
				}
				member := fixture.targets.runtime.registry.sessions["target"]
				if member.playerPosition.X != 2 || member.heroKnockbackLanding.objectID != 0 || len(member.arenaHitPushes) != 0 {
					t.Fatal("timer failure partially committed recipient motion")
				}
				if attempt == 2 && !fixture.timer.cancellations[0] {
					t.Fatal("outro preparation failure retained landing timer")
				}
				instanceID, allocationErr := fixture.targets.runtime.modifierPool.Allocate()
				if allocationErr != nil {
					t.Fatalf("lease reuse: %v", allocationErr)
				}
				if uint16(instanceID) != 0 {
					t.Fatal("timer failure leaked the allocated modifier slot")
				}
				releaseErr := fixture.targets.runtime.modifierPool.Release(instanceID)
				if releaseErr != nil {
					t.Fatalf("lease cleanup: %v", releaseErr)
				}
			})
		}
	}
}

func TestArenaHitPushFailedReplacementRetainsOwner(t *testing.T) {
	fixture := newArenaPushTestFixture(t)
	packets, err := fixture.apply()
	if err != nil || len(packets) == 0 {
		t.Fatalf("first push: %v", err)
	}
	current := fixture.run(t)
	before := fixture.targets.runtime.registry.sessions["target"]
	fixture.timer.failAt = 4
	packets, err = fixture.apply()
	if err == nil || len(packets) != 0 {
		t.Fatalf("replacement preparation packets=%d err=%v", len(packets), err)
	}
	after := fixture.targets.runtime.registry.sessions["target"]
	if fixture.run(t) != current || current.isRetired || after.playerPosition != before.playerPosition ||
		after.heroKnockbackLanding != before.heroKnockbackLanding || fixture.timer.cancellations[0] || fixture.timer.cancellations[1] {
		t.Fatal("failed preparation interrupted the already accepted recipient reaction")
	}
	if !fixture.timer.cancellations[2] {
		t.Fatal("failed replacement retained its prepared landing timer")
	}
	fixture.timer.jobs[0]()
	fixture.timer.jobs[1]()
	if !current.isRetired {
		t.Fatal("failed replacement prevented original lifetime completion")
	}
}

func TestArenaHitPushDoesNotClearNewMotionOwner(t *testing.T) {
	fixture := newArenaPushTestFixture(t)
	packets, err := fixture.apply()
	if err != nil || len(packets) == 0 {
		t.Fatalf("apply: %v", err)
	}
	run := fixture.run(t)
	registry := fixture.targets.runtime.registry
	member := registry.sessions["target"]
	member.heroKnockbackLanding.readyAt = member.heroKnockbackLanding.readyAt.Add(time.Second)
	newLanding := member.heroKnockbackLanding
	registry.sessions["target"] = member
	fixture.timer.jobs[0]()
	if len(registry.sessions["target"].pendingPacketBatches) != 0 {
		t.Fatal("old landing reset a replacement mover's animation")
	}
	fixture.timer.jobs[1]()
	member = registry.sessions["target"]
	if !run.isRetired || member.heroKnockbackLanding != newLanding || len(member.arenaHitPushes) != 0 || len(member.pendingPacketBatches) != 1 {
		t.Fatal("old expiry cleared another mover or failed to remove its own modifier")
	}
}
