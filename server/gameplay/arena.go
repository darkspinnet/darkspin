package gameplay

import (
	"cmp"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/combat"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

const (
	arenaLobbyTransitionRetryInterval = time.Second
	arenaOpeningCountdownDuration     = 5 * time.Second
)

type arenaMatchState struct {
	isCountdownScheduled bool
	isCombatActive       bool
	variantOffsets       [2]int
	random               *sim.SimulatorRandom
	cancel               func()
}

type arenaCountdownJob struct {
	registry *gameplaySessionRegistry
	program  Programs
	gameID   uint32
	logger   *log.Logger
}

type gameplayArenaRuntime struct {
	gameplayJoin *game.GameplayJoin
	registry     *gameplaySessionRegistry
	logger       *log.Logger
}

func (r gameplaySwitchRuntime) handleArenaSwitch(
	packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession, switchStartTime time.Time,
) ([][]byte, error) {
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == commandSession.generation &&
		peerSession.binding.Mode == game.ModeArena &&
		peerSession.stage.IsDungeon() && peerSession.squad != nil &&
		peerSession.deployedCreatureIndex <
			uint32(len(peerSession.binding.Creatures)) &&
		command.Common.ObjectID == peerSession.deployedObjectID &&
		command.Value < uint32(len(peerSession.binding.Creatures)) &&
		command.Value != peerSession.deployedCreatureIndex &&
		peerSession.binding.Creatures[command.Value].Noun != 0 &&
		peerSession.squad.IsDeployReady(switchStartTime)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return r.rejectArenaSwitch(command, "admission unavailable")
	}
	targetCharacter, isTargetFound := peerSession.squad.Character(command.Value)
	sourceCharacter, isSourceFound := peerSession.squad.Character(
		peerSession.deployedCreatureIndex,
	)
	if !isTargetFound || !isSourceFound || !targetCharacter.IsAvailable ||
		targetCharacter.HitPoints <= 0 {
		r.registry.mutex.Unlock()
		return r.rejectArenaSwitch(command, "character unavailable")
	}
	sourceCreatureIndex := peerSession.deployedCreatureIndex
	sourceObjectID := peerSession.deployedObjectID
	targetObjectID := zonehero.ObjectID(peerSession.binding.Slot, command.Value)
	if targetObjectID == 0 {
		r.registry.mutex.Unlock()
		return r.rejectArenaSwitch(command, "target object unavailable")
	}
	switchPackets, err := marshalCampaignCharacterSwitch(
		uint8(peerSession.binding.Slot), sourceObjectID, targetObjectID,
		command.Value, peerSession.binding.Creatures[sourceCreatureIndex],
		peerSession.binding.Creatures[command.Value],
		sourceCharacter.HitPoints, sourceCharacter.ManaPoints,
		targetCharacter.HitPoints, targetCharacter.ManaPoints,
		peerSession.playerPosition, command.Common.Orientation,
		uint64(switchStartTime.UnixMilli()), true,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSwitchMarshal: %w", err)
	}
	departurePackets, err := marshalCampaignCharacterDeparture(
		sourceObjectID, peerSession.binding.Creatures[sourceCreatureIndex],
		peerSession.playerPosition, uint64(switchStartTime.UnixMilli()),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSwitchDeparture: %w", err)
	}
	err = peerSession.squad.DeployWithCooldown(
		command.Value, switchStartTime, standardCreatureSwapCooldown,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSwitchSquad: %w", err)
	}
	err = peerSession.stopPlayerMovement(switchStartTime)
	if err != nil && r.logger != nil {
		r.logger.Printf(
			"RakNet Arena movement cleanup omitted during switch remote=%s: %v",
			packet.Address, err,
		)
	}
	peerSession.resetPassiveDamageReduction(sourceCreatureIndex)
	peerSession.resetFireRavagerBasic(sourceCreatureIndex)
	peerSession.resetTCShield(sourceCreatureIndex)
	interruptedBasic := peerSession.resetAbilityAdmissionForSwitch()
	peerSession.deployedCreatureIndex = command.Value
	peerSession.deployedObjectID = targetObjectID
	peerSession.isHeroSelectionPending = false
	peerSession.isHeroSelectionScheduled = false
	peerSession.heroSelectionReadyAt = time.Time{}
	peerSession.passiveStationarySince[command.Value] = switchStartTime
	peerSession.startTCShieldRecharge(command.Value, switchStartTime)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet Arena squad swap accepted game=%d user=%d source=%d target=%d creature=%d",
			peerSession.binding.GameID, peerSession.binding.UserID,
			sourceObjectID, targetObjectID, command.Value,
		)
	}
	step := gameplaySwitchArrivalStep{packets: switchPackets}
	producer := raknet.ScheduledPacketProducer{
		Delay: campaignCreatureWarpOutDelay, Produce: step.produce,
	}
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{producer},
	)
	cancel, scheduleErr := packet.Autonomous().ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("arena switch arrival cancellation unavailable")
	}
	if scheduleErr == nil {
		return departurePackets, nil
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet Arena squad swap arrival schedule failed source=%d target=%d: %v",
			sourceObjectID, targetObjectID, scheduleErr,
		)
	}
	return append(departurePackets, switchPackets...), nil
}

func (r gameplaySwitchRuntime) rejectArenaSwitch(
	command raknet.ActionCommandData, reason string,
) ([][]byte, error) {
	packet, err := actionraknet.Reject(command)
	if err != nil {
		return nil, fmt.Errorf("arenaSwitchReject: %w", err)
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet Arena squad swap rejected source=%d creature=%d reason=%s",
			command.Common.ObjectID, command.Value, reason,
		)
	}
	return [][]byte{packet}, nil
}

func (r gameplayMovementRuntime) handleArena(
	packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession,
) ([][]byte, error) {
	if command.Movement == nil ||
		!isFiniteZonePosition(command.Common.Position) ||
		!isFiniteZonePosition(command.Movement.GoalPosition) {
		return nil, errors.New("arena movement invalid")
	}
	r.campaign.registry.mutex.Lock()
	peerSession, isFound := r.campaign.registry.sessions[packet.Address.String()]
	isCurrent := isFound && peerSession.generation == commandSession.generation &&
		peerSession.stage.IsDungeon() &&
		peerSession.binding.Mode == game.ModeArena &&
		peerSession.deployedObjectID == command.Common.ObjectID &&
		peerSession.deployedHitPoint() > 0
	if !isCurrent {
		r.campaign.registry.mutex.Unlock()
		return nil, errors.New("arena movement unavailable")
	}
	_, _, err := peerSession.advancePlayerMovement(
		r.campaign.now(), command.Common.Position,
		command.Movement.GoalPosition, false, 0,
	)
	if err != nil {
		r.campaign.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaMovementAdvance: %w", err)
	}
	r.campaign.registry.sessions[packet.Address.String()] = peerSession
	r.campaign.registry.mutex.Unlock()
	return marshalZonePlayerMove(
		command.Common.ObjectID, command.Movement.GoalPosition,
	)
}

func (r campaignAbilityCommandRuntime) handleArenaCharacter(
	packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession,
) ([][]byte, error) {
	if command.Common.Type != raknet.ActionUseCharacterAbility ||
		command.Ability == nil {
		return r.rejectArenaCharacter(command, "character ability unavailable")
	}
	sessionKey := packet.Address.String()
	attackTime := r.now()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == commandSession.generation &&
		peerSession.stage.IsDungeon() &&
		peerSession.binding.Mode == game.ModeArena &&
		peerSession.deployedObjectID == command.Common.ObjectID &&
		peerSession.deployedHitPoint() > 0 &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures))
	if !isCurrent {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "source unavailable")
	}
	if !peerSession.isArenaCombatActive {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "round preparing")
	}
	matchState := r.registry.arenaMatches[peerSession.binding.GameID]
	if matchState == nil || !matchState.isCombatActive || matchState.random == nil {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "match unavailable")
	}
	random := matchState.random
	err := peerSession.advancePlayerPosition(attackTime, command.Common.Position)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaAttackPosition: %w", err)
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	definition, abilityID, isActive, isDefinitionFound :=
		r.arenaCharacterAbility(creature, command.Ability.Index, command.Ability.Rank)
	if !isDefinitionFound {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "ability definition unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "ability timing unavailable")
	}
	remainingManaPoint := peerSession.deployedManaPoint()
	cooldownKey := zoneability.CooldownKey(0)
	if isActive {
		cooldownKey = zoneability.HeroAbilityCooldown(abilityID)
		isReady := peerSession.abilityCooldownSession().IsReady(cooldownKey, attackTime) &&
			peerSession.isAbilityReleaseReady(attackTime)
		if !isReady {
			r.registry.mutex.Unlock()
			return r.rejectArenaCharacter(command, "ability cooldown or release unavailable")
		}
		manaCost, manaErr := game.ResolveAbilityManaCost(
			projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
			projected.ManaCoefficient, peerSession.isOverdriveActiveAt(attackTime),
		)
		if manaErr != nil || remainingManaPoint < manaCost {
			r.registry.mutex.Unlock()
			return r.rejectArenaCharacter(command, "ability power unavailable")
		}
		remainingManaPoint -= manaCost
	}
	damageRange, err := zoneability.ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "ability damage unavailable")
	}
	targetKey, targetSession, isTargetFound := r.arenaEnemyTargetLocked(
		peerSession, command.Ability.TargetID, command.Ability.CursorPosition,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "enemy target unavailable")
	}
	err = targetSession.advancePlayerPosition(attackTime, raknet.Vector3{})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTargetPosition: %w", err)
	}
	maximumRange := heroAbilityAdmissionRange(creature, projected)
	actorRadius, actorRadiusErr := r.program.FootprintRadiusByNoun(creature.Noun)
	if actorRadiusErr == nil && actorRadius > 0 {
		maximumRange += actorRadius
	}
	targetCreature := targetSession.binding.Creatures[targetSession.deployedCreatureIndex]
	targetRadius, targetRadiusErr := r.program.FootprintRadiusByNoun(targetCreature.Noun)
	if targetRadiusErr == nil && targetRadius > 0 {
		maximumRange += targetRadius
	}
	if projected.Kind == sim.AbilityKindMelee {
		maximumRange += campaignHeldMeleeCursorRadius
	}
	distance := zonegeometry.Distance(
		game.Vec3(peerSession.playerPosition), game.Vec3(targetSession.playerPosition),
	)
	if maximumRange <= 0 || distance > maximumRange {
		r.registry.sessions[sessionKey] = peerSession
		r.registry.sessions[targetKey] = targetSession
		r.registry.mutex.Unlock()
		if r.logger != nil {
			r.logger.Printf(
				"RakNet Arena ability range rejected source=%d target=%d index=%d distance=%.3f maximum=%.3f",
				peerSession.deployedObjectID, targetSession.deployedObjectID,
				command.Ability.Index, distance, maximumRange,
			)
		}
		return r.rejectArenaCharacter(command, "enemy target out of range")
	}
	selectedDamage, err := sim.SelectRankDamage(random, sim.DamageRange{
		Minimum: damageRange.Minimum, Maximum: damageRange.Maximum,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "ability damage selection unavailable")
	}
	critical, err := sim.ResolveCriticalDamage(
		random, selectedDamage, peerSession.binding.Difficulty,
		sim.CriticalProfile{
			Rating: creature.CriticalRating, AutoCrit: creature.AutoCrit,
			DamageIncrease: creature.CriticalDamageIncrease,
		},
		r.program.Critical,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return r.rejectArenaCharacter(command, "ability critical unavailable")
	}
	defenseReq := combat.DefenseRequest{
		Damage: critical.Damage, DamageSource: uint8(projected.DamageSource),
		DamageType: uint8(projected.DamageType), IsSourceKnown: projected.IsDamageSourceFound,
		IsTypeKnown: projected.IsDamageTypeFound, IsArea: projected.DescriptorMask&8 != 0,
		IsPeriodic: projected.DescriptorMask&4 != 0,
	}
	defenseFlags, defenseErr := targetSession.rollDefense(random, defenseReq, r.program.Critical)
	if defenseErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaDefense: %w", defenseErr)
	}
	if targetSession.heroModifierRun.IsDamageImmune() || targetSession.heroQuantumBlink != nil {
		defenseFlags = 0x40
	}
	damage := critical.Damage
	damage = arenaDamageAfterReduction(
		targetSession, peerSession, damage, uint8(projected.DamageSource), attackTime,
	)
	damage = combat.ReduceIncomingDamage(damage, targetSession.equipmentDefense(), defenseReq)
	if defenseFlags != 0 {
		damage = 0
	}
	if damage <= 0 && defenseFlags == 0 {
		defenseFlags = 0x40
	}
	damage = max(float32(0), min(damage, targetSession.deployedHitPoint()))
	hitPoint := max(float32(0), targetSession.deployedHitPoint()-damage)
	targetObjectID := targetSession.deployedObjectID
	acceptPacket, err := actionraknet.Accept(
		command, projected.Name, packet.SourceTime,
		projected.HitDelay, projected.ReleaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaAttackAccept: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], util.HashID(projected.Name),
		command.Ability.Index, packet.SourceTime,
		projected.HitDelay, projected.ReleaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaAttackRelease: %w", err)
	}
	direction := zonegeometry.Direction(
		game.Vec3(peerSession.playerPosition), game.Vec3(targetSession.playerPosition),
	)
	presentationPackets := make([][]byte, 0, 5)
	if isActive {
		animationPacket, animationErr := abilityraknet.Animation(
			peerSession.deployedObjectID, projected.AnimationName, packet.SourceTime,
		)
		if animationErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaAbilityAnimation: %w", animationErr)
		}
		presentationPackets = append(presentationPackets, animationPacket)
		spendPackets, spendErr := abilityraknet.SpendPresentation(
			peerSession.deployedObjectID, abilityID, projected.Cooldown,
			packet.SourceTime, remainingManaPoint,
		)
		if spendErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaAbilitySpend: %w", spendErr)
		}
		presentationPackets = append(presentationPackets, spendPackets...)
	} else {
		posePackets, poseErr := marshalZonePlayerAttackPose(
			peerSession.deployedObjectID, peerSession.playerPosition,
			raknet.Vector3(direction), targetSession.playerPosition, targetObjectID,
		)
		if poseErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaAttackPose: %w", poseErr)
		}
		presentationPackets = append(presentationPackets, posePackets...)
	}
	if projected.MuzzleEffectName != "" {
		muzzlePacket, muzzleErr := raknet.MarshalApplication(raknet.ObjectEffectMessage{
			Asset:    util.HashID(projected.MuzzleEffectName),
			ObjectID: peerSession.deployedObjectID, AttackerID: peerSession.deployedObjectID,
			Facing: raknet.Vector3(direction),
		})
		if muzzleErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaAbilityMuzzle: %w", muzzleErr)
		}
		presentationPackets = append(presentationPackets, muzzlePacket)
	}
	flags := uint16(0x0001)
	if hitPoint == 0 {
		flags |= 0x0004
	}
	if critical.IsCritical {
		flags |= 0x0008
	}
	if defenseFlags != 0 {
		flags = defenseFlags
		critical.IsCritical = false
	}
	eventPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: flags, DeltaHealth: damage, TargetID: targetObjectID,
		SourceID: peerSession.deployedObjectID, IntegerHPChange: -int32(damage),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaAttackEvent: %w", err)
	}
	impactPacket, err := arenaImpactPresentation(
		projected, peerSession.deployedObjectID, targetObjectID,
		targetSession.playerPosition, raknet.Vector3(direction), critical.IsCritical,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaAttackImpact: %w", err)
	}
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: targetObjectID, HitPoints: hitPoint, IsHitPointChanged: true,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaAttackHealth: %w", err)
	}
	previousManaPoint := peerSession.deployedManaPoint()
	cooldownReservation := zoneability.CooldownReservation{}
	releaseReservation := zoneaction.ReleaseReservation{}
	if isActive {
		var isReserved bool
		cooldownReservation, isReserved = peerSession.abilityCooldownSession().Reserve(
			cooldownKey, attackTime, projected.Cooldown,
		)
		if !isReserved {
			r.registry.mutex.Unlock()
			return r.rejectArenaCharacter(command, "ability cooldown unavailable")
		}
		releaseReservation, isReserved = peerSession.abilityReleaseSession().Reserve(
			attackTime, projected.ReleaseDelay,
		)
		if !isReserved {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			r.registry.mutex.Unlock()
			return r.rejectArenaCharacter(command, "ability release unavailable")
		}
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
		if err != nil {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaAbilityMana: %w", err)
		}
	}
	isMatchLethal := hitPoint <= 0 && targetSession.squad.LivingCount() == 1
	branchPacket := []byte(nil)
	if isMatchLethal {
		branchPacket, err = raknet.MarshalApplication(
			raknet.ArenaGameBranchMessage{IsAlternate: true},
		)
		if err != nil {
			rollbackErr := rollbackArenaAbilitySpend(
				&peerSession, isActive, previousManaPoint,
				cooldownReservation, releaseReservation,
			)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"arenaVictoryBranch: %w", errors.Join(err, rollbackErr),
			)
		}
		err = packet.AfterResponseCommit(func() {
			r.commitArenaVictory(
				peerSession.binding.GameID, uint32(peerSession.binding.Team),
				sessionKey, targetKey, targetSession.generation, branchPacket,
			)
		})
		if err != nil {
			rollbackErr := rollbackArenaAbilitySpend(
				&peerSession, isActive, previousManaPoint,
				cooldownReservation, releaseReservation,
			)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"arenaVictoryCommit: %w", errors.Join(err, rollbackErr),
			)
		}
	}
	transitionPackets, err := targetSession.applyArenaDamageHitPoints(
		hitPoint, packet.SourceTime,
	)
	if err != nil {
		rollbackErr := rollbackArenaAbilitySpend(
			&peerSession, isActive, previousManaPoint,
			cooldownReservation, releaseReservation,
		)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf(
			"arenaAttackDamage: %w", errors.Join(err, rollbackErr),
		)
	}
	privateTransitionPackets := arenaTargetPrivateTransitionPackets(transitionPackets)
	if len(privateTransitionPackets) != 0 {
		targetSession.queuePackets(privateTransitionPackets)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.sessions[targetKey] = targetSession
	r.registry.mutex.Unlock()
	packets := [][]byte{acceptPacket, releasePacket}
	packets = append(packets, presentationPackets...)
	packets = append(packets, eventPacket)
	if len(impactPacket) != 0 {
		packets = append(packets, impactPacket)
	}
	packets = append(packets, healthPacket)
	packets = append(packets, gameplayPeerPresentationPackets(transitionPackets)...)
	if len(branchPacket) != 0 {
		packets = append(packets, branchPacket)
	}
	r.logger.Printf(
		"RakNet Arena ability accepted game=%d source=%d target=%d index=%d ability=%q damage=%g critical=%t hit_point=%g",
		peerSession.binding.GameID, peerSession.deployedObjectID,
		targetObjectID, command.Ability.Index, projected.Name, damage,
		critical.IsCritical, hitPoint,
	)
	return packets, nil
}

func arenaImpactPresentation(
	definition sim.AbilityDefinition, sourceObjectID uint32, targetObjectID uint32,
	targetPosition raknet.Vector3, facing raknet.Vector3, isCritical bool,
) ([]byte, error) {
	effectName := definition.ImpactEffectName
	if definition.Kind == sim.AbilityKindMelee || effectName == "" {
		effectName = definition.HitEffectName
	}
	if definition.Kind == sim.AbilityKindToss &&
		definition.Toss.ImpactEffectName != "" {
		effectName = definition.Toss.ImpactEffectName
	}
	if definition.Kind == sim.AbilityKindCloudLob &&
		definition.CloudLob.ImpactEffectName != "" {
		effectName = definition.CloudLob.ImpactEffectName
	}
	if effectName == "" {
		return nil, nil
	}
	var message raknet.ApplicationMessage
	switch definition.Kind {
	case sim.AbilityKindProjectile, sim.AbilityKindProjectileBurst,
		sim.AbilityKindProjectileStatus, sim.AbilityKindToss,
		sim.AbilityKindCloudLob, sim.AbilityKindCursorArea,
		sim.AbilityKindTimedArea:
		message = raknet.PositionedEffectMessage{
			Asset: util.HashID(effectName), Position: targetPosition, Facing: facing,
		}
	default:
		message = raknet.ObjectEffectMessage{
			IsCritical: isCritical, Asset: util.HashID(effectName),
			ObjectID: targetObjectID, AttackerID: sourceObjectID, Facing: facing,
		}
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("impactMarshal: %w", err)
	}
	return packet, nil
}

func rollbackArenaAbilitySpend(
	peerSession *gameplayPeerSession, isActive bool, previousManaPoint float32,
	cooldownReservation zoneability.CooldownReservation,
	releaseReservation zoneaction.ReleaseReservation,
) error {
	if !isActive {
		return nil
	}
	err := peerSession.setDeployedManaPoints(previousManaPoint)
	peerSession.abilityCooldownSession().Rollback(cooldownReservation)
	peerSession.abilityReleaseSession().Rollback(releaseReservation)
	if err != nil {
		return fmt.Errorf("manaRollback: %w", err)
	}
	return nil
}

func arenaTargetPrivateTransitionPackets(packets [][]byte) [][]byte {
	privatePackets := make([][]byte, 0, 1)
	for _, packet := range packets {
		if len(packet) == 0 ||
			raknet.PacketID(packet[0]) != raknet.LabsPlayerUpdate {
			continue
		}
		privatePackets = append(privatePackets, packet)
	}
	return privatePackets
}

func (r campaignAbilityCommandRuntime) commitArenaVictory(
	gameID uint32, winnerTeam uint32, sourceSessionKey string,
	targetSessionKey string, targetGeneration uint64, branchPacket []byte,
) {
	if len(branchPacket) == 0 {
		return
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	targetSession, isTargetFound := r.registry.sessions[targetSessionKey]
	isCurrent := isTargetFound && targetSession.generation == targetGeneration &&
		targetSession.binding.GameID == gameID &&
		targetSession.binding.Mode == game.ModeArena &&
		targetSession.isZoneGameOver()
	if !isCurrent {
		return
	}
	recipientCount := 0
	for candidateKey, candidate := range r.registry.sessions {
		if candidate.binding.GameID != gameID ||
			candidate.binding.Mode != game.ModeArena {
			continue
		}
		candidate.isArenaCombatActive = false
		candidate.isArenaResultReady = true
		candidate.arenaWinningTeam = winnerTeam
		r.registry.clearActionLeasesLocked(
			candidateKey, candidate.transportGeneration,
		)
		if candidateKey != sourceSessionKey {
			candidate.queuePackets([][]byte{branchPacket})
			recipientCount++
		}
		r.registry.sessions[candidateKey] = candidate
	}
	if state := r.registry.arenaMatches[gameID]; state != nil {
		state.isCombatActive = false
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet Arena knockout committed game=%d winner_team=%d peers=%d",
			gameID, winnerTeam, recipientCount,
		)
	}
}

func (r campaignAbilityCommandRuntime) arenaCharacterAbility(
	creature game.GameplayCreature, abilityIndex uint32, rank int32,
) (sim.AbilityDefinition, uint32, bool, bool) {
	if abilityIndex == 0 {
		definition, isFound := r.program.PlayerBasicAbility[creature.Noun]
		return definition, util.HashID(definition.Name), false,
			isFound && definition.Name != ""
	}
	slot := zonecontent.HeroAbilitySlot(0)
	switch abilityIndex {
	case 2:
		slot = zonecontent.HeroAbilitySpecialTwo
	case 3:
		slot = zonecontent.HeroAbilityRandom
	default:
		return sim.AbilityDefinition{}, 0, false, false
	}
	ability, isFound := r.program.HeroAbility(creature.Noun, slot)
	if !isFound || ability.ID == 0 || ability.Definition.Name == "" {
		return sim.AbilityDefinition{}, 0, true, false
	}
	definition := projectHeroAbilityRank(ability.Definition, rank)
	return definition, ability.ID, true, true
}

func arenaDamageAfterReduction(
	target gameplayPeerSession, source gameplayPeerSession,
	damage float32, damageSource uint8, now time.Time,
) float32 {
	if damage <= 0 ||
		target.deployedCreatureIndex >= uint32(len(target.binding.Creatures)) {
		return damage
	}
	creature := target.binding.Creatures[target.deployedCreatureIndex]
	if creature.PassiveAuraRadius > 0 && zonegeometry.Distance(
		game.Vec3(source.playerPosition), game.Vec3(target.playerPosition),
	) > creature.PassiveAuraRadius {
		creature.PhysicalDamageReduction = 0
		creature.EnergyDamageReduction = 0
	}
	if target.isOverdriveActiveAt(now) &&
		creature.PassiveAbility == util.HashID("QuantumPositioning") {
		creature.EnergyDamageReduction += 0.50
	}
	reduction := target.passiveDamageReduction(now)
	if target.deployedObjectID == target.roarReductionObjectID &&
		now.Before(target.roarReductionExpiresAt) {
		reduction += 0.25
	}
	switch damageSource {
	case physicalDamageSource:
		reduction += creature.PhysicalDamageReduction
	case energyDamageSource:
		reduction += creature.EnergyDamageReduction
	}
	return combat.ApplyDamageReduction(damage, reduction)
}

func (r campaignAbilityCommandRuntime) arenaEnemyTargetLocked(
	peerSession gameplayPeerSession, targetObjectID uint32,
	cursorPosition raknet.Vector3,
) (string, gameplayPeerSession, bool) {
	selectedKey := ""
	selected := gameplayPeerSession{}
	selectedDistance := float32(0)
	selectionPosition := game.Vec3(peerSession.playerPosition)
	if isReportedZonePosition(cursorPosition) {
		selectionPosition = game.Vec3(cursorPosition)
	}
	for candidateKey, candidate := range r.registry.sessions {
		isEnemy := candidate.binding.GameID == peerSession.binding.GameID &&
			candidate.binding.Mode == game.ModeArena &&
			candidate.isArenaCombatActive &&
			candidate.binding.Team != 0 &&
			candidate.binding.Team != peerSession.binding.Team &&
			candidate.stage.IsDungeon() && candidate.deployedHitPoint() > 0 &&
			candidate.deployedCreatureIndex < uint32(len(candidate.binding.Creatures))
		if !isEnemy || targetObjectID != 0 && candidate.deployedObjectID != targetObjectID {
			continue
		}
		distance := zonegeometry.Distance(
			selectionPosition, game.Vec3(candidate.playerPosition),
		)
		if selectedKey != "" && distance >= selectedDistance {
			continue
		}
		selectedKey = candidateKey
		selected = candidate
		selectedDistance = distance
	}
	return selectedKey, selected, selectedKey != ""
}

func (r campaignAbilityCommandRuntime) rejectArenaCharacter(
	command raknet.ActionCommandData, reason string,
) ([][]byte, error) {
	packet, err := actionraknet.Reject(command)
	if err != nil {
		return nil, fmt.Errorf("arenaAttackReject: %w", err)
	}
	if r.logger != nil {
		targetObjectID := uint32(0)
		abilityIndex := uint32(0)
		if command.Ability != nil {
			targetObjectID = command.Ability.TargetID
			abilityIndex = command.Ability.Index
		}
		r.logger.Printf(
			"RakNet Arena action rejected source=%d target=%d index=%d reason=%s",
			command.Common.ObjectID, targetObjectID, abilityIndex, reason,
		)
	}
	return [][]byte{packet}, nil
}

// arenaFallbackSpawnPosition is retained only for incomplete content catalogs.
func arenaFallbackSpawnPosition(binding game.GameplayBinding) raknet.Vector3 {
	position := raknet.Vector3{X: -8, Y: -2}
	if binding.Team >= 2 {
		position.X = 8
	}
	if binding.Slot%2 != 0 {
		position.Y = 2
	}
	return position
}

type arenaPlacement struct {
	position    raknet.Vector3
	orientation raknet.Quaternion
}

func arenaSpawnPlacement(
	program Programs, binding game.GameplayBinding, group uint16, variantOffset int,
) arenaPlacement {
	spawns := program.ArenaSpawnGroup(binding.Level, group)
	if len(spawns) == 0 {
		return arenaPlacement{
			position:    arenaFallbackSpawnPosition(binding),
			orientation: raknet.Quaternion{W: 1},
		}
	}
	variant := (int(binding.Slot) + variantOffset) % len(spawns)
	spawn := spawns[variant]
	radians := float64(spawn.RotationDegrees) * math.Pi / 180
	position := raknet.Vector3{
		X: spawn.Position.X, Y: spawn.Position.Y, Z: spawn.Position.Z,
	}
	if len(spawns) == 1 && binding.ParticipantCount > 2 {
		partnerOffset := float32(-1.5)
		if binding.Slot%2 != 0 {
			partnerOffset = 1.5
		}
		position.X -= float32(math.Sin(radians)) * partnerOffset
		position.Y += float32(math.Cos(radians)) * partnerOffset
	}
	orientation := raknet.Quaternion{
		Z: float32(math.Sin(radians / 2)),
		W: float32(math.Cos(radians / 2)),
	}
	return arenaPlacement{position: position, orientation: orientation}
}

func (e *gameplaySessionRegistry) scheduleArenaCountdown(
	program Programs, gameID uint32,
) error {
	if e == nil || e.timer == nil {
		return errors.New("arena countdown timer unavailable")
	}
	e.mutex.Lock()
	if e.isClosed {
		e.mutex.Unlock()
		return errors.New("arena countdown registry closed")
	}
	state := e.arenaMatches[gameID]
	if state != nil && (state.isCountdownScheduled || state.isCombatActive) {
		e.mutex.Unlock()
		return nil
	}
	readyCount := 0
	expectedCount := 0
	var binding game.GameplayBinding
	for _, candidate := range e.sessions {
		if candidate.binding.GameID != gameID || candidate.binding.Mode != game.ModeArena {
			continue
		}
		binding = candidate.binding
		expectedCount = int(candidate.binding.ParticipantCount)
		if candidate.stage.IsDungeon() && candidate.dungeonSetup.IsCommitted() {
			readyCount++
		}
	}
	if expectedCount == 0 || readyCount != expectedCount {
		e.mutex.Unlock()
		return nil
	}
	state = &arenaMatchState{
		isCountdownScheduled: true,
		random: sim.NewSimulatorRandom(
			binding.GameID ^ (binding.Difficulty << 24) ^ 0xa20e,
		),
	}
	for teamIndex := range state.variantOffsets {
		spawns := program.ArenaSpawnGroup(binding.Level, uint16(teamIndex+3))
		if len(spawns) > 1 {
			state.variantOffsets[teamIndex] = rand.IntN(len(spawns))
		}
	}
	e.arenaMatches[gameID] = state
	e.mutex.Unlock()
	job := arenaCountdownJob{
		registry: e, program: program, gameID: gameID, logger: e.logger,
	}
	cancel, err := e.timer.Schedule(arenaOpeningCountdownDuration, job.execute)
	if err != nil {
		e.mutex.Lock()
		if e.arenaMatches[gameID] == state {
			delete(e.arenaMatches, gameID)
		}
		e.mutex.Unlock()
		return fmt.Errorf("arenaCountdownSchedule: %w", err)
	}
	e.mutex.Lock()
	if e.arenaMatches[gameID] == state {
		state.cancel = cancel
	}
	e.mutex.Unlock()
	if e.logger != nil {
		e.logger.Printf(
			"RakNet Arena opening countdown game=%d level=%q seconds=5 players=%d",
			gameID, binding.Level, readyCount,
		)
	}
	return nil
}

func (e arenaCountdownJob) execute() {
	if e.registry == nil {
		return
	}
	e.registry.mutex.Lock()
	state := e.registry.arenaMatches[e.gameID]
	if e.registry.isClosed || state == nil || state.isCombatActive {
		e.registry.mutex.Unlock()
		return
	}
	expectedCount := 0
	readyCount := 0
	for _, candidate := range e.registry.sessions {
		if candidate.binding.GameID != e.gameID || candidate.binding.Mode != game.ModeArena {
			continue
		}
		expectedCount = int(candidate.binding.ParticipantCount)
		if candidate.stage.IsDungeon() && candidate.dungeonSetup.IsCommitted() &&
			candidate.deployedObjectID != 0 {
			readyCount++
		}
	}
	if expectedCount == 0 || readyCount != expectedCount {
		delete(e.registry.arenaMatches, e.gameID)
		e.registry.mutex.Unlock()
		if e.logger != nil {
			e.logger.Printf(
				"RakNet Arena opening countdown cancelled game=%d ready=%d expected=%d",
				e.gameID, readyCount, expectedCount,
			)
		}
		return
	}
	packets := make([][]byte, 0, 8)
	positions := make(map[string]raknet.Vector3)
	for sessionKey, candidate := range e.registry.sessions {
		if candidate.binding.GameID != e.gameID || candidate.binding.Mode != game.ModeArena ||
			!candidate.stage.IsDungeon() || candidate.deployedObjectID == 0 {
			continue
		}
		teamIndex := int(candidate.binding.Team) - 1
		if teamIndex < 0 || teamIndex >= len(state.variantOffsets) {
			continue
		}
		placement := arenaSpawnPlacement(
			e.program, candidate.binding, candidate.binding.Team+2,
			state.variantOffsets[teamIndex],
		)
		teleportPacket, err := raknet.MarshalApplication(raknet.ObjectTeleportMessage{
			ObjectID: candidate.deployedObjectID,
			Position: placement.position, Orientation: placement.orientation,
		})
		if err != nil {
			e.registry.mutex.Unlock()
			if e.logger != nil {
				e.logger.Printf("RakNet Arena combat teleport game=%d omitted: %v", e.gameID, err)
			}
			return
		}
		packets = append(packets, teleportPacket)
		positions[sessionKey] = placement.position
	}
	if len(positions) != expectedCount {
		delete(e.registry.arenaMatches, e.gameID)
		e.registry.mutex.Unlock()
		if e.logger != nil {
			e.logger.Printf(
				"RakNet Arena combat teleport cancelled game=%d placements=%d expected=%d",
				e.gameID, len(positions), expectedCount,
			)
		}
		return
	}
	now := e.registry.now()
	motions := make(map[string]*zoneaction.Motion, len(positions))
	for sessionKey, position := range positions {
		motion, err := zoneaction.NewMotion(toSimPosition(position), now)
		if err != nil {
			delete(e.registry.arenaMatches, e.gameID)
			e.registry.mutex.Unlock()
			if e.logger != nil {
				e.logger.Printf(
					"RakNet Arena combat teleport game=%d omitted: %v", e.gameID, err,
				)
			}
			return
		}
		motions[sessionKey] = motion
	}
	for sessionKey, position := range positions {
		candidate := e.registry.sessions[sessionKey]
		candidate.playerMotion = motions[sessionKey]
		candidate.playerPosition = position
		candidate.isArenaCombatActive = true
		candidate.queuePackets(packets)
		e.registry.sessions[sessionKey] = candidate
	}
	state.isCombatActive = true
	state.isCountdownScheduled = false
	state.cancel = nil
	e.registry.mutex.Unlock()
	if e.logger != nil {
		e.logger.Printf(
			"RakNet Arena combat started game=%d teleports=%d", e.gameID, len(positions),
		)
	}
}

func (e gameplayArenaRuntime) handle(packet raknet.Packet) ([][]byte, error) {
	command, err := raknet.DecodeArenaPlayerCommand(packet.Payload)
	if err != nil {
		e.logger.Printf(
			"RakNet Arena command rejected for %s payload=%x error=%v",
			packet.Address, packet.Payload, err,
		)
		return nil, nil
	}
	e.logger.Printf(
		"RakNet Arena command received from %s type=%d accepted=%t deck_id=%d",
		packet.Address, command.Type, command.IsAccepted, command.DeckID,
	)
	if e.registry == nil {
		return nil, nil
	}
	sessionKey := packet.Address.String()
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	e.registry.mutex.RUnlock()
	if !isFound || peerSession.binding.Mode != game.ModeArena {
		return nil, nil
	}
	switch command.Type {
	case raknet.ArenaPlayerEnterLobby:
		return e.enterLobby(packet.Address.String(), peerSession.binding.GameID)
	case raknet.ArenaPlayerEnterResults:
		return e.enterResults(packet.Address.String(), peerSession.binding.GameID)
	case raknet.ArenaPlayerAcceptMission:
		return e.acceptSquad(sessionKey, peerSession, command)
	}
	return nil, nil
}

func (r gameplayPendingRuntime) completeDeveloperArenaVictory(
	packet raknet.Packet, queuedSession gameplayPeerSession,
) ([][]byte, bool, error) {
	branchPacket, err := raknet.MarshalApplication(
		raknet.ArenaGameBranchMessage{IsAlternate: true},
	)
	if err != nil {
		return nil, false, fmt.Errorf("arenaVictoryBranch: %w", err)
	}
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && currentSession.generation == queuedSession.generation &&
		currentSession.binding.Mode == game.ModeArena &&
		currentSession.binding.Team != 0
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("arenaVictorySession: replaced")
	}
	winnerTeam := currentSession.binding.Team
	recipientCount := 0
	for candidateKey, candidate := range r.registry.sessions {
		if candidate.binding.GameID != currentSession.binding.GameID ||
			candidate.binding.Mode != game.ModeArena {
			continue
		}
		candidate.isArenaResultReady = true
		candidate.arenaWinningTeam = uint32(winnerTeam)
		r.registry.clearActionLeasesLocked(
			candidateKey, candidate.transportGeneration,
		)
		if candidateKey != sessionKey {
			candidate.queuePackets([][]byte{branchPacket})
			recipientCount++
		}
		r.registry.sessions[candidateKey] = candidate
	}
	r.registry.mutex.Unlock()
	if r.logger != nil {
		r.logger.Printf(
			"RakNet developer Arena victory selected game=%d user=%d team=%d peers=%d",
			currentSession.binding.GameID, currentSession.binding.UserID,
			winnerTeam, recipientCount,
		)
	}
	return [][]byte{branchPacket}, true, nil
}

func (e gameplayArenaRuntime) enterResults(
	sessionKey string, gameID uint32,
) ([][]byte, error) {
	e.registry.mutex.RLock()
	requester, isFound := e.registry.sessions[sessionKey]
	isReady := isFound && requester.binding.GameID == gameID &&
		requester.binding.Mode == game.ModeArena &&
		requester.isArenaResultReady && requester.arenaWinningTeam != 0
	if !isReady {
		e.registry.mutex.RUnlock()
		return nil, nil
	}
	result := e.arenaResultBodyLocked(gameID, requester.arenaWinningTeam)
	e.registry.mutex.RUnlock()
	packet, err := raknet.MarshalApplication(raknet.ArenaResultsSnapshotMessage{
		Results: result,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaResultsMarshal: %w", err)
	}
	if e.logger != nil {
		e.logger.Printf(
			"RakNet Arena results sent game=%d user=%d winning_team=%d",
			gameID, requester.binding.UserID, requester.arenaWinningTeam,
		)
	}
	return [][]byte{packet}, nil
}

func (e gameplayArenaRuntime) arenaResultBodyLocked(
	gameID uint32, winnerTeam uint32,
) raknet.ArenaResultsBody {
	result := raknet.ArenaResultsBody{
		SelectorA: winnerTeam, RoundTimeSeconds: 1,
	}
	if winnerTeam == 1 {
		result.CountA = 2
	} else {
		result.CountB = 2
	}
	for _, candidate := range e.registry.sessions {
		binding := candidate.binding
		if binding.GameID != gameID || binding.Mode != game.ModeArena ||
			binding.Slot >= uint16(len(result.Player)) {
			continue
		}
		player := raknet.ArenaPlayerResult{
			PlayerID: uint64(binding.UserID), AvatarID: binding.AvatarID,
			Team: uint8(binding.Team),
		}
		for roundIndex := 0; roundIndex < 2; roundIndex++ {
			if uint32(binding.Team) == winnerTeam {
				player.Round[roundIndex].Outcome = 3
			}
			for creatureIndex, creature := range binding.Creatures {
				player.Round[roundIndex].CreatureResource[creatureIndex] =
					zonehero.AppearanceAsset(creature)
				character, isCharacterFound := candidate.squad.Character(
					uint32(creatureIndex),
				)
				if !isCharacterFound {
					continue
				}
				player.Round[roundIndex].HealthPercent[creatureIndex] =
					arenaResultPercent(character.HitPoints, creature.HitPoint)
				player.Round[roundIndex].ManaPercent[creatureIndex] =
					arenaResultPercent(character.ManaPoints, creature.PowerPoint)
			}
		}
		result.Player[binding.Slot] = player
	}
	return result
}

func arenaResultPercent(current float32, maximum float32) float32 {
	if maximum <= 0 {
		return 0
	}
	return min(float32(1), max(float32(0), current/maximum))
}

func (e gameplayArenaRuntime) enterLobby(
	sessionKey string, gameID uint32,
) ([][]byte, error) {
	branchPacket, err := raknet.MarshalApplication(raknet.ArenaGameBranchMessage{})
	if err != nil {
		return nil, fmt.Errorf("arenaBranchMarshal: %w", err)
	}
	recipients := 0
	e.registry.mutex.Lock()
	for candidateKey, candidate := range e.registry.sessions {
		if candidateKey == sessionKey || candidate.binding.GameID != gameID ||
			candidate.binding.Mode != game.ModeArena ||
			candidate.isArenaLobbyTransitionSent {
			continue
		}
		candidate.isArenaLobbyTransitionSent = true
		candidate.queuePackets([][]byte{branchPacket})
		e.registry.sessions[candidateKey] = candidate
		recipients++
	}
	requester := e.registry.sessions[sessionKey]
	requester.isArenaLobbyTransitionSent = true
	requester.isArenaLobbyEntered = true
	e.registry.sessions[sessionKey] = requester
	e.registry.mutex.Unlock()
	if e.logger != nil {
		e.logger.Printf(
			"RakNet Arena lobby transition game_id=%d source=%s peers=%d",
			gameID, sessionKey, recipients,
		)
	}
	playerPackets, err := e.lobbyPlayerPackets(gameID)
	if err != nil {
		return nil, fmt.Errorf("arenaLobbyPlayers: %w", err)
	}
	snapshotPackets, err := e.lobbySnapshot(gameID)
	if err != nil {
		return nil, fmt.Errorf("arenaLobbySnapshot: %w", err)
	}
	return append(playerPackets, snapshotPackets...), nil
}

// prepareSelectedDecks starts loading only after the complete roster accepts.
func (e gameplayArenaRuntime) prepareSelectedDecks(
	requesterKey string, gameID uint32, isRetry bool,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	bindingsByKey := make(map[string]game.GameplayBinding)
	var expectedMask uint32
	var readyMask uint32
	for candidateKey, candidate := range e.registry.sessions {
		binding := candidate.binding
		if binding.GameID != gameID || binding.Mode != game.ModeArena || binding.Slot >= 32 {
			continue
		}
		bindingsByKey[candidateKey] = binding
		expectedMask |= binding.PlayerMask
		if binding.SquadID != 0 && candidate.isArenaSquadAccepted {
			readyMask |= uint32(1) << binding.Slot
		}
	}
	if expectedMask == 0 || readyMask != expectedMask {
		return nil, nil
	}
	keys := make([]string, 0, len(bindingsByKey))
	for candidateKey := range bindingsByKey {
		keys = append(keys, candidateKey)
	}
	slices.SortFunc(keys, func(left, right string) int {
		return cmp.Compare(bindingsByKey[left].Slot, bindingsByKey[right].Slot)
	})
	isInitialPreparation := false
	for _, candidateKey := range keys {
		if !e.registry.sessions[candidateKey].isArenaPreparationSent {
			isInitialPreparation = true
			break
		}
	}
	packetsByKey := make(map[string][]byte, len(keys))
	for _, candidateKey := range keys {
		candidate := e.registry.sessions[candidateKey]
		if candidate.isArenaPreparationAcknowledged {
			continue
		}
		if isRetry {
			if isInitialPreparation && candidate.isArenaPreparationSent {
				continue
			}
			if !isInitialPreparation && candidateKey != requesterKey {
				continue
			}
		} else if candidate.isArenaPreparationSent {
			continue
		}
		packet, err := marshalZoneSetup(candidate.binding)
		if err != nil {
			return nil, fmt.Errorf("arenaSetup[%d]: %w", candidate.binding.Slot, err)
		}
		packetsByKey[candidateKey] = packet
	}
	var requesterPackets [][]byte
	preparedCount := 0
	for _, candidateKey := range keys {
		packet := packetsByKey[candidateKey]
		if len(packet) == 0 {
			continue
		}
		candidate := e.registry.sessions[candidateKey]
		candidate.isArenaPreparationSent = true
		if candidateKey == requesterKey {
			requesterPackets = append(requesterPackets, packet)
		} else {
			candidate.queuePackets([][]byte{packet})
		}
		e.registry.sessions[candidateKey] = candidate
		preparedCount++
	}
	if e.logger != nil && preparedCount != 0 && isInitialPreparation {
		e.logger.Printf(
			"RakNet Arena selected decks prepared game_id=%d players=%d mask=%#x",
			gameID, preparedCount, readyMask,
		)
	}
	return requesterPackets, nil
}

// lobbyPlayerPackets repeats the complete Arena player baseline immediately
// before the lobby snapshot. A peer can enter the Arena branch while its
// asynchronously queued roster is still in flight, but build 103 will retain
// the snapshot without constructing the lobby until every player is set up.
func (e gameplayArenaRuntime) lobbyPlayerPackets(gameID uint32) ([][]byte, error) {
	bindings := make([]game.GameplayBinding, 0, 6)
	e.registry.mutex.RLock()
	for _, candidate := range e.registry.sessions {
		if candidate.binding.GameID != gameID || candidate.binding.Mode != game.ModeArena {
			continue
		}
		bindings = append(bindings, candidate.binding)
	}
	e.registry.mutex.RUnlock()
	slices.SortFunc(bindings, func(left, right game.GameplayBinding) int {
		return cmp.Compare(left.Slot, right.Slot)
	})
	packets := make([][]byte, 0, len(bindings))
	for _, binding := range bindings {
		packet, err := marshalCampaignInitialPlayer(binding, raknet.PlayerStatus{})
		if err != nil {
			return nil, fmt.Errorf("arenaPlayer[%d]: %w", binding.Slot, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e gameplayArenaRuntime) lobbySnapshot(gameID uint32) ([][]byte, error) {
	result := raknet.ArenaResultsBody{}
	e.registry.mutex.RLock()
	for _, candidate := range e.registry.sessions {
		binding := candidate.binding
		if binding.GameID != gameID || binding.Mode != game.ModeArena ||
			binding.Slot >= 6 {
			continue
		}
		result.Player[binding.Slot] = raknet.ArenaPlayerResult{
			PlayerID: uint64(binding.UserID), AvatarID: binding.AvatarID,
			Team: uint8(binding.Team),
		}
	}
	e.registry.mutex.RUnlock()
	packet, err := raknet.MarshalApplication(raknet.ArenaLobbySnapshotMessage{
		LobbyFlag: 1, LobbyWord: 1, Results: result,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaLobbyMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}
