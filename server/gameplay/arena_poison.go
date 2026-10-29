package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const arenaPoisonNova sim.AbilityStatusKind = "poison_nova"

// Poison Nova's accepted direct hit leaves a recipient-owned five-tick poison.
// The caster's projected damage profile is fixed when the poison is applied.
func (e *arenaAbilityTargets) applyPoisonNova(result zonenpc.DamageResult,
	creature game.GameplayCreature, definition sim.AbilityDefinition, timestamp uint64,
) error {
	if definition.Name != poisonNovaName || definition.RootModifierID == 0 ||
		definition.StatusDuration <= 0 || definition.TickDuration <= 0 || definition.NumberOfTicks == 0 ||
		result.IsDamageImmune || result.IsDefeated || result.Damage <= 0 {
		return nil
	}
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.runtime.registry.timer == nil {
		return errors.New("arena poison timer unavailable")
	}
	key, target, isFound := e.member(result.ObjectID)
	if !isFound || target.isHeroDebuffImmune(result.ObjectID) || target.isOverdriveActiveAt(e.runtime.now()) {
		return nil
	}
	tickDefinition := definition
	tickDefinition.Name = poisonNovaName + "Poison"
	tickDefinition.MinimumDamage = definition.MinimumDamagePerTick
	tickDefinition.MaximumDamage = definition.MaximumDamagePerTick
	tickDefinition.DescriptorMask = 36
	tickDefinition.DamageType = 2
	tickDefinition.DamageSource = 1
	tickDefinition.IsDescriptorFound = true
	tickDefinition.IsDamageTypeFound = true
	tickDefinition.IsDamageSourceFound = true
	damage, err := zoneability.ProjectDamage(creature, tickDefinition,
		tickDefinition.MinimumDamage, tickDefinition.MaximumDamage)
	if err != nil {
		return fmt.Errorf("arenaPoisonDamage: %w", err)
	}
	status := &arenaHitStatus{runtime: e.runtime, targetKey: key,
		gameID: e.source.binding.GameID, objectID: result.ObjectID,
		index: target.deployedCreatureIndex, noun: target.binding.Creatures[target.deployedCreatureIndex].Noun,
		generation: target.generation, transportGeneration: target.transportGeneration,
		squad: target.squad, sourceObjectID: e.source.deployedObjectID,
		modifierID: definition.RootModifierID, kind: arenaPoisonNova,
		stackCount: 1, isCasterUnique: true, duration: definition.StatusDuration,
		expiresAt: e.runtime.now().Add(definition.StatusDuration), revision: 1}
	status.instanceID, err = e.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("arenaPoisonAllocate: %w", err)
	}
	createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: status.objectID, ModifierGUID: status.modifierID, InstanceID: status.instanceID,
		DurationMilliseconds: uint32(definition.StatusDuration.Milliseconds()), StackCount: 1,
		StartMilliseconds: timestamp, NPCAffixAssetID: 0,
	})
	if err != nil {
		return status.discard("arenaPoisonCreate", err)
	}
	run := &arenaBasicBurnRun{status: status, source: e.source, creature: creature, damage: damage,
		definition: tickDefinition, interval: definition.TickDuration,
		tickLimit: definition.NumberOfTicks, timestamp: timestamp}
	status.periodic = run
	status.cancel, err = e.runtime.registry.timer.Schedule(definition.TickDuration, run.tick)
	if err == nil && status.cancel == nil {
		err = errors.New("poison cancellation unavailable")
	}
	if err != nil {
		return status.discard("arenaPoisonSchedule", err)
	}
	if target.arenaHitStatuses == nil {
		target.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous := target.arenaHitStatuses[status.key()]; previous != nil {
		previous.retire(&target)
	}
	target.arenaHitStatuses[status.key()] = status
	status.queue(&target, [][]byte{createPacket})
	e.runtime.registry.sessions[key] = target
	return nil
}
