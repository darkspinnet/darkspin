package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignConeSchedule struct {
	runtime              campaignNPCActionRuntime
	packet               raknet.Packet
	sessionKey           string
	generation           uint64
	actionGeneration     uint64
	objectID             uint32
	timestamp            uint64
	nextDelay            time.Duration
	hitDelay             time.Duration
	plan                 zonenpc.AttackPlan
	laserTargetObjectIDs []uint32
	laserEndpoints       []game.Vec3
	laserZone            *campaignLaserZone
	twinLaserEndpointID  uint32
	twinLaserSecondID    uint32
	isCorruptor          bool
}

func campaignCryosBossChainTargets(
	primary zone.NPCTarget, targets []zone.NPCTarget, radius float32,
	maximumTargetCount uint32,
) []zone.NPCTarget {
	selectedTargets := []zone.NPCTarget{primary}
	selectedObjectIDs := map[uint32]bool{primary.ObjectID: true}
	for uint32(len(selectedTargets)) < maximumTargetCount {
		origin := selectedTargets[len(selectedTargets)-1].Position
		candidateIndex := -1
		candidateDistance := float32(math.MaxFloat32)
		for index, target := range targets {
			if selectedObjectIDs[target.ObjectID] || target.HitPoint <= 0 {
				continue
			}
			distance := target.Position.Sub(origin).Length()
			if distance > radius+target.FootprintRadius || distance >= candidateDistance {
				continue
			}
			candidateIndex = index
			candidateDistance = distance
		}
		if candidateIndex < 0 {
			break
		}
		candidate := targets[candidateIndex]
		selectedTargets = append(selectedTargets, candidate)
		selectedObjectIDs[candidate.ObjectID] = true
	}
	return selectedTargets
}

func (e campaignConeSchedule) resume(timestamp uint64) ([][]byte, error) {
	return e.runtime.produceEnemyCone(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func isCampaignConeTarget(
	source game.Vec3, facing game.Vec3, target zone.NPCTarget,
	radius float32, angle float32,
) bool {
	deltaX := target.Position.X - source.X
	deltaY := target.Position.Y - source.Y
	distance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if distance <= 0 || distance > radius+target.FootprintRadius {
		return false
	}
	facingLength := float32(math.Hypot(float64(facing.X), float64(facing.Y)))
	if facingLength <= 0 {
		return false
	}
	dot := (deltaX*facing.X + deltaY*facing.Y) / (distance * facingLength)
	minimumDot := float32(math.Cos(float64(angle) * math.Pi / 360))
	return dot >= minimumDot
}

func isCampaignLineTarget(
	source game.Vec3, endpoint game.Vec3, target zone.NPCTarget,
) bool {
	segment := endpoint.Sub(source)
	lengthSquared := segment.X*segment.X + segment.Y*segment.Y + segment.Z*segment.Z
	if lengthSquared <= 0 || target.FootprintRadius < 0 {
		return false
	}
	toTarget := target.Position.Sub(source)
	projection := (toTarget.X*segment.X + toTarget.Y*segment.Y +
		toTarget.Z*segment.Z) / lengthSquared
	if projection < 0 || projection > 1 {
		return false
	}
	closest := source.Add(segment.Scale(projection))
	distance := target.Position.Sub(closest).Length()
	return distance <= target.FootprintRadius
}

func isCampaignLaserPathClear(
	campaignZone *zone.Zone, source game.Vec3, target game.Vec3,
	footprintRadius float32,
) (bool, error) {
	if campaignZone == nil || campaignZone.Navigation() == nil {
		return true, nil
	}
	isDirect, err := zoneaction.NPCPathClear(
		campaignZone.Navigation(), source, target, footprintRadius,
	)
	if err != nil {
		return false, fmt.Errorf("laserPath: %w", err)
	}
	return isDirect, nil
}

func campaignConeDamagePercent(
	source game.Vec3, target game.Vec3, radius float32,
	minimumDamagePercent float32,
) float32 {
	if radius <= 0 || minimumDamagePercent <= 0 || minimumDamagePercent >= 1 {
		return 1
	}
	deltaX := target.X - source.X
	deltaY := target.Y - source.Y
	distance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	distancePercent := min(distance/radius, 1)
	return minimumDamagePercent +
		(1-minimumDamagePercent)*(1-distancePercent)
}

func (e campaignConeSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCAttackGenerationActiveAt(
		e.generation, e.objectID, e.plan.TargetObjectID,
		e.actionGeneration, e.runtime.now(),
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		if e.laserZone != nil {
			return e.finishLaserZone()
		}
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	primary, isPrimaryFound := peerSession.campaignNPCTarget(
		e.generation, e.plan.TargetObjectID,
	)
	if !isSourceFound || !isPrimaryFound {
		e.runtime.registry.mutex.Unlock()
		if e.laserZone != nil {
			return e.finishLaserZone()
		}
		return nil, nil
	}
	isLaserZone := e.plan.Profile.AbilityName == "CitadelSpecialThree_LaserZone"
	isTwinLaser := e.plan.Profile.AbilityName == "TwinLaser"
	if isLaserZone || (isTwinLaser && e.hitDelay == e.plan.Profile.HitDelay) {
		maximumDistance := e.plan.Profile.Range + source.Plan.NPCProfile.FootprintRadius +
			primary.FootprintRadius
		isInRange := primary.Position.Sub(source.Plan.Position).Length() <= maximumDistance
		isPathClear, pathErr := isCampaignLaserPathClear(
			peerSession.zone, source.Plan.Position, primary.Position,
			source.Plan.NPCProfile.FootprintRadius,
		)
		if pathErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyConeLaserPath: %w", pathErr)
		}
		if !isInRange || !isPathClear {
			cleanupPackets, cleanupErr := e.laserZone.finish()
			e.runtime.registry.mutex.Unlock()
			if cleanupErr != nil {
				return nil, fmt.Errorf("laserCancel: %w", cleanupErr)
			}
			e.runtime.releaseActionGeneration(
				e.sessionKey, e.generation, e.objectID, e.actionGeneration,
			)
			hitDelay := e.plan.Profile.HitDelay
			if e.hitDelay > 0 {
				hitDelay = e.hitDelay
			}
			if e.isCorruptor {
				// Retire the dodged beam and resume the boss's cooldown-aware
				// selector. Re-entering the cone producer after releasing its
				// action owner leaves the boss permanently inactive.
				nextPackets, nextErr := e.runtime.restartCorruptorAction(
					e.packet, e.sessionKey, e.generation, source,
					e.timestamp+uint64(hitDelay/time.Millisecond),
				)
				if nextErr != nil {
					return nil, fmt.Errorf("laserRestart: %w", nextErr)
				}
				return append(cleanupPackets, nextPackets...), nil
			}
			resumePackets, err := e.resume(
				e.timestamp + uint64(hitDelay/time.Millisecond),
			)
			if err != nil {
				return cleanupPackets, fmt.Errorf("enemyConeLaserResume: %w", err)
			}
			if !isTwinLaser || e.hitDelay <= e.plan.Profile.HitDelay {
				return append(cleanupPackets, resumePackets...), nil
			}
			endPackets, err := npcraknet.TwinLaserEnd(
				e.plan, e.twinLaserEndpointID,
				e.timestamp+uint64(hitDelay/time.Millisecond),
			)
			if err != nil {
				return nil, fmt.Errorf("enemyConeTwinLaserCancel: %w", err)
			}
			return append(endPackets, resumePackets...), nil
		}
	}
	if peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("enemy cone random unavailable")
	}
	facing := game.Vec3{
		X: primary.Position.X - source.Plan.Position.X,
		Y: primary.Position.Y - source.Plan.Position.Y,
	}
	packets := make([][]byte, 0)
	isLightningBeam := e.plan.Profile.AbilityName == "CryosBasicLightningRanged"
	isCryosBossChain := e.plan.Profile.AbilityName == "ChainLightningBolt"
	isMaserBeam := e.plan.Profile.AbilityName == "ScaldronBasicMaser_Shot"
	isOrcusDisease := e.plan.Profile.AbilityName == "VerdanthBossDiseaseCone"
	isOrcusGroundSlam := e.plan.Profile.AbilityName == "VerdanthBoss_GroundSlam"
	if isCryosBossChain {
		chainPackets, err := npcraknet.CryosBossChainStart(e.plan)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyConeCryosChainStart: %w", err)
		}
		packets = append(packets, chainPackets...)
	}
	if isOrcusGroundSlam {
		areaPacket, err := npcraknet.PositionedEffect(
			"ver_boss_thorn_barrage_physical_spikes.ServerEventDef",
			source.Plan.Position,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyConeOrcusArea: %w", err)
		}
		packets = append(packets, areaPacket)
	}
	if isLightningBeam {
		beamPacket, err := npcraknet.BeamEffect(e.plan)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyConeBeam: %w", err)
		}
		packets = append(packets, beamPacket)
	}
	if isTwinLaser {
		beamPlan := e.plan
		endpoints, endpointErr := e.twinLaserEndpoints(peerSession.zone)
		if endpointErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("laserEndpoints: %w", endpointErr)
		}
		e.laserEndpoints = endpoints[:]
		if e.hitDelay == e.plan.Profile.HitDelay {
			beamPackets, err := npcraknet.TwinLaserPairStart(
				beamPlan, [2]uint32{e.twinLaserEndpointID, e.twinLaserSecondID}, endpoints,
				e.timestamp+uint64(e.hitDelay/time.Millisecond),
			)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyConeTwinLaserStart: %w", err)
			}
			packets = append(packets, beamPackets...)
			if peerSession.campaignTwinLaserEndpointIDs == nil {
				peerSession.campaignTwinLaserEndpointIDs = make(map[uint32]uint32)
			}
			peerSession.campaignTwinLaserEndpointIDs[e.objectID] = e.twinLaserEndpointID
		}
	}
	if isLaserZone && e.hitDelay == e.plan.Profile.HitDelay {
		beamPackets, err := npcraknet.LaserZoneStart(e.plan, e.laserZone.objectIDs, e.laserEndpoints)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyConeLaserBeam: %w", err)
		}
		e.laserZone.isActive = true
		packets = append(packets, beamPackets...)
	}
	maserEndpoint := primary.Position
	if isMaserBeam {
		length := float32(math.Hypot(float64(facing.X), float64(facing.Y)))
		if length > 0 {
			maserEndpoint = source.Plan.Position.Add(game.Vec3{
				X: facing.X / length * e.plan.Profile.ProjectileDistance,
				Y: facing.Y / length * e.plan.Profile.ProjectileDistance,
			})
		}
		beamPacket, err := npcraknet.PositionedEffect(
			e.plan.Profile.TrailEffectName, maserEndpoint,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyConeMaserBeam: %w", err)
		}
		packets = append(packets, beamPacket)
	}
	poisonPlans := make([]zonenpc.AttackPlan, 0)
	shockPlans := make([]zonenpc.AttackPlan, 0)
	statDelta := sporenet.PlayerStatDelta{}
	targets := peerSession.zone.LiveNPCTargets()
	if isCryosBossChain {
		targets = campaignCryosBossChainTargets(
			primary, targets, e.plan.Profile.Radius,
			e.plan.Profile.MaximumTargetCount,
		)
	}
	if e.plan.Profile.MaximumTargetCount > 0 {
		for index, target := range targets {
			if target.ObjectID != primary.ObjectID {
				continue
			}
			targets[0], targets[index] = targets[index], targets[0]
			break
		}
	}
	selectedTargetCount := uint32(0)
	for _, target := range targets {
		if e.plan.Profile.MaximumTargetCount > 0 &&
			selectedTargetCount >= e.plan.Profile.MaximumTargetCount {
			break
		}
		isTargeted := isCampaignConeTarget(
			source.Plan.Position, facing, target,
			e.plan.Profile.Radius, e.plan.Profile.Angle,
		)
		if isLaserZone {
			isTargeted = false
			for _, endpoint := range e.laserEndpoints {
				if isCampaignLineTarget(e.plan.SourcePosition, endpoint, target) {
					isTargeted = true
					break
				}
			}
		} else if isTwinLaser {
			isTargeted = false
			for index, endpoint := range e.laserEndpoints {
				if isCampaignLineTarget(e.twinLaserOrigin(source, index), endpoint, target) {
					isClear, pathErr := isCampaignLaserPathClear(peerSession.zone, source.Plan.Position, target.Position, 0.1)
					if pathErr != nil {
						e.runtime.registry.mutex.Unlock()
						return packets, fmt.Errorf("twinLaserHitPath: %w", pathErr)
					}
					if !isClear {
						continue
					}
					isTargeted = true
					break
				}
			}
		} else if isLightningBeam {
			isTargeted = isCampaignLineTarget(
				source.Plan.Position, primary.Position, target,
			)
		} else if isMaserBeam {
			isTargeted = isCampaignLineTarget(
				source.Plan.Position, maserEndpoint, target,
			)
		}
		if isCryosBossChain {
			isTargeted = true
		}
		if !isTargeted {
			continue
		}
		if isCryosBossChain {
			if selectedTargetCount > 0 {
				chainPlan := e.plan
				chainPlan.SourceObjectID = targets[selectedTargetCount-1].ObjectID
				chainPlan.TargetObjectID = target.ObjectID
				chainPlan.Profile.TrailEffectName =
					e.plan.Profile.SecondaryTrailEffectName
				chainPacket, marshalErr := npcraknet.BeamEffect(chainPlan)
				if marshalErr != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("enemyConeCryosChain: %w", marshalErr)
				}
				packets = append(packets, chainPacket)
			}
		}
		if isOrcusDisease {
			selectedTargetCount++
			poisonPlans = append(poisonPlans, zonenpc.AttackPlan{
				SourceObjectID: e.objectID, TargetObjectID: target.ObjectID,
				SourcePosition: source.Plan.Position, TargetPosition: target.Position,
				Profile: e.plan.Profile,
			})
			continue
		}
		plan, err := zonenpc.PlanAreaAttackWithProfile(
			source, target.ObjectID, target.Position, e.plan.Profile,
		)
		if err != nil {
			continue
		}
		selectedTargetCount++
		damagePercent := campaignConeDamagePercent(
			source.Plan.Position, target.Position, e.plan.Profile.Radius,
			e.plan.Profile.MinimumDamagePercent,
		)
		if isLaserZone {
			damagePercent = 1
		}
		if isCryosBossChain {
			damagePercent = float32(math.Pow(0.7, float64(selectedTargetCount-1)))
		}
		plan.Profile.MinimumDamage *= damagePercent
		plan.Profile.MaximumDamage *= damagePercent
		if plan.Profile.ImpactEffectName != "" {
			impactPackets, marshalErr := npcraknet.AttackImpact(plan)
			if marshalErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyConeImpact: %w", marshalErr)
			}
			packets = append(packets, impactPackets)
		}
		result, err := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			source.Plan.NPCProfile.CriticalRating, e.runtime.program.Critical,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyConeCommit: %w", err)
		}
		hitDelay := e.plan.Profile.HitDelay
		if e.hitDelay > 0 {
			hitDelay = e.hitDelay
		}
		hitPackets, targetStatDelta, isApplied, err :=
			e.runtime.applyEnemyAreaAttackDamage(
				&peerSession, e.generation, plan, result,
				e.timestamp+uint64(hitDelay/time.Millisecond),
			)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyConeDamage: %w", err)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		if isApplied && isCryosBossChain {
			shockPlans = append(shockPlans, plan)
		}
		if isApplied && plan.Profile.ModifierName == "Poison" &&
			plan.Profile.ModifierChance > 0 {
			liveTarget, isLiveTargetFound := peerSession.campaignNPCTarget(
				e.generation, target.ObjectID,
			)
			isPoisonEligible := isLiveTargetFound && liveTarget.HitPoint > 0 &&
				liveTarget.HitPoint < target.HitPoint
			if isPoisonEligible {
				draw := peerSession.zone.NPCRandom().Float64() * 100
				if draw < float64(plan.Profile.ModifierChance) {
					poisonPlans = append(poisonPlans, plan)
				}
			}
		}
		if plan.Profile.ForcedMovementDistance > 0 &&
			peerSession.deployedHitPoint() > 0 {
			forcedPackets, forcedErr := e.runtime.applyEnemyForcedMovement(
				&peerSession, plan, target,
				e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond),
			)
			if forcedErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyConeKnockback: %w", forcedErr)
			}
			packets = append(packets, forcedPackets...)
		}
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	err := e.runtime.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		return nil, fmt.Errorf("enemyConeStats: %w", err)
	}
	for _, plan := range poisonPlans {
		modifierPackets, modifierErr := e.runtime.applyCampaignNPCPoison(
			e.packet, e.sessionKey, e.generation, plan,
			e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond),
		)
		if modifierErr != nil {
			return nil, fmt.Errorf("enemyConePoison: %w", modifierErr)
		}
		packets = append(packets, modifierPackets...)
	}
	for _, plan := range shockPlans {
		modifierPackets, modifierErr := e.runtime.applyCampaignNPCTimedModifier(
			e.packet, e.sessionKey, e.generation, plan,
			e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond),
		)
		if modifierErr != nil {
			return nil, fmt.Errorf("enemyConeShock: %w", modifierErr)
		}
		packets = append(packets, modifierPackets...)
	}
	if isCryosBossChain {
		cleanupDelay := e.plan.Profile.ReleaseDelay - e.plan.Profile.HitDelay
		if cleanupDelay <= 0 {
			return nil, errors.New("enemy cone Cryos chain cleanup delay invalid")
		}
		err := scheduleNPCProducer(e.runtime.registry, e.packet, cleanupDelay, e.releaseCryosBossChain)
		if err != nil {
			return nil, fmt.Errorf("enemyConeCryosChainCleanupSchedule: %w", err)
		}
	}
	return packets, nil
}

func (e campaignConeSchedule) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	isSourceCurrent := isCurrent &&
		peerSession.campaignTwinLaserEndpointIDs[e.objectID] == e.twinLaserEndpointID
	if isSourceCurrent {
		delete(peerSession.campaignTwinLaserEndpointIDs, e.objectID)
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := npcraknet.TwinLaserSweepCleanup(
		e.plan, e.twinLaserEndpointID,
		e.timestamp+uint64(e.nextDelay/time.Millisecond), isSourceCurrent,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyConeTwinLaserEnd: %w", err)
	}
	if e.twinLaserSecondID != 0 {
		secondPackets, secondErr := npcraknet.TwinLaserCleanup(e.plan, e.twinLaserSecondID,
			e.timestamp+uint64(e.nextDelay/time.Millisecond), false)
		if secondErr != nil {
			return packets, fmt.Errorf("laserSecondCleanup: %w", secondErr)
		}
		packets = append(packets, secondPackets...)
	}
	return packets, nil
}

func (e campaignConeSchedule) releaseCryosBossChain() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := npcraknet.CryosBossChainEnd(e.plan)
	if err != nil {
		return nil, fmt.Errorf("enemyConeCryosChainEnd: %w", err)
	}
	return packets, nil
}

func (e campaignConeSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.nextDelay/time.Millisecond)
	if e.laserZone != nil {
		packets, err := e.finishLaserZone()
		if err != nil {
			return nil, fmt.Errorf("laserNextCleanup: %w", err)
		}
		nextPackets, err := e.resume(timestamp)
		if err != nil {
			return packets, fmt.Errorf("laserNext: %w", err)
		}
		return append(packets, nextPackets...), nil
	}
	if e.isCorruptor {
		step := campaignNPCFirstActionStep{
			runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
			generation: e.generation, objectID: e.objectID,
			actionGeneration: e.actionGeneration, timestamp: timestamp,
		}
		return step.produce()
	}
	if e.plan.Profile.AbilityName == "VerdanthBossDiseaseCone" ||
		e.plan.Profile.AbilityName == "VerdanthBoss_GroundSlam" {
		return e.runtime.produceDronePunch(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	}
	if e.plan.Profile.AbilityName == "ZelemBossPush" {
		return e.runtime.producePolarisPhaseTransition(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
			polarisNextMarkSeeker,
		)
	}
	if e.plan.Profile.AbilityName == "NomadBioSpecialTwoSwipe" {
		step := campaignNPCFirstActionStep{
			runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
			generation: e.generation, objectID: e.objectID, timestamp: timestamp,
			actionGeneration: e.actionGeneration,
		}
		return step.produce()
	}
	return e.resume(timestamp)
}

func (r campaignNPCActionRuntime) produceEnemyCone(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	launchPackets, isLaunchHandled, launchErr := r.produceArcturusLaunch(packet, sessionKey, generation, objectID, timestamp)
	if launchErr != nil {
		return nil, fmt.Errorf("enemyConeArcturus: %w", launchErr)
	}
	if isLaunchHandled {
		return launchPackets, nil
	}
	phasePackets, isPhaseHandled, phaseErr := r.produceCitadelSpecialFour(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if phaseErr != nil {
		return nil, fmt.Errorf("enemyConeCitadelSpecialFour: %w", phaseErr)
	}
	if isPhaseHandled {
		return phasePackets, nil
	}
	resume := campaignConeSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyConeStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	cleansePackets, isCleanseSelected, err := r.produceScaldronBasicMaserCleanse(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyConeCleanse: %w", err)
	}
	if isCleanseSelected {
		return cleansePackets, nil
	}
	consumePackets, isConsumeHandled, err := r.produceCarrionConsume(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyConeConsume: %w", err)
	}
	if isConsumeHandled {
		return consumePackets, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(generation, enemy.TargetObjectID)
	profile, isProfileFound := campaignNPCActionProfile(
		enemy.Plan, target.Position, target.FootprintRadius,
	)
	nextDelay := profile.Cooldown
	if profile.AbilityName == "ZelemBossPush" {
		nextDelay = max(profile.HitDelay, profile.ReleaseDelay)
	}
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound || !isProfileFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	resume.actionGeneration = enemy.ActionGeneration
	_, resume.isCorruptor = zonenpc.ScaldronBossMeleeProfile(
		enemy.Plan.NounName, profile,
	)
	if resume.isCorruptor {
		nextDelay = max(profile.HitDelay, profile.ReleaseDelay)
	}
	if profile.AbilityName == "CryosBasicLightningRanged" &&
		target.Position.Sub(enemy.Plan.Position).Length() < profile.MinimumRange {
		fizzlePacket, fizzleErr := npcraknet.AnimationState(
			objectID, profile.EndAnimationName, timestamp,
		)
		if fizzleErr != nil {
			return nil, fmt.Errorf("enemyConeFizzleMarshal: %w", fizzleErr)
		}
		const fizzleDelay = 2300 * time.Millisecond
		step := campaignElectronBursterFleeStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, objectID: objectID,
			timestamp: timestamp + uint64(fizzleDelay/time.Millisecond),
		}
		cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
			Delay: fizzleDelay, Produce: step.produce,
		}})
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyConeFizzleSchedule: %w", scheduleErr)
		}
		return [][]byte{fizzlePacket}, nil
	}
	if profile.Family == zonenpc.ActionLeap &&
		profile.AbilityName == "NomadBioSpecialTwoJumpAttack" {
		return r.produceEnemyLeap(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if profile.Family == zonenpc.ActionMelee &&
		profile.AbilityName == "CryosBossMelee" {
		return r.produceEnemyMelee(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if profile.AbilityName == "SawBladeShot" {
		return r.produceZelemShot(packet, sessionKey, generation, objectID, timestamp)
	}
	if profile.Family != zonenpc.ActionCone {
		return nil, nil
	}
	isLaserZone := profile.AbilityName == "CitadelSpecialThree_LaserZone"
	if isLaserZone {
		isPathClear, pathErr := isCampaignLaserPathClear(
			peerSession.zone, enemy.Plan.Position, target.Position,
			enemy.Plan.NPCProfile.FootprintRadius,
		)
		if pathErr != nil {
			return nil, fmt.Errorf("enemyConeLaserAdmission: %w", pathErr)
		}
		if !isPathClear {
			profile.Range = 1
		}
	}
	if profile.AbilityName == "TwinLaser" && zonenpc.ArcturusRank(enemy.Plan.NounName) > 0 {
		// Chunk 565: one-second wind-up, 0.1 seconds of hold per metre,
		// then 1.9 seconds of end animation (including the 0.5-second sweep).
		holdDuration := time.Duration(float64(target.Position.Sub(enemy.Plan.Position).Length()) * 0.1 * float64(time.Second))
		profile.ReleaseDelay = profile.HitDelay + holdDuration + 1900*time.Millisecond
		profile.Cooldown = profile.ReleaseDelay
		nextDelay = profile.ReleaseDelay
	}
	var plan zonenpc.AttackPlan
	if profile.AbilityName == "VerdanthBossDiseaseCone" {
		// The breath applies disease without a direct damage hit.
		plan, err = zonenpc.PlanControlWithProfile(
			enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
		)
	} else {
		plan, err = zonenpc.PlanAttackWithProfile(
			enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
		)
	}
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("conePursuit: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, fmt.Errorf("coneAttack: %w", err)
		}
		packets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyConePursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, resume.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyConePursuitSchedule: %w", scheduleErr)
		}
		return packets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyConeStart: %w", err)
	}
	resume.plan = plan
	resume.nextDelay = nextDelay
	if profile.AbilityName == "TwinLaser" {
		endpointObjectID, reserveErr := peerSession.zone.ReserveObjectIDs(2)
		if reserveErr != nil {
			return nil, fmt.Errorf("enemyConeTwinLaserEndpoint: %w", reserveErr)
		}
		resume.twinLaserEndpointID = endpointObjectID
		resume.twinLaserSecondID = endpointObjectID + 1
	}
	if isLaserZone {
		facing := target.Position.Sub(enemy.Plan.Position)
		laserTargets := peerSession.zone.LiveNPCTargets()
		for index, candidate := range laserTargets {
			if candidate.ObjectID != target.ObjectID {
				continue
			}
			laserTargets[0], laserTargets[index] = laserTargets[index], laserTargets[0]
			break
		}
		for _, candidate := range laserTargets {
			if uint32(len(resume.laserTargetObjectIDs)) >= profile.MaximumTargetCount {
				break
			}
			if !isCampaignConeTarget(
				enemy.Plan.Position, facing, candidate, profile.Range, profile.Angle,
			) {
				continue
			}
			isPathClear, pathErr := isCampaignLaserPathClear(
				peerSession.zone, enemy.Plan.Position, candidate.Position,
				enemy.Plan.NPCProfile.FootprintRadius,
			)
			if pathErr != nil {
				return nil, fmt.Errorf("enemyConeLaserPlacement: %w", pathErr)
			}
			if !isPathClear {
				continue
			}
			resume.laserTargetObjectIDs = append(
				resume.laserTargetObjectIDs, candidate.ObjectID,
			)
			resume.laserEndpoints = append(resume.laserEndpoints, candidate.Position)
		}
		if len(resume.laserEndpoints) == 0 {
			r.releaseActionGeneration(
				sessionKey, generation, objectID, enemy.ActionGeneration,
			)
			return nil, nil
		}
		markerCount := uint32(len(resume.laserEndpoints) + 1)
		firstObjectID, reserveErr := peerSession.zone.ReserveObjectIDs(markerCount)
		if reserveErr != nil {
			return nil, fmt.Errorf("laserMarkers: %w", reserveErr)
		}
		resume.laserZone = &campaignLaserZone{objectIDs: make([]uint32, markerCount)}
		for index := range resume.laserZone.objectIDs {
			resume.laserZone.objectIDs[index] = firstObjectID + uint32(index)
		}
	}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: resume.hit},
		{Delay: nextDelay, Produce: resume.next},
	}
	if profile.AbilityName == "TwinLaser" {
		producers = producers[:0]
		channelDuration := time.Duration(
			float64(plan.TargetPosition.Sub(plan.SourcePosition).Length())*
				0.1*float64(time.Second),
		) + 500*time.Millisecond
		channelTickCount := uint32(math.Ceil(
			float64(channelDuration) / float64(profile.TickDuration),
		))
		channelTickCount = max(channelTickCount, 1)
		for tickIndex := uint32(0); tickIndex < channelTickCount; tickIndex++ {
			delay := profile.HitDelay + time.Duration(tickIndex)*profile.TickDuration
			pulse := resume
			pulse.hitDelay = delay
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: delay, Produce: pulse.hit,
			})
		}
		finishDelay := profile.HitDelay + channelDuration
		endAnimation := resume
		endAnimation.hitDelay = finishDelay - 500*time.Millisecond
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: endAnimation.hitDelay, Produce: endAnimation.endTwinLaserAnimation,
		})
		for delay := profile.HitDelay + 50*time.Millisecond; delay < finishDelay; delay += 50 * time.Millisecond {
			movement := resume
			movement.hitDelay = delay
			producers = append(producers, raknet.ScheduledPacketProducer{Delay: delay, Produce: movement.moveTwinLaser})
		}
		finish := resume
		finish.nextDelay = finishDelay
		producers = append(producers,
			raknet.ScheduledPacketProducer{Delay: finishDelay, Produce: finish.finish},
			raknet.ScheduledPacketProducer{Delay: nextDelay, Produce: resume.next},
		)
	}
	if profile.AbilityName == "CitadelSpecialThree_LaserZone" {
		producers = producers[:0]
		for delay := profile.HitDelay; delay < nextDelay; delay += time.Second {
			pulse := resume
			pulse.hitDelay = delay
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: delay, Produce: pulse.hit,
			})
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: nextDelay, Produce: resume.next,
		})
	}
	var intangibleRun *campaignNPCIntangibleRun
	if profile.AbilityName == "StagnantNovaAbove" {
		modifierPackets, run, modifierErr := r.prepareStagnantNovaIntangible(
			sessionKey, generation, plan, timestamp,
		)
		if modifierErr != nil {
			return nil, fmt.Errorf("enemyConeIntangible: %w", modifierErr)
		}
		startPackets = append(startPackets, modifierPackets...)
		intangibleRun = run
		if run != nil {
			expiry := campaignNPCIntangibleExpiry{
				runtime: r, sessionKey: sessionKey,
				generation: generation, run: run,
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: profile.EmergeDelay, Produce: expiry.produce,
			})
		}
	}
	sortScheduledPacketProducersByDelay(producers)
	cancel, err := scheduleNPCProducers(r.registry, packet, producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.rollbackStagnantNovaIntangible(sessionKey, generation, intangibleRun)
		return nil, fmt.Errorf("enemyConeSchedule: %w", err)
	}
	if intangibleRun != nil && !r.activateStagnantNovaIntangible(
		sessionKey, generation, intangibleRun, cancel,
	) {
		cancel()
		r.rollbackStagnantNovaIntangible(sessionKey, generation, intangibleRun)
		return nil, nil
	}
	return startPackets, nil
}
