package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

type campaignNPCGravityOrb struct {
	objectID uint32
	position game.Vec3
	plan     zonenpc.SpawnPlan
}

type campaignNPCGravityOrbRun struct {
	isSpawned      bool
	spawnTimestamp uint64
	profile        zonenpc.GravityOrbProfile
	orbs           []campaignNPCGravityOrb
	cancel         raknet.CancelSchedule
}

type campaignNPCGravityOrbSchedule struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	sourceID         uint32
	actionGeneration uint64
	timestamp        uint64
	nextDelay        time.Duration
	isCorruptor      bool
	run              *campaignNPCGravityOrbRun
}

type campaignNPCGravityOrbPullStep struct {
	schedule  campaignNPCGravityOrbSchedule
	timestamp uint64
}

func (e campaignNPCGravityOrbPullStep) produce() ([][]byte, error) {
	return e.schedule.pull(e.timestamp)
}

func (e campaignNPCGravityOrbSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCGravityOrbs[e.sourceID] == e.run
}

func (e campaignNPCGravityOrbSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(peerSession, isFound) {
		delete(peerSession.campaignNPCGravityOrbs, e.sourceID)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	e.runtime.releaseActionGeneration(
		e.sessionKey, e.generation, e.sourceID, e.actionGeneration,
	)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignNPCGravityOrbSchedule) spawn() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound) &&
		peerSession.isCampaignNPCSourceGenerationActive(
			e.generation, e.sourceID, e.actionGeneration,
		)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.sourceID)
	if !isSourceFound || source.IsDefeated || source.TargetObjectID == 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	plans := make([]zonenpc.SpawnPlan, 0, len(e.run.orbs))
	for _, orb := range e.run.orbs {
		plans = append(plans, orb.plan)
	}
	err := peerSession.zone.NPCs().Add(plans, source.TargetObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("enemyGravityOrbAdd", err)
	}
	err = peerSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: plans, TargetObjectID: source.TargetObjectID,
	}, peerSession.binding.UserID, e.generation)
	if err != nil {
		rollbackErr := peerSession.zone.NPCs().RollbackAdd(plans)
		e.runtime.registry.mutex.Unlock()
		return e.fail("enemyGravityOrbPublish", errors.Join(err, rollbackErr))
	}
	e.run.isSpawned = true
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets := make([][]byte, 0, len(e.run.orbs)*7)
	for _, orb := range e.run.orbs {
		orbPackets, err := npcraknet.TargetedSpawn(
			orb.plan, source.TargetObjectID,
		)
		if err != nil {
			return e.fail("enemyGravityOrbSpawn", err)
		}
		effectPacket, err := npcraknet.GravityOrbAttachedEffect(
			e.run.profile.StartupEffectName, orb.objectID,
		)
		if err != nil {
			return e.fail("enemyGravityOrbStartup", err)
		}
		packets = append(packets, orbPackets...)
		packets = append(packets, effectPacket)
	}
	return packets, nil
}

func (e campaignNPCGravityOrbSchedule) effect(assetName string) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound) && e.run.isSpawned
	orbs := make([]campaignNPCGravityOrb, 0, len(e.run.orbs))
	if isCurrent {
		for _, orb := range e.run.orbs {
			_, isOrbLive := peerSession.zone.NPCs().LiveNPC(orb.objectID)
			if isOrbLive {
				orbs = append(orbs, orb)
			}
		}
	}
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets := make([][]byte, 0, len(orbs)*2)
	for _, orb := range orbs {
		removePacket, err := npcraknet.GravityOrbEffectRemoval(orb.objectID)
		if err != nil {
			return e.fail("enemyGravityOrbEffectRemove", err)
		}
		packet, err := npcraknet.GravityOrbAttachedEffect(
			assetName, orb.objectID,
		)
		if err != nil {
			return e.fail("enemyGravityOrbEffect", err)
		}
		packets = append(packets, removePacket, packet)
	}
	return packets, nil
}

func (e campaignNPCGravityOrbSchedule) stable() ([][]byte, error) {
	return e.effect(e.run.profile.StableEffectName)
}

func (e campaignNPCGravityOrbSchedule) unstable() ([][]byte, error) {
	return e.effect(e.run.profile.UnstableEffectName)
}

func (e campaignNPCGravityOrbSchedule) pull(timestamp uint64) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || !e.run.isSpawned ||
		peerSession.zone == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCTarget(peerSession.deployedObjectID)
	isHeroTargetFound := isTargetFound && target.IsHero &&
		target.UserID == peerSession.binding.UserID &&
		target.PeerGeneration == e.generation
	packets := make([][]byte, 0)
	now := e.runtime.now()
	for _, orb := range e.run.orbs {
		_, isOrbLive := peerSession.zone.NPCs().LiveNPC(orb.objectID)
		if !isOrbLive {
			continue
		}
		for projectileObjectID, run := range peerSession.campaignNPCProjectiles {
			snapshot := run.Snapshot(now)
			if !snapshot.IsActive || zonegeometry.Distance(
				orb.position,
				game.Vec3{
					X: snapshot.Position.X, Y: snapshot.Position.Y,
					Z: snapshot.Position.Z,
				},
			) > e.run.profile.Radius {
				continue
			}
			gravityPackets, err := run.ApplyGravity(
				now,
				sim.Position{
					X: orb.position.X, Y: orb.position.Y, Z: orb.position.Z,
				},
				e.run.profile.ProjectilePullDistance,
				e.run.profile.CooldownModifyPeriod,
			)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail(
					fmt.Sprintf("enemyGravityOrbProjectile[%d]", projectileObjectID),
					err,
				)
			}
			packets = append(packets, gravityPackets...)
		}
		if !isHeroTargetFound {
			continue
		}
		deltaX := orb.position.X - target.Position.X
		deltaY := orb.position.Y - target.Position.Y
		distance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
		if distance <= 0 || distance > e.run.profile.Radius {
			continue
		}
		step := min(distance, e.run.profile.PlayerPullDistance)
		desired := target.Position
		desired.X += deltaX / distance * step
		desired.Y += deltaY / distance * step
		destination, isDestinationFound, err :=
			zonenavigation.DirectMovementDestination(
				peerSession.zone.Navigation(), target.Position, desired,
				peerSession.deployedCampaignFootprintRadius(),
			)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("enemyGravityOrbDestination", err)
		}
		if !isDestinationFound {
			continue
		}
		plan := zonenpc.AttackPlan{
			SourceObjectID: orb.objectID, TargetObjectID: target.ObjectID,
			SourcePosition: orb.position, TargetPosition: target.Position,
			Profile: zonenpc.ActionProfile{
				ForcedMovementSpeed: e.run.profile.PlayerPullDistance,
			},
		}
		movementPackets, err := npcraknet.ForcedMovement(plan, destination, timestamp)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("enemyGravityOrbMovement", err)
		}
		previousPosition := peerSession.playerPosition
		motionSnapshot := peerSession.playerMotionSnapshot()
		err = peerSession.teleportPlayer(
			now,
			raknet.Vector3{X: destination.X, Y: destination.Y, Z: destination.Z},
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("enemyGravityOrbTargetMove", err)
		}
		err = peerSession.syncZoneHeroPose()
		if err != nil {
			if peerSession.playerMotion == nil {
				peerSession.playerPosition = previousPosition
			} else {
				peerSession.restorePlayerMotion(
					motionSnapshot, peerSession.playerMotionRevision(),
				)
			}
			e.runtime.registry.mutex.Unlock()
			return e.fail("enemyGravityOrbTargetSync", err)
		}
		target.Position = destination
		packets = append(packets, movementPackets...)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	return packets, nil
}

func (e campaignNPCGravityOrbSchedule) fizzle() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	delete(peerSession.campaignNPCGravityOrbs, e.sourceID)
	e.run.cancel = nil
	isSpawned := e.run.isSpawned
	liveOrbs := make([]campaignNPCGravityOrb, 0, len(e.run.orbs))
	if isSpawned {
		objectIDs := make([]uint32, 0, len(e.run.orbs))
		for _, orb := range e.run.orbs {
			objectIDs = append(objectIDs, orb.objectID)
			_, isOrbLive := peerSession.zone.NPCs().LiveNPC(orb.objectID)
			if isOrbLive {
				liveOrbs = append(liveOrbs, orb)
			}
		}
		err := peerSession.zone.NPCs().Despawn(objectIDs)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyGravityOrbDespawn: %w", err)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if !isSpawned {
		return nil, nil
	}
	packets := make([][]byte, 0, len(liveOrbs)*3)
	for _, orb := range liveOrbs {
		removePacket, err := npcraknet.GravityOrbEffectRemoval(orb.objectID)
		if err != nil {
			return nil, fmt.Errorf("enemyGravityOrbEffectRemove: %w", err)
		}
		effectPacket, err := npcraknet.GravityOrbEffect(
			e.run.profile.FizzleEffectName, orb.position,
		)
		if err != nil {
			return nil, fmt.Errorf("enemyGravityOrbFizzle: %w", err)
		}
		deletePacket, err := npcraknet.GravityOrbDelete(orb.objectID)
		if err != nil {
			return nil, fmt.Errorf("enemyGravityOrbDelete: %w", err)
		}
		packets = append(packets, removePacket, effectPacket, deletePacket)
	}
	return packets, nil
}

func (e campaignNPCGravityOrbSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.nextDelay/time.Millisecond)
	if e.isCorruptor {
		step := campaignNPCFirstActionStep{
			runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
			generation: e.generation, objectID: e.sourceID,
			actionGeneration: e.actionGeneration, timestamp: timestamp,
		}
		return step.produce()
	}
	packets, err := e.runtime.producePolarisPhaseTransition(
		e.packet, e.sessionKey, e.generation, e.sourceID, timestamp,
		polarisNextPush,
	)
	if err != nil {
		return e.fail("enemyGravityOrbNext", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) producePolarisGravityOrb(
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
	npc, isNPCFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, npc.TargetObjectID,
	)
	if !isNPCFound || npc.IsDefeated || !isTargetFound {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ZelemGravityOrbProfile(npc.Plan.NounName)
	isCorruptor := false
	if !isProfileFound {
		profile, isProfileFound = zonenpc.ScaldronBossGravityOrbProfile(
			npc.Plan.NounName,
		)
		isCorruptor = isProfileFound
	}
	if !isProfileFound {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, errors.New("enemy Polaris gravity orb profile unavailable")
	}
	if !isCorruptor && peerSession.campaignNPCGravityOrbs[objectID] != nil {
		r.registry.mutex.Unlock()
		return r.producePolarisPhaseTransition(
			packet, sessionKey, generation, objectID, timestamp, polarisNextPush,
		)
	}
	player := make(map[uint64]struct{})
	for _, liveTarget := range peerSession.zone.LiveNPCTargets() {
		if liveTarget.IsHero {
			player[liveTarget.UserID] = struct{}{}
		}
	}
	orbCount := profile.BaseOrbCount + uint32(len(player))*profile.AddedOrbPerPlayer
	positions, err := zonenpc.PlanGravityOrbPositions(
		npc.Plan.Position, target.Position, orbCount, profile,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyGravityOrbPlacement: %w", err)
	}
	firstObjectID, err := peerSession.zone.ReserveObjectIDs(orbCount)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyGravityOrbReserve: %w", err)
	}
	orbs := make([]campaignNPCGravityOrb, 0, orbCount)
	hitPoint := r.program.NonPlayerHitPoint[util.HashID(profile.NounName)]
	if hitPoint <= 0 {
		hitPoint = 50
	}
	footprintRadius, footprintErr := r.program.FootprintRadius(profile.NounName)
	if footprintErr != nil || footprintRadius <= 0 {
		footprintRadius = 0.5
	}
	for index, position := range positions {
		npcProfile := game.CampaignNPCProfile{
			ChallengeValue: 25, NPCRank: 1, IsTargetable: true,
			PlayerCountHealthScale: 0.25,
			HitPoint:               hitPoint, PowerPoint: 10,
			Strength: 10, Dexterity: 10, Mind: 10,
			DodgeRating: 60, ResistRating: 60, CriticalRating: 45,
			GraphicsScale: 1, FootprintRadius: footprintRadius,
			DifficultyDamageMultiplier: npc.Plan.NPCProfile.DifficultyDamageMultiplier,
			IsKnown:                    true,
		}
		plan := zonenpc.SpawnPlan{
			ObjectID: firstObjectID + uint32(index), OwnerObjectID: objectID,
			NounName: profile.NounName, Position: position,
			IsFixture: true, IsRewardSuppressed: true, NPCProfile: npcProfile,
		}
		orbs = append(orbs, campaignNPCGravityOrb{
			objectID: plan.ObjectID, position: position, plan: plan,
		})
	}
	run := &campaignNPCGravityOrbRun{
		spawnTimestamp: timestamp + uint64(profile.Cast.HitDelay/time.Millisecond),
		profile:        profile, orbs: orbs,
	}
	if peerSession.campaignNPCGravityOrbs == nil {
		peerSession.campaignNPCGravityOrbs = make(map[uint32]*campaignNPCGravityOrbRun)
	}
	previousRun := peerSession.campaignNPCGravityOrbs[objectID]
	peerSession.campaignNPCGravityOrbs[objectID] = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	plan := zonenpc.AttackPlan{
		SourceObjectID: objectID, TargetObjectID: target.ObjectID,
		ActionGeneration: npc.ActionGeneration,
		SourcePosition:   npc.Plan.Position, TargetPosition: target.Position,
		Profile: profile.Cast,
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		r.restorePolarisGravityOrbRun(
			sessionKey, generation, objectID, run, previousRun,
		)
		return nil, fmt.Errorf("enemyGravityOrbStart: %w", err)
	}
	schedule := campaignNPCGravityOrbSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceID: objectID,
		actionGeneration: npc.ActionGeneration, timestamp: timestamp,
		nextDelay:   max(profile.Cast.HitDelay, profile.Cast.ReleaseDelay),
		isCorruptor: isCorruptor, run: run,
	}
	spawnDelay := profile.Cast.HitDelay
	producers := []raknet.ScheduledPacketProducer{
		{Delay: spawnDelay, Produce: schedule.spawn},
		{Delay: spawnDelay + profile.ActivationDelay, Produce: schedule.stable},
		{Delay: spawnDelay + profile.InstabilityDelay, Produce: schedule.unstable},
		{Delay: spawnDelay + profile.Lifetime, Produce: schedule.fizzle},
		{Delay: schedule.nextDelay, Produce: schedule.next},
	}
	firstPullDelay := spawnDelay + profile.ActivationDelay +
		profile.CooldownModifyPeriod
	orbEndDelay := spawnDelay + profile.Lifetime
	for delay := firstPullDelay; delay < orbEndDelay; delay += profile.CooldownModifyPeriod {
		step := campaignNPCGravityOrbPullStep{
			schedule:  schedule,
			timestamp: timestamp + uint64(delay/time.Millisecond),
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: delay, Produce: step.produce,
		})
	}
	sortScheduledPacketProducersByDelay(producers)
	cancel, err := packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.restorePolarisGravityOrbRun(
			sessionKey, generation, objectID, run, previousRun,
		)
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyGravityOrbSchedule: %w", err)
	}
	run.cancel = cancel
	if previousRun != nil && previousRun.cancel != nil {
		previousRun.cancel()
	}
	return startPackets, nil
}

func (r campaignNPCActionRuntime) restorePolarisGravityOrbRun(
	sessionKey string, generation uint64, objectID uint32,
	run *campaignNPCGravityOrbRun, previousRun *campaignNPCGravityOrbRun,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if isFound && peerSession.generation == generation &&
		peerSession.campaignNPCGravityOrbs[objectID] == run {
		if previousRun == nil {
			delete(peerSession.campaignNPCGravityOrbs, objectID)
		} else {
			peerSession.campaignNPCGravityOrbs[objectID] = previousRun
		}
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}
