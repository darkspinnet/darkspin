package gameplay

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The melee's 500ms callback consumes each live recipient-owned poison/burn
// and applies its remaining authored ticks immediately with the initiating
// damage snapshot. The DOT owner, not the Expunge caster, supplies the damage.
func (e campaignMeleeSchedule) expungeArena() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.binding.GameID]
	if !isFound || source.generation != e.generation || source.basicAttack != e.run ||
		source.binding.Mode != game.ModeArena || !source.isArenaCombatActive ||
		match == nil || !match.isCombatActive || match.random == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	probe := arenaAbilityTargets{runtime: e.runtime, source: source}
	key, target, isFound := probe.member(e.targetObjectID)
	if !isFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	finished := make([]arenaAbilityTargets, 0)
	statuses := make([]*arenaHitStatus, 0, len(target.arenaHitStatuses))
	for _, status := range target.arenaHitStatuses {
		if status != nil {
			statuses = append(statuses, status)
		}
	}
	slices.SortFunc(statuses, func(left, right *arenaHitStatus) int {
		return cmp.Compare(left.instanceID, right.instanceID)
	})
	for _, status := range statuses {
		if !status.matches(target) {
			continue
		}
		var remaining uint32
		var creature game.GameplayCreature
		var definition sim.AbilityDefinition
		var originalSource gameplayPeerSession
		if run := status.periodic; run != nil && run.tickCount < run.tickLimit {
			remaining = run.tickLimit - run.tickCount
			creature, definition, originalSource = run.creature, run.definition, run.source
		} else if run := status.affliction; run != nil && run.completed < uint32(len(run.definition.HitDelays)) {
			remaining = uint32(len(run.definition.HitDelays)) - run.completed
			creature, definition, originalSource = run.creature, run.definition, run.source
		} else {
			continue
		}
		if definition.Name == "Terrify" {
			status.retire(&target)
			if status.kind == arenaHeroFear && !target.isEnemyFearActive(e.runtime.now()) {
				stopPackets, stopErr := target.stopHeroFearMovement(e.runtime.now())
				if stopErr == nil {
					status.queue(&target, stopPackets)
				}
			}
			e.runtime.registry.sessions[key] = target
			continue
		}
		minimumPerTick := definition.MinimumDamage
		maximumPerTick := definition.MaximumDamage
		if definition.Name == "AfflictionBolt" {
			minimumPerTick, maximumPerTick = definition.MinimumDamagePerTick, definition.MaximumDamagePerTick
		}
		expungeDefinition := expungeDamageDefinition(definition, remaining, minimumPerTick, maximumPerTick)
		damage, err := zoneability.ProjectDamage(creature, expungeDefinition,
			expungeDefinition.MinimumDamage, expungeDefinition.MaximumDamage)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			for _, pending := range finished {
				pending.finish()
			}
			return nil, fmt.Errorf("arenaExpungeProjection: %w", err)
		}
		selected, err := sim.SelectRankDamage(match.random, sim.DamageRange{
			Minimum: damage.Minimum, Maximum: damage.Maximum})
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			for _, pending := range finished {
				pending.finish()
			}
			return nil, fmt.Errorf("arenaExpungeSelect: %w", err)
		}
		targets := arenaAbilityTargets{runtime: e.runtime, source: originalSource,
			definition: expungeDefinition, timestamp: e.packet.SourceTime + 500,
			hits: make(map[uint32]arenaAbilityHit)}
		status.retire(&target)
		e.runtime.registry.sessions[key] = target
		result, err := targets.Hit(zonenpc.HitRequest{
			SourceObjectID: status.sourceObjectID, TargetObjectID: status.objectID,
			Damage: selected, Metadata: zoneability.NPCDamageMetadata(expungeDefinition),
		})
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			for _, pending := range finished {
				pending.finish()
			}
			return nil, fmt.Errorf("arenaExpungeHit: %w", err)
		}
		packets, err := targets.packets([]zoneability.AreaResult{{Damage: result, Definition: expungeDefinition}})
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			for _, pending := range append(finished, targets) {
				pending.finish()
			}
			return nil, fmt.Errorf("arenaExpungePackets: %w", err)
		}
		e.runtime.registry.queueArenaForcePulsePacketsLocked(source.binding.GameID, packets)
		finished = append(finished, targets)
		latest, isLatestFound := e.runtime.registry.sessions[key]
		if !isLatestFound || latest.deployedHitPoint() <= 0 {
			break
		}
		target = latest
	}
	e.runtime.registry.mutex.Unlock()
	for _, pending := range finished {
		pending.finish()
	}
	return nil, nil
}
