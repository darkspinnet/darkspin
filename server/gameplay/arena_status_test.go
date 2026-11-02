package gameplay

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The shared owner must isolate matches, survive its caster leaving, and keep
// stale expiry callbacks from releasing a refreshed or replaced recipient.
func TestArenaAcceptedHitStatusOwnership(t *testing.T) {
	at := time.Now()
	member := gameplayPeerSession{}
	member.generation, member.transportGeneration, member.deployedObjectID = 2, 3, 10
	member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7, UserID: 1}
	member.binding.Creatures[0] = game.GameplayCreature{Noun: 8}
	member.squad = &squad.Session{}
	status := &arenaHitStatus{gameID: 7, objectID: 10, generation: 2, transportGeneration: 3,
		noun: 8, squad: member.squad, modifierID: 20, instanceID: 30,
		kind: sim.AbilityStatusKindStun, expiresAt: at.Add(time.Second)}
	member.arenaHitStatuses = map[arenaHitStatusKey]*arenaHitStatus{{modifierID: 20}: status}
	if !member.isEnemyStunActive(at) || member.isEnemyStunActive(at.Add(time.Second)) {
		t.Fatal("stun gate does not follow accepted target lifetime")
	}
	for _, name := range []string{"game", "actor", "peer", "transport", "squad", "slot", "noun"} {
		changed := member
		switch name {
		case "game":
			changed.binding.GameID = 9
		case "actor":
			changed.deployedObjectID = 11
		case "peer":
			changed.generation = 9
		case "transport":
			changed.transportGeneration = 9
		case "squad":
			changed.squad = &squad.Session{}
		case "slot":
			changed.deployedCreatureIndex = 1
		case "noun":
			changed.binding.Creatures[0].Noun = 9
		}
		if changed.isArenaHitStunActive(at) {
			t.Fatalf("%s inherited another target's stun", name)
		}
	}
	status.kind = sim.AbilityStatusKindSlow
	status.movementScale, status.attackScale = .5, .8
	movement, attack := member.arenaHitSlowBuffs(at)
	if movement != -.5 || attack > -.199 || attack < -.201 {
		t.Fatal("shared slow projections do not match authored scales")
	}
	status.isRetired = true
	movement, attack = member.arenaHitSlowBuffs(at)
	if movement != 0 || attack != 0 {
		t.Fatal("retired modifier retained numerical effects")
	}
}

type arenaStatusTestTimer struct {
	jobs          []func()
	cancellations []bool
	failure       error
}

type arenaStatusTestCancellation struct {
	timer *arenaStatusTestTimer
	index int
}

func (e arenaStatusTestCancellation) cancel() { e.timer.cancellations[e.index] = true }

func (e *arenaStatusTestTimer) Schedule(delay time.Duration, execute func()) (func(), error) {
	if e.failure != nil {
		return nil, fmt.Errorf("timerFailure: %w", e.failure)
	}
	e.jobs = append(e.jobs, execute)
	e.cancellations = append(e.cancellations, false)
	cancellation := arenaStatusTestCancellation{timer: e, index: len(e.jobs) - 1}
	return cancellation.cancel, nil
}

func TestArenaAcceptedHitStatusAdmission(t *testing.T) {
	at := time.Now()
	timer := &arenaStatusTestTimer{}
	registry := newGameplaySessionRegistry(timer, time.Now, nil)
	registry.arenaMatches[7] = &arenaMatchState{isCombatActive: true}
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
		if key == "target" {
			member.binding.Creatures[0].PartAttribute[73] = 1
		}
		member.generation, member.transportGeneration, member.deployedObjectID = uint64(index+1), uint64(index+1), uint32(index+10)
		member.isArenaCombatActive = true
		member.stage.EnterDungeon()
		var err error
		member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, IsAvailable: true}}, 0)
		if err != nil {
			t.Fatalf("squad: %v", err)
		}
		registry.sessions[key] = member
	}
	runtime := campaignAbilityCommandRuntime{registry: registry, modifierPool: newModifierPool(), now: time.Now}
	definition := sim.AbilityDefinition{StatusKind: sim.AbilityStatusKindStun, RootModifierID: 20, StatusDuration: time.Second}
	plan := zoneability.AreaPlan{Definition: definition}
	results := []zoneability.AreaResult{}
	for _, id := range []uint32{11, 12, 13} {
		results = append(results, zoneability.AreaResult{Damage: zonenpc.DamageResult{ObjectID: id, Damage: 5}})
	}
	packets, err := runtime.applyAcceptedHitStatus(raknet.Packet{}, "source", 1, 10, 100, plan, results)
	if err != nil || len(packets) != 0 {
		t.Fatalf("apply: packets=%d err=%v", len(packets), err)
	}
	target := registry.sessions["target"]
	if !target.isEnemyStunActive(at) || len(target.arenaHitStatuses) != 1 || len(timer.jobs) != 1 {
		t.Fatal("accepted living hostile hit did not create target-owned stun")
	}
	if len(registry.sessions["ally"].arenaHitStatuses) != 0 || len(registry.sessions["foreign"].arenaHitStatuses) != 0 || len(registry.sessions["foreign"].pendingPacketBatches) != 0 {
		t.Fatal("accepted status crossed team/game boundary")
	}
	delete(registry.sessions, "source")
	if !target.isEnemyStunActive(at) {
		t.Fatal("caster departure canceled recipient modifier")
	}
	status := target.arenaHitStatuses[arenaHitStatusKey{modifierID: 20}]
	timer.jobs[0]()
	if !status.isRetired || registry.sessions["target"].isArenaHitStunActive(at) {
		t.Fatal("expiry did not retire recipient modifier")
	}
}

func TestArenaAcceptedHitStatusStaleExpiry(t *testing.T) {
	registry := newGameplaySessionRegistry(nil, time.Now, nil)
	pool := newModifierPool()
	instanceID, err := pool.Allocate()
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	old := &arenaHitStatus{runtime: campaignAbilityCommandRuntime{registry: registry, modifierPool: pool},
		targetKey: "target", modifierID: 20, instanceID: instanceID, gameID: 7, objectID: 10, generation: 2}
	current := &arenaHitStatus{modifierID: 20, instanceID: instanceID + 1}
	member := gameplayPeerSession{}
	member.arenaHitStatuses = map[arenaHitStatusKey]*arenaHitStatus{{modifierID: 20}: current}
	registry.sessions["target"] = member
	arenaHitStatusExpiry{status: old, revision: old.revision}.expire()
	if !old.isRetired || registry.sessions["target"].arenaHitStatuses[arenaHitStatusKey{modifierID: 20}] != current || len(registry.sessions["target"].pendingPacketBatches) != 0 {
		t.Fatal("stale expiry changed refreshed owner")
	}
	instanceID, err = pool.Allocate()
	if err != nil {
		t.Fatalf("replacementAllocate: %v", err)
	}
	old = &arenaHitStatus{runtime: campaignAbilityCommandRuntime{registry: registry, modifierPool: pool},
		targetKey: "target", modifierID: 20, instanceID: instanceID, gameID: 7, objectID: 10, generation: 2}
	member.arenaHitStatuses[arenaHitStatusKey{modifierID: 20}] = old
	registry.sessions["target"] = member
	arenaHitStatusExpiry{status: old, revision: old.revision}.expire()
	if !old.isRetired || len(registry.sessions["target"].pendingPacketBatches) != 0 {
		t.Fatal("old actor expiry presented against replacement")
	}
}

func TestArenaAcceptedHitStatusLethalRetirement(t *testing.T) {
	for _, isReserveAlive := range []bool{false, true} {
		registry := newGameplaySessionRegistry(nil, time.Now, nil)
		pool := newModifierPool()
		member := gameplayPeerSession{}
		member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7, UserID: 1}
		member.binding.Creatures[0] = game.GameplayCreature{ID: 1, Noun: 8, HitPoint: 100, MaximumHitPoint: 100}
		member.binding.Creatures[1] = game.GameplayCreature{ID: 2, Noun: 9, HitPoint: 50, MaximumHitPoint: 50}
		member.generation, member.transportGeneration, member.deployedObjectID = 2, 3, 1
		characters := [squad.Size]squad.Character{{HitPoints: 100, IsAvailable: true}}
		if isReserveAlive {
			characters[1] = squad.Character{HitPoints: 50, IsAvailable: true}
		}
		var err error
		member.squad, err = squad.New(characters, 0)
		if err != nil {
			t.Fatalf("squad: %v", err)
		}
		instanceID, err := pool.Allocate()
		if err != nil {
			t.Fatalf("allocate: %v", err)
		}
		status := &arenaHitStatus{runtime: campaignAbilityCommandRuntime{registry: registry, modifierPool: pool, now: time.Now},
			targetKey: "target", gameID: 7, objectID: 1, index: 0, noun: 8, generation: 2,
			transportGeneration: 3, squad: member.squad, modifierID: 20, instanceID: instanceID,
			kind: sim.AbilityStatusKindStun, expiresAt: time.Now().Add(time.Second)}
		member.arenaHitStatuses = map[arenaHitStatusKey]*arenaHitStatus{{modifierID: 20}: status}
		member.enemyControlPresentedObjectID = 1
		packets, err := member.applyDamageHitPointsWithOutcome(0, uint64(time.Now().UnixMilli()), false)
		if err != nil {
			t.Fatalf("lethal reserve=%t packets=%d: %v", isReserveAlive, len(packets), err)
		}
		if !status.isRetired || len(member.arenaHitStatuses) != 0 || member.enemyControlPresentedObjectID != 0 {
			t.Fatalf("lethal reserve=%t retained defeated actor's modifier/control", isReserveAlive)
		}
		if isReserveAlive && member.deployedCreatureIndex != 1 {
			t.Fatal("lethal retirement prevented ordinary automatic reserve deployment")
		}
	}
}

func TestArenaAcceptedHitStatusCasterOwnership(t *testing.T) {
	for _, assetName := range []string{"TCShieldedSentinelSupport", "MissileTempestSupport"} {
		checkArenaStatusCasterOwnership(t, assetName)
	}
}

func checkArenaStatusCasterOwnership(t *testing.T, assetName string) {
	t.Helper()
	timer := &arenaStatusTestTimer{}
	registry := newGameplaySessionRegistry(timer, time.Now, nil)
	registry.arenaMatches[7] = &arenaMatchState{isCombatActive: true}
	for index, key := range []string{"first", "second", "target"} {
		member := gameplayPeerSession{}
		member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7, UserID: uint64(index + 1), Team: 1}
		if key == "target" {
			member.binding.Team = 2
		}
		member.binding.Creatures[0] = game.GameplayCreature{Noun: 8, HitPoint: 100, MaximumHitPoint: 100}
		member.generation, member.transportGeneration, member.deployedObjectID = uint64(index+1), uint64(index+1), uint32(index+10)
		member.isArenaCombatActive = true
		member.stage.EnterDungeon()
		var err error
		member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, IsAvailable: true}}, 0)
		if err != nil {
			t.Fatalf("squad: %v", err)
		}
		registry.sessions[key] = member
	}
	runtime := campaignAbilityCommandRuntime{registry: registry, modifierPool: newModifierPool(), now: time.Now}
	definition := sim.AbilityDefinition{Name: assetName, StatusKind: sim.AbilityStatusKindSlow,
		RootModifierID: 20, StatusDuration: time.Second, StatusMovementScale: .5, StatusAttackScale: 1}
	plan := zoneability.AreaPlan{Definition: definition}
	results := []zoneability.AreaResult{{Damage: zonenpc.DamageResult{ObjectID: 12, Damage: 5}}}
	target := registry.sessions["target"]
	target.binding.Creatures[0].PartAttribute[83] = 1
	registry.sessions["target"] = target
	var packets [][]byte
	var err error
	for index, key := range []string{"first", "second", "first"} {
		source := registry.sessions[key]
		if assetName == "MissileTempestSupport" {
			// Flak requests its modifier before damage; no DamageResult is required.
			registry.mutex.Lock()
			packets, err = runtime.applyArenaTargetStatusLocked(key, source.generation, source.deployedObjectID, uint64(100+index), plan, []uint32{12})
			registry.mutex.Unlock()
		} else {
			packets, err = runtime.applyAcceptedHitStatus(raknet.Packet{}, key, source.generation, source.deployedObjectID, uint64(100+index), plan, results)
		}
		if err != nil || len(packets) != 0 {
			t.Fatalf("apply[%d]: packets=%d err=%v", index, len(packets), err)
		}
	}
	target = registry.sessions["target"]
	firstKey := arenaHitStatusKey{modifierID: 20, sourceObjectID: 10}
	secondKey := arenaHitStatusKey{modifierID: 20, sourceObjectID: 11}
	first, second := target.arenaHitStatuses[firstKey], target.arenaHitStatuses[secondKey]
	if len(target.arenaHitStatuses) != 2 || first == nil || second == nil || first.instanceID == second.instanceID || target.enemyMovementSpeedBuff() != -.9 {
		t.Fatal("caster-unique slows did not retain two independent instances and aggregate movement")
	}
	timer.jobs[0]() // Canceled callback for the refreshed first caster.
	if registry.sessions["target"].arenaHitStatuses[firstKey] != first || first.isRetired || second.isRetired {
		t.Fatal("stale same-caster callback retired a current owner")
	}
	timer.jobs[1]()
	target = registry.sessions["target"]
	if !second.isRetired || first.isRetired || len(target.arenaHitStatuses) != 1 || target.enemyMovementSpeedBuff() != -.5 {
		t.Fatal("one caster expiry removed another caster's slow or failed to restore aggregate movement")
	}
	timer.jobs[2]()
	target = registry.sessions["target"]
	if !first.isRetired || len(target.arenaHitStatuses) != 0 || target.enemyMovementSpeedBuff() != 0 {
		t.Fatal("last caster expiry did not restore movement")
	}
}

func TestArenaAcceptedHitStatusRejectsLegacyAction(t *testing.T) {
	for _, kind := range []sim.AbilityStatusKind{sim.AbilityStatusKindStun, sim.AbilityStatusKindSilence} {
		at := time.Now()
		registry := newGameplaySessionRegistry(nil, time.Now, nil)
		member := gameplayPeerSession{}
		member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7, UserID: 1}
		member.binding.Creatures[0] = game.GameplayCreature{Noun: 8, HitPoint: 100, PowerPoint: 30}
		member.generation, member.transportGeneration, member.deployedObjectID = 2, 3, 1
		member.isArenaCombatActive = true
		member.stage.EnterDungeon()
		var err error
		member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, ManaPoints: 30, IsAvailable: true}}, 0)
		if err != nil {
			t.Fatalf("squad: %v", err)
		}
		status := &arenaHitStatus{gameID: 7, objectID: 1, generation: 2, transportGeneration: 3,
			noun: 8, squad: member.squad, modifierID: 20, instanceID: 30,
			kind: kind, expiresAt: at.Add(time.Second)}
		member.arenaHitStatuses = map[arenaHitStatusKey]*arenaHitStatus{status.key(): status}
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10000}
		registry.sessions[address.String()] = member
		var logs bytes.Buffer
		runtime := campaignAbilityCommandRuntime{registry: registry, now: time.Now, logger: log.New(&logs, "", 0)}
		command := raknet.ActionCommandData{Common: raknet.ActionCommon{Type: raknet.ActionUseCharacterAbility, ObjectID: 1},
			Ability: &raknet.ActionAbilityData{Index: 2, Rank: 1}}
		packets, err := runtime.handleArenaCharacter(raknet.Packet{Address: address}, command, member)
		if err != nil {
			t.Fatalf("legacyReject: %v", err)
		}
		wantReason := "stunned"
		if kind == sim.AbilityStatusKindSilence {
			wantReason = "silenced"
		}
		if len(packets) != 1 || len(packets[0]) < 3 || raknet.PacketID(packets[0][0]) != raknet.ActionCommandResponse ||
			raknet.ActionResponseType(packets[0][2]) != raknet.ActionResponseRejected || !strings.Contains(logs.String(), "reason="+wantReason) {
			t.Fatalf("legacy fallback did not reject %s before match/definition lookup", kind)
		}
		after := registry.sessions[address.String()]
		if after.deployedHitPoint() != 100 || after.deployedManaPoint() != 30 || len(after.pendingPacketBatches) != 0 {
			t.Fatal("rejected legacy action mutated resources or queued gameplay effects")
		}
	}
}

type arenaStatusTestClock struct{ at time.Time }

func (e *arenaStatusTestClock) now() time.Time { return e.at }

func TestArenaAcceptedHitStatusChainOnlyImmunity(t *testing.T) {
	for _, kind := range []sim.AbilityStatusKind{sim.AbilityStatusKindSlow, sim.AbilityStatusKindStun, sim.AbilityStatusKindSilence} {
		for _, guard := range []string{"none", "debuff", "overdrive"} {
			checkArenaStatusChainOnlyImmunity(t, kind, guard)
		}
	}
}

func checkArenaStatusChainOnlyImmunity(t *testing.T, kind sim.AbilityStatusKind, guard string) {
	t.Helper()
	at := time.Now()
	timer := &arenaStatusTestTimer{}
	registry := newGameplaySessionRegistry(timer, time.Now, nil)
	registry.arenaMatches[7] = &arenaMatchState{isCombatActive: true}
	for index, key := range []string{"source", "target"} {
		member := gameplayPeerSession{}
		member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7, UserID: uint64(index + 1), Team: uint16(index + 1)}
		member.binding.Creatures[0] = game.GameplayCreature{Noun: 8, HitPoint: 100, MaximumHitPoint: 100}
		member.generation, member.transportGeneration, member.deployedObjectID = uint64(index+1), uint64(index+1), uint32(index+1)
		member.isArenaCombatActive = true
		member.stage.EnterDungeon()
		var err error
		member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, IsAvailable: true}}, 0)
		if err != nil {
			t.Fatalf("squad: %v", err)
		}
		if key == "target" {
			member.binding.Creatures[0].PartAttribute[73] = 1
			member.binding.Creatures[0].PartAttribute[78] = 1
			member.binding.Creatures[0].PartAttribute[83] = 1
			if guard == "debuff" {
				member.heroModifierRun = &heroModifierRun{isDebuffImmune: true}
			}
			if guard == "overdrive" {
				member.binding.IsOverdriveUnlocked = true
				member.overdriveObjectID = member.deployedObjectID
				member.overdriveStartsAt, member.overdriveExpiresAt = at.Add(-time.Second), at.Add(time.Minute)
			}
		}
		registry.sessions[key] = member
	}
	runtime := campaignAbilityCommandRuntime{registry: registry, modifierPool: newModifierPool(), now: time.Now}
	plan := zoneability.AreaPlan{Definition: sim.AbilityDefinition{StatusKind: kind, RootModifierID: 20,
		StatusDuration: time.Second, StatusMovementScale: .5, StatusAttackScale: 1}}
	packets, err := runtime.applyArenaTargetStatus("source", 1, 1, 100, plan, []uint32{2})
	if err != nil || len(packets) != 0 {
		t.Fatalf("apply[%s/%s]: packets=%d err=%v", kind, guard, len(packets), err)
	}
	target := registry.sessions["target"]
	if guard == "none" {
		if len(target.arenaHitStatuses) != 1 || len(timer.jobs) != 1 {
			t.Fatalf("Arena specific immunity incorrectly excluded %s", kind)
		}
		return
	}
	if len(target.arenaHitStatuses) != 0 || len(timer.jobs) != 0 || len(target.pendingPacketBatches) != 0 || target.deployedHitPoint() != 100 {
		t.Fatalf("universal guard[%s/%s] admitted or mutated status", kind, guard)
	}
}

func TestArenaAcceptedHitSilenceRefresh(t *testing.T) {
	for _, duration := range []time.Duration{3 * time.Second, time.Second} {
		clock := &arenaStatusTestClock{at: time.Now()}
		timer := &arenaStatusTestTimer{}
		registry := newGameplaySessionRegistry(timer, clock.now, nil)
		registry.arenaMatches[7] = &arenaMatchState{isCombatActive: true}
		for index, key := range []string{"first", "second", "target"} {
			member := gameplayPeerSession{}
			member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7, UserID: uint64(index + 1), Team: 1}
			if key == "target" {
				member.binding.Team = 2
			}
			member.binding.Creatures[0] = game.GameplayCreature{Noun: 8, HitPoint: 100, MaximumHitPoint: 100}
			member.generation, member.transportGeneration, member.deployedObjectID = uint64(index+1), uint64(index+1), uint32(index+10)
			member.isArenaCombatActive = true
			member.stage.EnterDungeon()
			var err error
			member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, IsAvailable: true}}, 0)
			if err != nil {
				t.Fatalf("squad: %v", err)
			}
			registry.sessions[key] = member
		}
		runtime := campaignAbilityCommandRuntime{registry: registry, modifierPool: newModifierPool(), now: clock.now}
		definition := sim.AbilityDefinition{Name: "TCShieldedSentinelSupport", StatusKind: sim.AbilityStatusKindSilence,
			RootModifierID: 20, StatusDuration: duration}
		plan := zoneability.AreaPlan{Definition: definition}
		results := []zoneability.AreaResult{{Damage: zonenpc.DamageResult{ObjectID: 12, Damage: 5}}}
		target := registry.sessions["target"]
		target.binding.Creatures[0].PartAttribute[78] = 1
		registry.sessions["target"] = target
		target.binding.Creatures[0].PartAttribute[14] = .25
		target.enemySilenceExpiresAt = clock.at.Add(10 * time.Second)
		registry.sessions["target"] = target
		packets, err := runtime.applyAcceptedHitStatus(raknet.Packet{}, "first", 1, 10, 1000, plan, results)
		if err != nil || len(packets) != 0 {
			t.Fatalf("initial: packets=%d err=%v", len(packets), err)
		}
		target = registry.sessions["target"]
		key := arenaHitStatusKey{modifierID: 20}
		status := target.arenaHitStatuses[key]
		if status == nil || status.sourceObjectID != 10 || status.duration != duration || !target.isArenaHitSilenceActive(clock.at) || target.arenaSilenceAmount(clock.at) != 2.25 {
			t.Fatal("initial silence did not preserve base/legacy contributions and creation identity")
		}
		instanceID := status.instanceID
		clock.at = clock.at.Add(500 * time.Millisecond)
		if duration == time.Second {
			plan.Definition.StatusDuration = 3 * time.Second
		} else {
			plan.Definition.StatusDuration = time.Second
		}
		packets, err = runtime.applyAcceptedHitStatus(raknet.Packet{}, "second", 2, 11, 2000, plan, results)
		if err != nil || len(packets) != 0 {
			t.Fatalf("refresh: packets=%d err=%v", len(packets), err)
		}
		target = registry.sessions["target"]
		if target.arenaHitStatuses[key] != status || status.instanceID != instanceID || status.sourceObjectID != 10 || status.duration != duration || status.expiresAt != clock.at.Add(duration) || status.revision != 2 || !timer.cancellations[0] {
			t.Fatal("cross-rank/caster refresh replaced creation identity/duration or failed timer handoff")
		}
		expectedUpdate, err := raknet.MarshalApplication(raknet.ModifierUpdatedMessage{TargetID: 12, InstanceID: instanceID, StartMilliseconds: 2000, StackCount: 1})
		if err != nil {
			t.Fatalf("updateMarshal: %v", err)
		}
		batch := target.pendingPacketBatches[len(target.pendingPacketBatches)-1]
		if len(batch.packets) != 1 || !bytes.Equal(batch.packets[0], expectedUpdate) {
			t.Fatal("refresh did not emit same-instance native start/stack update")
		}
		timer.jobs[0]()
		if status.isRetired || registry.sessions["target"].arenaHitStatuses[key] != status {
			t.Fatal("stale same-instance expiry retired refresh")
		}
		previousDeadline, previousRevision := status.expiresAt, status.revision
		previousBatchCount := len(target.pendingPacketBatches)
		timer.failure = errors.New("scheduled failure")
		packets, err = runtime.applyAcceptedHitStatus(raknet.Packet{}, "second", 2, 11, 2500, plan, results)
		if err == nil || len(packets) != 0 || status.expiresAt != previousDeadline || status.revision != previousRevision || timer.cancellations[1] || len(registry.sessions["target"].pendingPacketBatches) != previousBatchCount {
			t.Fatal("failed refresh changed prior timer/deadline or published an update")
		}
		timer.failure = nil
		clock.at = status.expiresAt
		timer.jobs[1]()
		target = registry.sessions["target"]
		if !status.isRetired || target.isArenaHitSilenceActive(clock.at) || target.arenaSilenceAmount(clock.at) != 1.25 {
			t.Fatal("expiry erased equipment/legacy silence or kept Arena silence")
		}
		expectedAttribute, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{ObjectID: 12, Value: map[uint8]float32{14: 1.25}})
		if err != nil {
			t.Fatalf("attributeMarshal: %v", err)
		}
		batch = target.pendingPacketBatches[len(target.pendingPacketBatches)-1]
		if len(batch.packets) != 2 || !bytes.Equal(batch.packets[1], expectedAttribute) {
			t.Fatal("native silence attribute was not restored with remaining contributions")
		}
	}
}
