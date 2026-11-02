package gameplay

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

// Campaign AI uses companion combat authority, but hero nouns require the
// native player roster and deployment path. Only its human owner earns rewards.
type campaignBotRun struct {
	world               *zone.Zone
	binding             game.GameplayBinding
	objectID            uint32
	ownerObjectID       uint32
	transportGeneration uint64
	creature            game.GameplayCreature
	creatureIndex       uint32
	restoredPosition    game.Vec3
	ability             sim.AbilityDefinition
	abilities           []sim.AbilityDefinition
	abilityIndex        uint32
	nextAbilityAt       time.Time
	hitPoint            float32
	nextDecisionAt      time.Time
	isDefeated          bool
}

func (e gameplayPendingRuntime) pollCampaignBots(ctx context.Context, packet raknet.Packet) ([][]byte, error) {
	key := packet.Address.String()
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[key]
	isReady := isFound && member.binding.Mode == game.ModeChain && !member.binding.IsServerBot &&
		member.stage.IsDungeon() && member.dungeonSetup.IsCommitted() &&
		!member.isRejoinPending && !member.isZoneTerminal() && member.zone != nil && member.deployedHitPoint() > 0
	e.registry.mutex.RUnlock()
	if !isReady {
		return nil, nil
	}
	meleeNouns := make(map[uint32]bool)
	for noun, ability := range e.damage.npc.program.PlayerBasicAbility {
		if ability.Kind == sim.AbilityKindMelee && ability.Range > 0 {
			meleeNouns[noun] = true
		}
	}
	bindings, err := e.damage.gameplayJoin.BotBindings(ctx, member.binding, meleeNouns)
	if err != nil {
		return nil, fmt.Errorf("campaignBotBindings: %w", err)
	}
	packets := make([][]byte, 0)
	for _, binding := range bindings {
		botPackets, botErr := e.pollCampaignBot(packet, member.generation, binding)
		if botErr != nil {
			return nil, fmt.Errorf("campaignBot[%d]: %w", binding.Slot, botErr)
		}
		packets = append(packets, botPackets...)
	}
	return packets, nil
}

func (e gameplayPendingRuntime) pollCampaignBot(packet raknet.Packet, generation uint64, binding game.GameplayBinding) ([][]byte, error) {
	slot := binding.Slot
	key := packet.Address.String()
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[key]
	if !isFound || member.generation != generation || member.zone == nil || member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if member.campaignBots == nil {
		member.campaignBots = make(map[uint16]*campaignBotRun)
	}
	run := member.campaignBots[slot]
	if run == nil || run.world != member.zone {
		var createErr error
		run, createErr = newCampaignBotRun(member, e.damage.npc.program, slot, binding)
		if createErr != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("botPlan: %w", createErr)
		}
		member.campaignBots[slot] = run
		if restored, isRestored := member.zone.RestoredCampaignBot(slot); isRestored {
			run.hitPoint = restored.HitPoint
			run.restoredPosition = restored.Position
			run.isDefeated = restored.IsDefeated
		}
	}
	if run.isDefeated || e.now().Before(run.nextDecisionAt) {
		e.registry.sessions[key] = member
		e.registry.mutex.Unlock()
		return nil, nil
	}
	run.nextDecisionAt = e.now().Add(playerAIInterval)
	actor, isActorFound := member.zone.Companion().Snapshot(run.objectID)
	if isActorFound {
		run.hitPoint = actor.HitPoint
		member.zone.UpdateCampaignBot(slot, actor.Position, actor.HitPoint)
		if actor.HitPoint <= 0 {
			run.isDefeated = true
			member.zone.DefeatCampaignBot(slot)
			e.registry.sessions[key] = member
			e.registry.mutex.Unlock()
			return nil, nil
		}
	}
	if run.objectID == 0 || run.transportGeneration != member.transportGeneration {
		if isActorFound {
			run.restoredPosition = actor.Position
		} else if saved, isSaved := member.zone.CampaignBot(slot); isSaved {
			run.restoredPosition = saved.Position
			run.hitPoint = saved.HitPoint
		}
		packets, spawnErr := e.spawnCampaignBotLocked(&member, run, slot)
		e.registry.sessions[key] = member
		e.registry.mutex.Unlock()
		if spawnErr != nil {
			return nil, fmt.Errorf("botSpawn: %w", spawnErr)
		}
		member.zone.SaveCheckpointIfSafe(zonecheckpoint.ReasonDeployment)
		return packets, nil
	}
	if !isActorFound || actor.HitPoint <= 0 {
		run.isDefeated = true
		member.zone.DefeatCampaignBot(slot)
		e.registry.sessions[key] = member
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if run.ownerObjectID != member.deployedObjectID {
		// Keep the native hero and its appearance alive when the human swaps.
		// Only the server-owned follow target changes; it is not a new summon.
		actor.OwnerObjectID = member.deployedObjectID
		err := member.zone.Companion().Put(actor)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("botFollowOwner: %w", err)
		}
		run.ownerObjectID = member.deployedObjectID
	}
	if member.sagePassiveActivations == nil {
		member.sagePassiveActivations = make(map[uint32]summonCompanionActivation)
	}
	activation := member.sagePassiveActivations[run.objectID]
	if activation.Attack != nil {
		e.registry.sessions[key] = member
		e.registry.mutex.Unlock()
		return nil, nil
	}
	ability := run.ability
	now := e.now()
	isSkill := len(run.abilities) > 0 && !now.Before(run.nextAbilityAt)
	if isSkill {
		ability = run.abilities[run.abilityIndex%uint32(len(run.abilities))]
	}
	targets := member.zone.NPCs().LiveSnapshots()
	retargetPackets := make([][]byte, 0)
	if campaignBotHasCloserTarget(actor, targets) {
		stopPackets, stopErr := companionraknet.PursuitStop(actor.ObjectID, actor.Position)
		if stopErr != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("botRetargetStop: %w", stopErr)
		}
		if member.zone.Companion().CancelPursuit(actor.ObjectID, actor.PursuitObjectID,
			actor.Position, actor.PursuitRevision) {
			retargetPackets = stopPackets
		}
	}
	plan, isPlanFound, err := member.zone.Companion().ReserveActorAttack(run.objectID, targets,
		ability.Range, now, ability.HitDelay+ability.Cooldown)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("botReserve: %w", err)
	}
	if !isPlanFound {
		pursuit, isPursuitFound, pursuitErr := member.zone.Companion().ReserveActorPursuit(run.objectID,
			targets, ability.Range, playerAIEngagementDistance, zonecompanion.CompatibilityMovementSpeed, now)
		e.registry.sessions[key] = member
		e.registry.mutex.Unlock()
		if pursuitErr != nil {
			return nil, fmt.Errorf("botPursuit: %w", pursuitErr)
		}
		if !isPursuitFound {
			return retargetPackets, nil
		}
		packets, marshalErr := companionraknet.Pursuit(pursuit)
		if marshalErr != nil {
			e.damage.cancelCompanionPursuit(key, generation, pursuit.ObjectID, pursuit.TargetObjectID,
				pursuit.Position, pursuit.Revision)
			return nil, fmt.Errorf("botPursuitPacket: %w", marshalErr)
		}
		step := campaignCompanionPursuitStep{runtime: e.damage, packet: packet.Autonomous(),
			sessionKey: key, generation: generation, sourceTime: packet.SourceTime, plan: pursuit}
		err = step.schedule()
		if err != nil {
			return nil, fmt.Errorf("botPursuitSchedule: %w", err)
		}
		return append(retargetPackets, packets...), nil
	}
	start, err := newSummonCompanionAttack(summonCompanionAttackInput{
		Ability: ability, CompanionID: plan.ObjectID, TargetID: plan.TargetObjectID,
		Companion: sim.Position(plan.Position), Target: sim.Position(plan.TargetPosition),
		TargetHitPoint: plan.TargetHitPoint, Damage: ability.MinimumDamage, SourceTime: packet.SourceTime,
		CenterRange: plan.CenterRange, StartedAt: now,
	})
	if err != nil {
		member.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("botAttack: %w", err)
	}
	activation.Attack = start.Run
	activation.TargetObjectID = plan.TargetObjectID
	activation.CooldownEnd = plan.CooldownEnd
	if isSkill {
		run.nextAbilityAt = now.Add(playerAIAbilityInterval)
		run.abilityIndex = (run.abilityIndex + 1) % uint32(len(run.abilities))
	}
	member.sagePassiveActivations[plan.ObjectID] = activation
	e.registry.sessions[key] = member
	e.registry.mutex.Unlock()
	step := campaignCompanionAttackStep{runtime: e.damage, packet: packet.Autonomous(), sessionKey: key,
		generation: generation, sourceTime: packet.SourceTime, plan: plan, start: start,
		ability: ability, criticalNounID: run.creature.Noun, isCampaignBot: true}
	err = step.schedule()
	if err != nil {
		return nil, fmt.Errorf("botAttackSchedule: %w", err)
	}
	return append(retargetPackets, start.Packet...), nil
}

// Campaign bots can reconsider an in-flight chase each decision tick. Other
// companions retain their ordinary pursuit reservation and completion policy.
func campaignBotHasCloserTarget(actor zonecompanion.Actor, targets []zonenpc.Snapshot) bool {
	if actor.PursuitObjectID == 0 {
		return false
	}
	nearestDistance := float32(math.MaxFloat32)
	currentDistance := float32(0)
	nearestID := uint32(0)
	isCurrentFound := false
	for _, target := range targets {
		if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
			target.Plan.IsFixture || target.Faction == zonenpc.FactionPlayerAligned {
			continue
		}
		distance := actor.Position.Sub(target.Plan.Position).Length()
		if target.Plan.ObjectID == actor.PursuitObjectID {
			currentDistance = distance
			isCurrentFound = true
		}
		if distance < nearestDistance || distance == nearestDistance &&
			(nearestID == 0 || target.Plan.ObjectID < nearestID) {
			nearestDistance = distance
			nearestID = target.Plan.ObjectID
		}
	}
	if !isCurrentFound {
		return true
	}
	return nearestID != 0 && nearestID != actor.PursuitObjectID && nearestDistance < currentDistance
}

func newCampaignBotRun(member gameplayPeerSession, program Programs, slot uint16, binding game.GameplayBinding) (*campaignBotRun, error) {
	creatures := append([]game.GameplayCreature(nil), binding.Creatures[:]...)
	candidates := make([]game.GameplayCreature, 0)
	nouns := make(map[uint32]bool)
	for _, creature := range creatures {
		definition, isFound := program.PlayerBasicAbility[creature.Noun]
		if !isFound || definition.Kind != sim.AbilityKindMelee || definition.Range <= 0 || nouns[creature.Noun] {
			continue
		}
		nouns[creature.Noun] = true
		candidates = append(candidates, creature)
	}
	if len(candidates) == 0 {
		// Every imported melee hero noun is already installed client content.
		// A ranged-only squad supplies comparable health and gear statistics.
		for noun, definition := range program.PlayerBasicAbility {
			if definition.Kind != sim.AbilityKindMelee || definition.Range <= 0 {
				continue
			}
			creature := member.binding.Creatures[0]
			creature.Noun = noun
			creature.ID, creature.AppearanceVersion = noun, 1
			candidates = append(candidates, creature)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("campaign bot melee content unavailable")
	}
	slices.SortFunc(candidates, func(first, second game.GameplayCreature) int {
		if first.Noun < second.Noun {
			return -1
		}
		if first.Noun > second.Noun {
			return 1
		}
		return 0
	})
	creature := candidates[(member.binding.RunSeed+uint64(slot))%uint64(len(candidates))]
	if restored, isRestored := member.zone.RestoredCampaignBot(slot); isRestored &&
		restored.CreatureIndex < uint32(len(binding.Creatures)) &&
		binding.Creatures[restored.CreatureIndex].Noun != 0 {
		creature = binding.Creatures[restored.CreatureIndex]
	}
	ability := program.PlayerBasicAbility[creature.Noun]
	ability, err := zoneability.ProjectTiming(creature, ability)
	if err != nil {
		return nil, fmt.Errorf("botTiming: %w", err)
	}
	damageRange, err := game.ResolveAbilityDamageRange(game.AbilityDamage{
		Minimum: ability.MinimumDamage, Maximum: ability.MaximumDamage, Coefficient: ability.DamageCoefficient,
	}, creature.DamageProfile)
	if err != nil {
		return nil, fmt.Errorf("botDamage: %w", err)
	}
	ability.MinimumDamage, ability.MaximumDamage = damageRange.Minimum, damageRange.Maximum
	ability.ReleaseDelay = max(ability.ReleaseDelay, ability.HitDelay, playerAIInterval)
	ability.Cooldown = max(ability.Cooldown, playerAIInterval)
	abilities, err := campaignBotMeleeSkills(creature, program)
	if err != nil {
		return nil, fmt.Errorf("botSkills: %w", err)
	}
	var creatureIndex uint32
	isRosterCreature := false
	for index, rosterCreature := range binding.Creatures {
		if rosterCreature.Noun == creature.Noun && rosterCreature.ID == creature.ID {
			creatureIndex = uint32(index)
			isRosterCreature = true
			break
		}
	}
	if !isRosterCreature {
		binding.Creatures[0] = creature
	}
	return &campaignBotRun{world: member.zone, binding: binding, creature: creature, ability: ability,
		creatureIndex: creatureIndex, abilities: abilities}, nil
}

func campaignBotMeleeSkills(
	creature game.GameplayCreature, program Programs,
) ([]sim.AbilityDefinition, error) {
	slots := []zonecontent.HeroAbilitySlot{
		zonecontent.HeroAbilityRandom,
		zonecontent.HeroAbilitySpecialOne,
		zonecontent.HeroAbilitySpecialTwo,
	}
	abilities := make([]sim.AbilityDefinition, 0, len(slots))
	for _, slot := range slots {
		heroAbility, isFound := program.HeroAbility(creature.Noun, slot)
		if !isFound || heroAbility.ID == 0 || heroAbility.Definition.Kind != sim.AbilityKindMelee ||
			heroAbility.Definition.Range <= 0 || heroAbility.Definition.MinimumDamage <= 0 ||
			heroAbility.Definition.MaximumDamage < heroAbility.Definition.MinimumDamage {
			continue
		}
		ability := projectHeroAbilityRank(heroAbility.Definition, 1)
		projected, err := zoneability.ProjectTiming(creature, ability)
		if err != nil {
			return nil, fmt.Errorf("botSkillTiming[%d]: %w", slot, err)
		}
		damageRange, err := game.ResolveAbilityDamageRange(game.AbilityDamage{
			Minimum: projected.MinimumDamage, Maximum: projected.MaximumDamage,
			Coefficient: projected.DamageCoefficient,
		}, creature.DamageProfile)
		if err != nil {
			return nil, fmt.Errorf("botSkillDamage[%d]: %w", slot, err)
		}
		if damageRange.Minimum <= 0 || damageRange.Maximum < damageRange.Minimum {
			continue
		}
		projected.MinimumDamage = damageRange.Minimum
		projected.MaximumDamage = damageRange.Maximum
		projected.ReleaseDelay = max(projected.ReleaseDelay, projected.HitDelay, playerAIInterval)
		projected.Cooldown = max(projected.Cooldown, playerAIInterval)
		abilities = append(abilities, projected)
	}
	return abilities, nil
}

func (e gameplayPendingRuntime) spawnCampaignBotLocked(member *gameplayPeerSession, run *campaignBotRun, slot uint16) ([][]byte, error) {
	objectID := zonehero.ObjectID(slot, run.creatureIndex)
	position := game.Vec3(member.playerPosition)
	position.X += 2
	position.Z += float32(slot)
	if run.restoredPosition != (game.Vec3{}) {
		position = run.restoredPosition
	}
	footprint, err := e.damage.npc.program.FootprintRadiusByNoun(run.creature.Noun)
	if err != nil {
		return nil, fmt.Errorf("botFootprint: %w", err)
	}
	hitPoint := max(float32(1), run.creature.HitPoint, run.creature.MaximumHitPoint)
	maximumHitPoint := hitPoint
	if run.hitPoint > 0 {
		hitPoint = min(hitPoint, run.hitPoint)
	}
	packets, err := marshalZoneHeroRoster(zoneprojection.HeroRoster{
		UserID: run.binding.UserID, PlayerSlot: slot, Team: run.binding.Team,
		Roster: game.GameplayRoster{AvatarLevel: run.binding.AvatarLevel, AvatarXP: run.binding.AvatarXP,
			ChainProgression: run.binding.ChainProgression, DNA: run.binding.DNA,
			IsOverdriveUnlocked: run.binding.IsOverdriveUnlocked, Creatures: run.binding.Creatures},
		Position: position, CreatureIndex: run.creatureIndex,
	})
	if err != nil {
		return nil, fmt.Errorf("botCreate: %w", err)
	}
	joinedPacket, err := raknet.MarshalApplication(raknet.PlayerSlotMessage{
		ID: raknet.PlayerJoined, Slot: uint8(slot),
	})
	if err != nil {
		return nil, fmt.Errorf("botJoined: %w", err)
	}
	// Build 103 allocates the owning CPlayer on PlayerJoined, before the
	// initial squad definition and ObjectCreate can bind a hero to that slot.
	packets = append([][]byte{joinedPacket}, packets...)
	for index, message := range []raknet.ApplicationMessage{
		raknet.LabsPlayerStatusMessage{Slot: uint8(slot), Status: 8, Progress: 1},
		raknet.AttributeDataUpdateMessage{ObjectID: objectID, Value: map[uint8]float32{
			4: maximumHitPoint, 11: zonecompanion.CompatibilityMovementSpeed, 12: zonecompanion.CompatibilityMovementSpeed}},
		raknet.CombatantDataUpdateMessage{ObjectID: objectID, HitPoints: hitPoint},
	} {
		encoded, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("botAttribute[%d]: %w", index, marshalErr)
		}
		packets = append(packets, encoded)
	}
	err = member.zone.Companion().Put(zonecompanion.Actor{UserID: member.binding.UserID, PeerGeneration: member.generation,
		ObjectID: objectID, OwnerObjectID: member.deployedObjectID, Noun: run.creature.Noun, Position: position,
		FootprintRadius: footprint, HitPoint: hitPoint, MaximumHitPoint: maximumHitPoint, BodyScale: 1,
		IsTargetable: true, IsCombatant: true, MovementSpeed: zonecompanion.CompatibilityMovementSpeed, FollowDistance: 3,
		// FollowOwner skips actors whose follow is managed by another feature.
		IsFollowManaged: false})
	if err != nil {
		return nil, fmt.Errorf("botActorPut: %w", err)
	}
	run.objectID, run.ownerObjectID = objectID, member.deployedObjectID
	run.transportGeneration = member.transportGeneration
	run.hitPoint = hitPoint
	member.zone.SetCampaignBot(zonecheckpoint.CampaignBot{
		Request: game.BotRequest{OwnerUserID: run.binding.BotOwnerUserID, Slot: slot,
			Team: run.binding.Team, DisplayName: "AI Companion", AvatarID: run.binding.AvatarID,
			IsAppearanceResolved: true,
			Roster: game.GameplayRoster{AvatarLevel: run.binding.AvatarLevel, AvatarXP: run.binding.AvatarXP,
				ChainProgression: run.binding.ChainProgression, DNA: run.binding.DNA,
				IsOverdriveUnlocked: run.binding.IsOverdriveUnlocked, Creatures: run.binding.Creatures}},
		CreatureIndex: run.creatureIndex, Position: position, HitPoint: hitPoint,
	})
	if e.logger != nil {
		e.logger.Printf("Campaign AI hero spawned game=%d owner=%d object=%d noun=%#x", member.binding.GameID,
			member.binding.UserID, objectID, run.creature.Noun)
	}
	return packets, nil
}
