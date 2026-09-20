package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const playerAIInterval = 250 * time.Millisecond

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
	moveUntil        time.Time
	combatGoal       raknet.Vector3
	isStrafeLeft     bool
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
	member.playerAI = playerAIState{isEnabled: isEnabled, nextAbilityAt: e.now().Add(8 * time.Second)}
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
	return append(packets, movementPackets...), nil
}

func (e gameplayPendingRuntime) planPlayerAILocked(
	member *gameplayPeerSession, now time.Time,
) (raknet.ActionCommandData, bool, error) {
	command := raknet.ActionCommandData{Common: raknet.ActionCommon{
		ObjectID: member.deployedObjectID, Orientation: raknet.Quaternion{W: 1},
	}}
	if member.binding.Mode == game.ModeArena && !member.isArenaCombatActive {
		return command, false, nil
	}
	err := member.advancePlayerPosition(now, raknet.Vector3{})
	if err != nil {
		return command, false, fmt.Errorf("aiPosition: %w", err)
	}
	command.Common.Position = member.playerPosition
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
	ally, isAllyFound := e.playerAIAllyLocked(*member)
	creature := member.binding.Creatures[member.deployedCreatureIndex]
	definition, isBasicFound := e.action.ability.program.PlayerBasicAbility[creature.Noun]
	isRanged := isBasicFound && definition.Kind != sim.AbilityKindMelee && definition.Range > 5
	target, isTargetFound := e.playerAITargetLocked(*member, ally, isAllyFound, now)
	distance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(target.position))
	attackRange := heroAbilityAdmissionRange(creature, definition)
	attackRange += member.deployedCampaignFootprintRadius() + target.radius
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
			return playerAIMove(member, command, goal)
		}
		if !isAttackReady && now.Before(member.playerAI.moveUntil) {
			return playerAIMove(member, command, member.playerAI.combatGoal)
		}
	}
	if !isTargetFound {
		if member.isEnemyRootActive(now) {
			return command, false, nil
		}
		goal := member.playerPosition
		if isAllyFound {
			goal = playerFollowGoal(member.playerPosition, ally.playerPosition)
		}
		return playerAIMove(member, command, goal)
	}
	if !isBasicFound || definition.Range <= 0 {
		return command, false, nil
	}
	if distance > max(float32(1), attackRange*0.9) {
		if member.isEnemyRootActive(now) {
			return command, false, nil
		}
		goal := playerAIApproach(member.playerPosition, target.position, max(float32(1), attackRange*0.7))
		return playerAIMove(member, command, goal)
	}
	if member.playerAI.isMoving && (!isRanged || !isAttackReady) {
		return playerAIMove(member, command, member.playerPosition)
	}
	if isRanged && !isAttackReady && !member.isEnemyRootActive(now) && !now.Before(member.playerAI.nextRepositionAt) {
		member.playerAI.isStrafeLeft = !member.playerAI.isStrafeLeft
		goal := playerAIStrafeGoal(member.playerPosition, target.position, member.playerAI.isStrafeLeft)
		member.playerAI.combatGoal = goal
		member.playerAI.moveUntil = now.Add(500 * time.Millisecond)
		member.playerAI.nextRepositionAt = now.Add(2500 * time.Millisecond)
		return playerAIMove(member, command, goal)
	}
	if now.Before(member.playerAI.nextAttackAt) ||
		(member.basicSequence != nil && !member.basicSequence.IsReady(now)) {
		return command, false, nil
	}
	abilityIndex := uint32(0)
	if !now.Before(member.playerAI.nextAbilityAt) {
		member.playerAI.nextAbilityAt = now.Add(8 * time.Second)
		abilityCount := uint32(2)
		if member.binding.Mode == game.ModeChain {
			abilityCount = 3
		}
		member.playerAI.abilityIndex = (member.playerAI.abilityIndex + 1) % abilityCount
		candidateIndex := uint32(2) + member.playerAI.abilityIndex
		active, abilityID, isActive, isFound := e.action.ability.arenaCharacterAbility(creature, candidateIndex, 1)
		if candidateIndex == 4 {
			candidateIndex = 6 + member.deployedCreatureIndex
			ability, isAbilityFound := e.action.ability.program.HeroAbility(creature.Noun, zonecontent.HeroAbilitySpecialOne)
			active, abilityID = projectHeroAbilityRank(ability.Definition, 1), ability.ID
			isFound, isActive = isAbilityFound && ability.ID != 0 && active.Name != "", true
		}
		isSelfModifier := member.binding.Mode == game.ModeChain && active.Kind == sim.AbilityKindModifier
		isOffensive := active.MaximumDamage > 0 || active.MinimumDamagePerTick > 0 || active.DamageCoefficient > 0
		if isFound && isActive && (isOffensive || isSelfModifier) &&
			(isSelfModifier || distance <= heroAbilityAdmissionRange(creature, active)) &&
			member.abilityCooldownSession().IsReady(zoneability.HeroAbilityCooldown(abilityID), now) {
			manaCost, manaErr := game.ResolveAbilityManaCost(active.ManaCost,
				creature.DamageProfile.PrimaryAttribute, active.ManaCoefficient, member.isOverdriveActiveAt(now))
			if manaErr == nil && member.deployedManaPoint() >= manaCost {
				abilityIndex, definition = candidateIndex, active
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
