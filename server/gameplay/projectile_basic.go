package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (r campaignAbilityCommandRuntime) handleProjectileBasic(
	request campaignCharacterAbilityRequest,
	peerSession gameplayPeerSession, creature game.GameplayCreature,
	definition sim.AbilityDefinition, plan zoneability.BasicPlan,
	targetObjectID uint32, maximumRange float32,
	activeAbilityID uint32, isElectronSphereRequest bool,
	isBasicHeldRepeat bool, isBasicHeldInput bool,
	sessionKey string, abilityStartTime time.Time,
	directAggro campaignDirectAggroPublication,
) ([][]byte, error) {
	var err error
	packet := request.packet
	command := request.command
	previousProjectileObjectID := peerSession.nextProjectileObjectID
	cooldownKey := zoneability.CooldownProjectileBasic
	isActiveRequest := activeAbilityID != 0
	if isActiveRequest {
		cooldownKey = zoneability.HeroAbilityCooldown(activeAbilityID)
	}
	if !peerSession.abilityCooldownSession().IsReady(cooldownKey, abilityStartTime) ||
		!peerSession.isAbilityReleaseReady(abilityStartTime) {
		r.registry.mutex.Unlock()
		return request.reject("cooldown or release unavailable")
	}
	projectileDefinition := plan.Definition
	previousBasicSequence := peerSession.basicSequenceSession().Snapshot()
	selection, selectionErr := zoneability.SelectActivation(projectileDefinition)
	if !isActiveRequest {
		if !isBasicHeldRepeat {
			peerSession.basicSequenceSession().SetHeld(isBasicHeldInput)
		}
		selection, selectionErr = peerSession.basicSequenceSession().Accept(
			abilityStartTime, projectileDefinition,
		)
	}
	if selectionErr != nil {
		if !isActiveRequest {
			peerSession.basicSequenceSession().Restore(
				previousBasicSequence,
				peerSession.basicSequenceSession().Revision(),
			)
		}
		r.registry.mutex.Unlock()
		return request.reject(selectionErr.Error())
	}
	basicSequenceRevision := peerSession.basicSequenceSession().Revision()
	sequenceRollback := campaignBasicSequenceRollback{
		sequence: peerSession.basicSequenceSession(), snapshot: previousBasicSequence,
		revision: basicSequenceRevision, isArmed: !isActiveRequest,
	}
	defer sequenceRollback.restore()
	projectileDefinition.AnimationName = selection.AnimationName
	projectileDefinition.HitDelay = selection.HitDelay
	projectileDefinition.ReleaseDelay = selection.ReleaseDelay
	projectileEffectName := ""
	if projectileDefinition.Name == "SoulRavagerBasic" {
		projectileEffectName = peerSession.soulRavagerBasicProjectileEffect()
	}
	remainingManaPoint := peerSession.deployedManaPoint()
	if isActiveRequest {
		manaCost, manaErr := game.ResolveAbilityManaCost(
			projectileDefinition.ManaCost, creature.DamageProfile.PrimaryAttribute,
			projectileDefinition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignElectronManaProjection: %w", manaErr)
		}
		if remainingManaPoint < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("power unavailable")
		}
		remainingManaPoint -= manaCost
	}
	projectileDefinition.Speed, err = zoneability.ProjectProjectileSpeed(
		creature, projectileDefinition,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileSpeed: %w", err)
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(targetObjectID)
	targetPosition := command.Ability.TargetPosition
	if !isReportedZonePosition(targetPosition) {
		targetPosition = command.Ability.CursorPosition
	}
	reportedTargetPosition := targetPosition
	isClientTargetReported := isReportedZonePosition(reportedTargetPosition)
	if isEnemyFound && !isClientTargetReported {
		targetPosition = raknet.Vector3{
			X: enemy.Plan.Position.X, Y: enemy.Plan.Position.Y, Z: enemy.Plan.Position.Z,
		}
	}
	if !isEnemyFound && (!isReportedZonePosition(targetPosition) ||
		!isFiniteZonePosition(targetPosition)) {
		r.registry.mutex.Unlock()
		return request.reject("cursor position unavailable")
	}
	actorPosition := peerSession.playerPosition
	actorFootprint := peerSession.deployedCampaignFootprintRadius()
	actorHeight := actorFootprint
	actorPhysics, isActorPhysicsFound :=
		r.program.NounPhysicsByID[creature.Noun]
	if isActorPhysicsFound {
		actorHeight = max(
			actorHeight,
			(actorPhysics.BoundMinimum.Z+actorPhysics.BoundMaximum.Z)*0.5,
		)
	}
	actorPosition.Z += max(float32(0), actorHeight)
	if !isEnemyFound {
		targetPosition.Z = actorPosition.Z
	}
	projectileGeometry := zonenpc.ProjectileGeometry{}
	isTargetGeometryFallback := false
	if isEnemyFound {
		projectileGeometry, isTargetGeometryFallback, err =
			campaignTargetProjectileGeometry(r.program, enemy)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignProjectileGeometry: %w", err)
		}
		// The command's target point is an observation. Aim from the same
		// authoritative geometry that collision will query during flight.
		targetPosition = raknet.Vector3(campaignProjectileAimPosition(
			enemy.Plan.Position, enemy.Plan.NPCProfile.FootprintRadius,
			projectileGeometry,
		))
	}
	projectileOffsets := selection.ProjectileOffsets
	if len(projectileOffsets) == 0 {
		projectileOffsets = []sim.Position{{}}
	}
	projectileAngles := selection.ProjectileAngles
	if len(projectileAngles) != len(projectileOffsets) {
		projectileAngles = make([]float32, len(projectileOffsets))
	}
	firstProjectileObjectID, err := peerSession.reserveCampaignProjectileIDs(
		uint32(len(projectileOffsets)), 1000,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileObjectID: %w", err)
	}
	facing := geometryraknet.Direction(actorPosition, targetPosition)
	runTargetObjectID := targetObjectID
	targetHitPoint := float32(0)
	if runTargetObjectID == 0 {
		runTargetObjectID = command.Common.ObjectID
	} else {
		targetHitPoint = enemy.HitPoint
	}
	projectileObjectIDs := make([]uint32, 0, len(projectileOffsets))
	projectileRuns := make([]*abilityraknet.ProjectileRun, 0, len(projectileOffsets))
	projectileSchedules := make([]campaignProjectileSchedule, 0, len(projectileOffsets))
	immediatePackets := make([][]byte, 0, len(projectileOffsets)*2)
	isMissileTempestHoming := isEnemyFound && peerSession.isMissileTempestHoming(
		peerSession.deployedCreatureIndex, abilityStartTime,
	)
	missileTempestHomingDelay := time.Duration(0)
	if isMissileTempestHoming {
		missileTempestHomingDelay = time.Millisecond
	}
	for projectileIndex, projectileOffset := range projectileOffsets {
		projectileObjectID := firstProjectileObjectID + uint32(projectileIndex)
		projectileSource := campaignHeroProjectileSource(
			actorPosition, facing, projectileOffset,
		)
		projectileTarget, projectileFacing := campaignProjectileSpreadTarget(
			projectileSource, targetPosition, projectileAngles[projectileIndex],
		)
		launchPosition := campaignProjectileLaunchPosition(
			game.Vec3(projectileSource), game.Vec3(projectileTarget), actorFootprint,
		)
		travelDistance := zoneability.Distance(
			launchPosition, game.Vec3(projectileTarget),
		)
		var flight *sim.ProjectileFlight
		if !isElectronSphereRequest && !isMissileTempestHoming {
			travelDistance = projectileDefinition.Distance
			flight, err = sim.NewProjectileFlight(sim.ProjectileFlightInput{
				Position: sim.Position(launchPosition),
				Direction: sim.Position(geometryraknet.Direction(
					raknet.Vector3(launchPosition), projectileTarget,
				)),
				HalfExtent:      r.program.ProjectileHalfExtent,
				Speed:           projectileDefinition.Speed,
				Acceleration:    projectileDefinition.Acceleration,
				MaximumDistance: travelDistance,
			})
			if err != nil {
				stopProjectileRuns(projectileRuns)
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("projectileFlight[%d]: %w", projectileIndex, err)
			}
		}
		collisionDelay, travelErr := zoneability.ProjectileTravelDuration(
			travelDistance, projectileDefinition.Speed,
			projectileDefinition.Acceleration,
		)
		if travelErr != nil {
			stopProjectileRuns(projectileRuns)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignProjectileTravel[%d]: %w", projectileIndex, travelErr)
		}
		collisionDelay = max(collisionDelay, abilityraknet.ProjectileCollisionTick)
		if flight != nil {
			collisionDelay = max(flight.Duration(), abilityraknet.ProjectileCollisionTick)
		}
		impactDeadline := projectileDefinition.HitDelay + collisionDelay
		projectilePlan := plan
		if flight == nil && projectileDefinition.DamagePerSpeedUnit > 0 {
			impactSpeed, speedErr := zoneability.ProjectileSpeedAfter(
				projectileDefinition.Speed, projectileDefinition.Acceleration,
				collisionDelay,
			)
			if speedErr != nil {
				stopProjectileRuns(projectileRuns)
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignProjectileImpactSpeed[%d]: %w", projectileIndex, speedErr)
			}
			bonusDamage := float32(math.Round(float64(
				impactSpeed * projectileDefinition.DamagePerSpeedUnit,
			)))
			projectilePlan.Damage.Minimum += bonusDamage
			projectilePlan.Damage.Maximum += bonusDamage
		}
		projectileRun, launchPackets, runErr := abilityraknet.NewProjectileRun(
			abilityraknet.ProjectileInput{
				StartedAt: abilityStartTime,
				Ability:   projectileDefinition, ActorObjectID: command.Common.ObjectID,
				TargetObjectID: runTargetObjectID, ProjectileObjectID: projectileObjectID,
				ActorPosition: toSimPosition(projectileSource),
				TargetPosition: sim.Position{
					X: projectileTarget.X, Y: projectileTarget.Y, Z: projectileTarget.Z,
				},
				ActorFacing: sim.Position{
					X: projectileFacing.X, Y: projectileFacing.Y, Z: projectileFacing.Z,
				},
				ImpactPosition: sim.Position{
					X: projectileTarget.X, Y: projectileTarget.Y, Z: projectileTarget.Z,
				},
				FootprintRadius: actorFootprint,
				CollisionDelay:  collisionDelay, IsDirectHit: isEnemyFound,
				HomingDelay: missileTempestHomingDelay, IsHoming: isMissileTempestHoming,
				IsCollisionExternallyDriven: true, Damage: projectilePlan.Damage.Maximum,
				IsCollisionSampled: flight != nil,
				TargetHitPoint:     targetHitPoint, ActorTeam: 1, SourceTime: packet.SourceTime,
				IsActivationSuppressed: projectileIndex > 0,
				IsCooldownSuppressed:   projectileIndex > 0,
				IsReleaseSuppressed:    projectileIndex > 0,
				ProjectileEffectName:   projectileEffectName,
			},
		)
		if runErr != nil {
			stopProjectileRuns(projectileRuns)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("projectileRun[%d]: %w", projectileIndex, runErr)
		}
		projectileRun.AddDeadline(impactDeadline)
		if flight != nil {
			// One shared schedule retains cast release even after an early hit.
			for deadline := projectileDefinition.HitDelay + abilityraknet.ProjectileCollisionTick; deadline < impactDeadline; deadline += abilityraknet.ProjectileCollisionTick {
				projectileRun.AddDeadline(deadline)
			}
		}
		projectileObjectIDs = append(projectileObjectIDs, projectileObjectID)
		projectileRuns = append(projectileRuns, projectileRun)
		immediatePackets = append(immediatePackets, launchPackets...)
		projectileSchedules = append(projectileSchedules, campaignProjectileSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: peerSession.generation, sourceObjectID: command.Common.ObjectID,
			targetObjectID: targetObjectID, projectileObjectID: projectileObjectID,
			impactDeadline: impactDeadline, maximumRange: maximumRange,
			sourcePosition: game.Vec3(projectileSource), targetPosition: projectileTarget,
			travelDistance: travelDistance, actorFootprint: actorFootprint,
			geometry: projectileGeometry, facing: projectileFacing,
			creature: creature, definition: definition,
			projectile: projectileDefinition, plan: projectilePlan,
			binding: peerSession.binding, run: projectileRun,
			isElectronSphere: isElectronSphereRequest,
			flight:           flight, startedAt: abilityStartTime,
		})
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err != nil {
		stopProjectileRuns(projectileRuns)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileStop: %w", err)
	}
	movementPackets, marshalErr := marshalZonePlayerAttackPose(
		command.Common.ObjectID, peerSession.playerPosition, facing, targetPosition,
		targetObjectID,
	)
	if marshalErr != nil {
		stopProjectileRuns(projectileRuns)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileStopMarshal: %w", marshalErr)
	}
	ackPacket, marshalErr := abilityraknet.Acknowledge(
		abilityraknet.AcknowledgeRequest{
			SyncStamp:    command.Common.Unknown[0],
			ResponseType: raknet.ActionResponseAccepted,
			ObjectID:     plan.AbilityID, AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds: packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime +
				uint64(projectileDefinition.HitDelay/time.Millisecond),
			SourceEndMilliseconds: packet.SourceTime +
				uint64(projectileDefinition.ReleaseDelay/time.Millisecond),
		},
	)
	if marshalErr != nil {
		stopProjectileRuns(projectileRuns)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileAck: %w", marshalErr)
	}
	releaseResponsePacket, marshalErr := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], plan.AbilityID, command.Ability.Index,
		packet.SourceTime, projectileDefinition.HitDelay,
		projectileDefinition.ReleaseDelay,
	)
	if marshalErr != nil {
		stopProjectileRuns(projectileRuns)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileReleaseResponse: %w", marshalErr)
	}
	cooldownPacket, marshalErr := abilityraknet.Cooldown(
		abilityraknet.CooldownRequest{
			ObjectID: command.Common.ObjectID, AbilityID: plan.AbilityID,
			Duration: projectileDefinition.Cooldown, StartTime: packet.SourceTime,
		},
	)
	if marshalErr != nil {
		stopProjectileRuns(projectileRuns)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileCooldown: %w", marshalErr)
	}
	var manaPacket []byte
	if isActiveRequest {
		manaPacket, marshalErr = abilityraknet.Mana(
			command.Common.ObjectID, remainingManaPoint,
		)
		if marshalErr != nil {
			stopProjectileRuns(projectileRuns)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignElectronMana: %w", marshalErr)
		}
	}
	previousManaPoint := peerSession.deployedManaPoint()
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			cooldownKey, abilityStartTime, projectileDefinition.Cooldown,
		)
	if !isCooldownReserved {
		stopProjectileRuns(projectileRuns)
		r.registry.mutex.Unlock()
		return request.reject("cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projectileDefinition.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		stopProjectileRuns(projectileRuns)
		r.registry.mutex.Unlock()
		return request.reject("release unavailable")
	}
	heldGeneration := peerSession.basicSequenceSession().HeldGeneration()
	generation := peerSession.generation
	binding := peerSession.binding
	if peerSession.sageAttacks == nil {
		peerSession.sageAttacks = make(map[uint32]*abilityraknet.ProjectileRun)
	}
	for index, projectileObjectID := range projectileObjectIDs {
		peerSession.sageAttacks[projectileObjectID] = projectileRuns[index]
	}
	peerSession.retainAttackPose(
		facing, targetPosition, targetObjectID,
		abilityStartTime.Add(projectileDefinition.ReleaseDelay),
	)
	if isActiveRequest {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		stopProjectileRuns(projectileRuns)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignElectronManaCommit: %w", err)
	}
	firstSecondaryDeadline := time.Duration(0)
	if isElectronSphereRequest {
		firstDelay, secondaryErr :=
			zoneability.ElectronSphereSecondaryDelay(
				peerSession.zone.Population().Random(),
				projectileDefinition.LightningSecondary,
			)
		if secondaryErr != nil {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			_ = peerSession.setDeployedManaPoints(previousManaPoint)
			stopProjectileRuns(projectileRuns)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignElectronSecondaryFirstDelay: %w", secondaryErr,
			)
		}
		firstSecondaryDeadline = projectileDefinition.HitDelay + firstDelay
	}
	repeatDelay := max(projectileDefinition.Cooldown, projectileDefinition.ReleaseDelay)
	if !isActiveRequest {
		repeatDelay = peerSession.basicSequenceSession().CooldownEnd().Sub(abilityStartTime)
	}
	r.registry.sessions[sessionKey] = peerSession
	sequenceRollback.isArmed = false
	r.registry.mutex.Unlock()

	producers := make([]raknet.ScheduledPacketProducer, 0, len(projectileRuns)*3)
	for _, projectileSchedule := range projectileSchedules {
		for _, deadline := range projectileSchedule.run.Deadlines() {
			producers = append(
				producers, projectileSchedule.producer(deadline),
			)
		}
	}
	releaseStep := campaignProjectileReleaseStep{
		registry: r.registry, sessionKey: sessionKey,
		generation: generation, sourceObjectID: command.Common.ObjectID,
		timestamp: packet.SourceTime +
			uint64(projectileDefinition.ReleaseDelay/time.Millisecond),
		packet: releaseResponsePacket,
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay:   projectileDefinition.ReleaseDelay,
		Produce: releaseStep.produce,
	})
	if isElectronSphereRequest {
		projectileSchedule := projectileSchedules[0]
		secondarySchedule := campaignElectronSecondarySchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceObjectID: command.Common.ObjectID,
			projectileObjectID: projectileSchedule.projectileObjectID,
			impactDeadline:     projectileSchedule.impactDeadline,
			travelDistance:     projectileSchedule.travelDistance,
			startPosition:      raknet.Vector3(projectileSchedule.sourcePosition), facing: facing,
			creature: creature, definition: projectileDefinition,
			binding: binding, run: projectileSchedule.run,
		}
		if firstSecondaryDeadline < projectileSchedule.impactDeadline {
			producers = append(
				producers,
				secondarySchedule.producer(firstSecondaryDeadline),
			)
		}
	}
	repeatPayload := append([]byte(nil), packet.Payload...)
	repeatPacket := packet
	repeatPacket.Payload = repeatPayload
	repeatPacket.SourceTime += uint64(repeatDelay / time.Millisecond)
	repeatStep := campaignHeldRepeatStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		heldGeneration: heldGeneration, packet: repeatPacket,
		command: command,
	}
	if isBasicHeldInput {
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: repeatDelay, Produce: repeatStep.produce,
		})
	}
	sortScheduledPacketProducersByDelay(producers)
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	scheduleFailure := campaignProjectileScheduleFailure{
		runtime: r, sessionKey: sessionKey, generation: generation,
		projectileObjectIDs:        projectileObjectIDs,
		previousProjectileObjectID: previousProjectileObjectID,
		creatureIndex:              peerSession.deployedCreatureIndex,
		previousManaPoint:          previousManaPoint, runs: projectileRuns,
		previousSequence:    previousBasicSequence,
		sequenceRevision:    basicSequenceRevision,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation,
	}
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(
			producers, scheduleFailure.handle,
		)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		scheduleFailure.handle(err)
		scheduleFailure.rollbackAdmission()
		return nil, fmt.Errorf("campaignProjectileSchedule: %w", err)
	}
	for _, projectileRun := range projectileRuns {
		projectileRun.SetCancel(cancel)
	}
	for _, projectileSchedule := range projectileSchedules {
		r.logger.Printf(
			"RakNet projectile trajectory launched kind=hero-basic projectile=%d source=%d target=%d target_noun=%q target_geometry_fallback=%t ability=%q origin=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) client_target=(%.3f,%.3f,%.3f) client_target_reported=%t client_drift=%.3f travel=%.3f delay_ms=%d facing=(%.3f,%.3f,%.3f)",
			projectileSchedule.projectileObjectID, command.Common.ObjectID,
			targetObjectID, enemy.Plan.NounName, isTargetGeometryFallback,
			projectileDefinition.Name, projectileSchedule.sourcePosition.X,
			projectileSchedule.sourcePosition.Y, projectileSchedule.sourcePosition.Z,
			targetPosition.X, targetPosition.Y, targetPosition.Z,
			reportedTargetPosition.X, reportedTargetPosition.Y,
			reportedTargetPosition.Z, isClientTargetReported,
			zonegeometry.Distance(
				game.Vec3(reportedTargetPosition), game.Vec3(targetPosition),
			), projectileSchedule.travelDistance,
			(projectileSchedule.impactDeadline - projectileDefinition.HitDelay).Milliseconds(),
			facing.X, facing.Y, facing.Z,
		)
	}
	startPackets := append(immediatePackets, cooldownPacket)
	if len(manaPacket) != 0 {
		startPackets = append(startPackets, manaPacket)
	}
	responsePackets := append([][]byte{ackPacket}, movementPackets...)
	responsePackets = append(responsePackets, startPackets...)
	return directAggro.publish(responsePackets)
}
