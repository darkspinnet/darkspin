package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (r campaignNPCActionRuntime) produceZelemShot(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	return r.produceZelemShotWithVolley(
		packet, sessionKey, generation, objectID, timestamp,
		campaignNPCProjectileVolley{},
	)
}

func (r campaignNPCActionRuntime) produceZelemShotWithProfile(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, profile zonenpc.ActionProfile,
) ([][]byte, error) {
	return r.produceZelemShotWithVolley(
		packet, sessionKey, generation, objectID, timestamp,
		campaignNPCProjectileVolley{plan: zonenpc.AttackPlan{Profile: profile}},
	)
}

func (r campaignNPCActionRuntime) scheduleZelemVolleyBoundary(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, volley campaignNPCProjectileVolley,
	profile zonenpc.ActionProfile,
) error {
	elapsed := time.Duration(timestamp-volley.startTimestamp) * time.Millisecond
	nextDelay := max(
		time.Duration(0),
		max(profile.Cooldown, profile.ReleaseDelay)-elapsed,
	)
	schedule := campaignNPCProjectileSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: objectID,
		timestamp: timestamp, nextDelay: nextDelay,
		kind: campaignNPCProjectileZelem, plan: volley.plan,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: nextDelay, Produce: schedule.next,
	}})
	if err != nil {
		return fmt.Errorf("enemyVolleyBoundarySchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("enemy volley boundary cancellation unavailable")
	}
	return nil
}

func (r campaignNPCActionRuntime) produceZelemShotWithVolley(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, volley campaignNPCProjectileVolley,
) ([][]byte, error) {
	if !volley.isActive {
		launchPackets, isLaunchHandled, launchErr := r.produceArcturusLaunch(packet, sessionKey, generation, objectID, timestamp)
		if launchErr != nil {
			return nil, fmt.Errorf("enemyShotArcturus: %w", launchErr)
		}
		if isLaunchHandled {
			return launchPackets, nil
		}
	}
	r.registry.mutex.RLock()
	peekSession, isPeekFound := r.registry.sessions[sessionKey]
	isTurtleActive := false
	isLob := false
	isCryosRezMelee := false
	isHomerMelee := false
	isHybridMelee := false
	isSpecialTwoMelee := false
	isNashiraMelee := false
	if isPeekFound && peekSession.generation == generation &&
		peekSession.zone != nil && peekSession.zone.NPCs() != nil {
		peekNPC, isPeekNPCFound := peekSession.zone.NPCs().NPC(objectID)
		isTurtleActive = isPeekNPCFound && peekNPC.IsTurtleActive
		peekProfile, isPeekProfileFound := zonenpc.ActionProfileForPlan(peekNPC.Plan)
		peekTarget, isPeekTargetFound := peekSession.campaignNPCTarget(
			generation, peekNPC.TargetObjectID,
		)
		if isPeekNPCFound && isPeekTargetFound && isPeekProfileFound &&
			peekProfile.AbilityName == "NocturnaSpecialHomer" {
			selectedProfile, isSelectedFound := campaignNPCActionProfile(
				peekNPC.Plan, peekTarget.Position, peekTarget.FootprintRadius,
			)
			isHomerMelee = isSelectedFound &&
				selectedProfile.AbilityName == "NocturnaSpecialHomerMelee"
		}
		_, isHybridFamily := zonenpc.ZelemBasicHybridMeleeProfile(
			peekNPC.Plan.NounName,
		)
		if isPeekNPCFound && isPeekTargetFound && isHybridFamily {
			selectedProfile, isSelectedFound := campaignNPCActionProfile(
				peekNPC.Plan, peekTarget.Position, peekTarget.FootprintRadius,
			)
			isHybridMelee = isSelectedFound &&
				selectedProfile.AbilityName == "ZelemBasicHybridMelee"
		}
		_, isSpecialTwoFamily := zonenpc.CitadelSpecialTwoMeleeProfile(
			peekNPC.Plan.NounName,
		)
		if isPeekNPCFound && isPeekTargetFound && isSpecialTwoFamily {
			selectedProfile, isSelectedFound := campaignNPCActionProfile(
				peekNPC.Plan, peekTarget.Position, peekTarget.FootprintRadius,
			)
			isSpecialTwoMelee = isSelectedFound &&
				selectedProfile.AbilityName == "CitadelSpecialTwo_Melee"
		}
		_, isNashiraFamily := zonenpc.NashiraSwipeProfile(peekNPC.Plan.NounName)
		if isPeekNPCFound && isPeekTargetFound && isNashiraFamily {
			selectedProfile, isSelectedFound := campaignNPCActionProfile(
				peekNPC.Plan, peekTarget.Position, peekTarget.FootprintRadius,
			)
			isNashiraMelee = isSelectedFound &&
				selectedProfile.AbilityName == "ShadowBossSwipe"
		}
		isLob = isPeekNPCFound && isPeekProfileFound &&
			zonenpc.IsTossActionProfile(peekProfile) && !isNashiraMelee
		if isPeekNPCFound && isPeekProfileFound &&
			peekProfile.AbilityName == "SleepMushroom" {
			peekTarget, isPeekTargetFound := peekSession.campaignNPCTarget(
				generation, peekNPC.TargetObjectID,
			)
			isCryosRezMelee = isPeekTargetFound && (zonegeometry.Distance(
				peekNPC.Plan.Position, peekTarget.Position,
			) <= 10 || timestamp < peekSession.campaignNPCNextSleepMushrooms[objectID])
			isLob = isLob && !isCryosRezMelee
		}
	}
	r.registry.mutex.RUnlock()
	err := r.startCampaignSuppressionAura(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("suppressionAuraStart: %w", err)
	}
	if isTurtleActive {
		return nil, nil
	}
	if isCryosRezMelee {
		return r.produceCryosRezMelee(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if isHomerMelee || isHybridMelee || isSpecialTwoMelee || isNashiraMelee {
		return r.produceEnemyMelee(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if isLob {
		return r.produceEnemyLob(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	r.registry.mutex.RLock()
	buffSession, isBuffSessionFound := r.registry.sessions[sessionKey]
	buffSource, isBuffSourceFound := zonenpc.Snapshot{}, false
	if isBuffSessionFound && buffSession.generation == generation &&
		buffSession.zone != nil && buffSession.zone.NPCs() != nil {
		buffSource, isBuffSourceFound = buffSession.zone.NPCs().NPC(objectID)
	}
	r.registry.mutex.RUnlock()
	if isBuffSourceFound && !volley.isActive {
		clonePackets, isCloneSelected, cloneErr := r.produceScaldronBasicDopplerClone(
			packet, sessionKey, generation, buffSource, timestamp,
		)
		if cloneErr != nil {
			return nil, fmt.Errorf("enemyDopplerClone: %w", cloneErr)
		}
		if isCloneSelected {
			return clonePackets, nil
		}
		buffPackets, isBuffSelected, buffErr := r.produceZelemSpecialThreeEnergyBuff(
			packet, sessionKey, generation, buffSource, timestamp,
		)
		if buffErr != nil {
			return nil, fmt.Errorf("enemyEnergyBuff: %w", buffErr)
		}
		if isBuffSelected {
			return buffPackets, nil
		}
	}
	if !volley.isActive {
		sinkholePackets, isSinkholeSelected, sinkholeErr :=
			r.produceScaldronBasicSinkhole(
				packet, sessionKey, generation, objectID, timestamp,
			)
		if sinkholeErr != nil {
			return nil, fmt.Errorf("enemySinkhole: %w", sinkholeErr)
		}
		if isSinkholeSelected {
			return sinkholePackets, nil
		}
	}
	resume := campaignNPCProjectileSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: objectID, timestamp: timestamp,
		kind: campaignNPCProjectileZelem,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyZelemShotStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	healSession, isHealSessionFound := r.registry.sessions[sessionKey]
	source, isSourceFound := zonenpc.Snapshot{}, false
	if isHealSessionFound && healSession.generation == generation &&
		healSession.zone != nil && healSession.zone.NPCs() != nil {
		source, isSourceFound = healSession.zone.NPCs().NPC(objectID)
	}
	r.registry.mutex.RUnlock()
	if isSourceFound && !volley.isActive {
		fleePackets, isFleeSelected, fleeErr := r.produceRayKillerFlee(
			packet, sessionKey, generation, objectID, timestamp,
		)
		if fleeErr != nil {
			return nil, fmt.Errorf("enemyRayKillerFlee: %w", fleeErr)
		}
		if isFleeSelected {
			return fleePackets, nil
		}
		healPackets, isHealSelected, healErr :=
			r.produceVerdanthBasicHealerRootedHeal(
				packet, sessionKey, generation, source, timestamp,
			)
		if healErr != nil {
			return nil, fmt.Errorf("enemyRootedHeal: %w", healErr)
		}
		if isHealSelected {
			return healPackets, nil
		}
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	if isCurrent && volley.isActive {
		isCurrent = peerSession.isCampaignNPCSourceGenerationActive(
			generation, objectID, volley.plan.ActionGeneration,
		)
	}
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound || enemy.IsDefeated {
		r.registry.mutex.Unlock()
		if volley.isActive {
			r.releaseActionGeneration(
				sessionKey, generation, objectID,
				volley.plan.ActionGeneration,
			)
		} else {
			r.releaseAction(sessionKey, generation, objectID)
		}
		return nil, nil
	}
	peerSession, err = r.pursuit.advanceTargetPoseLocked(
		peerSession, enemy.TargetObjectID,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemTargetPose: %w", err)
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		if volley.isActive {
			err = r.scheduleZelemVolleyBoundary(
				packet, sessionKey, generation, objectID, timestamp,
				volley, volley.plan.Profile,
			)
			if err != nil {
				r.releaseActionGeneration(
					sessionKey, generation, objectID,
					volley.plan.ActionGeneration,
				)
				return nil, fmt.Errorf("enemyZelemMissingTarget: %w", err)
			}
			return nil, nil
		}
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if zonenpc.ArcturusRank(enemy.Plan.NounName) != 0 && !volley.isActive {
		profile, isProfileFound = campaignNPCActionProfile(enemy.Plan, target.Position, target.FootprintRadius)
		if isProfileFound && profile.Family == zonenpc.ActionCone {
			r.registry.mutex.Unlock()
			return r.produceEnemyCone(packet, sessionKey, generation, objectID, timestamp)
		}
	}
	if volley.plan.Profile.AbilityName != "" {
		profile = volley.plan.Profile
		isProfileFound = true
	}
	_, isHomerFamily := zonenpc.NocturnaSpecialHomerMeleeProfile(enemy.Plan.NounName)
	if isHomerFamily {
		profile = scaleNocturnaSpecialHomerCooldown(
			profile, uint32(len(peerSession.zone.Snapshot().Members)),
		)
	}
	if !isProfileFound {
		r.registry.mutex.Unlock()
		return nil, errors.New("enemy projectile profile unavailable")
	}
	if !volley.isActive {
		_, slowAttackScale := peerSession.zone.NPCs().SlowProfile(
			objectID, r.now(),
		)
		profile = applyNPCSlowTiming(profile, slowAttackScale)
	}
	if !volley.isActive && profile.Family == zonenpc.ActionZelemRanged &&
		profile.TeleportNormalDistance > 0 {
		// A ranged Zelem can select its authored blink again after a strafe.
		// Route that profile through blink instead of validating it as a shot.
		r.registry.mutex.Unlock()
		return r.produceZelemBlink(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if profile.AbilityName == "CryosElementalSpecialThree" && !volley.isActive {
		targetObjectIDs := make([]uint32, 0)
		targetObjectIDs = append(targetObjectIDs, target.ObjectID)
		for _, candidate := range peerSession.zone.LiveNPCTargets() {
			if candidate.ObjectID == target.ObjectID || candidate.HitPoint <= 0 ||
				zonegeometry.Distance(enemy.Plan.Position, candidate.Position) >
					profile.Range {
				continue
			}
			targetObjectIDs = append(targetObjectIDs, candidate.ObjectID)
		}
		profile.ProjectileShotCount = uint32(len(targetObjectIDs))
		profile.IsProjectileParallelVolley = len(targetObjectIDs) > 1
		volley.targetObjectIDs = targetObjectIDs
	}
	if volley.isActive && len(volley.targetObjectIDs) > volley.shotIndex {
		selected, isSelectedFound := peerSession.campaignNPCTarget(
			generation, volley.targetObjectIDs[volley.shotIndex],
		)
		if !isSelectedFound {
			r.registry.mutex.Unlock()
			err = r.scheduleZelemVolleyBoundary(
				packet, sessionKey, generation, objectID, timestamp,
				volley, profile,
			)
			if err != nil {
				r.releaseActionGeneration(
					sessionKey, generation, objectID,
					volley.plan.ActionGeneration,
				)
				return nil, fmt.Errorf("enemyZelemVolleyTarget: %w", err)
			}
			return nil, nil
		}
		target = selected
	}
	isControlProjectile := profile.AbilityName == "Puller"
	ability, abilityErr := campaignNPCProjectileAbility(profile)
	if abilityErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemAbility: %w", abilityErr)
	}
	plan := zonenpc.AttackPlan{}
	var planErr error
	if volley.isActive {
		if profile.ProjectileShotCount < 2 ||
			(len(volley.targetObjectIDs) == 0 &&
				volley.plan.TargetObjectID != target.ObjectID) {
			r.registry.mutex.Unlock()
			return nil, nil
		}
		volleyRange, rangeErr := zoneaction.NPCStopDistance(
			profile.Range, enemy.Plan.NPCProfile.FootprintRadius,
			target.FootprintRadius,
		)
		isVolleyTargetInRange := rangeErr == nil &&
			zonegeometry.Distance(enemy.Plan.Position, target.Position) < volleyRange
		if !isVolleyTargetInRange {
			r.registry.mutex.Unlock()
			r.logger.Printf(
				"RakNet campaign projectile volley stopped object=%d target=%d shot=%d/%d distance=%.3f range=%.3f reason=live target out of range",
				objectID, target.ObjectID, volley.shotIndex+1,
				profile.ProjectileShotCount,
				zonegeometry.Distance(enemy.Plan.Position, target.Position),
				volleyRange,
			)
			err = r.scheduleZelemVolleyBoundary(
				packet, sessionKey, generation, objectID, timestamp,
				volley, profile,
			)
			if err != nil {
				r.releaseActionGeneration(
					sessionKey, generation, objectID,
					volley.plan.ActionGeneration,
				)
				return nil, fmt.Errorf("enemyZelemVolleyRange: %w", err)
			}
			return nil, nil
		}
		plan = volley.plan
		plan.TargetObjectID = target.ObjectID
		plan.SourcePosition = enemy.Plan.Position
		plan.TargetPosition = target.Position
		plan.Profile = profile
	} else {
		if isControlProjectile {
			plan, planErr = zonenpc.PlanControlWithProfile(
				enemy, target.ObjectID, target.Position, profile,
				target.FootprintRadius,
			)
		} else {
			plan, planErr = zonenpc.PlanAttackWithProfile(
				enemy, target.ObjectID, target.Position, profile,
				target.FootprintRadius,
			)
		}
	}
	if planErr != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil || !action.IsPursuitNeeded {
			r.registry.mutex.Unlock()
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyHybridPursuitMarshal: %w", marshalErr)
		}
		r.registry.mutex.Unlock()
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, resume.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf(
				"RakNet campaign projectile pursuit not scheduled object=%d: %v",
				objectID, scheduleErr,
			)
		}
		return pursuitPackets, nil
	}
	if peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		return nil, errors.New("enemy random unavailable")
	}
	result := zonenpc.AttackResult{}
	if !isControlProjectile {
		result, err = zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			enemy.Plan.NPCProfile.CriticalRating, r.program.Critical,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyZelemCommit: %w", err)
		}
	}
	geometry, geometryErr := r.program.ProjectileGeometry(0)
	if geometryErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemGeometry: %w", geometryErr)
	}
	projectileObjectID, objectIDErr := peerSession.reserveCampaignProjectileIDs(1, 1000)
	if objectIDErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemObjectID: %w", objectIDErr)
	}
	isProjectileVolley := profile.ProjectileShotCount > 1 &&
		(profile.ProjectileShotInterval > 0 || profile.IsProjectileParallelVolley ||
			len(profile.ProjectileShotDeadlines) > 0)
	if isProjectileVolley && !volley.isActive {
		volley = campaignNPCProjectileVolley{
			isActive: true, startTimestamp: timestamp,
			retainedTargetPosition: target.Position,
			targetObjectIDs:        volley.targetObjectIDs,
			plan:                   plan,
		}
	}
	aimTarget := target
	if volley.isActive && !profile.IsProjectileTrackingBetweenShots {
		aimTarget.Position = volley.retainedTargetPosition
	}
	aimGeometry := geometry
	if isCampaignNPCProjectileSampled(profile) {
		var isAimGeometryFallback bool
		aimGeometry, isAimGeometryFallback = r.projectileTargetGeometryLocked(&peerSession, aimTarget, geometry)
		if isAimGeometryFallback {
			r.logger.Printf("RakNet enemy projectile aim geometry fallback source=%d target=%d", objectID, aimTarget.ObjectID)
		}
	}
	sourcePosition, targetPosition := campaignNPCProjectileEndpoints(
		enemy, aimTarget, aimGeometry, r.program.NounPhysics[enemy.Plan.NounName],
	)
	if profile.AbilityName == "SawBladeShot" && !volley.isActive {
		sourcePosition.Z += profile.ProjectileOffset.Z
	}
	if profile.AbilityName == "ZelemBasicHybridProjectile" {
		sourcePosition.Z += profile.ProjectileOffset.Z
		sourcePosition = campaignNPCParallelProjectileSource(
			sourcePosition, targetPosition, profile.ProjectileOffset, 0,
		)
	}
	projectileOffset := profile.ProjectileOffset
	if volley.isActive && volley.shotIndex < len(profile.ProjectileOffsets) {
		projectileOffset = profile.ProjectileOffsets[volley.shotIndex]
	}
	if volley.isActive {
		sourcePosition.Z += projectileOffset.Z
	}
	if volley.isActive && profile.IsProjectileParallelVolley {
		if volley.shotIndex < len(profile.ProjectileOffsets) {
			sourcePosition = campaignNPCProjectileSourceOffset(
				sourcePosition, targetPosition, projectileOffset,
			)
		} else {
			sourcePosition = campaignNPCParallelProjectileSource(
				sourcePosition, targetPosition, projectileOffset,
				volley.shotIndex,
			)
		}
	}
	if volley.isActive && (len(profile.ProjectileShotAngles) > volley.shotIndex ||
		profile.ProjectileSpreadAngle > 0) {
		spread := float64(0)
		if len(profile.ProjectileShotAngles) > volley.shotIndex {
			spread = float64(profile.ProjectileShotAngles[volley.shotIndex]) *
				math.Pi / 180
		} else {
			spread = peerSession.zone.NPCRandom().Float64() *
				float64(profile.ProjectileSpreadAngle) * math.Pi / 180
			if volley.shotIndex%2 == 1 {
				spread = -spread
			}
		}
		deltaX := targetPosition.X - sourcePosition.X
		deltaY := targetPosition.Y - sourcePosition.Y
		cosine := float32(math.Cos(spread))
		sine := float32(math.Sin(spread))
		targetPosition.X = sourcePosition.X + deltaX*cosine - deltaY*sine
		targetPosition.Y = sourcePosition.Y + deltaX*sine + deltaY*cosine
	}
	if profile.IsProjectileLeadingTarget ||
		profile.ProjectileMaximumLeadAngle > 0 {
		maximumLeadAngle := profile.ProjectileMaximumLeadAngle
		if maximumLeadAngle <= 0 {
			maximumLeadAngle = 180
		}
		targetPosition = zonenpc.PredictProjectileTarget(
			sourcePosition, targetPosition, aimTarget.LinearVelocity,
			profile.ProjectileSpeed, maximumLeadAngle,
		)
	}
	if profile.IsProjectilePiercing && profile.AbilityName != "ZelemSpecialThree" &&
		profile.AbilityName != "CryosElementalSpecialThree" {
		targetPosition = campaignNPCProjectileRangeEndpoint(
			sourcePosition, targetPosition, profile.ProjectileDistance,
		)
	}
	isDelayedVolleyShot := volley.isActive && volley.shotIndex > 0 &&
		volley.shotIndex < len(profile.ProjectileShotDeadlines) &&
		profile.ProjectileShotDeadlines[volley.shotIndex] > 0
	if volley.isActive && volley.shotIndex > 0 &&
		(!profile.IsProjectileParallelVolley || isDelayedVolleyShot) {
		ability.HitDelay = 0
		ability.ReleaseDelay = 0
	}
	launchPosition := campaignProjectileLaunchPosition(
		sourcePosition, targetPosition, enemy.Plan.NPCProfile.FootprintRadius,
	)
	distance := zoneability.Distance(launchPosition, targetPosition)
	collisionDelay := max(
		time.Duration(float64(distance)/float64(ability.Speed)*float64(time.Second)),
		abilityraknet.ProjectileCollisionTick,
	)
	impactDeadline := ability.HitDelay + collisionDelay
	facing := geometryraknet.Direction(
		raknet.Vector3{
			X: sourcePosition.X, Y: sourcePosition.Y, Z: sourcePosition.Z,
		},
		raknet.Vector3{
			X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
		},
	)
	isActivationSuppressed := profile.AnimationName == "" ||
		(volley.isActive && volley.shotIndex > 0)
	startedAt := r.now()
	flight, flightErr := newCampaignNPCProjectileFlight(profile, ability, launchPosition,
		sim.Position(facing), geometry.ProjectileHalfExtent, startedAt)
	if flightErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemFlight: %w", flightErr)
	}
	if flight != nil {
		distance = ability.Distance
		collisionDelay = max(flight.collision.Duration(), abilityraknet.ProjectileCollisionTick)
		impactDeadline = ability.HitDelay + collisionDelay
	}
	projectileRun, immediatePackets, err := abilityraknet.NewProjectileRun(abilityraknet.ProjectileInput{
		StartedAt: startedAt, IsCollisionSampled: flight != nil,
		Ability: ability, ActorObjectID: objectID, TargetObjectID: target.ObjectID,
		ProjectileObjectID: projectileObjectID,
		ActorPosition: sim.Position{
			X: sourcePosition.X, Y: sourcePosition.Y, Z: sourcePosition.Z,
		},
		TargetPosition: sim.Position{
			X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
		},
		ActorFacing: sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},

		ImpactPosition: sim.Position{
			X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
		},
		FootprintRadius: enemy.Plan.NPCProfile.FootprintRadius,
		CollisionDelay:  collisionDelay, IsDirectHit: true, IsCollisionExternallyDriven: true,
		IsPiercing:  profile.IsProjectilePiercing,
		HomingDelay: profile.HomingDelay, IsHoming: profile.HomingDelay > 0,
		Damage: result.Damage, IsCritical: result.IsCritical,
		IsControl:      isControlProjectile,
		TargetHitPoint: target.HitPoint, TargetManaPoint: target.ManaPoint,
		ActorTeam: 0, SourceTime: timestamp,
		IsActivationSuppressed: isActivationSuppressed,
		IsTurnSuppressed:       true,
		IsCooldownSuppressed:   volley.isActive && volley.shotIndex > 0,
		IsReleaseSuppressed:    volley.isActive && volley.shotIndex > 0,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemProjectileRun: %w", err)
	}
	// Caster pose uses the root and selected target, never muzzle offsets,
	// spread, leading, or the projectile's maximum-range endpoint.
	if !volley.isActive || volley.shotIndex == 0 || profile.IsProjectileTrackingBetweenShots {
		facingPlan := plan
		facingPlan.SourcePosition = enemy.Plan.Position
		facingPlan.TargetPosition = aimTarget.Position
		if profile.IsProjectileTrackingBetweenShots {
			// Explicit Lua tracking is independent of faceTargetOnCreate.
			facingPlan.Profile.IsFacingPolicyKnown = true
			facingPlan.Profile.IsFacingSuppressed = false
		}
		turnPackets, turnErr := npcraknet.FaceTarget(facingPlan, profile.IsProjectileTrackingBetweenShots)
		if turnErr != nil {
			projectileRun.Stop()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyProjectileTurn: %w", turnErr)
		}
		if !peerSession.zone.NPCs().CommitFacing(facingPlan) {
			projectileRun.Stop()
			r.registry.mutex.Unlock()
			return nil, nil
		}
		immediatePackets = append(turnPackets, immediatePackets...)
	}
	trackErr := peerSession.trackCampaignNPCProjectile(projectileObjectID, projectileRun)
	if trackErr != nil {
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyZelemProjectileTrack: %w", trackErr)
	}
	if flight == nil {
		projectileRun.AddDeadline(impactDeadline)
	}
	lastDeadline := projectileRun.LastDeadline()
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	projectileDeadlines := projectileRun.Deadlines()
	projectileSchedule := campaignNPCProjectileSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: objectID,
		projectileObjectID: projectileObjectID, timestamp: timestamp,
		impactDeadline: impactDeadline, lastDeadline: lastDeadline,
		kind: campaignNPCProjectileZelem, source: enemy, target: aimTarget,
		plan: plan, result: result, geometry: geometry, facing: facing,
		binding: binding, run: projectileRun, volley: volley, flight: flight,
		projectileSourcePosition:    sourcePosition,
		projectileTargetPosition:    targetPosition,
		isProjectileTrajectoryFound: true,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(projectileDeadlines))
	for _, scheduledDeadline := range projectileDeadlines {
		producers = append(
			producers, projectileSchedule.producer(scheduledDeadline),
		)
	}
	if profile.HomingDelay > 0 {
		expiryDelay := ability.HitDelay + time.Duration(
			float64(profile.ProjectileDistance)/float64(profile.ProjectileSpeed)*
				float64(time.Second),
		) + 5*time.Second
		expiryStep := campaignNPCProjectileExpiryStep{schedule: projectileSchedule}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: expiryDelay, Produce: expiryStep.produce,
		})
	}
	if volley.isActive {
		elapsed := time.Duration(timestamp-volley.startTimestamp) * time.Millisecond
		nextDeadline := profile.Cooldown
		if volley.shotIndex+1 < int(profile.ProjectileShotCount) {
			nextShotIndex := volley.shotIndex + 1
			if nextShotIndex < len(profile.ProjectileShotDeadlines) {
				nextDeadline = profile.ProjectileShotDeadlines[nextShotIndex]
			} else if profile.IsProjectileParallelVolley {
				nextDeadline = 0
			} else {
				nextDeadline = profile.HitDelay +
					time.Duration(volley.shotIndex+1)*profile.ProjectileShotInterval
			}
		} else {
			nextDeadline = max(profile.Cooldown, profile.ReleaseDelay)
			if profile.AbilityName == "ZelemMarkSeeker" {
				nextDeadline = max(profile.HitDelay, profile.ReleaseDelay)
			}
		}
		projectileSchedule.nextDelay = max(time.Duration(0), nextDeadline-elapsed)
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: projectileSchedule.nextDelay, Produce: projectileSchedule.next,
		})
		sortScheduledPacketProducersByDelay(producers)
	} else if profile.ProjectileNoun != "" {
		nextDelay := max(ability.Cooldown, ability.ReleaseDelay)
		if isControlProjectile {
			nextDelay = max(
				ability.ReleaseDelay,
				impactDeadline+profile.ForcedMovementDuration,
			)
		}
		projectileSchedule.nextDelay = nextDelay
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: nextDelay, Produce: projectileSchedule.next,
		})
		sortScheduledPacketProducersByDelay(producers)
	}
	cancel, scheduleErr := packet.ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.projectile.retire(
			sessionKey, generation, projectileObjectID, projectileRun,
		)
		return nil, fmt.Errorf("enemyZelemSchedule: %w", scheduleErr)

	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[sessionKey]
	isTracked := isCurrentFound && currentSession.generation == generation &&
		currentSession.campaignNPCProjectiles[projectileObjectID] == projectileRun
	if isTracked && flight == nil {
		projectileRun.SetCancel(cancel)
	}
	r.registry.mutex.Unlock()
	if !isTracked {
		cancel()
		projectileRun.Stop()
		return nil, nil
	}
	r.logger.Printf(
		"RakNet projectile trajectory launched kind=npc projectile=%d source=%d target=%d ability=%q volley_shot=%d/%d origin=(%.3f,%.3f,%.3f) launch=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) target_velocity=(%.3f,%.3f,%.3f) travel=%.3f delay_ms=%d homing=%t piercing=%t",
		projectileObjectID, objectID, target.ObjectID, profile.AbilityName,
		volley.shotIndex+1, max(uint32(1), profile.ProjectileShotCount),
		sourcePosition.X, sourcePosition.Y, sourcePosition.Z,
		launchPosition.X, launchPosition.Y, launchPosition.Z,
		targetPosition.X, targetPosition.Y, targetPosition.Z,
		aimTarget.LinearVelocity.X, aimTarget.LinearVelocity.Y,
		aimTarget.LinearVelocity.Z, distance, collisionDelay.Milliseconds(),
		profile.HomingDelay > 0, profile.IsProjectilePiercing,
	)
	return immediatePackets, nil
}
