package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The shared six-tick channel keeps its authored animation, beam attachment,
// protection, healing and release schedule. Arena supplies a live player target.
// The registry is locked on entry and unlocked on every return.
func (e heroDrainSchedule) tickArenaNecroLocked(source gameplayPeerSession, deadline time.Duration) ([][]byte, error) {
	timestamp := e.packet.SourceTime + uint64(deadline/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	target, isFound := targets.NPC(e.targetObjectID)
	_, _, isPlayerTarget := targets.member(e.targetObjectID)
	if !isFound || !isPlayerTarget || target.HitPoint <= 0 ||
		!isInsideZoneTrigger(source.playerPosition, raknet.Vector3(target.Plan.Position), e.definition.Range) {
		source.heroDrain = nil
		e.run.cast.release.Rollback(e.run.cast.releaseReservation)
		e.run.clearCancel()
		e.runtime.registry.sessions[e.sessionKey] = source
		packets, err := e.endPackets(deadline)
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("arenaNecroEnd: %w", err)
		}
		return packets, nil
	}
	e.run.isProtectionActive = true
	e.run.cast.activateLocked(e.run.cast.revision)
	tickDefinition := e.definition
	tickDefinition.MinimumDamage = e.definition.MinimumDamagePerTick
	tickDefinition.MaximumDamage = e.definition.MaximumDamagePerTick
	damage, err := zoneability.ProjectDamage(e.creature, tickDefinition,
		tickDefinition.MinimumDamage, tickDefinition.MaximumDamage)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaNecroDamage: %w", err)
	}
	match := e.runtime.registry.arenaMatches[source.binding.GameID]
	if match == nil || match.random == nil || !match.isCombatActive {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	results, err := zoneability.CommitArea(match.random, targets, zoneability.AreaPlan{
		SourceObjectID: e.sourceObjectID, AbilityID: util.HashID(e.definition.Name),
		Definition: tickDefinition, Damage: damage, Target: []zonenpc.Snapshot{target},
		Center: game.Vec3(target.Plan.Position), IsSingleTarget: true,
	}, e.creature, source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaNecroCommit: %w", err)
	}
	if len(results) == 0 || results[0].Damage.Damage <= 0 {
		e.runtime.registry.sessions[e.sessionKey] = source
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, nil
	}
	healing, err := zoneability.ProjectHealing(e.creature, e.definition, e.definition.MinimumHealingPerTick)
	if err == nil {
		healing, err = game.ApplyTargetHealingReduction(healing,
			source.healingTargetProfile(source.deployedCreatureIndex))
	}
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaNecroHealing: %w", err)
	}
	maximum, _, err := source.deployedResourceMaximum()
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaNecroMaximum: %w", err)
	}
	previous := source.deployedHitPoint()
	health := min(maximum, previous+healing)
	healed := health - previous
	if healed > 0 {
		_, err = source.setDeployedHitPoints(health)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			targets.finish()
			return nil, fmt.Errorf("arenaNecroHeal: %w", err)
		}
		match.result.recordHealing(source.binding.UserID, source.binding.UserID, healed)
	}
	effectPackets, err := e.run.attachEffects(e.definition.MuzzleEffectName, e.definition.HitEffectName)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaNecroEffects: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = source
	e.run.queuePresentationLocked(effectPackets)
	var healingPackets [][]byte
	if healed > 0 {
		healingPackets, err = abilityraknet.ChannelDrainHealing(e.sourceObjectID, health, healed)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			targets.finish()
			return nil, fmt.Errorf("arenaNecroHealPacket: %w", err)
		}
		e.runtime.registry.queueArenaAbilityResourcesLocked(source, healingPackets)
	}
	e.runtime.registry.mutex.Unlock()
	packets, err := targets.packets(results)
	targets.finish()
	if err != nil {
		return nil, fmt.Errorf("arenaNecroPackets: %w", err)
	}
	return append(packets, healingPackets...), nil
}
