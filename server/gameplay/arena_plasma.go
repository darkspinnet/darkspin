package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

var arenaPlasmaBurnDefinition = sim.AbilityDefinition{
	Name: "PlasmaSentinelBurn", MinimumDamage: 1, MaximumDamage: 1,
	DamageCoefficient: .05, DamageType: 3, DamageSource: 1,
	IsDamageTypeFound: true, IsDamageSourceFound: true,
	IsDescriptorFound: true, DescriptorMask: 36,
}

// The build-103 basic chooses an alternating hit modifier by animation index,
// then rolls its authored chance. The recipient owns the resulting modifier.
func (e *arenaAbilityTargets) applyPlasmaModifierLocked(result zonenpc.DamageResult,
	sourceCreature game.GameplayCreature, rank uint32, animationIndex int, timestamp uint64,
) error {
	if e.definition.Name != "PlasmaSentinelBasic" || rank == 0 ||
		result.IsDefeated || result.IsDamageImmune || result.IsShieldStarted || result.IsTurtleStarted {
		return nil
	}
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if match == nil || match.random == nil {
		return errors.New("arena plasma random unavailable")
	}
	plan, isApplied, err := zoneability.PlanPlasmaModifier(match.random,
		e.definition.MeleeHitPolicies, animationIndex, sourceCreature)
	if err != nil {
		return fmt.Errorf("arenaPlasmaPlan: %w", err)
	}
	if !isApplied {
		return nil
	}
	key, target, isFound := e.member(result.ObjectID)
	if !isFound || target.isHeroDebuffImmune(result.ObjectID) || target.isOverdriveActiveAt(e.runtime.now()) {
		return nil
	}
	if e.runtime.registry.timer == nil {
		return errors.New("arena plasma timer unavailable")
	}
	if plan.Kind == zoneability.PlasmaModifierShock {
		return e.applyPlasmaShockLocked(key, target, result.ObjectID, rank, timestamp, plan)
	}
	return e.applyPlasmaBurnLocked(key, target, result.ObjectID, rank, timestamp, plan)
}

func (e *arenaAbilityTargets) applyPlasmaShockLocked(key string, target gameplayPeerSession,
	objectID uint32, rank uint32, timestamp uint64, plan zoneability.PlasmaModifierPlan,
) error {
	status, createPacket, err := e.prepareBasicStatusLocked(key, target, objectID,
		"PlasmaSentinelShock", sim.AbilityStatusKindStun, rank, 1, plan.Duration, timestamp)
	if err != nil {
		return fmt.Errorf("arenaPlasmaShockPrepare: %w", err)
	}
	if target.arenaHitStatuses == nil {
		target.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous := target.arenaHitStatuses[status.key()]; previous != nil {
		previous.retire(&target)
	}
	e.retireOtherPlasmaStatusLocked(&target, status.modifierID)
	target.arenaHitStatuses[status.key()] = status
	attributePackets, err := status.refresh(&target)
	if err != nil {
		status.retire(&target)
		e.runtime.registry.sessions[key] = target
		return fmt.Errorf("arenaPlasmaShockAttributes: %w", err)
	}
	status.queue(&target, append([][]byte{createPacket}, attributePackets...))
	e.runtime.registry.sessions[key] = target
	return nil
}

func (e *arenaAbilityTargets) applyPlasmaBurnLocked(key string, target gameplayPeerSession,
	objectID uint32, rank uint32, timestamp uint64, plan zoneability.PlasmaModifierPlan,
) error {
	if plan.Tick <= 0 || plan.Duration <= 0 || plan.Duration%plan.Tick != 0 {
		return errors.New("arena plasma burn timing invalid")
	}
	status := &arenaHitStatus{
		runtime: e.runtime, targetKey: key, gameID: e.source.binding.GameID,
		objectID: objectID, index: target.deployedCreatureIndex,
		noun:       target.binding.Creatures[target.deployedCreatureIndex].Noun,
		generation: target.generation, transportGeneration: target.transportGeneration,
		squad: target.squad, sourceObjectID: e.source.deployedObjectID,
		modifierID: plan.ModifierID, kind: arenaBasicBurn, rank: rank, stackCount: 1,
		duration: plan.Duration, expiresAt: e.runtime.now().Add(plan.Duration), revision: 1,
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("arenaPlasmaBurnAllocate: %w", err)
	}
	status.instanceID = instanceID
	createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: objectID, ModifierGUID: plan.ModifierID, InstanceID: instanceID,
		DurationMilliseconds: uint32(plan.Duration.Milliseconds()), StackCount: 1,
		StartMilliseconds: timestamp, NPCAffixAssetID: 0,
	})
	if err != nil {
		return status.discard("arenaPlasmaBurnCreate", err)
	}
	run := &arenaBasicBurnRun{status: status, source: e.source, damage: plan.Damage,
		definition: arenaPlasmaBurnDefinition, interval: plan.Tick,
		tickLimit: uint32(plan.Duration / plan.Tick), timestamp: timestamp}
	status.cancel, err = e.runtime.registry.timer.Schedule(plan.Tick, run.tick)
	if err == nil && status.cancel == nil {
		err = errors.New("arena plasma burn cancellation unavailable")
	}
	if err != nil {
		return status.discard("arenaPlasmaBurnSchedule", err)
	}
	if target.arenaHitStatuses == nil {
		target.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous := target.arenaHitStatuses[status.key()]; previous != nil {
		previous.retire(&target)
	}
	e.retireOtherPlasmaStatusLocked(&target, status.modifierID)
	target.arenaHitStatuses[status.key()] = status
	status.queue(&target, [][]byte{createPacket})
	e.runtime.registry.sessions[key] = target
	return nil
}

func (e *arenaAbilityTargets) retireOtherPlasmaStatusLocked(target *gameplayPeerSession, modifierID uint32) {
	otherID := util.HashID("PlasmaSentinelShock")
	if modifierID == otherID {
		otherID = util.HashID("PlasmaSentinelBurn")
	}
	if previous := target.arenaHitStatuses[arenaHitStatusKey{modifierID: otherID}]; previous != nil {
		previous.retire(target)
	}
}
