package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignChargeSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	plan       zonenpc.AttackPlan
}

func (e campaignChargeSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.clearNoctGhostChargeProtection(
		e.sessionKey, e.generation, e.objectID,
	)
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

type campaignGhostProtectionExpiryStep struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	expiresAt  time.Time
}

func (e campaignGhostProtectionExpiryStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.generation == e.generation && peerSession.zone != nil {
		peerSession.zone.NPCs().ExpireChargeProtection(e.objectID, e.expiresAt)
	}
	e.runtime.registry.mutex.RUnlock()
	return nil, nil
}

func (e campaignChargeSchedule) move() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCAttackActiveAt(
		e.generation, e.objectID, e.plan.TargetObjectID, e.runtime.now(),
	)
	isCurrent = isCurrent && peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.plan.ActionGeneration,
	)
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, e.plan.TargetObjectID,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, nil
	}
	movementProfile := e.plan.Profile
	movementProfile.Range = movementProfile.ForcedMovementStopDistance
	movementProfile.MovementSpeed *= 1 + movementProfile.MovementSpeedBuff
	movementProfile.MovementSpeed += movementProfile.ForcedMovementSpeed
	isGhostCharge := movementProfile.AbilityName == "NoctGhostCharge"
	if isGhostCharge {
		expiresAt := e.runtime.now().Add(8 * time.Second)
		err := peerSession.zone.NPCs().ApplyChargeProtection(
			e.objectID, expiresAt,
		)
		if err != nil {
			return e.fail("enemyGhostProtection", err)
		}
		expiry := campaignGhostProtectionExpiryStep{
			runtime: e.runtime, sessionKey: e.sessionKey,
			generation: e.generation, objectID: e.objectID,
			expiresAt: expiresAt,
		}
		cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet,
			[]raknet.ScheduledPacketProducer{{
				Delay: 8 * time.Second, Produce: expiry.produce,
			}},
		)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr != nil {
			peerSession.zone.NPCs().ClearChargeProtection(e.objectID)
			return e.fail("enemyGhostProtectionSchedule", scheduleErr)
		}
	}
	action := zonenpc.FirstActionPlan{}
	var err error
	if movementProfile.AbilityName == "NoctGhostCharge" {
		destination, isDestinationFound, destinationErr :=
			noctGhostChargeMovementDestination(
				peerSession.zone.Navigation(), enemy.Plan.Position, target.Position,
				enemy.Plan.NPCProfile.FootprintRadius, target.FootprintRadius,
			)
		if destinationErr != nil {
			return e.fail("enemyGhostDestination", destinationErr)
		}
		if !isDestinationFound {
			return e.followup(
				e.timestamp + uint64(e.plan.Profile.HitDelay/time.Millisecond),
			)
		}
		action = zonenpc.FirstActionPlan{
			ObjectID: enemy.Plan.ObjectID, TargetObjectID: target.ObjectID,
			SourcePosition: enemy.Plan.Position, TargetPosition: destination,
			Profile: movementProfile, IsPursuitNeeded: true,
		}
	} else if movementProfile.AbilityName == "NocturnaSpecialDriftCharge" {
		desired := noctGhostChargeDestination(
			enemy.Plan.Position, target.Position,
			enemy.Plan.NPCProfile.FootprintRadius, target.FootprintRadius,
		)
		destination, isDestinationFound, destinationErr :=
			navigationClippedMovementDestination(
				peerSession.zone.Navigation(), enemy.Plan.Position, desired,
				enemy.Plan.NPCProfile.FootprintRadius,
			)
		if destinationErr != nil {
			return e.fail("enemyDriftDestination", destinationErr)
		}
		if !isDestinationFound {
			return e.followup(
				e.timestamp + uint64(e.plan.Profile.HitDelay/time.Millisecond),
			)
		}
		action = zonenpc.FirstActionPlan{
			ObjectID: enemy.Plan.ObjectID, TargetObjectID: target.ObjectID,
			SourcePosition: enemy.Plan.Position, TargetPosition: destination,
			Profile: movementProfile,
			IsPursuitNeeded: zonegeometry.Distance(
				enemy.Plan.Position, destination,
			) > movementProfile.Range,
		}
	} else if isTemplateContactCharge(movementProfile.AbilityName) {
		movementProfile.Range = 2 * max(
			enemy.Plan.NPCProfile.FootprintRadius, float32(0),
		)
		action = zonenpc.FirstActionPlan{
			ObjectID: enemy.Plan.ObjectID, TargetObjectID: target.ObjectID,
			SourcePosition: enemy.Plan.Position, TargetPosition: target.Position,
			Profile: movementProfile,
			IsPursuitNeeded: zonegeometry.Distance(
				enemy.Plan.Position, target.Position,
			) > movementProfile.Range,
		}
	} else {
		action, err = campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, movementProfile,
			target.FootprintRadius,
		)
	}
	if err != nil {
		if isGhostCharge {
			peerSession.zone.NPCs().ClearChargeProtection(e.objectID)
		}
		return e.fail("enemyChargeMovePlan", err)
	}
	if !action.IsPursuitNeeded {
		return e.followup(e.timestamp + uint64(e.plan.Profile.HitDelay/time.Millisecond))
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		if isGhostCharge {
			peerSession.zone.NPCs().ClearChargeProtection(e.objectID)
		}
		return e.fail("enemyChargeMoveMarshal", err)
	}
	if movementProfile.AbilityName == "NocturnaSpecialDriftCharge" ||
		movementProfile.AbilityName == "BoomerCharge" {
		statePackets, stateErr := npcraknet.ChargeMovementState(
			e.objectID, movementProfile.ForcedMovementSpeed, 0,
			movementProfile.AbilityName != "NocturnaSpecialDriftCharge",
		)
		if stateErr != nil {
			return e.fail("enemyDriftMovementState", stateErr)
		}
		packets = append(statePackets, packets...)
	}
	err = e.runtime.pursuit.schedule(
		e.packet, e.sessionKey, e.generation, e.objectID,
		e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond),
		action.TargetPosition, movementProfile, e.followup,
	)
	if err != nil {
		if isGhostCharge {
			peerSession.zone.NPCs().ClearChargeProtection(e.objectID)
		}
		return e.fail("enemyChargeMoveSchedule", err)
	}
	if e.plan.Profile.LoopAnimationName == "" {
		return packets, nil
	}
	loopPacket, err := npcraknet.AnimationState(
		e.objectID, e.plan.Profile.LoopAnimationName,
		e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond),
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet campaign NPC charge loop presentation omitted object=%d: %v",
			e.objectID, err,
		)
		return packets, nil
	}
	return append([][]byte{loopPacket}, packets...), nil
}

func isTemplateContactCharge(abilityName string) bool {
	return abilityName == "DartingAttack" || abilityName == "CryosBasicCharge" ||
		abilityName == "ScaldronBoss_ShadowCharge"
}

func verdanthBasicPickyFarthestTarget(
	enemy zonenpc.Snapshot, targets []zone.NPCTarget,
) (zone.NPCTarget, bool) {
	selected := zone.NPCTarget{}
	selectedDistance := float32(-1)
	for _, candidate := range targets {
		if candidate.ObjectID == 0 || candidate.HitPoint <= 0 {
			continue
		}
		centerDistance := zonegeometry.Distance(
			enemy.Plan.Position, candidate.Position,
		)
		if centerDistance > 30 {
			continue
		}
		surfaceDistance := max(
			float32(0), centerDistance-
				enemy.Plan.NPCProfile.FootprintRadius-candidate.FootprintRadius,
		)
		if surfaceDistance < 10 || surfaceDistance <= selectedDistance {
			continue
		}
		selected = candidate
		selectedDistance = surfaceDistance
	}
	return selected, selected.ObjectID != 0
}

func noctGhostChargeDestination(
	source game.Vec3, target game.Vec3,
	sourceFootprintRadius float32, targetFootprintRadius float32,
) game.Vec3 {
	deltaX := target.X - source.X
	deltaY := target.Y - source.Y
	deltaZ := target.Z - source.Z
	centerDistance := float32(math.Sqrt(float64(
		deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
	)))
	if centerDistance <= 0 || sourceFootprintRadius < 0 ||
		targetFootprintRadius < 0 {
		return target
	}
	surfaceDistance := max(
		float32(0), centerDistance-sourceFootprintRadius-targetFootprintRadius,
	)
	travelDistance := surfaceDistance + 2*targetFootprintRadius +
		5*sourceFootprintRadius
	scale := travelDistance / centerDistance
	return game.Vec3{
		X: source.X + deltaX*scale,
		Y: source.Y + deltaY*scale,
		Z: source.Z + deltaZ*scale,
	}
}

func noctGhostChargeMovementDestination(
	mesh *basenavigation.Mesh, source game.Vec3, target game.Vec3,
	sourceFootprintRadius float32, targetFootprintRadius float32,
) (game.Vec3, bool, error) {
	desired := noctGhostChargeDestination(
		source, target, sourceFootprintRadius, targetFootprintRadius,
	)
	destination, isDestinationFound, err := navigationClippedMovementDestination(
		mesh, source, desired, sourceFootprintRadius,
	)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("ghostDestinationClip: %w", err)
	}
	return destination, isDestinationFound, nil
}

func navigationClippedMovementDestination(
	mesh *basenavigation.Mesh, source game.Vec3, desired game.Vec3,
	footprintRadius float32,
) (game.Vec3, bool, error) {
	if mesh == nil {
		return desired, true, nil
	}
	destination, isFound, err := zoneaction.NPCDirectMovementDestination(
		mesh, source, desired, footprintRadius,
	)
	if err != nil {
		return game.Vec3{}, false, fmt.Errorf("chargeDirect: %w", err)
	}
	if isFound {
		return destination, true, nil
	}
	minimumPortion := float32(0)
	maximumPortion := float32(1)
	delta := desired.Sub(source)
	destination = source
	for range 12 {
		portion := (minimumPortion + maximumPortion) * 0.5
		candidate := source.Add(delta.Scale(portion))
		projected, isProjected, projectionErr :=
			zoneaction.NPCDirectMovementDestination(
				mesh, source, candidate, footprintRadius,
			)
		if projectionErr != nil {
			return game.Vec3{}, false,
				fmt.Errorf("chargeProjection: %w", projectionErr)
		}
		if !isProjected {
			maximumPortion = portion
			continue
		}
		destination = projected
		minimumPortion = portion
	}
	return destination, zonegeometry.Distance(source, destination) > 0.1, nil
}

func (e campaignChargeSchedule) followup(timestamp uint64) ([][]byte, error) {
	var restoredStatePackets [][]byte
	if e.plan.Profile.AbilityName == "NocturnaSpecialDriftCharge" ||
		e.plan.Profile.AbilityName == "BoomerCharge" {
		var stateErr error
		restoredStatePackets, stateErr = npcraknet.ChargeMovementState(
			e.objectID, 0, e.plan.Profile.StealthType, true,
		)
		if stateErr != nil {
			return e.fail("enemyDriftRestoreState", stateErr)
		}
	}
	if e.plan.Profile.AbilityName == "VerdanthBasicPicky" {
		return e.runtime.produceVerdanthBasicPickyLanding(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp, e.plan,
		)
	}
	if isTemplateContactCharge(e.plan.Profile.AbilityName) {
		return e.runtime.produceChargeContactHit(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp, e.plan,
		)
	}
	relaxPacket, err := npcraknet.ChargeRelax(
		e.objectID, e.plan.Profile.EndAnimationName, timestamp,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet campaign NPC charge relax presentation omitted object=%d: %v",
			e.objectID, err,
		)
		relaxPacket = nil
	}
	followupTimestamp := timestamp +
		uint64(e.plan.Profile.EndAnimationDelay/time.Millisecond)
	var followupPackets [][]byte
	switch e.plan.Profile.AbilityName {
	case "BoomerCharge":
		followupPackets, err = e.runtime.produceBoomerSmash(
			e.packet, e.sessionKey, e.generation, e.objectID, followupTimestamp,
		)
	case "NomadSpecialOneCharge":
		followupPackets, err = e.runtime.produceNomadChargeAttack(
			e.packet, e.sessionKey, e.generation, e.objectID, followupTimestamp,
		)
	case "NocturnaSpecialDriftCharge":
		followupPackets, err = e.runtime.produceChargeSegmentCollision(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp, e.plan,
		)
	case "NoctGhostCharge":
		followupPackets, err = e.runtime.produceChargeSegmentCollision(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp, e.plan,
		)
	default:
		err = fmt.Errorf("unsupported ability %q", e.plan.Profile.AbilityName)
	}
	if err != nil {
		return e.fail("enemyChargeFollowup", err)
	}
	if e.plan.Profile.AbilityName == "NoctGhostCharge" {
		cleanupPackets, cleanupErr := e.runtime.finishNoctGhostCharge(
			e.sessionKey, e.generation, e.objectID, e.plan,
		)
		if cleanupErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign NPC ghost charge cleanup presentation omitted object=%d: %v",
				e.objectID, cleanupErr,
			)
		}
		followupPackets = append(followupPackets, cleanupPackets...)
		if relaxPacket != nil {
			followupPackets = append(followupPackets, relaxPacket)
		}
		return followupPackets, nil
	}
	if e.plan.Profile.AbilityName == "NocturnaSpecialDriftCharge" {
		if relaxPacket != nil {
			followupPackets = append(followupPackets, relaxPacket)
		}
		return append(followupPackets, restoredStatePackets...), nil
	}
	if e.plan.Profile.AbilityName == "BoomerCharge" {
		if relaxPacket != nil {
			followupPackets = append([][]byte{relaxPacket}, followupPackets...)
		}
		return append(followupPackets, restoredStatePackets...), nil
	}
	if relaxPacket == nil {
		return followupPackets, nil
	}
	return append([][]byte{relaxPacket}, followupPackets...), nil
}

func (r campaignNPCActionRuntime) clearNoctGhostChargeProtection(
	sessionKey string, generation uint64, objectID uint32,
) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if isFound && peerSession.generation == generation && peerSession.zone != nil {
		peerSession.zone.NPCs().ClearChargeProtection(objectID)
	}
	r.registry.mutex.RUnlock()
}

func (r campaignNPCActionRuntime) finishNoctGhostCharge(
	sessionKey string, generation uint64, objectID uint32,
	chargePlan zonenpc.AttackPlan,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || enemy.IsDefeated {
		return nil, nil
	}
	position, isProjected, err := zoneaction.NPCProjectPosition(
		peerSession.zone.Navigation(), enemy.Plan.Position,
		enemy.Plan.NPCProfile.FootprintRadius,
	)
	if err != nil {
		peerSession.zone.NPCs().ClearChargeProtection(objectID)
		return nil, fmt.Errorf("ghostProject: %w", err)
	}
	if !isProjected {
		position = enemy.Plan.Position
	}
	facing := enemy.Facing.Scale(-1)
	if facing.Length() <= 0 {
		facing = chargePlan.SourcePosition.Sub(position)
	}
	if facing.Length() <= 0 {
		peerSession.zone.NPCs().ClearChargeProtection(objectID)
		return nil, errors.New("ghost facing unavailable")
	}
	err = peerSession.zone.NPCs().SetPosition(objectID, position)
	if err != nil {
		peerSession.zone.NPCs().ClearChargeProtection(objectID)
		return nil, fmt.Errorf("ghostPosition: %w", err)
	}
	err = peerSession.zone.NPCs().FacePosition(objectID, position.Add(facing))
	if err != nil {
		peerSession.zone.NPCs().ClearChargeProtection(objectID)
		return nil, fmt.Errorf("ghostFacing: %w", err)
	}
	peerSession.zone.NPCs().ClearChargeProtection(objectID)
	packets, err := npcraknet.ChargeCleanup(objectID, position, facing)
	if err != nil {
		return nil, fmt.Errorf("ghostMarshal: %w", err)
	}
	return packets, nil
}

type campaignEnemyChargeNextStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignEnemyChargeNextStep) resume(timestamp uint64) ([][]byte, error) {
	packets, err := e.runtime.produceEnemyCharge(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("enemyChargeResume: %w", err)
	}
	return packets, nil
}

func (e campaignEnemyChargeNextStep) produce() ([][]byte, error) {
	packets, err := e.runtime.produceEnemyCharge(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("enemyChargeNext: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceChargeContactHit(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, chargePlan zonenpc.AttackPlan,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	readyTimestamp := peerSession.campaignNPCChargeReadiness[objectID]
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	hitProfile := chargePlan.Profile
	hitProfile.Family = zonenpc.ActionMelee
	hitProfile.Range = hitProfile.ForcedMovementStopDistance
	if isTemplateContactCharge(hitProfile.AbilityName) {
		// AdjustedPursuitRange(2.5) is exactly the authored surface hit gap of 2.
		hitProfile.Range = 2.5
	}
	hitProfile.HitDelay = 0
	hitProfile.ReleaseDelay = 0
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackMelee,
	}
	hitPlan := chargePlan
	hitPlan.Profile = hitProfile
	schedule := campaignNPCAttackSchedule{request: request, plan: hitPlan}
	packets, err := schedule.hit()
	if err != nil {
		return nil, fmt.Errorf("enemyDartingHit: %w", err)
	}
	if chargePlan.Profile.AbilityName == "ScaldronBoss_ShadowCharge" {
		next := campaignNPCFirstActionStep{
			runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
			objectID: objectID, actionGeneration: chargePlan.ActionGeneration, timestamp: timestamp,
		}
		nextPackets, nextErr := next.produce()
		if nextErr != nil {
			return nil, fmt.Errorf("chargeNext: %w", nextErr)
		}
		return append(packets, nextPackets...), nil
	}
	nextTimestamp := max(timestamp, readyTimestamp)
	if chargePlan.Profile.AbilityName == "CryosBasicCharge" {
		relaxPacket, relaxErr := npcraknet.ChargeRelax(
			objectID, chargePlan.Profile.EndAnimationName, timestamp,
		)
		if relaxErr != nil {
			return nil, fmt.Errorf("enemyChargeContactRelax: %w", relaxErr)
		}
		packets = append(packets, relaxPacket)
		nextTimestamp = timestamp +
			uint64(chargePlan.Profile.EndAnimationDelay/time.Millisecond)
	}
	next := campaignEnemyChargeNextStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: nextTimestamp,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay:   time.Duration(nextTimestamp-timestamp) * time.Millisecond,
		Produce: next.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyChargeContactNextSchedule: %w", err)
	}
	return packets, nil
}

func campaignChargeSegmentTargets(
	targets []zone.NPCTarget, start game.Vec3, end game.Vec3,
	triggerRadius float32,
) []zone.NPCTarget {
	if triggerRadius <= 0 {
		return nil
	}
	result := make([]zone.NPCTarget, 0, len(targets))
	for _, target := range targets {
		if target.ObjectID == 0 || target.HitPoint <= 0 ||
			!zonegeometry.SegmentIntersectsSphere(
				start, end, target.Position,
				triggerRadius+target.FootprintRadius,
			) {
			continue
		}
		result = append(result, target)
	}
	return result
}

type campaignNocturnaDriftNextSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignNocturnaDriftNextSchedule) produce() ([][]byte, error) {
	return e.runtime.produceEnemyCharge(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
}

func (r campaignNPCActionRuntime) produceChargeSegmentCollision(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, chargePlan zonenpc.AttackPlan,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	readyTimestamp := peerSession.campaignNPCChargeReadiness[objectID]
	r.registry.mutex.RUnlock()
	if !isEnemyFound {
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isProfileFound ||
		(profile.AbilityName != "NocturnaSpecialDriftCharge" &&
			profile.AbilityName != "NoctGhostCharge") {
		return nil, nil
	}
	nextTimestamp := max(
		readyTimestamp,
		timestamp+uint64(profile.EndAnimationDelay/time.Millisecond),
	)
	nextDelay := time.Duration(nextTimestamp-timestamp) * time.Millisecond
	next := campaignNocturnaDriftNextSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: nextTimestamp,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: nextDelay, Produce: next.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyDriftNextSchedule: %w", err)
	}
	r.registry.mutex.Lock()
	peerSession, isFound = r.registry.sessions[sessionKey]
	isCurrent = isFound && peerSession.isCampaignNPCAttackActiveAt(
		generation, objectID, chargePlan.TargetObjectID, r.now(),
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound = peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound || peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		cancel()
		if !isEnemyFound {
			return nil, nil
		}
		return nil, errors.New("enemy drift random unavailable")
	}
	targets := campaignChargeSegmentTargets(
		peerSession.zone.LiveNPCTargets(), chargePlan.SourcePosition,
		enemy.Plan.Position, enemy.Plan.NPCProfile.FootprintRadius,
	)
	packets := make([][]byte, 0)
	modifierPlan := make([]zonenpc.AttackPlan, 0, len(targets))
	statDelta := sporenet.PlayerStatDelta{}
	for _, target := range targets {
		plan, planErr := zonenpc.PlanAreaAttackWithProfile(
			enemy, target.ObjectID, target.Position, profile,
		)
		if planErr != nil {
			continue
		}
		if plan.Profile.ImpactEffectName != "" {
			impactPacket, marshalErr := npcraknet.AttackImpact(plan)
			if marshalErr != nil {
				r.registry.mutex.Unlock()
				cancel()
				return nil, fmt.Errorf("enemyDriftImpact: %w", marshalErr)
			}
			packets = append(packets, impactPacket)
		}
		result, commitErr := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			enemy.Plan.NPCProfile.CriticalRating, r.program.Critical,
		)
		if commitErr != nil {
			r.registry.mutex.Unlock()
			cancel()
			return nil, fmt.Errorf("enemyDriftCommit: %w", commitErr)
		}
		hitPackets, targetStatDelta, isApplied, damageErr :=
			r.applyEnemyAreaAttackDamage(
				&peerSession, generation, plan, result, timestamp,
			)
		if damageErr != nil {
			r.registry.mutex.Unlock()
			cancel()
			return nil, fmt.Errorf("enemyDriftDamage: %w", damageErr)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		if isApplied && profile.ModifierName != "" &&
			profile.ModifierDuration > 0 {
			modifierPlan = append(modifierPlan, plan)
		}
	}
	auraProfile, isAuraProfileFound := zonenpc.NocturnaSpecialDriftAuraProfile(
		enemy.Plan.NounName,
	)
	auraHealing := float32(0)
	if isAuraProfileFound {
		for _, target := range peerSession.zone.LiveNPCTargets() {
			if zonegeometry.Distance(enemy.Plan.Position, target.Position) >
				auraProfile.Radius+enemy.Plan.NPCProfile.FootprintRadius+
					target.FootprintRadius {
				continue
			}
			plan, planErr := zonenpc.PlanAreaAttackWithProfile(
				enemy, target.ObjectID, target.Position, auraProfile,
			)
			if planErr != nil {
				continue
			}
			result, commitErr := zonenpc.CommitAttack(
				peerSession.zone.NPCRandom(), plan,
				enemy.Plan.NPCProfile.CriticalRating, r.program.Critical,
			)
			if commitErr != nil {
				r.registry.mutex.Unlock()
				cancel()
				return nil, fmt.Errorf("enemyDriftAuraCommit: %w", commitErr)
			}
			hitPackets, targetStatDelta, isApplied, damageErr :=
				r.applyEnemyAreaAttackDamage(
					&peerSession, generation, plan, result, timestamp,
				)
			if damageErr != nil {
				r.registry.mutex.Unlock()
				cancel()
				return nil, fmt.Errorf("enemyDriftAuraDamage: %w", damageErr)
			}
			packets = append(packets, hitPackets...)
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
			if isApplied {
				auraHealing += result.Damage * auraProfile.LifeSteal
			}
		}
	}
	if auraHealing > 0 {
		healed, amount, healErr := peerSession.zone.NPCs().Heal(
			enemy.Plan.ObjectID, auraHealing,
		)
		if healErr != nil {
			r.registry.mutex.Unlock()
			cancel()
			return nil, fmt.Errorf("enemyDriftAuraHeal: %w", healErr)
		}
		if amount > 0 {
			healPackets, marshalErr := npcraknet.HealDelta(
				enemy.Plan.ObjectID, healed, amount,
			)
			if marshalErr != nil {
				r.registry.mutex.Unlock()
				cancel()
				return nil, fmt.Errorf("enemyDriftAuraHealMarshal: %w", marshalErr)
			}
			packets = append(packets, healPackets...)
		}
	}
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	err = r.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("enemyDriftStats: %w", err)
	}
	for _, plan := range modifierPlan {
		modifierPackets, modifierErr := r.applyCampaignNPCTimedModifier(
			packet, sessionKey, generation, plan, timestamp,
		)
		if modifierErr != nil {
			cancel()
			return nil, fmt.Errorf("enemyDriftSilence: %w", modifierErr)
		}
		packets = append(packets, modifierPackets...)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceEnemyCharge(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCChargeReadiness == nil {
		peerSession.campaignNPCChargeReadiness = make(map[uint32]uint64)
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if isEnemyFound && isProfileFound &&
		profile.AbilityName == "VerdanthBasicPicky" {
		selected, isSelected := verdanthBasicPickyFarthestTarget(
			enemy, peerSession.zone.LiveNPCTargets(),
		)
		if isSelected && selected.ObjectID != enemy.TargetObjectID {
			replaced, isReplaced, replaceErr := peerSession.zone.NPCs().Retarget(
				objectID, zonenpc.Target{
					ObjectID: selected.ObjectID, Position: selected.Position,
					FootprintRadius: selected.FootprintRadius,
					Faction:         zonenpc.FactionPlayerAligned,
					Owner: zonenpc.ActionOwner{
						UserID:         selected.UserID,
						PeerGeneration: selected.PeerGeneration,
					},
					IsAlive: true,
				},
			)
			if replaceErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyPickyRetarget: %w", replaceErr)
			}
			if isReplaced {
				enemy = replaced
			}
		}
	}
	target, isTargetFound := peerSession.campaignNPCTarget(generation, enemy.TargetObjectID)
	chargePursuit := zonenpc.FirstActionPlan{}
	isChargePursuit := false
	if isEnemyFound && isTargetFound && isProfileFound &&
		profile.Family == zonenpc.ActionCharge {
		var actionErr error
		chargePursuit, actionErr = campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyChargePursuitPlan: %w", actionErr)
		}
		isChargePursuit = chargePursuit.IsPursuitNeeded
	}
	if isChargePursuit {
		r.registry.mutex.Unlock()
		packets, marshalErr := npcraknet.Pursuit(chargePursuit)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyChargePursuitMarshal: %w", marshalErr)
		}
		step := campaignEnemyChargeNextStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, objectID: objectID,
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			chargePursuit.TargetPosition, profile, step.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyChargePursuitSchedule: %w", scheduleErr)
		}
		return packets, nil
	}
	pickySurfaceDistance := float32(0)
	if isEnemyFound && isTargetFound {
		pickySurfaceDistance = max(
			float32(0), zonegeometry.Distance(enemy.Plan.Position, target.Position)-
				enemy.Plan.NPCProfile.FootprintRadius-target.FootprintRadius,
		)
	}
	isPickyMelee := isEnemyFound && isTargetFound && isProfileFound &&
		profile.AbilityName == "VerdanthBasicPicky" &&
		(!zonenpc.ShouldVerdanthBasicPickyCharge(
			pickySurfaceDistance,
		) || peerSession.campaignNPCChargeReadiness[objectID] > timestamp)
	if isPickyMelee {
		r.registry.mutex.Unlock()
		return r.produceVerdanthBasicPickyAttack(
			packet, sessionKey, generation, objectID, timestamp, false,
		)
	}
	isCryosHeadbutt := isEnemyFound && isTargetFound && isProfileFound &&
		profile.AbilityName == "CryosBasicCharge" &&
		zonenpc.ShouldCryosBasicChargeHeadbutt(
			zonegeometry.Distance(enemy.Plan.Position, target.Position),
		)
	if isCryosHeadbutt {
		r.registry.mutex.Unlock()
		return r.produceCryosBasicChargeHeadbutt(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	readyTimestamp := peerSession.campaignNPCChargeReadiness[objectID]
	if readyTimestamp > timestamp && profile.AbilityName != "ScaldronBoss_ShadowCharge" {
		r.registry.mutex.Unlock()
		if isProfileFound && profile.AbilityName == "NoctGhostCharge" {
			return r.produceNoctGhostChargerPose(
				packet, sessionKey, generation, objectID, timestamp, readyTimestamp,
			)
		}
		// Retargeting or pursuit can resume before the previous charge's
		// cooldown expires. Keep a continuation while the action is owned;
		// otherwise the NPC stays claimed with no attack left to run.
		return r.scheduleEnemyChargeRetry(
			packet, sessionKey, generation, objectID, timestamp, readyTimestamp,
		)
	}
	if isProfileFound && profile.AbilityName == "DartingAttack" {
		if peerSession.zone.NPCRandom() == nil {
			r.registry.mutex.Unlock()
			return nil, errors.New("enemy darting phase random unavailable")
		}
		isDartingSelected := peerSession.zone.NPCRandom().Float64() < 0.5
		if !isDartingSelected {
			r.registry.sessions[sessionKey] = peerSession
			r.registry.mutex.Unlock()
			return r.produceVerdanthBasicSkeetCircle(
				packet, sessionKey, generation, objectID, timestamp,
			)
		}
	}
	if isEnemyFound && isTargetFound && isProfileFound &&
		profile.Family == zonenpc.ActionCharge {
		peerSession.campaignNPCChargeReadiness[objectID] = timestamp +
			uint64(profile.Cooldown/time.Millisecond)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if !isEnemyFound || !isTargetFound || !isProfileFound ||
		profile.Family != zonenpc.ActionCharge {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: objectID, TargetObjectID: target.ObjectID,
		SourcePosition: enemy.Plan.Position, TargetPosition: target.Position,
		ActionGeneration: enemy.ActionGeneration, Profile: profile,
	}
	if !peerSession.zone.NPCs().CommitFacing(plan) {
		return nil, nil
	}
	packets, err := npcraknet.ChargeStart(plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyChargeStart: %w", err)
	}
	if !peerSession.zone.NPCs().CommitCorruptorAction(plan, timestamp) {
		return nil, nil
	}
	schedule := campaignChargeSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
		objectID: objectID, timestamp: timestamp, plan: plan,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: profile.HitDelay, Produce: schedule.move,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyChargeSchedule: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceNoctGhostChargerPose(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, chargeReadyTimestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	profile := zonenpc.NoctGhostChargerPoseProfile()
	posePacket, err := npcraknet.AnimationState(
		objectID, profile.AnimationName, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyGhostPoseMarshal: %w", err)
	}
	poseEndTimestamp := timestamp + uint64(profile.ReleaseDelay/time.Millisecond)
	nextTimestamp := max(poseEndTimestamp, chargeReadyTimestamp)
	next := campaignEnemyChargeNextStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: nextTimestamp,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay:   time.Duration(nextTimestamp-timestamp) * time.Millisecond,
		Produce: next.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyGhostPoseSchedule: %w", err)
	}
	return [][]byte{posePacket}, nil
}

type campaignPickyLandingStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
}

func (e campaignPickyLandingStep) attack(timestamp uint64) ([][]byte, error) {
	return e.runtime.produceVerdanthBasicPickyAttack(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp, true,
	)
}

func (r campaignNPCActionRuntime) produceVerdanthBasicPickyLanding(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, chargePlan zonenpc.AttackPlan,
) ([][]byte, error) {
	landPacket, err := npcraknet.AnimationState(
		objectID, chargePlan.Profile.EndAnimationName, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyPickyLand: %w", err)
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, chargePlan.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	movementProfile := chargePlan.Profile
	movementProfile.Range = 0.75
	movementProfile.MovementSpeed += movementProfile.ForcedMovementSpeed
	action, err := campaignNPCActionWithProfile(
		enemy.Plan, target.ObjectID, target.Position, movementProfile,
		target.FootprintRadius,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyPickyFinalApproachPlan: %w", err)
	}
	step := campaignPickyLandingStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
	}
	if !action.IsPursuitNeeded {
		attackPackets, attackErr := step.attack(timestamp)
		if attackErr != nil {
			return nil, attackErr
		}
		return append([][]byte{landPacket}, attackPackets...), nil
	}
	pursuitPackets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, fmt.Errorf("enemyPickyFinalApproachMarshal: %w", err)
	}
	err = r.pursuit.schedule(
		packet, sessionKey, generation, objectID, timestamp,
		action.TargetPosition, movementProfile, step.attack,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyPickyFinalApproachSchedule: %w", err)
	}
	return append([][]byte{landPacket}, pursuitPackets...), nil
}

func (r campaignNPCActionRuntime) produceVerdanthBasicPickyAttack(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, isCharging bool,
) ([][]byte, error) {
	kind := campaignNPCAttackPickyMelee
	profile := zonenpc.VerdanthBasicPickyMeleeProfile()
	if isCharging {
		kind = campaignNPCAttackPickyCharging
		profile = zonenpc.VerdanthBasicPickyChargingAttackProfile()
	}
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: kind,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyPickyAttackStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemyPickyAttackPursuitPlan: %w", actionErr)
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyPickyAttackPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, request.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyPickyAttackPursuitSchedule: %w", scheduleErr)
		}
		return pursuitPackets, nil
	}
	packets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyPickyAttackStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(plan)
	if err != nil {
		return nil, fmt.Errorf("enemyPickyAttackTimeline: %w", err)
	}
	schedule := campaignNPCAttackSchedule{request: request, plan: plan}
	next := campaignEnemyChargeNextStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		timestamp: timestamp + uint64(timeline.NextDelay/time.Millisecond),
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: timeline.HitDelay, Produce: schedule.hit},
		{Delay: timeline.NextDelay, Produce: next.produce},
	})
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyPickyAttackSchedule: %w", scheduleErr)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) scheduleEnemyChargeRetry(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, readyTimestamp uint64,
) ([][]byte, error) {
	if readyTimestamp <= timestamp {
		return r.produceEnemyCharge(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	next := campaignEnemyChargeNextStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: readyTimestamp,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay:   time.Duration(readyTimestamp-timestamp) * time.Millisecond,
		Produce: next.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyChargeRetrySchedule: %w", err)
	}
	return nil, nil
}

func (r campaignNPCActionRuntime) produceCryosBasicChargeHeadbutt(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackCryosChargeHeadbutt,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyHeadbuttStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	chargeProfile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isProfileFound || chargeProfile.AbilityName != "CryosBasicCharge" {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	headbuttCooldown := 2 * time.Second
	switch chargeProfile.Cooldown {
	case 8 * time.Second:
		headbuttCooldown = 1750 * time.Millisecond
	case 6 * time.Second:
		headbuttCooldown = 1500 * time.Millisecond
	}
	profile := zonenpc.CryosBasicChargeHeadbuttProfile(
		headbuttCooldown, chargeProfile.MovementSpeed,
		chargeProfile.NonCombatMovementSpeed,
	)
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemyHeadbuttPursuitPlan: %w", actionErr)
		}
		packets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyHeadbuttPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, request.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyHeadbuttPursuitSchedule: %w", scheduleErr)
		}
		return packets, nil
	}
	packets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyHeadbuttStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(plan)
	if err != nil {
		return nil, fmt.Errorf("enemyHeadbuttTimeline: %w", err)
	}
	schedule := campaignNPCAttackSchedule{request: request, plan: plan}
	next := campaignEnemyChargeNextStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		timestamp: timestamp + uint64(timeline.NextDelay/time.Millisecond),
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: timeline.HitDelay, Produce: schedule.hit},
		{Delay: timeline.NextDelay, Produce: next.produce},
	})
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyHeadbuttSchedule: %w", scheduleErr)
	}
	return packets, nil
}

type campaignNomadChargeAttackSchedule struct {
	attack    campaignNPCAttackSchedule
	nextDelay time.Duration
}

func (e campaignNomadChargeAttackSchedule) next() ([][]byte, error) {
	req := e.attack.request
	timestamp := req.timestamp + uint64(e.nextDelay/time.Millisecond)
	packets, err := req.runtime.produceEnemyCharge(
		req.packet, req.sessionKey, req.generation, req.objectID, timestamp,
	)
	if err != nil {
		req.runtime.releaseAction(req.sessionKey, req.generation, req.objectID)
		return nil, fmt.Errorf("enemyChargeAttackNext: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceNomadChargeAttack(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackChargeFollowup,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyChargeAttackStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	chargeReadyTimestamp := peerSession.campaignNPCChargeReadiness[objectID]
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile := zonenpc.NomadSpecialOneChargingAttackProfile()
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemyChargeAttackPursuitPlan: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, nil
		}
		packets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyChargeAttackPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, request.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyChargeAttackPursuitSchedule: %w", scheduleErr)
		}
		return packets, nil
	}
	packets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyChargeAttackStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(plan)
	if err != nil {
		return nil, fmt.Errorf("enemyChargeAttackTimeline: %w", err)
	}
	nextDelay := timeline.NextDelay
	if chargeReadyTimestamp > timestamp {
		readyDelay := time.Duration(chargeReadyTimestamp-timestamp) * time.Millisecond
		nextDelay = max(nextDelay, readyDelay)
	}
	schedule := campaignNomadChargeAttackSchedule{
		attack:    campaignNPCAttackSchedule{request: request, plan: plan},
		nextDelay: nextDelay,
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: timeline.HitDelay, Produce: schedule.attack.hit},
		{Delay: nextDelay, Produce: schedule.next},
	})
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyChargeAttackSchedule: %w", scheduleErr)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceBoomerSmash(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(generation, enemy.TargetObjectID)
	chargeReadyTimestamp := peerSession.campaignNPCChargeReadiness[objectID]
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile := zonenpc.BoomerSmashProfile()
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		if timestamp >= chargeReadyTimestamp {
			return r.produceEnemyCharge(packet, sessionKey, generation, objectID, timestamp)
		}
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemySmashPursuitPlan: %w", actionErr)
		}
		packets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemySmashPursuitMarshal: %w", marshalErr)
		}
		request := campaignNPCAttackRequest{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, objectID: objectID, timestamp: timestamp,
			kind: campaignNPCAttackBoomerSmash,
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, request.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemySmashPursuitSchedule: %w", scheduleErr)
		}
		return packets, nil
	}
	packets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemySmashStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(plan)
	if err != nil {
		return nil, fmt.Errorf("enemySmashTimeline: %w", err)
	}
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackBoomerSmash,
	}
	schedule := campaignNPCAttackSchedule{request: request, plan: plan}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: timeline.HitDelay, Produce: schedule.hit},
		{Delay: timeline.NextDelay, Produce: schedule.next},
	})
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemySmashSchedule: %w", scheduleErr)
	}
	return packets, nil
}
