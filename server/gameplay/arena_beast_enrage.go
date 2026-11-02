package gameplay

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func (e *beastPetEnrageRun) activateArena() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(member, isFound) || member.binding.Mode != game.ModeArena ||
		member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	pet := e.arenaWorld.combatPets[e.objectID]
	if pet == nil || pet.kind != arenaCombatPetBeast {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	creature := e.runtime.registry.projectPassiveCreature(member,
		member.deployedCreatureIndex, e.runtime.now())
	e.healingProfile = creature.HealingProfile
	e.criticalRating = creature.CriticalRating
	e.autoCrit = creature.AutoCrit
	e.difficulty = member.binding.Difficulty
	member.beastPetDamageIncrease = beastPetEnrageDamageIncrease
	healPackets, err := e.arenaHealLocked(&member)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaBeastInitialHeal: %w", err)
	}
	modifierPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: member.deployedObjectID, TargetObjectID: e.objectID,
		ModifierID: util.HashID("BeastPetRage"), InstanceID: e.instanceID,
		Duration: beastPetEnrageDuration, Timestamp: e.timestamp})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaBeastRageModifier: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID("beastSentinel_cast_enrage.ServerEventDef"), ObjectID: e.objectID})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaBeastRageEffect: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.objectID, Value: map[uint8]float32{113: beastPetEnrageBodyScale}})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaBeastRageScale: %w", err)
	}
	err = e.arenaWorld.companion.AddBuff(e.objectID,
		zonecompanion.Buff{DamageBuff: beastPetEnrageDamageIncrease})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaBeastRageBuff: %w", err)
	}
	err = e.arenaWorld.companion.SetBodyScale(e.objectID, beastPetEnrageBodyScale)
	if err != nil {
		e.arenaWorld.companion.RemoveBuff(e.objectID,
			zonecompanion.Buff{DamageBuff: beastPetEnrageDamageIncrease})
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaBeastRageBody: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	return append([][]byte{modifierPacket, effectPacket, attributePacket}, healPackets...), nil
}

func (e *beastPetEnrageRun) healArena() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(member, isFound) || member.binding.Mode != game.ModeArena {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.arenaHealLocked(&member)
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageHeal: %w", err)
	}
	return packets, nil
}

func (e *beastPetEnrageRun) arenaHealLocked(member *gameplayPeerSession) ([][]byte, error) {
	actor, isFound := e.arenaWorld.companion.Snapshot(e.objectID)
	if !isFound || actor.HitPoint <= 0 {
		return nil, nil
	}
	multiplier := e.runtime.program.Critical.DamageBonus
	if multiplier <= 0 || math.IsNaN(float64(multiplier)) || math.IsInf(float64(multiplier), 0) {
		return nil, errors.New("Arena Beast rage multiplier invalid")
	}
	random, err := e.runtime.registry.abilityRandomLocked(*member)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageRandom: %w", err)
	}
	amount := actor.MaximumHitPoint * beastPetEnrageHealingPercent
	rangeDraw := float32(random.Float64())
	amount = amount + (amount-amount)*rangeDraw
	healing, err := game.ResolveAbilityHealing(game.AbilityHealing{
		Amount: amount, Descriptor: 4096, IsDescriptorFound: true}, e.healingProfile)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageResolve: %w", err)
	}
	isCritical, err := sim.RollCritical(random, e.criticalRating, e.autoCrit,
		e.difficulty, e.runtime.program.Critical.RatingConversions)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageCritical: %w", err)
	}
	flags := uint16(0x0002)
	if isCritical {
		healing *= multiplier
		flags |= 0x0008
	}
	if math.IsNaN(float64(healing)) || math.IsInf(float64(healing), 0) {
		return nil, errors.New("Arena Beast rage healing invalid")
	}
	hitPoint := min(actor.MaximumHitPoint, actor.HitPoint+healing)
	if hitPoint <= actor.HitPoint {
		return nil, nil
	}
	integerChange := int32(hitPoint) - int32(actor.HitPoint)
	healPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: flags, DeltaHealth: -healing, TargetID: e.objectID,
		SourceID: member.deployedObjectID, IntegerHPChange: -integerChange})
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageEvent: %w", err)
	}
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: e.objectID, HitPoints: hitPoint, IsHitPointChanged: true})
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageHealth: %w", err)
	}
	isRestored, err := e.arenaWorld.companion.RestoreHitPoint(actor, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageRestore: %w", err)
	}
	if !isRestored {
		return nil, nil
	}
	return [][]byte{healPacket, healthPacket}, nil
}

func (e *beastPetEnrageRun) expireArena() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(member, isFound) || member.binding.Mode != game.ModeArena {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	member.beastPetDamageIncrease = 0
	member.beastPetEnrage = nil
	e.arenaWorld.companion.ClearBodyScale(e.objectID)
	e.arenaWorld.companion.RemoveBuff(e.objectID,
		zonecompanion.Buff{DamageBuff: beastPetEnrageDamageIncrease})
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	e.cancel = nil
	err := e.runtime.modifierPool.Release(e.instanceID)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Beast rage release: %v", err)
	}
	modifierPacket, err := effectraknet.ModifierDelete(e.objectID, e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageDelete: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.objectID, Value: map[uint8]float32{113: 0}})
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRageReset: %w", err)
	}
	return [][]byte{modifierPacket, attributePacket}, nil
}
