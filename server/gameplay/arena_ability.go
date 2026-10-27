package gameplay

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
)

// Reuse ordinary hero execution once its world dependencies support Arena.
// World-dependent families remain on the existing path until their player
// target adapters are connected; never admit one into a nil campaign world.
func (e campaignAbilityCommandRuntime) handleArenaAbility(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData,
	member gameplayPeerSession,
) ([][]byte, error) {
	if member.isKnockbackActive(e.now()) {
		return e.rejectArenaCharacter(command, "knockback in progress")
	}
	if command.Ability == nil || member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
		return e.rejectArenaCharacter(command, "ability source unavailable")
	}
	creature := member.binding.Creatures[member.deployedCreatureIndex]
	slot := zonecontent.HeroAbilitySlot(0)
	switch command.Ability.Index {
	case 2:
		slot = zonecontent.HeroAbilitySpecialTwo
	case 3:
		slot = zonecontent.HeroAbilityRandom
	case 6, 7, 8:
		index := command.Ability.Index - 6
		if index >= uint32(len(member.binding.Creatures)) {
			return e.rejectArenaCharacter(command, "support source unavailable")
		}
		creature = member.binding.Creatures[index]
		slot = zonecontent.HeroAbilitySpecialOne
	}
	ability, isFound := e.program.HeroAbility(creature.Noun, slot)
	definition := ability.Definition
	if command.Ability.Index == 0 {
		definition, isFound = e.program.PlayerBasicAbility[creature.Noun]
	} else {
		definition = projectHeroAbilityRank(definition, command.Ability.Rank)
	}
	isShared := isFound && (ability.AssetName == "CastEnrage" || ability.AssetName == "TreeOfLife" ||
		definition.Name == "MissileTempestActive" ||
		(definition.Kind == sim.AbilityKindToss && definition.Toss.Behavior == sim.TossAbilityBehaviorTrapper) ||
		ability.AssetName == "ArborealMight" || ability.AssetName == "SpacetimeRandom2" ||
		isSharedArenaArea(definition, command.Ability.Index) || isSharedArenaProjectile(definition) ||
		(command.Ability.Index == 0 && isSharedArenaMelee(definition)))
	if !isShared {
		return e.handleArenaCharacter(packet, command, member)
	}
	if command.Common.Type == raknet.ActionUseSquadAbility {
		command.Common.Type = raknet.ActionUseCharacterAbility
	}
	if command.Common.Type != raknet.ActionUseCharacterAbility {
		return e.rejectArenaCharacter(command, "character request unavailable")
	}

	e.registry.mutex.RLock()
	current, isCurrent := e.registry.sessions[packet.Address.String()]
	match := e.registry.arenaMatches[member.binding.GameID]
	isReady := isCurrent && current.generation == member.generation &&
		current.binding.Mode == game.ModeArena && current.isArenaCombatActive &&
		match != nil && match.isCombatActive && current.stage.IsDungeon() &&
		current.deployedObjectID == command.Common.ObjectID && current.deployedHitPoint() > 0
	e.registry.mutex.RUnlock()
	if !isReady {
		return e.rejectArenaCharacter(command, "round or source unavailable")
	}
	packets, err := e.handleCharacter(ctx, packet, command, current)
	if err != nil {
		return nil, fmt.Errorf("arenaHeroAbility: %w", err)
	}
	breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), current.generation, packets)
	if breakErr != nil {
		return nil, fmt.Errorf("arenaModifierBreak: %w", breakErr)
	}
	return append(packets, breakPackets...), nil
}

// These predicates describe migrated execution dependencies, not a claim that
// every hero-specific on-hit callback has been verified against player targets.
func isSharedArenaMelee(definition sim.AbilityDefinition) bool {
	return definition.Kind == sim.AbilityKindMelee &&
		len(definition.MeleeHitPolicies) == 0 && definition.ChainLightning.MaximumHopCount == 0 &&
		definition.StatusDuration == 0 && definition.LifeSteal == 0
}

func isSharedArenaArea(definition sim.AbilityDefinition, index uint32) bool {
	if index == 0 && definition.Kind == sim.AbilityKindPointBlank && definition.BonusDamageMultiplier <= 0 {
		return false
	}
	if definition.Kind != sim.AbilityKindCursorArea && definition.Kind != sim.AbilityKindCone &&
		definition.Kind != sim.AbilityKindPointBlank {
		return false
	}
	if definition.StatusDuration != 0 || definition.StatusKind != "" ||
		definition.RootModifierID != 0 || definition.LifeSteal != 0 {
		return false
	}
	switch definition.Name {
	case "LightspeedTempestActive", binarySentinelSupportName, "LightningRogueSupport", "TechRandom":
		return false
	}
	return true
}

func isSharedArenaProjectile(definition sim.AbilityDefinition) bool {
	return definition.Kind == sim.AbilityKindProjectile &&
		(definition.LightningSecondary.MaximumCandidates == 0 || definition.Name == "PlasmaRandom_LightningBall") && definition.ChainLightning.MaximumHopCount == 0 &&
		definition.StatusDuration == 0 && definition.RootModifierID == 0 &&
		definition.LifeSteal == 0 && (definition.DamageHealingFraction == 0 || definition.Name == "SupportHealerBasic") &&
		definition.Name != "LightspeedTempestBasic"
}
