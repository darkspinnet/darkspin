package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const arenaBasicBurnInterval = 500 * time.Millisecond
const arenaBasicBurnTicks = 8

var arenaBasicBurnDefinition = sim.AbilityDefinition{
	Name: "FireRavagerBurn", MinimumDamage: 1, MaximumDamage: 1,
	DamageCoefficient: .05, DamageType: 3, DamageSource: 1,
	IsDamageTypeFound: true, IsDamageSourceFound: true,
	IsDescriptorFound: true, DescriptorMask: 36,
}

// Build-103 FireRavagerBurn is CasterUnique and keeps the damage profile from
// its creation. The recipient owns the timer so a caster switch cannot retire it.
type arenaBasicBurnRun struct {
	status     *arenaHitStatus
	source     gameplayPeerSession
	creature   game.GameplayCreature
	damage     game.DamageRange
	definition sim.AbilityDefinition
	interval   time.Duration
	tickLimit  uint32
	timestamp  uint64
	tickCount  uint32
}

func (e *arenaAbilityTargets) applyBasicBurnLocked(result zonenpc.DamageResult,
	sourceCreature game.GameplayCreature, rank uint32, timestamp uint64,
) error {
	if e.runtime.registry.timer == nil {
		return errors.New("arena burn timer unavailable")
	}
	key, target, isFound := e.member(result.ObjectID)
	if !isFound || target.isHeroDebuffImmune(result.ObjectID) || target.isOverdriveActiveAt(e.runtime.now()) {
		return nil
	}
	damage, err := zoneability.ProjectDamage(sourceCreature, arenaBasicBurnDefinition, 1, 1)
	if err != nil {
		return fmt.Errorf("arenaBurnDamage: %w", err)
	}
	status := &arenaHitStatus{
		runtime: e.runtime, targetKey: key, gameID: e.source.binding.GameID,
		objectID: result.ObjectID, index: target.deployedCreatureIndex,
		noun:       target.binding.Creatures[target.deployedCreatureIndex].Noun,
		generation: target.generation, transportGeneration: target.transportGeneration,
		squad: target.squad, sourceObjectID: e.source.deployedObjectID,
		modifierID: util.HashID("FireRavagerBurn"), kind: arenaBasicBurn,
		rank: rank, stackCount: 1, isCasterUnique: true,
		duration: 4 * time.Second, expiresAt: e.runtime.now().Add(4 * time.Second), revision: 1,
	}
	status.instanceID, err = e.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("arenaBurnAllocate: %w", err)
	}
	createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: status.objectID, ModifierGUID: status.modifierID,
		InstanceID: status.instanceID, DurationMilliseconds: 4000, StackCount: 1,
		StartMilliseconds: timestamp, SourceID: status.sourceObjectID,
	})
	if err != nil {
		return status.discard("arenaBurnCreate", err)
	}
	run := &arenaBasicBurnRun{status: status, source: e.source, creature: sourceCreature, damage: damage,
		definition: arenaBasicBurnDefinition, interval: arenaBasicBurnInterval,
		tickLimit: arenaBasicBurnTicks, timestamp: timestamp}
	status.periodic = run
	status.cancel, err = e.runtime.registry.timer.Schedule(arenaBasicBurnInterval, run.tick)
	if err == nil && status.cancel == nil {
		err = errors.New("arena burn cancellation unavailable")
	}
	if err != nil {
		return status.discard("arenaBurnSchedule", err)
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

func (e *arenaBasicBurnRun) tick() {
	status := e.status
	status.runtime.registry.mutex.Lock()
	member, isFound := status.runtime.registry.sessions[status.targetKey]
	match := status.runtime.registry.arenaMatches[status.gameID]
	if !isFound || !status.matches(member) || match == nil || match.random == nil || !match.isCombatActive ||
		member.deployedHitPoint() <= 0 {
		status.cancel = nil
		status.retire(&member)
		if isFound {
			status.runtime.registry.sessions[status.targetKey] = member
		}
		status.runtime.registry.mutex.Unlock()
		return
	}
	selected, err := sim.SelectRankDamage(match.random, sim.DamageRange{
		Minimum: e.damage.Minimum, Maximum: e.damage.Maximum,
	})
	if err != nil {
		e.retireFailedLocked(&member, "arenaBurnSelect", err)
		return
	}
	timestamp := e.timestamp + uint64(e.tickCount+1)*uint64(e.interval/time.Millisecond)
	targets := arenaAbilityTargets{
		runtime: status.runtime, source: e.source, definition: e.definition,
		timestamp: timestamp, hits: make(map[uint32]arenaAbilityHit),
	}
	result, err := targets.Hit(zonenpc.HitRequest{
		SourceObjectID: status.sourceObjectID, TargetObjectID: status.objectID,
		Damage: selected, IsPeriodic: true,
		Metadata: zoneability.NPCDamageMetadata(e.definition),
	})
	if err != nil {
		e.retireFailedLocked(&member, "arenaBurnHit", err)
		return
	}
	packets, err := targets.packets([]zoneability.AreaResult{{Damage: result,
		Definition: e.definition}})
	if err != nil {
		member = status.runtime.registry.sessions[status.targetKey]
		e.retireFailedLocked(&member, "arenaBurnPackets", err)
		targets.finish()
		return
	}
	e.queueHitLocked(packets)
	e.tickCount++
	member = status.runtime.registry.sessions[status.targetKey]
	if e.tickCount >= e.tickLimit || result.IsDefeated {
		status.cancel = nil
		status.retire(&member)
		status.runtime.registry.sessions[status.targetKey] = member
		status.runtime.registry.mutex.Unlock()
		targets.finish()
		return
	}
	status.cancel, err = status.runtime.registry.timer.Schedule(e.interval, e.tick)
	if err == nil && status.cancel == nil {
		err = errors.New("arena burn cancellation unavailable")
	}
	if err != nil {
		status.cancel = nil
		status.retire(&member)
		status.runtime.registry.sessions[status.targetKey] = member
		if status.runtime.logger != nil {
			status.runtime.logger.Printf("Arena burn next tick failed target=%d: %v", status.objectID, err)
		}
	}
	status.runtime.registry.mutex.Unlock()
	targets.finish()
}

func (e *arenaBasicBurnRun) queueHitLocked(packets [][]byte) {
	status := e.status
	presentations := gameplayPeerPresentationPackets(packets)
	for key, member := range status.runtime.registry.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != status.gameID ||
			member.isRejoinPending || !member.stage.IsDungeon() {
			continue
		}
		if member.binding.UserID == e.source.binding.UserID {
			member.queuePackets(packets)
		} else {
			member.queuePackets(presentations)
		}
		status.runtime.registry.sessions[key] = member
	}
}

func (e *arenaBasicBurnRun) retireFailedLocked(member *gameplayPeerSession, step string, cause error) {
	status := e.status
	if status.runtime.logger != nil {
		status.runtime.logger.Printf("Arena burn callback failed target=%d: %s: %v", status.objectID, step, cause)
	}
	status.cancel = nil
	status.retire(member)
	status.runtime.registry.sessions[status.targetKey] = *member
	status.runtime.registry.mutex.Unlock()
}
