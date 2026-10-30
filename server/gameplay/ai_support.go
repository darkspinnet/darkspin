package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type playerAISkill struct {
	index      uint32
	id         uint32
	creature   game.GameplayCreature
	definition sim.AbilityDefinition
}

func (e gameplayPendingRuntime) playerAISkills(member gameplayPeerSession) []playerAISkill {
	creature := member.binding.Creatures[member.deployedCreatureIndex]
	skills := make([]playerAISkill, 0, 5)
	for _, index := range []uint32{2, 3} {
		definition, id, isActive, isFound := e.action.ability.arenaCharacterAbility(creature, index, 1)
		if isFound && isActive {
			skills = append(skills, playerAISkill{index: index, id: id, creature: creature, definition: definition})
		}
	}
	for index, source := range member.binding.Creatures {
		character, isFound := member.squad.Character(uint32(index))
		if !isFound || !character.IsAvailable || character.HitPoints <= 0 {
			continue
		}
		ability, isFound := e.action.ability.program.HeroAbility(source.Noun, zonecontent.HeroAbilitySpecialOne)
		if isFound && ability.ID != 0 && ability.Definition.Name != "" {
			skills = append(skills, playerAISkill{index: 6 + uint32(index), id: ability.ID,
				creature: source, definition: projectHeroAbilityRank(ability.Definition, 1)})
		}
	}
	return skills
}

func playerAIIsDefense(definition sim.AbilityDefinition) bool {
	return definition.Kind == sim.AbilityKindModifier &&
		(definition.Name == "TechRandom2" || definition.Name == "ShadowRavagerSupport" ||
			definition.Name == "QuantumState" || definition.StatusDamageReduction > 0)
}

func playerAIIsHeal(definition sim.AbilityDefinition) bool {
	return definition.Kind == sim.AbilityKindHealingTicks || definition.Kind == sim.AbilityKindAreaHealing
}

func playerAIPowerRatio(member gameplayPeerSession) float32 {
	maximum := member.characterResourceMaximums(member.deployedCreatureIndex).manaPoint
	if maximum <= 0 {
		return 0
	}
	return min(float32(1), member.deployedManaPoint()/maximum)
}

func (e gameplayPendingRuntime) playerAIManaReserve(member gameplayPeerSession, now time.Time, isPriorityTarget bool) (float32, error) {
	if member.binding.IsServerBot && member.binding.Mode == game.ModeChain {
		maximum := member.characterResourceMaximums(member.deployedCreatureIndex).manaPoint
		for _, skill := range e.playerAISkills(member) {
			if playerAIIsHeal(skill.definition) {
				return maximum * 0.80, nil
			}
		}
		if isPriorityTarget {
			return 0, nil
		}
		return maximum * 0.50, nil
	}
	reserve := float32(0)
	for _, skill := range e.playerAISkills(member) {
		if (!playerAIIsDefense(skill.definition) && !playerAIIsHeal(skill.definition)) ||
			!member.abilityCooldownSession().IsReady(zoneability.HeroAbilityCooldown(skill.id), now) {
			continue
		}
		cost, err := game.ResolveAbilityManaCost(skill.definition.ManaCost,
			skill.creature.DamageProfile.PrimaryAttribute, skill.definition.ManaCoefficient, member.isOverdriveActiveAt(now))
		if err != nil {
			return 0, fmt.Errorf("reserveCost: %w", err)
		}
		reserve = max(reserve, cost)
	}
	return reserve, nil
}

func (e gameplayPendingRuntime) planPlayerAISupportLocked(member *gameplayPeerSession, command raknet.ActionCommandData, now time.Time) (raknet.ActionCommandData, bool, error) {
	isHighPower := member.binding.IsServerBot && member.binding.Mode == game.ModeChain &&
		playerAIPowerRatio(*member) >= 0.80
	bestScore := float32(0)
	bestSkill := playerAISkill{}
	bestTarget := gameplayPeerSession{}
	bestTiming := sim.AbilityDefinition{}
	bestPosition := raknet.Vector3{}
	for _, skill := range e.playerAISkills(*member) {
		isHeal := playerAIIsHeal(skill.definition)
		isDefense := playerAIIsDefense(skill.definition)
		if (!isHeal && !isDefense) ||
			!member.abilityCooldownSession().IsReady(zoneability.HeroAbilityCooldown(skill.id), now) ||
			(isHeal && (member.heroHealingTicks != nil || member.treeOfLifeRun != nil)) ||
			(isDefense && member.heroModifierRun != nil) {
			continue
		}
		cost, err := game.ResolveAbilityManaCost(skill.definition.ManaCost, skill.creature.DamageProfile.PrimaryAttribute,
			skill.definition.ManaCoefficient, member.isOverdriveActiveAt(now))
		if err != nil {
			return command, false, fmt.Errorf("supportCost: %w", err)
		}
		if cost > member.deployedManaPoint() {
			continue
		}
		timing, err := zoneability.ProjectTiming(skill.creature, skill.definition)
		if err != nil {
			return command, false, fmt.Errorf("supportTiming: %w", err)
		}
		for _, candidate := range e.registry.sessions {
			if !isPlayerAISession(candidate) || candidate.deployedHitPoint() <= 0 ||
				candidate.binding.GameID != member.binding.GameID || candidate.binding.Mode != member.binding.Mode ||
				(member.binding.Mode == game.ModeArena && candidate.binding.Team != member.binding.Team) ||
				(member.binding.Mode == game.ModeChain && candidate.zone != member.zone) {
				continue
			}
			isSelf := candidate.deployedObjectID == member.deployedObjectID
			if isSelf {
				candidate = *member
			}
			isAreaHeal := skill.definition.Kind == sim.AbilityKindAreaHealing
			// Area heals can be placed near an ally; other non-targeted skills
			// remain centered on the caster.
			if !isSelf && skill.definition.Name != "FieldMedicSupport" && !isAreaHeal {
				continue
			}
			castPosition := candidate.playerPosition
			if !isSelf {
				candidateDistance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(candidate.playerPosition))
				admissionRange := heroAbilityAdmissionRange(skill.creature, skill.definition)
				if isAreaHeal && admissionRange <= 0 {
					if candidateDistance > skill.definition.Radius {
						continue
					}
					castPosition = member.playerPosition
				} else if candidateDistance > admissionRange {
					continue
				}
			}
			maximum := candidate.characterHitPointMaximum(candidate.deployedCreatureIndex)
			if maximum <= 0 {
				continue
			}
			ratio := candidate.deployedHitPoint() / maximum
			pressure := candidate.playerAI.survival.recentLoss(now)
			isCleanse := isSelf && skill.definition.Name == "TechRandom2" && candidate.isEnemyRootActive(now)
			healThreshold := float32(0.60)
			if isHighPower && isHeal {
				healThreshold = 0.90
			}
			if !isCleanse && (ratio > healThreshold || (isDefense && (pressure <= 0 || (ratio > 0.30 && pressure < 0.15)))) {
				continue
			}
			score := (1-ratio)*100 + pressure*100
			if isCleanse {
				score += 60
			}
			if ratio <= 0.30 {
				score += 50
			}
			if isHeal {
				healing, healErr := zoneability.ProjectHealing(skill.creature, skill.definition, skill.definition.MinimumHealingPerTick)
				if healErr != nil {
					return command, false, fmt.Errorf("supportHealing: %w", healErr)
				}
				firstHealAt := timing.HitDelay
				if skill.definition.Kind == sim.AbilityKindHealingTicks {
					firstHealAt += timing.TickDuration
				}
				incoming := pressure * maximum / float32(playerAIDamageWindow.Seconds())
				// A slow heal cannot stabilize an actor likely to die before its
				// first tick. Let a shield or the existing swap roll handle it.
				if isSelf && incoming*float32(firstHealAt.Seconds()) >= candidate.deployedHitPoint() {
					continue
				}
				swapIndex, swapChance := playerAISurvivalSwap(*member, now)
				if isSelf && swapIndex >= 0 && swapChance > 0 && member.squad.IsDeployReady(now) &&
					healing < incoming*max(float32(1), float32(timing.TickDuration.Seconds())) {
					continue
				}
				minimumMissing := healing * 0.75
				if isHighPower {
					minimumMissing = max(maximum*0.05, healing*0.10)
				}
				if healing <= 0 || maximum-candidate.deployedHitPoint() < minimumMissing {
					continue
				}
				score += 25
			}
			if score > bestScore || (score == bestScore && candidate.binding.UserID < bestTarget.binding.UserID) {
				bestScore, bestSkill, bestTarget, bestTiming, bestPosition = score, skill, candidate, timing, castPosition
			}
		}
	}
	if bestScore == 0 {
		return command, false, nil
	}
	delay := playerAICastDelay(bestTiming)
	member.playerAI.nextDecisionAt = now.Add(delay)
	member.playerAI.nextAttackAt = now.Add(delay)
	member.playerAI.nextAbilityAt = now.Add(playerAIAbilityIntervalFor(*member))
	// Give healing time to land before considering a swap that would cancel it.
	member.playerAI.survival.nextRollAt = now.Add(max(delay, min(4*time.Second, bestTiming.Duration)))
	command.Common.Type = raknet.ActionUseCharacterAbility
	if bestSkill.index >= 6 && !isGenericHeroActiveKind(bestSkill.definition.Kind) {
		command.Common.Type = raknet.ActionUseSquadAbility
	}
	command.Ability = &raknet.ActionAbilityData{Index: bestSkill.index, Rank: 1,
		TargetID: bestTarget.deployedObjectID, CursorPosition: bestPosition, TargetPosition: bestPosition}
	return command, true, nil
}
