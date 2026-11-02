package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Strangling Briars retains the shared sphere schedule. Each hostile pulse
// targets live Arena opponents; its friendly callback heals current teammates.
// The registry mutex is held on entry and released before returning.
func (e targetedAOESchedule) produceArenaSupportHealerLocked(
	peerSession gameplayPeerSession, deadline time.Duration,
) ([][]byte, error) {
	outputPackets, pulses, err := e.run.Advance(context.Background(), deadline)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaBriarAdvance: %w", err)
	}
	finishedTargets := make([]*arenaAbilityTargets, 0, len(pulses))
	for _, pulse := range pulses {
		timestamp := e.packet.SourceTime + uint64(deadline/time.Millisecond)
		current := e.runtime.registry.sessions[e.sessionKey]
		targets := e.runtime.registry.abilityTargetsLocked(
			e.runtime, current, e.definition, timestamp,
		).(*arenaAbilityTargets)
		plan, planErr := zoneability.PlanTargetedAOEPulse(
			targets, e.sourceObjectID,
			game.Vec3{X: pulse.Position.X, Y: pulse.Position.Y, Z: pulse.Position.Z},
			e.creature, e.definition, pulse.DamageMinimum, pulse.DamageMaximum,
			pulse.DamageCoefficient,
		)
		if planErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaBriarPlan: %w", planErr)
		}
		if pulse.IsFirstTargetOnly && len(plan.Target) > 1 {
			plan.Target = plan.Target[:1]
		}
		random, randomErr := e.runtime.registry.abilityRandomLocked(current)
		if randomErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaBriarRandom: %w", randomErr)
		}
		results := make([]zoneability.AreaResult, 0, len(plan.Target))
		for _, target := range plan.Target {
			single := plan
			single.Target = []zonenpc.Snapshot{target}
			committed, commitErr := zoneability.CommitArea(
				random, targets, single, e.creature, current.binding.Difficulty,
				e.runtime.program.Critical,
			)
			if commitErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("arenaBriarCommit: %w", commitErr)
			}
			results = append(results, committed...)
			for _, result := range committed {
				if !result.Damage.IsDefeated && !result.Damage.IsDamageImmune {
					statusPlan := plan
					statusPlan.Definition.StatusKind = sim.AbilityStatusKindSlow
					statusPlan.Definition.RootModifierID = util.HashID("DrainSnareModifier")
					statusPlan.Definition.StatusDuration = time.Second
					statusPlan.Definition.StatusMovementScale = 0.25
					if e.rank == 7 || e.rank == 8 {
						statusPlan.Definition.StatusMovementScale = 0.4
					}
					statusPlan.Definition.StatusAttackScale = 1
					statusPackets, statusErr := e.runtime.applyArenaTargetStatusLocked(
						e.sessionKey, e.generation, e.sourceObjectID, timestamp,
						statusPlan, []uint32{result.Damage.ObjectID},
					)
					if statusErr != nil {
						e.runtime.registry.mutex.Unlock()
						return nil, fmt.Errorf("arenaBriarSlow: %w", statusErr)
					}
					outputPackets = append(outputPackets, statusPackets...)
				}
				if result.Damage.IsDefeated {
					continue
				}
				roll, rollErr := sim.ScriptRandom(random)
				if rollErr != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("arenaBriarRootRoll: %w", rollErr)
				}
				if roll >= supportHealerRootChance(e.rank) {
					continue
				}
				rootPlan := plan
				rootPlan.Definition.StatusKind = arenaChargeRoot
				rootPlan.Definition.RootModifierID = util.HashID("SupportHealer_RootModifier")
				rootPlan.Definition.StatusDuration = 2 * time.Second
				rootPackets, rootErr := e.runtime.applyArenaTargetStatusLocked(
					e.sessionKey, e.generation, e.sourceObjectID, timestamp,
					rootPlan, []uint32{result.Damage.ObjectID},
				)
				if rootErr != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("arenaBriarRoot: %w", rootErr)
				}
				outputPackets = append(outputPackets, rootPackets...)
			}
		}
		impactPackets, packetErr := targets.packets(results)
		if packetErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaBriarPackets: %w", packetErr)
		}
		outputPackets = append(outputPackets, impactPackets...)
		current = e.runtime.registry.sessions[e.sessionKey]
		healingPackets, healingErr := e.healArenaSupportPartyLocked(current, pulse)
		if healingErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaBriarHealing: %w", healingErr)
		}
		outputPackets = append(outputPackets, healingPackets...)
		finishedTargets = append(finishedTargets, targets)
	}
	if deadline == e.finalDeadline {
		current := e.runtime.registry.sessions[e.sessionKey]
		current.sphereAttack = nil
		current.sphereObjectID = 0
		e.run.ClearCancel()
		e.runtime.registry.sessions[e.sessionKey] = current
	}
	e.runtime.registry.mutex.Unlock()
	for _, targets := range finishedTargets {
		targets.finish()
	}
	if deadline == e.definition.ReleaseDelay && len(e.releaseResponse) != 0 {
		outputPackets = append(outputPackets, e.releaseResponse)
	}
	return outputPackets, nil
}

func (e targetedAOESchedule) healArenaSupportPartyLocked(
	caster gameplayPeerSession, pulse sim.AreaPulseIntent,
) ([][]byte, error) {
	packets := make([][]byte, 0)
	for key, member := range e.runtime.registry.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != caster.binding.GameID ||
			member.binding.Team != caster.binding.Team || !member.isArenaCombatActive ||
			!member.stage.IsDungeon() || member.deployedHitPoint() <= 0 ||
			game.Vec3(member.playerPosition).Sub(game.Vec3(pulse.Position)).Length() > pulse.Radius {
			continue
		}
		critical, err := e.criticalSupportHealingLocked(caster, pulse)
		if err != nil {
			return nil, fmt.Errorf("arenaBriarCritical: %w", err)
		}
		reduced, err := game.ApplyTargetHealingReduction(
			critical.Damage, member.healingTargetProfile(member.deployedCreatureIndex),
		)
		if err != nil {
			return nil, fmt.Errorf("arenaBriarReduction: %w", err)
		}
		if reduced <= 0 {
			continue
		}
		before := member.deployedHitPoint()
		hitPoint := min(member.characterHitPointMaximum(member.deployedCreatureIndex), before+reduced)
		healingPackets, err := supportHealerHealingPackets(
			e.sourceObjectID, member.deployedObjectID, before, hitPoint, reduced, critical.IsCritical,
		)
		if err != nil {
			return nil, fmt.Errorf("arenaBriarFeedback: %w", err)
		}
		resourcePacket, err := member.marshalCampaignCharacterResourceAt(member.deployedCreatureIndex, hitPoint)
		if err != nil {
			return nil, fmt.Errorf("arenaBriarResource: %w", err)
		}
		if hitPoint > before {
			isGameOver, commitErr := member.setDeployedHitPoints(hitPoint)
			if commitErr != nil {
				return nil, fmt.Errorf("arenaBriarCommit: %w", commitErr)
			}
			if isGameOver {
				return nil, fmt.Errorf("arenaBriarCommit: healed squad became terminal")
			}
		}
		e.runtime.registry.sessions[key] = member
		match := e.runtime.registry.arenaMatches[caster.binding.GameID]
		if match != nil && match.isCombatActive {
			match.result.recordHealing(caster.binding.UserID, member.binding.UserID, hitPoint-before)
		}
		packets = append(packets, healingPackets...)
		packets = append(packets, resourcePacket)
		e.runtime.registry.queueArenaAbilityResourcesLocked(caster, [][]byte{resourcePacket})
	}
	return packets, nil
}
