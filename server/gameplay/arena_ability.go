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
	if member.arenaEffectPool != nil {
		e.effectPool = member.arenaEffectPool
		e.damage.effectPool = member.arenaEffectPool
	}
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
	if isFound && (ability.AssetName == "ClaymoreTrap" || ability.AssetName == "PipeBomb" || ability.AssetName == "TurretTrap") {
		packets, err := e.handleArenaClaymore(packet, command, member, ability.ID, definition)
		if err != nil {
			return nil, fmt.Errorf("arenaClaymore: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaClaymoreBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	if isFound && ability.AssetName == "SummonSprite" && definition.Kind == sim.AbilityKindSummonBuff {
		packets, err := e.handleArenaSprite(packet, command, member, ability.ID, definition)
		if err != nil {
			return nil, fmt.Errorf("arenaSprite: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaSpriteBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	if isFound && (definition.Name == "PhantomCharge" || definition.Name == "EntanglingRush" || definition.Name == "BeastCharge" || definition.Name == "BioRandom2") {
		packets, err := e.handleArenaHeroCharge(packet, command, member, ability.ID, definition)
		if err != nil {
			return nil, fmt.Errorf("arenaHeroCharge: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaChargeBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	if isFound && definition.Name == "GravityStorm" && definition.Kind == sim.AbilityKindChannelArea {
		packets, err := e.handleArenaGravityStorm(packet, command, member, ability.ID, definition)
		if err != nil {
			return nil, fmt.Errorf("arenaGravityStorm: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaGravityBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	if isFound && definition.Name == "Sprout" && definition.Kind == sim.AbilityKindCloudLob {
		packets, err := e.handleArenaSprout(packet, command, member, definition)
		if err != nil {
			return nil, fmt.Errorf("arenaSprout: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaSproutBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	if isFound && definition.Name == "RootingPlague" && definition.Kind == sim.AbilityKindInfection {
		packets, err := e.handleArenaRootingPlague(packet, command, member, ability.ID, definition)
		if err != nil {
			return nil, fmt.Errorf("arenaRootingPlague: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaPlagueBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	if isFound && ability.AssetName == "LightningRogueActive" && definition.Kind == sim.AbilityKindTeleportStrike {
		packets, err := e.handleArenaRide(packet, command, member, ability.ID, definition)
		if err != nil {
			return nil, fmt.Errorf("arenaRide: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaRideBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	if isFound && ability.AssetName == "QuantumBlink" && definition.Kind == sim.AbilityKindQuantumBlink {
		packets, err := e.handleArenaQuantumBlink(packet, command, member, ability.ID, definition)
		if err != nil {
			return nil, fmt.Errorf("arenaQuantumBlink: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaQuantumBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	if isFound && (ability.AssetName == fireTempestSupportName || ability.AssetName == supportHealerName) &&
		definition.Kind == sim.AbilityKindTargetedAOE {
		packets, err := e.handleSquad(packet, command, member)
		if err != nil {
			return nil, fmt.Errorf("arenaTargetedSupport: %w", err)
		}
		e.recordScenarioAbilityDispatch(packet, command, member, packets, e.now())
		breakPackets, breakErr := e.breakHeroModifierOnAcceptedAbility(packet, packet.Address.String(), member.generation, packets)
		if breakErr != nil {
			return nil, fmt.Errorf("arenaTargetedSupportBreak: %w", breakErr)
		}
		return append(packets, breakPackets...), nil
	}
	isShared := isFound && (ability.AssetName == "CastEnrage" || ability.AssetName == "CastSoulLink" || ability.AssetName == "Ghostform" || ability.AssetName == "DeathsEmbrace" || ability.AssetName == "ShadowRavagerSupport" || ability.AssetName == "ShadowRavagerActive" || ability.AssetName == "EnergySentinelSupport" || ability.AssetName == "TreeOfLife" || ability.AssetName == "SummonBeast" || definition.Name == "EnergySentinelBasic" || definition.Name == "PlasmaSentinelBasic" ||
		definition.Name == "MissileTempestActive" || definition.Name == "FireRavagerSupport" ||
		definition.Name == "SoulRavagerActive" ||
		definition.Name == "SoulRavagerSupport" || definition.Name == "LightningTempest_Active" ||
		definition.Name == "PlasmaRandom_WebbedLightning" ||
		definition.Name == "TechRandom3" || definition.Name == "FieldMedicActive" ||
		definition.Name == "FieldMedicSupport" || definition.Name == "PlasmaSentinelSupport" || definition.Name == "PlasmaSentinelActive" || definition.Name == "FireTempestActive" ||
		definition.Name == "Sporogenesis" ||
		definition.Name == "VoodooTempestActive" || definition.Name == "VoodooTempestSupport" ||
		definition.Name == "NecroRandom" || definition.Name == "LightningRogueSupport" ||
		definition.Name == "AfflictionBolt" || definition.Name == "Terrify" ||
		definition.Name == "Psistorm" || definition.Name == "SleepingCloud" || definition.Name == "RepulsionWave" ||
		definition.Name == "SpacetimeRandom" || definition.Name == "QuantumState" || definition.Name == "TechRandom2" ||
		(definition.Kind == sim.AbilityKindToss && (definition.Toss.Behavior == sim.TossAbilityBehaviorTrapper ||
			definition.Toss.Behavior == sim.TossAbilityBehaviorVoodoo)) ||
		ability.AssetName == "ArborealMight" || ability.AssetName == "SpacetimeRandom2" ||
		isSharedArenaArea(definition, command.Ability.Index) || isSharedArenaProjectile(definition) ||
		(definition.Name == "LFPoisonRavager_Expunge" && definition.Kind == sim.AbilityKindMelee) ||
		(isSharedArenaMelee(definition) && (command.Ability.Index == 0 || definition.RootModifierID == 0)))
	if !isShared {
		if e.logger != nil {
			e.logger.Printf("Arena hero ability has no route asset=%s name=%s kind=%s index=%d",
				ability.AssetName, definition.Name, definition.Kind, command.Ability.Index)
		}
		return e.rejectArenaCharacter(command, "Arena hero ability route unavailable")
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
	var packets [][]byte
	var err error
	if ability.AssetName == "Ghostform" {
		if command.Common.Type != raknet.ActionUseSquadAbility {
			return e.rejectArenaCharacter(command, "ghost form squad request unavailable")
		}
		packets, err = e.handleSquad(packet, command, current)
		if err == nil {
			e.recordScenarioAbilityDispatch(packet, command, current, packets, e.now())
		}
	} else {
		if command.Common.Type == raknet.ActionUseSquadAbility {
			command.Common.Type = raknet.ActionUseCharacterAbility
		}
		if command.Common.Type != raknet.ActionUseCharacterAbility {
			return e.rejectArenaCharacter(command, "character request unavailable")
		}
		packets, err = e.handleCharacter(ctx, packet, command, current)
	}
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
		len(definition.MeleeHitPolicies) == 0 &&
		definition.StatusDuration == 0 && definition.LifeSteal == 0
}

func isSharedArenaArea(definition sim.AbilityDefinition, index uint32) bool {
	if definition.Name == "TimeRavagerSupport" && definition.Kind == sim.AbilityKindTeleportArea {
		return true
	}
	// Meteor's accepted-hit stun uses the target-owned Arena status runtime.
	if definition.Name == "PlasmaRandom" && definition.Kind == sim.AbilityKindCursorArea {
		return true
	}
	// Dazing Shot's snare and stacked silence belong to their recipients.
	if definition.Name == "TCShieldedSentinelSupport" && definition.Kind == sim.AbilityKindCursorArea &&
		(definition.StatusKind == sim.AbilityStatusKindSlow || definition.StatusKind == sim.AbilityStatusKindSilence) {
		return true
	}
	if definition.Name == "TCShieldedSentinelActive" && definition.Kind == sim.AbilityKindPointBlank &&
		definition.StatusKind == sim.AbilityStatusKindTaunt {
		return true
	}
	if definition.Name == poisonNovaName && definition.Kind == sim.AbilityKindPointBlank &&
		definition.RootModifierID != 0 && definition.TickDuration > 0 && definition.NumberOfTicks > 0 {
		return true
	}
	if definition.Name == binarySentinelActiveName && definition.Kind == sim.AbilityKindCone &&
		definition.RootModifierID != 0 {
		return true
	}
	if definition.Name == binarySentinelSupportName && definition.Kind == sim.AbilityKindCone {
		return true
	}
	if definition.Name == "LightspeedTempestActive" && definition.Kind == sim.AbilityKindCursorArea {
		return true
	}
	if definition.Name == "MissileTempestSupport" && definition.Kind == sim.AbilityKindCursorArea {
		return true
	}
	if definition.Name == "TechRandom" && definition.Kind == sim.AbilityKindCursorArea {
		return true
	}
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
	case "LightspeedTempestActive", binarySentinelSupportName, "LightningRogueSupport":
		return false
	}
	return true
}

func isSharedArenaProjectile(definition sim.AbilityDefinition) bool {
	return definition.Kind == sim.AbilityKindProjectile &&
		(definition.LightningSecondary.MaximumCandidates == 0 || definition.Name == "PlasmaRandom_LightningBall") && definition.ChainLightning.MaximumHopCount == 0 &&
		definition.StatusDuration == 0 && definition.RootModifierID == 0 &&
		definition.LifeSteal == 0 && (definition.DamageHealingFraction == 0 || definition.Name == "SupportHealerBasic")
}
