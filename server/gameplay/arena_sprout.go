package gameplay

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const arenaSproutPollInterval = 100 * time.Millisecond

type arenaSproutWorld struct {
	registry *gameplaySessionRegistry
	gameID   uint32
	runs     map[uint32]*arenaSproutRun
	targets  map[uint32]*arenaSproutTarget
	cancel   raknet.CancelSchedule
	revision uint64
}

type arenaSproutTarget struct {
	objectID         uint32
	origin           *arenaSproutRun
	status           *arenaHitStatus
	nextTickAt       time.Time
	outsideWaitCount uint32
}

type arenaSproutPoll struct {
	world    *arenaSproutWorld
	revision uint64
}

func (e *gameplaySessionRegistry) arenaSproutWorldLocked(gameID uint32) (*arenaSproutWorld, error) {
	world, err := e.arenaAbilityWorldLocked(gameID)
	if err != nil {
		return nil, fmt.Errorf("sproutAbilityWorld: %w", err)
	}
	if world.sproutWorld == nil {
		world.sproutWorld = &arenaSproutWorld{registry: e, gameID: gameID,
			runs: make(map[uint32]*arenaSproutRun), targets: make(map[uint32]*arenaSproutTarget)}
	}
	return world.sproutWorld, nil
}

func (e *arenaSproutWorld) scheduleLocked() error {
	if e.cancel != nil || (len(e.runs) == 0 && len(e.targets) == 0) {
		return nil
	}
	if e.registry.timer == nil {
		return errors.New("Arena Sprout timer unavailable")
	}
	e.revision++
	step := arenaSproutPoll{world: e, revision: e.revision}
	cancel, err := e.registry.timer.Schedule(arenaSproutPollInterval, step.execute)
	if err == nil && cancel == nil {
		err = errors.New("Arena Sprout timer cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("sproutPollSchedule: %w", err)
	}
	e.cancel = cancel
	return nil
}

func (e arenaSproutPoll) execute() {
	world := e.world
	world.registry.mutex.Lock()
	defer world.registry.mutex.Unlock()
	match := world.registry.arenaMatches[world.gameID]
	if match == nil || !match.isCombatActive || match.abilityWorld == nil ||
		match.abilityWorld.sproutWorld != world || world.revision != e.revision {
		return
	}
	world.cancel = nil
	now := world.registry.now()
	for _, run := range world.runs {
		if run.isLanded && !now.Before(run.expiresAt) {
			run.retireLocked()
		}
	}
	err := world.resolveLocked(now)
	if err != nil && world.registry.logger != nil {
		world.registry.logger.Printf("Arena Sprout poison poll failed game=%d: %v", world.gameID, err)
	}
	err = world.scheduleLocked()
	if err != nil && world.registry.logger != nil {
		world.registry.logger.Printf("Arena Sprout reschedule failed game=%d: %v", world.gameID, err)
	}
}

func (e *arenaSproutWorld) retireTargetLocked(target *arenaSproutTarget) {
	if e.targets[target.objectID] != target {
		return
	}
	delete(e.targets, target.objectID)
	member, isFound := e.registry.sessions[target.status.targetKey]
	if !isFound {
		member = gameplayPeerSession{}
	}
	target.status.retire(&member)
	if isFound {
		e.registry.sessions[target.status.targetKey] = member
	}
}

func (e *gameplaySessionRegistry) retireArenaSproutsLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil || match.abilityWorld.sproutWorld == nil {
		return
	}
	world := match.abilityWorld.sproutWorld
	if world.cancel != nil {
		world.cancel()
		world.cancel = nil
	}
	world.revision++
	for _, run := range world.runs {
		run.retireLocked()
	}
	for _, target := range world.targets {
		world.retireTargetLocked(target)
	}
	match.abilityWorld.sproutWorld = nil
}

func (e *gameplaySessionRegistry) retireArenaSproutsForOwnerLocked(gameID uint32, objectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil || match.abilityWorld.sproutWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.sproutWorld.runs {
		if run.sourceObjectID == objectID && !run.isLanded {
			run.retireLocked()
		}
	}
}

// A region records entries independently. Its target-owned SproutPoison keeps
// ticking after departure until the authored outside wait count is exhausted.
func (e *arenaSproutWorld) resolveLocked(now time.Time) error {
	runIDs := make([]uint32, 0, len(e.runs))
	for objectID, run := range e.runs {
		if run.isLanded && now.Before(run.expiresAt) {
			runIDs = append(runIDs, objectID)
		}
	}
	slices.Sort(runIDs)
	insideRuns := make(map[uint32]*arenaSproutRun)
	enteredRuns := make(map[uint32]*arenaSproutRun)
	for _, runID := range runIDs {
		run := e.runs[runID]
		targets := arenaAbilityTargets{runtime: run.runtime, source: run.source}
		occupants := make(map[uint32]struct{})
		for _, snapshot := range targets.LiveSnapshots() {
			if snapshot.Plan.Position.Sub(game.Vec3(run.plan.Destination)).Length() > run.plan.Definition.CloudLob.CloudRadius {
				continue
			}
			objectID := snapshot.Plan.ObjectID
			if _, _, isFound := targets.member(objectID); !isFound {
				continue
			}
			occupants[objectID] = struct{}{}
			if insideRuns[objectID] == nil {
				insideRuns[objectID] = run
			}
			if _, wasInside := run.occupants[objectID]; !wasInside && enteredRuns[objectID] == nil {
				enteredRuns[objectID] = run
			}
		}
		run.occupants = occupants
	}
	for objectID, run := range enteredRuns {
		if e.targets[objectID] != nil {
			continue
		}
		err := e.enterTargetLocked(run, objectID, now)
		if err != nil {
			return fmt.Errorf("sproutEnter[%d]: %w", objectID, err)
		}
	}
	for objectID, target := range e.targets {
		if target.status.isRetired {
			delete(e.targets, objectID)
			continue
		}
		if !now.Before(target.nextTickAt) {
			if insideRuns[objectID] != nil {
				target.outsideWaitCount = 0
			} else {
				target.outsideWaitCount++
				if target.outsideWaitCount >= target.origin.plan.Definition.CloudLob.TickCount {
					e.retireTargetLocked(target)
					continue
				}
			}
			target.nextTickAt = now.Add(target.origin.plan.Definition.CloudLob.TickDuration)
			err := e.damageTargetLocked(target, now)
			if err != nil {
				return fmt.Errorf("sproutTick[%d]: %w", objectID, err)
			}
		}
	}
	return nil
}

func (e *arenaSproutWorld) enterTargetLocked(run *arenaSproutRun, objectID uint32, now time.Time) error {
	targets := arenaAbilityTargets{runtime: run.runtime, source: run.source}
	key, member, isFound := targets.member(objectID)
	if !isFound || member.isHeroDebuffImmune(objectID) || member.isOverdriveActiveAt(now) {
		return nil
	}
	instanceID, err := run.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("sproutModifierAllocate: %w", err)
	}
	status := &arenaHitStatus{runtime: run.runtime, targetKey: key, gameID: run.gameID,
		objectID: objectID, index: member.deployedCreatureIndex,
		noun:       member.binding.Creatures[member.deployedCreatureIndex].Noun,
		generation: member.generation, transportGeneration: member.transportGeneration,
		squad: member.squad, sourceObjectID: run.sourceObjectID,
		modifierID: util.HashID("SproutPoison"), instanceID: instanceID,
		kind: arenaBasicBurn, stackCount: 1, expiresAt: now.Add(time.Hour)}
	// Build-103 SproutPoison inherits DOT149.GetRemainingDamage with its
	// default zero tick range. Expunge consumes the modifier and adds only
	// its own coefficient, independently of the continuing cloud damage.
	status.periodic = &arenaBasicBurnRun{status: status, source: run.source,
		creature: run.creature, definition: sim.AbilityDefinition{
			DamageType: 2, DamageSource: 1, IsDamageTypeFound: true, IsDamageSourceFound: true,
		}, tickLimit: 1}
	created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: objectID, ModifierGUID: status.modifierID, InstanceID: instanceID,
		StackCount: 1, NPCAffixAssetID: 0,
		StartMilliseconds: run.packet.SourceTime + uint64(now.Sub(run.startedAt)/time.Millisecond),
	})
	if err != nil {
		return status.discard("sproutModifierCreate", err)
	}
	entryPackets, err := abilityraknet.CloudEntryEffects(run.plan.Definition, run.sourceObjectID, []uint32{objectID})
	if err != nil {
		return status.discard("sproutEntryEffect", err)
	}
	if member.arenaHitStatuses == nil {
		member.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous := member.arenaHitStatuses[status.key()]; previous != nil {
		previous.retire(&member)
	}
	member.arenaHitStatuses[status.key()] = status
	status.queue(&member, append([][]byte{created}, entryPackets...))
	e.registry.sessions[key] = member
	target := &arenaSproutTarget{objectID: objectID, origin: run, status: status,
		nextTickAt: now.Add(run.plan.Definition.CloudLob.TickDuration)}
	e.targets[objectID] = target
	err = e.damageTargetLocked(target, now)
	if err != nil {
		return fmt.Errorf("sproutEntryDamage: %w", err)
	}
	return nil
}

func (e *arenaSproutWorld) damageTargetLocked(target *arenaSproutTarget, now time.Time) error {
	run := target.origin
	match := e.registry.arenaMatches[e.gameID]
	if match == nil || !match.isCombatActive {
		return nil
	}
	targets := e.registry.abilityTargetsLocked(run.runtime, run.source, run.plan.Definition,
		run.packet.SourceTime+uint64(now.Sub(run.startedAt)/time.Millisecond)).(*arenaAbilityTargets)
	snapshot, isFound := targets.NPC(target.objectID)
	if !isFound {
		e.retireTargetLocked(target)
		targets.finish()
		return nil
	}
	definition := run.plan.Definition
	definition.IsWeaponDamageRange = false
	definition.DescriptorMask = 1<<2 | 1<<5 | 1<<7
	definition.IsDescriptorFound = true
	minimum := run.creature.MinimumWeaponDamage / float32(definition.CloudLob.TickCount)
	maximum := run.creature.MaximumWeaponDamage / float32(definition.CloudLob.TickCount)
	damage, err := zoneability.ProjectDamage(run.creature, definition, minimum, maximum)
	if err != nil {
		targets.finish()
		return fmt.Errorf("sproutDamageProject: %w", err)
	}
	plan := zoneability.AreaPlan{SourceObjectID: run.sourceObjectID, AbilityID: run.plan.AbilityID,
		Definition: definition, Damage: damage, Center: game.Vec3(run.plan.Destination),
		Target: []zonenpc.Snapshot{snapshot}}
	results, err := zoneability.CommitArea(match.random, targets, plan, run.creature,
		run.source.binding.Difficulty, run.runtime.program.Critical)
	if err != nil {
		targets.finish()
		return fmt.Errorf("sproutDamageCommit: %w", err)
	}
	packets, err := targets.packets(results)
	if err != nil {
		targets.finish()
		return fmt.Errorf("sproutDamagePublish: %w", err)
	}
	e.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	targets.finish()
	return nil
}
