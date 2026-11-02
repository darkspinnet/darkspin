package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

func (e *arenaRideRun) breakRootLocked() {
	source, isCurrent := e.currentLocked()
	if !isCurrent {
		return
	}
	for _, status := range source.arenaHitStatuses {
		if status.kind == arenaChargeRoot && status.matches(source) {
			status.retire(&source)
		}
	}
	if source.enemyRootTargetObjectID == e.objectID {
		source.enemyRootTargetObjectID = 0
		source.enemyRootExpiresAt = time.Time{}
		packets, err := source.refreshEnemyImmobilizedAttributes(e.runtime.now())
		if err == nil {
			e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
		} else if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Ride the Lightning root break presentation failed owner=%d: %v", e.objectID, err)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = source
}

func (e campaignAbilityCommandRuntime) handleArenaRide(
	packet raknet.Packet, command raknet.ActionCommandData, member gameplayPeerSession,
	abilityID uint32, definition sim.AbilityDefinition,
) ([][]byte, error) {
	if command.Ability == nil || command.Common.Type != raknet.ActionUseCharacterAbility ||
		command.Ability.Index != 2 || definition.Name != "LightningRogueActive" ||
		definition.Kind != sim.AbilityKindTeleportStrike || abilityID == 0 ||
		packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return e.rejectArenaCharacter(command, "Ride the Lightning unavailable")
	}
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	startedAt := e.now()
	source, isFound := e.registry.sessions[sessionKey]
	match := e.registry.arenaMatches[member.binding.GameID]
	isReady := isFound && source.generation == member.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == member.binding.GameID && source.isArenaCombatActive && source.stage.IsDungeon() &&
		match != nil && match.isCombatActive && match.navigation != nil &&
		source.deployedObjectID == command.Common.ObjectID && source.deployedHitPoint() > 0 &&
		source.isAbilityReleaseReady(startedAt)
	if !isReady {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Ride the Lightning source unavailable")
	}
	if source.isEnemyStunActive(startedAt) || source.isEnemySilenceActive(startedAt) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Ride the Lightning source controlled")
	}
	err := source.advancePlayerPosition(startedAt, command.Common.Position)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRidePosition: %w", err)
	}
	creature := e.registry.projectPassiveCreature(source, source.deployedCreatureIndex, startedAt)
	strikeRange := source.deployedCampaignActorFootprintRadius() + zonenavigation.ProjectionDistance
	targets := e.registry.abilityTargetsLocked(e, source, definition, packet.SourceTime).(*arenaAbilityTargets)
	target, isTargetFound := targets.NPC(command.Ability.TargetID)
	if isTargetFound {
		strikeRange += target.Plan.ActorFootprintRadius()
	}
	requested := command.Ability.CursorPosition
	if !isReportedZonePosition(requested) {
		requested = command.Ability.TargetPosition
	}
	if isTargetFound {
		requested = raknet.Vector3(target.Plan.Position)
	}
	if !isReportedZonePosition(requested) || !isFiniteZonePosition(requested) {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Ride the Lightning destination unavailable")
	}
	var landing game.Vec3
	var isLandingFound bool
	if isTargetFound {
		landing, isLandingFound, err = zonenavigation.MeleeTeleportDestination(match.navigation,
			game.Vec3(source.playerPosition), target.Plan.Position,
			source.deployedCampaignNavigationRadius(), strikeRange)
	} else {
		landing, isLandingFound, err = zonenavigation.ProjectPosition(match.navigation,
			game.Vec3(requested), source.deployedCampaignNavigationRadius())
	}
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideLanding: %w", err)
	}
	if !isLandingFound {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Ride the Lightning landing unavailable")
	}
	destination := raknet.Vector3(landing)
	cooldown, err := zoneability.ProjectCooldown(creature, definition)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideCooldown: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(definition.ManaCost,
		creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
		source.isOverdriveActiveAt(startedAt))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideMana: %w", err)
	}
	if source.deployedManaPoint() < manaCost {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Ride the Lightning power unavailable")
	}
	plan := zoneability.BasicPlan{}
	if isTargetFound && target.Plan.Position.Sub(landing).Length() <= strikeRange {
		plan, err = zoneability.PlanProjected(targets, command.Common.ObjectID,
			command.Ability.TargetID, landing, creature, definition, strikeRange)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaRidePlan: %w", err)
		}
	}
	remainingMana := source.deployedManaPoint() - manaCost
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: abilityID, AbilityIndex: command.Ability.Index,
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime + uint64(definition.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    packet.SourceTime + uint64(definition.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(command.Common.Unknown[0], abilityID,
		command.Ability.Index, packet.SourceTime, definition.HitDelay, definition.ReleaseDelay)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideRelease: %w", err)
	}
	teleportPackets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: command.Common.ObjectID, Position: landing,
		Orientation: game.Quaternion(command.Common.Orientation),
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideTeleportPackets: %w", err)
	}
	arrivalAnimation, err := abilityraknet.Animation(command.Common.ObjectID,
		definition.OutAnimationName, packet.SourceTime+uint64(definition.HitDelay/time.Millisecond))
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideAnimation: %w", err)
	}
	startPackets, err := abilityraknet.StartSpendPresentation(command.Common.ObjectID,
		abilityID, definition.AnimationName, cooldown, packet.SourceTime, remainingMana)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideStart: %w", err)
	}
	world, err := e.registry.arenaAbilityWorldLocked(source.binding.GameID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideWorld: %w", err)
	}
	if world.rides[command.Common.ObjectID] != nil {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Ride the Lightning already active")
	}
	cooldownReservation, isReserved := source.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(abilityID), startedAt, cooldown)
	if !isReserved {
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Ride the Lightning cooldown unavailable")
	}
	releaseReservation, isReserved := source.abilityReleaseSession().Reserve(startedAt, definition.ReleaseDelay)
	if !isReserved {
		source.abilityCooldownSession().Rollback(cooldownReservation)
		e.registry.mutex.Unlock()
		return e.rejectArenaCharacter(command, "Ride the Lightning release unavailable")
	}
	previousMotion := source.playerMotionSnapshot()
	previousMana := source.deployedManaPoint()
	err = source.stopPlayerMovement(startedAt)
	if err == nil {
		err = source.setDeployedManaPoints(remainingMana)
	}
	if err != nil {
		source.restorePlayerMotion(previousMotion, source.playerMotionRevision())
		source.abilityCooldownSession().Rollback(cooldownReservation)
		source.abilityReleaseSession().Rollback(releaseReservation)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideCommit: %w", err)
	}
	run := &arenaRideRun{runtime: e, packet: packet, world: world,
		sessionKey: sessionKey, gameID: source.binding.GameID, generation: source.generation,
		objectID: command.Common.ObjectID, abilityID: abilityID,
		destination: destination, strikeRange: strikeRange, creature: creature, definition: definition,
		plan: plan, arrivalPackets: append(teleportPackets, arrivalAnimation),
		releasePacket: releasePacket, startedAt: startedAt}
	if world.rides == nil {
		world.rides = make(map[uint32]*arenaRideRun)
	}
	world.rides[run.objectID] = run
	e.registry.sessions[sessionKey] = source
	e.registry.mutex.Unlock()
	producers := []raknet.ScheduledPacketProducer{
		{Delay: definition.HitDelay, Produce: run.arrival},
		{Delay: definition.ReleaseDelay, Produce: run.release},
	}
	producers = e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, run.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("Ride the Lightning cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		e.registry.mutex.Lock()
		latest, isCurrent := run.currentLocked()
		if isCurrent {
			delete(world.rides, run.objectID)
			latest.restorePlayerMotion(previousMotion, latest.playerMotionRevision())
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			manaErr := latest.setDeployedManaPoints(previousMana)
			if manaErr != nil && e.logger != nil {
				e.logger.Printf("Arena Ride the Lightning mana rollback failed owner=%d: %v", run.objectID, manaErr)
			}
			e.registry.sessions[sessionKey] = latest
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideSchedule: %w", err)
	}
	e.registry.mutex.Lock()
	_, isCurrent := run.currentLocked()
	if isCurrent {
		run.cancel = cancel
		run.breakRootLocked()
	} else {
		cancel()
	}
	e.registry.mutex.Unlock()
	response := append([][]byte{ackPacket}, startPackets...)
	return response, nil
}
