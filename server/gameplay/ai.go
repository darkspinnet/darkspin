package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const playerAIInterval = 250 * time.Millisecond
const playerAIAbilityInterval = 4 * time.Second
const playerAISwapHealthRatio = float32(0.25)
const playerAISwapAdvantage = float32(0.20)
const playerAIFleeHealthRatio = float32(0.15)

type playerAIState struct {
	isEnabled        bool
	isMoving         bool
	nextDecisionAt   time.Time
	nextAbilityAt    time.Time
	nextAttackAt     time.Time
	abilityIndex     uint32
	syncStamp        uint8
	nextRepositionAt time.Time
	nextDodgeAt      time.Time
	nextFleeAt       time.Time
	survival         playerAISurvival
	moveUntil        time.Time
	combatGoal       raknet.Vector3
	isStrafeLeft     bool
	nextIdleWanderAt time.Time
	idleWanderUntil  time.Time
	idleWanderGoal   raknet.Vector3
	isIdleWandering  bool
}

func (e *gameplaySessionRegistry) cancelPlayerAI(sessionKey string, transportGeneration uint64) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	member, isFound := e.sessions[sessionKey]
	if !isFound || member.transportGeneration != transportGeneration || !member.playerAI.isEnabled {
		return
	}
	member.playerAI = playerAIState{}
	member.followTargetUserID = 0
	e.clearActionLeasesLocked(sessionKey, transportGeneration)
	e.sessions[sessionKey] = member
}

func (e gameplayPendingRuntime) togglePlayerAI(
	packet raknet.Packet, queued gameplayPeerSession,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[packet.Address.String()]
	if !isFound || member.generation != queued.generation ||
		member.transportGeneration != queued.transportGeneration ||
		!isPlayerAISession(member) || member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, false, errors.New("AI requires an active multiplayer session")
	}
	isEnabled := !member.playerAI.isEnabled
	if isEnabled && e.playerAIParticipantCountLocked(member) < 2 {
		e.registry.mutex.Unlock()
		return nil, false, errors.New("AI requires another connected player")
	}
	err := member.stopPlayerMovement(e.now())
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("aiStop: %w", err)
	}
	member.playerAI = playerAIState{isEnabled: isEnabled, nextAbilityAt: e.now().Add(playerAIAbilityInterval)}
	member.followTargetUserID = 0
	member.basicSequenceSession().ReleaseHeld()
	member.campaignPlayerPursuitSession().Cancel()
	e.registry.clearActionLeasesLocked(packet.Address.String(), member.transportGeneration)
	e.registry.sessions[packet.Address.String()] = member
	e.registry.mutex.Unlock()
	packets, err := marshalPlayerAILocomotion(member, false)
	if err != nil {
		return nil, false, fmt.Errorf("aiToggleMove: %w", err)
	}
	e.registry.queuePeerPresentation(gameplayProducerIdentityFromSession(packet.Address.String(), member, true), packets)
	e.logger.Printf("RakNet player AI game=%d user=%d enabled=%t", member.binding.GameID, member.binding.UserID, isEnabled)
	return packets, true, nil
}

func isPlayerAISession(member gameplayPeerSession) bool {
	return (member.binding.Mode == game.ModeChain || member.binding.Mode == game.ModeArena) &&
		member.stage.IsDungeon() && member.dungeonSetup.IsCommitted() &&
		!member.isRejoinPending && member.squad != nil && member.deployedObjectID != 0 &&
		member.deployedCreatureIndex < uint32(len(member.binding.Creatures))
}

func (e gameplayPendingRuntime) playerAIParticipantCountLocked(member gameplayPeerSession) int {
	count := 0
	for _, candidate := range e.registry.sessions {
		if candidate.binding.GameID == member.binding.GameID &&
			candidate.binding.Mode == member.binding.Mode && isPlayerAISession(candidate) {
			count++
		}
	}
	return count
}

func (e gameplayPendingRuntime) pollPlayerAI(ctx context.Context, packet raknet.Packet) ([][]byte, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[packet.Address.String()]
	now := e.now()
	if !isFound || !member.playerAI.isEnabled || now.Before(member.playerAI.nextDecisionAt) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if !isPlayerAISession(member) || member.isZoneTerminal() ||
		e.playerAIParticipantCountLocked(member) < 2 {
		member.playerAI = playerAIState{}
		e.registry.clearActionLeasesLocked(packet.Address.String(), member.transportGeneration)
		err := member.stopPlayerMovement(now)
		e.registry.sessions[packet.Address.String()] = member
		e.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("aiDisableStop: %w", err)
		}
		packets, err := marshalPlayerAILocomotion(member, false)
		if err != nil {
			return nil, fmt.Errorf("aiDisableMove: %w", err)
		}
		e.registry.queuePeerPresentation(gameplayProducerIdentityFromSession(packet.Address.String(), member, true), packets)
		if member.binding.IsServerBot && member.binding.Mode == game.ModeChain {
			return nil, nil
		}
		return packets, nil
	}
	member.playerAI.nextDecisionAt = now.Add(playerAIInterval)
	command, isCommandReady, err := e.planPlayerAILocked(&member, now)
	e.registry.sessions[packet.Address.String()] = member
	e.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("aiPlan: %w", err)
	}
	if !isCommandReady {
		return nil, nil
	}
	// Use normal action admission and scheduling, but never send acknowledgements
	// for synthetic input to a client that has no matching pending command.
	packets, err := e.action.handleCommand(ctx, playerAIPacket(packet), command, true)
	if err != nil {
		return nil, fmt.Errorf("aiAction: %w", err)
	}
	packets = playerAIPresentation(packets)
	if command.Movement == nil {
		if member.binding.IsServerBot && member.binding.Mode == game.ModeChain {
			// Normal action publication already queues the public result for
			// every human, including this socketless actor's transport owner.
			return nil, nil
		}
		return packets, nil
	}
	e.registry.mutex.RLock()
	current, isCurrent := e.registry.sessions[packet.Address.String()]
	e.registry.mutex.RUnlock()
	if !isCurrent || !current.playerAI.isEnabled || current.generation != member.generation {
		return packets, nil
	}
	movementPackets, err := marshalPlayerAILocomotion(current, command.Movement.GoalFlags != 0x20)
	if err != nil {
		return nil, fmt.Errorf("aiLocomotion: %w", err)
	}
	if member.binding.IsServerBot && member.binding.Mode == game.ModeChain {
		// Human movement uses its dedicated adapter path. Synthetic movement
		// needs the same peer queue as abilities and delayed passive updates.
		e.registry.queuePeerPresentation(gameplayProducerIdentityFromSession(packet.Address.String(), current, true),
			append(packets, movementPackets...))
		return nil, nil
	}
	return append(packets, movementPackets...), nil
}

func (e gameplayPendingRuntime) planPlayerAILocked(
	member *gameplayPeerSession, now time.Time,
) (raknet.ActionCommandData, bool, error) {
	// The shared GOAP-style priority order is survive/swap, engage a live target,
	// maintain the hero's range, then use an available skill or basic attack.
	command := raknet.ActionCommandData{Common: raknet.ActionCommon{
		ObjectID: member.deployedObjectID, Orientation: raknet.Quaternion{W: 1},
	}}
	if member.binding.Mode == game.ModeArena && !member.isArenaCombatActive {
		return command, false, nil
	}
	if member.binding.Mode == game.ModeArena {
		e.registry.bindArenaMotionLocked(member)
	}
	err := member.advancePlayerPosition(now, raknet.Vector3{})
	if err != nil {
		return command, false, fmt.Errorf("aiPosition: %w", err)
	}
	command.Common.Position = member.playerPosition
	member.playerAI.survival.observe(*member, now)
	member.playerAI.syncStamp++
	command.Common.Unknown[0] = member.playerAI.syncStamp
	if member.deployedHitPoint() <= 0 {
		for index, character := range member.squad.State().Characters {
			if character.IsAvailable && character.HitPoints > 0 {
				command.Common.Type = raknet.ActionSwitchCharacter
				command.Value = uint32(index)
				member.playerAI.nextDecisionAt = now.Add(time.Second)
				return command, true, nil
			}
		}
		return command, false, nil
	}
	if !member.isAbilityReleaseReady(now) || member.heroInputLockRemaining(now) > 0 ||
		member.isEnemyStunActive(now) || member.isEnemySleepActive(now) || member.isEnemyFearActive(now) {
		return command, false, nil
	}
	// Healing is checked on every decision tick, independently of the slower
	// offensive skill timer, so a newly injured teammate can be helped promptly.
	supportCommand, isSupportReady, supportErr := e.planPlayerAISupportLocked(member, command, now)
	if supportErr != nil {
		return command, false, fmt.Errorf("aiSupport: %w", supportErr)
	}
	if isSupportReady {
		return supportCommand, true, nil
	}
	if member.heroHealingTicks != nil {
		incoming := member.playerAI.survival.recentLoss(now) *
			member.characterHitPointMaximum(member.deployedCreatureIndex) / float32(playerAIDamageWindow.Seconds())
		if member.deployedHitPoint() > incoming*1.5 {
			// Preserve a useful healing channel; another attack or swap would
			// cancel the recovery that the survival planner just paid for.
			return command, false, nil
		}
	}
	switchIndex, swapChance := playerAISurvivalSwap(*member, now)
	isSwap := false
	if switchIndex >= 0 && !now.Before(member.squad.Snapshot().DeployCooldownEnd) &&
		!now.Before(member.playerAI.survival.nextRollAt) {
		member.playerAI.survival.nextRollAt = now.Add(2 * time.Second)
		random, randomErr := e.registry.abilityRandomLocked(*member)
		if randomErr != nil {
			return command, false, fmt.Errorf("aiSurvivalRandom: %w", randomErr)
		}
		isSwap = random.Float64() < float64(swapChance)
	}
	if isSwap {
		command.Common.Type = raknet.ActionSwitchCharacter
		command.Value = uint32(switchIndex)
		member.playerAI.isMoving = false
		member.playerAI.nextDecisionAt = now.Add(time.Second)
		return command, true, nil
	}
	ally, isAllyFound := e.playerAIAllyLocked(*member)
	creature := member.binding.Creatures[member.deployedCreatureIndex]
	definition, isBasicFound := e.action.ability.program.PlayerBasicAbility[creature.Noun]
	isRanged := isBasicFound && definition.Kind != sim.AbilityKindMelee && definition.Range > 5
	isSupportRanged := isRanged && isCampaignSupportBot(*member)
	supportRetreatRatio := float32(0.60)
	supportApproachRatio := float32(0.72)
	if isSupportRanged && isAllyFound &&
		!isCampaignSupportHero(ally.binding.Creatures[ally.deployedCreatureIndex]) {
		supportRetreatRatio = 0.65
		supportApproachRatio = 0.75
	}
	target, isTargetFound := e.playerAITargetLocked(*member, ally, isAllyFound, now)
	distance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(target.position))
	attackRange := heroAbilityAdmissionRange(creature, definition)
	attackRange += member.deployedCampaignActorFootprintRadius() + target.radius
	isAttackReady := isBasicFound && isTargetFound && definition.Range > 0 &&
		distance <= max(float32(1), attackRange*0.9) &&
		!now.Before(member.playerAI.nextAttackAt) &&
		(member.basicSequence == nil || member.basicSequence.IsReady(now))
	if isRanged && !member.isEnemyRootActive(now) {
		goal, isThreatened := e.playerAIDodgeGoalLocked(*member, now)
		// Allow one evasive command before a ready shot, then reserve the next
		// decision for firing instead of extending the dodge on every tick.
		if isThreatened && (!isAttackReady || !now.Before(member.playerAI.nextDodgeAt)) {
			member.playerAI.combatGoal = goal
			member.playerAI.moveUntil = now.Add(600 * time.Millisecond)
			member.playerAI.nextDodgeAt = now.Add(time.Second)
			member.playerAI.nextRepositionAt = now.Add(2 * time.Second)
			return e.playerAIMoveLocked(member, command, goal)
		}
		if !isAttackReady && now.Before(member.playerAI.moveUntil) {
			return e.playerAIMoveLocked(member, command, member.playerAI.combatGoal)
		}
	}
	if !isTargetFound {
		if member.isEnemyRootActive(now) {
			return command, false, nil
		}
		if member.binding.IsServerBot && member.binding.Mode == game.ModeChain && isAllyFound {
			return e.playerAICampaignIdleLocked(member, command, ally, now)
		}
		goal := member.playerPosition
		if isAllyFound {
			goal = playerFollowGoal(member.playerPosition, ally.playerPosition)
		}
		return e.playerAIMoveLocked(member, command, goal)
	}
	member.playerAI.isIdleWandering = false
	member.playerAI.nextIdleWanderAt = time.Time{}
	if !isBasicFound || definition.Range <= 0 {
		return command, false, nil
	}
	currentMaximum := member.characterHitPointMaximum(member.deployedCreatureIndex)
	if currentMaximum > 0 && member.deployedHitPoint()/currentMaximum <= playerAIFleeHealthRatio &&
		!member.isEnemyRootActive(now) && !now.Before(member.playerAI.nextFleeAt) {
		goal := playerAIFlee(member.playerPosition, target.position, 7)
		member.playerAI.nextFleeAt = now.Add(4 * time.Second)
		member.playerAI.combatGoal = goal
		member.playerAI.moveUntil = now.Add(750 * time.Millisecond)
		return e.playerAIMoveLocked(member, command, goal)
	}
	retreatRatio := float32(0.55)
	if isSupportRanged {
		retreatRatio = supportRetreatRatio
	}
	if isRanged && distance < attackRange*retreatRatio && !member.isEnemyRootActive(now) &&
		!now.Before(member.playerAI.nextRepositionAt) {
		retreatDistance := attackRange*0.7 - distance
		if isSupportRanged {
			retreatDistance = attackRange*supportApproachRatio - distance
		}
		goal := playerAIFlee(member.playerPosition, target.position, retreatDistance)
		member.playerAI.combatGoal = goal
		member.playerAI.moveUntil = now.Add(500 * time.Millisecond)
		repositionDelay := 1500 * time.Millisecond
		if isSupportRanged {
			repositionDelay = 2 * time.Second
		}
		member.playerAI.nextRepositionAt = now.Add(repositionDelay)
		return e.playerAIMoveLocked(member, command, goal)
	}
	if distance > max(float32(1), attackRange*0.9) {
		if member.isEnemyRootActive(now) {
			return command, false, nil
		}
		approachDistance := attackRange * 0.7
		if isSupportRanged {
			approachDistance = attackRange * supportApproachRatio
		}
		goal := playerAIApproach(member.playerPosition, target.position, max(float32(1), approachDistance))
		return e.playerAIMoveLocked(member, command, goal)
	}
	if member.playerAI.isMoving && (!isRanged || !isAttackReady) {
		return e.playerAIMoveLocked(member, command, member.playerPosition)
	}
	if isRanged && !isAttackReady && !member.isEnemyRootActive(now) && !now.Before(member.playerAI.nextRepositionAt) {
		member.playerAI.isStrafeLeft = !member.playerAI.isStrafeLeft
		goal := playerAIStrafeGoal(member.playerPosition, target.position, member.playerAI.isStrafeLeft)
		member.playerAI.combatGoal = goal
		member.playerAI.moveUntil = now.Add(500 * time.Millisecond)
		member.playerAI.nextRepositionAt = now.Add(2500 * time.Millisecond)
		return e.playerAIMoveLocked(member, command, goal)
	}
	if now.Before(member.playerAI.nextAttackAt) ||
		(member.basicSequence != nil && !member.basicSequence.IsReady(now)) {
		return command, false, nil
	}
	abilityIndex := uint32(0)
	if !now.Before(member.playerAI.nextAbilityAt) {
		member.playerAI.nextAbilityAt = now.Add(playerAIAbilityIntervalFor(*member))
		abilityCount := uint32(2)
		if member.binding.Mode == game.ModeChain {
			abilityCount = 3
		}
		attemptCount := uint32(1)
		if member.binding.IsServerBot && member.binding.Mode == game.ModeChain && playerAIPowerRatio(*member) >= 0.80 {
			attemptCount = abilityCount
		}
		for attempt := uint32(0); attempt < attemptCount; attempt++ {
			member.playerAI.abilityIndex = (member.playerAI.abilityIndex + 1) % abilityCount
			candidateIndex := uint32(2) + member.playerAI.abilityIndex
			active, abilityID, isActive, isFound := e.action.ability.arenaCharacterAbility(creature, candidateIndex, 1)
			if candidateIndex == 4 {
				candidateIndex = 6 + member.deployedCreatureIndex
				ability, isAbilityFound := e.action.ability.program.HeroAbility(creature.Noun, zonecontent.HeroAbilitySpecialOne)
				active, abilityID = projectHeroAbilityRank(ability.Definition, 1), ability.ID
				isFound, isActive = isAbilityFound && ability.ID != 0 && active.Name != "", true
			}
			isOffensive := active.MaximumDamage > 0 || active.MinimumDamagePerTick > 0 || active.DamageCoefficient > 0
			isControl := (active.StatusKind == sim.AbilityStatusKindStun || active.StatusKind == sim.AbilityStatusKindSleep ||
				active.StatusKind == sim.AbilityStatusKindFear) && isAllyFound && target.aggroTargetID == ally.deployedObjectID &&
				ally.deployedHitPoint() < ally.characterHitPointMaximum(ally.deployedCreatureIndex)*0.40
			if !isFound || !isActive || (!isOffensive && !isControl) ||
				distance > heroAbilityAdmissionRange(creature, active) ||
				!member.abilityCooldownSession().IsReady(zoneability.HeroAbilityCooldown(abilityID), now) {
				continue
			}
			manaCost, manaErr := game.ResolveAbilityManaCost(active.ManaCost,
				creature.DamageProfile.PrimaryAttribute, active.ManaCoefficient, member.isOverdriveActiveAt(now))
			if manaErr != nil {
				return command, false, fmt.Errorf("aiSkillCost: %w", manaErr)
			}
			reserve, reserveErr := e.playerAIManaReserve(*member, now, target.isPriority)
			if reserveErr != nil {
				return command, false, fmt.Errorf("aiManaReserve: %w", reserveErr)
			}
			if member.deployedManaPoint() >= manaCost+reserve ||
				(!(member.binding.IsServerBot && member.binding.Mode == game.ModeChain) &&
					target.healthRatio <= 0.15 && member.deployedManaPoint() >= manaCost) {
				abilityIndex, definition = candidateIndex, active
				break
			}
		}
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		return command, false, fmt.Errorf("aiTiming: %w", err)
	}
	castDelay := playerAICastDelay(projected)
	member.playerAI.nextAttackAt = now.Add(castDelay)
	if abilityIndex == 0 {
		member.playerAI.nextAttackAt = now.Add(max(castDelay, projected.Cooldown))
	}
	if abilityIndex == 0 && !isRanged {
		castDelay = max(castDelay, projected.Cooldown)
	}
	member.playerAI.nextDecisionAt = now.Add(castDelay)
	command.Common.Type = raknet.ActionUseCharacterAbility
	if abilityIndex >= 6 && !isGenericHeroActiveKind(definition.Kind) {
		command.Common.Type = raknet.ActionUseSquadAbility
	}
	command.Ability = &raknet.ActionAbilityData{TargetID: target.objectID,
		CursorPosition: target.position, TargetPosition: target.position, Index: abilityIndex, Rank: 1}
	if abilityIndex != 0 && definition.Kind == sim.AbilityKindModifier {
		command.Ability.TargetID = member.deployedObjectID
		command.Ability.CursorPosition = member.playerPosition
		command.Ability.TargetPosition = member.playerPosition
	}
	return command, true, nil
}

func playerAICastDelay(definition sim.AbilityDefinition) time.Duration {
	delay := max(playerAIInterval, definition.ReleaseDelay, definition.HitDelay)
	for _, hitDelay := range definition.HitDelays {
		delay = max(delay, hitDelay)
	}
	return delay + 50*time.Millisecond
}

func playerAIAbilityIntervalFor(member gameplayPeerSession) time.Duration {
	if member.binding.IsServerBot && member.binding.Mode == game.ModeChain && playerAIPowerRatio(member) >= 0.80 {
		return playerAIAbilityInterval / 2
	}
	return playerAIAbilityInterval
}

func playerAISurvivalSwap(member gameplayPeerSession, now time.Time) (int, float32) {
	if member.squad == nil || member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
		return -1, 0
	}
	currentMaximum := member.characterHitPointMaximum(member.deployedCreatureIndex)
	if currentMaximum <= 0 {
		return -1, 0
	}
	currentRatio := member.deployedHitPoint() / currentMaximum
	recentLoss := member.playerAI.survival.recentLoss(now)
	swapChance := float32(0)
	if currentRatio <= playerAISwapHealthRatio {
		swapChance = 0.20
		if currentRatio <= playerAIFleeHealthRatio {
			swapChance += 0.20
		}
		swapChance = min(float32(0.95), swapChance+recentLoss)
	} else if currentRatio <= 0.70 && recentLoss >= 0.25 {
		// Losing a quarter of maximum health in four seconds suggests a bad
		// matchup even before the ordinary low-health threshold is reached.
		swapChance = min(float32(0.85), 0.20+recentLoss)
	}
	if swapChance == 0 {
		return -1, 0
	}
	state := member.squad.State()
	bestIndex := -1
	bestRatio := currentRatio
	for characterIndex, character := range state.Characters {
		if uint32(characterIndex) == member.deployedCreatureIndex || !character.IsAvailable || character.HitPoints <= 0 {
			continue
		}
		maximum := member.characterHitPointMaximum(uint32(characterIndex))
		if maximum <= 0 {
			continue
		}
		healthRatio := character.HitPoints / maximum
		if healthRatio >= currentRatio+playerAISwapAdvantage && healthRatio > bestRatio {
			bestIndex = characterIndex
			bestRatio = healthRatio
		}
	}
	return bestIndex, swapChance
}

func playerAIFlee(source, target raknet.Vector3, distance float32) raknet.Vector3 {
	deltaX := source.X - target.X
	deltaY := source.Y - target.Y
	deltaZ := source.Z - target.Z
	lengthSquared := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
	length := float32(math.Sqrt(float64(lengthSquared)))
	if length <= 0.01 {
		deltaX = 1
		deltaY = 0
		deltaZ = 0
	} else {
		deltaX, deltaY, deltaZ = deltaX/length, deltaY/length, deltaZ/length
	}
	return raknet.Vector3{X: source.X + deltaX*distance,
		Y: source.Y + deltaY*distance, Z: source.Z + deltaZ*distance}
}

func playerAIMove(member *gameplayPeerSession, command raknet.ActionCommandData,
	goal raknet.Vector3,
) (raknet.ActionCommandData, bool, error) {
	isMoving := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(goal)) > 0.5
	if !isMoving && !member.playerAI.isMoving {
		return command, false, nil
	}
	member.playerAI.isMoving = isMoving
	flags := uint32(0x20)
	if isMoving {
		flags = 0x01
	}
	command.Common.Type = raknet.ActionMovement
	command.Movement = &raknet.ActionMovementData{GoalPosition: goal, GoalFlags: flags}
	return command, true, nil
}

func marshalPlayerAILocomotion(member gameplayPeerSession, isMoving bool) ([][]byte, error) {
	goal := member.playerMovementGoal
	flags := uint32(0x01)
	if !isMoving {
		flags, goal = 0x20, member.playerPosition
	}
	targetID := uint32(0)
	stopDistance := float32(0.1)
	packet, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID: member.deployedObjectID, Locomotion: raknet.LocomotionReflection{
			GoalFlags: &flags, GoalPosition: &goal, PartialGoalPosition: &goal,
			TargetObjectID: &targetID, DesiredStopDistance: &stopDistance,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aiMoveMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}
