package gameplay

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const missileTempestScatterRadius = float32(6)

type heroBurstSchedule struct {
	runtime                    campaignAbilityCommandRuntime
	packet                     raknet.Packet
	sessionKey                 string
	generation                 uint64
	sourceObjectID             uint32
	targetObjectID             uint32
	targetPosition             raknet.Vector3
	sourcePosition             game.Vec3
	firstProjectileObjectID    uint32
	previousProjectileObjectID uint32
	creatureIndex              uint32
	previousManaPoint          float32
	passiveKillStack           uint32
	creature                   game.GameplayCreature
	definition                 sim.AbilityDefinition
	admissionRange             float32
	plan                       zoneability.BasicPlan
	binding                    game.GameplayBinding
	run                        *abilityraknet.BurstRun
	cooldownReservation        zoneability.CooldownReservation
	releaseReservation         zoneaction.ReleaseReservation
	releaseResponse            []byte
	hitCounts                  map[uint32]uint32
	webbedStatusTargets        map[uint32]bool
	shotTargetObjectIDs        map[int]uint32
	shotSourcePositions        map[int]game.Vec3
	shotTargetPositions        map[int]raknet.Vector3
	shotTravelDistances        map[int]float32
	shotGeometries             map[int]zonenpc.ProjectileGeometry
}

type heroBurstStep struct {
	schedule heroBurstSchedule
	index    int
	deadline time.Duration
	isLaunch bool
}

func (e heroBurstSchedule) applyLifeSteal(
	peerSession *gameplayPeerSession, damage float32,
) ([][]byte, float32, error) {
	if e.definition.LifeSteal <= 0 || damage <= 0 {
		return nil, 0, nil
	}
	healing, err := game.ApplyTargetHealingReduction(
		damage*e.definition.LifeSteal, e.creature.HealingTargetProfile,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("lifeStealReduction: %w", err)
	}
	maximumHitPoint, _, err := peerSession.deployedResourceMaximum()
	if err != nil {
		return nil, 0, fmt.Errorf("lifeStealMaximum: %w", err)
	}
	previousHitPoint := peerSession.deployedHitPoint()
	hitPoint := min(maximumHitPoint, previousHitPoint+healing)
	healedAmount := hitPoint - previousHitPoint
	if healedAmount <= 0 {
		return nil, 0, nil
	}
	_, err = peerSession.setDeployedHitPoints(hitPoint)
	if err != nil {
		return nil, 0, fmt.Errorf("lifeStealCommit: %w", err)
	}
	packets, err := abilityraknet.ChannelDrainHealing(
		e.sourceObjectID, hitPoint, healedAmount,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("lifeStealMarshal: %w", err)
	}
	resourcePacket, err := peerSession.marshalCampaignCharacterResource(
		peerSession.deployedCreatureIndex,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("lifeStealResource: %w", err)
	}
	return append(packets, resourcePacket), healedAmount, nil
}

func (e heroBurstSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroBurstAttacks[e.firstProjectileObjectID] == e.run
}

func (e heroBurstSchedule) producer(
	index int, deadline time.Duration, isLaunch bool,
) raknet.ScheduledPacketProducer {
	step := heroBurstStep{
		schedule: e, index: index, deadline: deadline,
		isLaunch: isLaunch,
	}
	return raknet.ScheduledPacketProducer{
		Delay: deadline, Produce: step.produce,
	}
}

func (e heroBurstStep) produce() ([][]byte, error) {
	if e.isLaunch {
		return e.produceLaunch()
	}
	return e.produceImpact()
}

func (e heroBurstStep) produceLaunch() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound := schedule.runtime.registry.sessions[schedule.sessionKey]
	if !schedule.isCurrent(peerSession, isFound) {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	var presentationPackets [][]byte
	if schedule.definition.Name == "PlasmaRandom_WebbedLightning" && e.index == 0 {
		animationPacket, animationErr := abilityraknet.Animation(
			schedule.sourceObjectID, schedule.definition.SecondaryAnimationName,
			schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond),
		)
		if animationErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroBurstCastAnimation: %w", animationErr)
		}
		presentationPackets = append(presentationPackets, animationPacket)
	}
	targetObjectID := schedule.targetObjectID
	targetPosition := schedule.targetPosition
	actorPosition := raknet.Vector3(schedule.sourcePosition)
	if schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingArc {
		targetObjectID, targetPosition = schedule.planArcTarget(peerSession, e.index)
		schedule.shotTargetObjectIDs[e.index] = targetObjectID
	} else if schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingRadial {
		targetPosition = schedule.planTargetPosition(peerSession, e.index)
	} else if schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingCursorArea {
		targetObjectID = 0
		targetPosition = schedule.planTargetPosition(peerSession, e.index)
		if schedule.definition.Name == "MissileTempestActive" {
			actorPosition = targetPosition
			actorPosition.Z += 15
		}
	}
	if schedule.definition.Name == "LightningTempest_Active" {
		selectedTargetObjectID, selectedPosition := schedule.selectRetargetShot(peerSession)
		targetObjectID = selectedTargetObjectID
		schedule.shotTargetObjectIDs[e.index] = targetObjectID
		targetPosition = selectedPosition
		actorPosition = raknet.Vector3(selectedPosition)
		actorPosition.Z += 10
	}
	geometry := zonenpc.ProjectileGeometry{}
	if targetObjectID != 0 {
		target, isTargetFound := peerSession.zone.NPCs().NPC(targetObjectID)
		if isTargetFound && !target.IsDefeated && target.HitPoint > 0 {
			var geometryErr error
			geometry, _, geometryErr = campaignTargetProjectileGeometry(
				schedule.runtime.program, target,
			)
			if geometryErr != nil {
				schedule.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("heroBurstGeometry[%d]: %w", e.index, geometryErr)
			}
			if schedule.definition.Name == "LightningTempest_Active" {
				targetPosition = raknet.Vector3(campaignProjectileAimPosition(
					target.Plan.Position, target.Plan.NPCProfile.FootprintRadius,
					geometry,
				))
				actorPosition = targetPosition
				actorPosition.Z += 10
			}
		}
	}
	facing := zoneability.ProjectileDirection(actorPosition, targetPosition)
	launchPosition := campaignProjectileLaunchPosition(
		game.Vec3(actorPosition), game.Vec3(targetPosition),
		peerSession.deployedCampaignFootprintRadius(),
	)
	schedule.shotSourcePositions[e.index] = game.Vec3(actorPosition)
	schedule.shotTargetPositions[e.index] = targetPosition
	schedule.shotTravelDistances[e.index] = zoneability.Distance(
		launchPosition, game.Vec3(targetPosition),
	)
	schedule.shotGeometries[e.index] = geometry
	isTargetValid := targetObjectID != 0 &&
		schedule.definition.BurstTargeting != sim.ProjectileBurstTargetingRadial &&
		schedule.definition.BurstTargeting != sim.ProjectileBurstTargetingCursorArea
	err := schedule.run.PrepareLaunch(
		e.index, sim.Position(actorPosition), sim.Position(targetPosition),
		sim.Position(facing), targetObjectID, isTargetValid,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstPrepare[%d]: %w", e.index, err)
	}
	if e.index+1 < len(schedule.definition.HitDelays) &&
		schedule.definition.HitDelays[e.index+1] == e.deadline {
		schedule.runtime.registry.mutex.Unlock()
		return presentationPackets, nil
	}
	packets, err := schedule.run.Advance(context.Background(), e.deadline)
	schedule.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("heroBurstLaunch[%d]: %w", e.index, err)
	}
	schedule.runtime.logger.Printf(
		"RakNet projectile trajectory launched kind=hero-burst projectile=%d source=%d target=%d ability=%q shot=%d origin=(%.3f,%.3f,%.3f) launch=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) travel=%.3f",
		schedule.firstProjectileObjectID+uint32(e.index),
		schedule.sourceObjectID, targetObjectID, schedule.definition.Name,
		e.index, actorPosition.X, actorPosition.Y, actorPosition.Z,
		launchPosition.X, launchPosition.Y, launchPosition.Z,
		targetPosition.X, targetPosition.Y, targetPosition.Z,
		schedule.shotTravelDistances[e.index],
	)
	return append(presentationPackets, packets...), nil
}

func (e heroBurstStep) produceImpact() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound := schedule.runtime.registry.sessions[schedule.sessionKey]
	if !schedule.isCurrent(peerSession, isFound) {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingRadial ||
		schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingArc ||
		schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingCursorArea {
		return e.produceRadialImpact(peerSession)
	}
	if schedule.definition.Name == "LightningTempest_Active" {
		return e.produceRetargetImpact(peerSession)
	}
	livePlan, planErr := zoneability.PlanBasic(zoneability.BasicCommand{
		NPCs: peerSession.zone.NPCs(), SourceObjectID: schedule.sourceObjectID,
		TargetObjectID: schedule.targetObjectID,
		AbilityID:      schedule.plan.AbilityID,
		SourcePosition: game.Vec3(peerSession.playerPosition),
		MaximumRange:   schedule.admissionRange,
		Definition:     schedule.definition,
		Damage:         schedule.plan.Damage,
	})
	liveNPC, isTargetFound :=
		peerSession.zone.NPCs().NPC(schedule.targetObjectID)
	isHitChanceAccepted := schedule.definition.ShotHitChance <= 0 ||
		peerSession.zone.Population().Random().Float64() <=
			float64(schedule.definition.ShotHitChance)
	if planErr != nil || !isTargetFound || !isHitChanceAccepted {
		impactPosition := schedule.planTargetPosition(peerSession, e.index)
		packets, err := schedule.run.ResolveCollision(
			context.Background(), e.index, e.deadline, false, false,
			0, schedule.plan.Damage.Maximum, false,
			sim.Position(impactPosition), sim.Position{X: 1},
		)
		schedule.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("heroBurstMiss[%d]: %w", e.index, err)
		}
		return packets, nil
	}
	shotSourcePosition := schedule.shotSourcePositions[e.index]
	shotTargetPosition := schedule.shotTargetPositions[e.index]
	shotGeometry := schedule.shotGeometries[e.index]
	launchPosition := campaignProjectileLaunchPosition(
		shotSourcePosition, game.Vec3(shotTargetPosition),
		peerSession.deployedCampaignFootprintRadius(),
	)
	collision, err := zonenpc.ResolveProjectileCollision(
		launchPosition, game.Vec3(shotTargetPosition), liveNPC.Plan.Position,
		schedule.shotTravelDistances[e.index], shotGeometry,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstCollision[%d]: %w", e.index, err)
	}
	if !collision.IsDirectHit {
		packets, resolveErr := schedule.run.ResolveCollision(
			context.Background(), e.index, e.deadline, false, false,
			0, schedule.plan.Damage.Maximum, false, collision.Position,
			sim.Position(zoneability.ProjectileDirection(
				raknet.Vector3(shotSourcePosition), shotTargetPosition,
			)),
		)
		schedule.runtime.registry.mutex.Unlock()
		if resolveErr != nil {
			return nil, fmt.Errorf("heroBurstDodge[%d]: %w", e.index, resolveErr)
		}
		schedule.runtime.logger.Printf(
			"RakNet projectile trajectory resolved kind=hero-burst projectile=%d source=%d target=%d ability=%q shot=%d outcome=miss aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) endpoint=(%.3f,%.3f,%.3f)",
			schedule.firstProjectileObjectID+uint32(e.index),
			schedule.sourceObjectID, schedule.targetObjectID,
			schedule.definition.Name, e.index, shotTargetPosition.X,
			shotTargetPosition.Y, shotTargetPosition.Z, liveNPC.Plan.Position.X,
			liveNPC.Plan.Position.Y, liveNPC.Plan.Position.Z,
			collision.Position.X, collision.Position.Y, collision.Position.Z,
		)
		return packets, nil
	}
	result, err := zoneability.CommitBasic(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
		livePlan, schedule.creature, peerSession.binding.Difficulty,
		schedule.runtime.program.Critical,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstCommit[%d]: %w", e.index, err)
	}
	transition, err := peerSession.applyCampaignDamageTransition(result.Damage)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstTransition[%d]: %w", e.index, err)
	}
	healingPackets, healedAmount, err := schedule.applyLifeSteal(
		&peerSession, result.Damage.Damage,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstLifeSteal[%d]: %w", e.index, err)
	}
	impactPosition := raknet.Vector3(collision.Position)
	facing := zoneability.ProjectileDirection(
		raknet.Vector3(shotSourcePosition), impactPosition,
	)
	packets, err := schedule.run.ResolveCollision(
		context.Background(), e.index, e.deadline, true, true,
		result.Damage.PreviousHealth, result.Damage.Damage,
		result.IsCritical, sim.Position(impactPosition), sim.Position(facing),
	)
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	schedule.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("heroBurstHit[%d]: %w", e.index, err)
	}
	schedule.runtime.logger.Printf(
		"RakNet projectile trajectory resolved kind=hero-burst projectile=%d source=%d target=%d ability=%q shot=%d outcome=hit aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) contact=(%.3f,%.3f,%.3f)",
		schedule.firstProjectileObjectID+uint32(e.index),
		schedule.sourceObjectID, schedule.targetObjectID,
		schedule.definition.Name, e.index, shotTargetPosition.X,
		shotTargetPosition.Y, shotTargetPosition.Z, liveNPC.Plan.Position.X,
		liveNPC.Plan.Position.Y, liveNPC.Plan.Position.Z,
		impactPosition.X, impactPosition.Y, impactPosition.Z,
	)
	packets = filterProjectileCombatPackets(packets)
	timestamp := schedule.packet.SourceTime + uint64(e.deadline/time.Millisecond)
	resultPackets, err := schedule.runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp, schedule.binding,
		[]zoneability.AreaResult{{
			Snapshot: liveNPC, Damage: result.Damage,
			IsCritical: result.IsCritical, Definition: schedule.definition,
		}}, []campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("heroBurstPublish[%d]: %w", e.index, err)
	}
	packets = append(packets, resultPackets...)
	packets = append(packets, healingPackets...)
	if healedAmount > 0 {
		err = schedule.runtime.stats.Record(
			context.Background(), schedule.binding,
			sporenet.PlayerStatDelta{
				PVEHealing:         float64(healedAmount),
				PVEHealingReceived: float64(healedAmount),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("heroBurstHealingStats[%d]: %w", e.index, err)
		}
	}
	return packets, nil
}

func (e heroBurstSchedule) selectRetargetShot(
	peerSession gameplayPeerSession,
) (uint32, raknet.Vector3) {
	random := peerSession.zone.Population().Random()
	isHit := e.definition.ShotHitChance <= 0 ||
		random.Float64() <= float64(e.definition.ShotHitChance)
	if isHit {
		candidate := make([]zonenpc.Snapshot, 0)
		for _, npc := range peerSession.zone.NPCs().LiveSnapshots() {
			if npc.Faction != zonenpc.FactionNonPlayerAligned || npc.Plan.IsFixture ||
				npc.Plan.Position.Sub(e.sourcePosition).Length() > e.definition.Distance {
				continue
			}
			isUsed := false
			for _, objectID := range e.shotTargetObjectIDs {
				if objectID == npc.Plan.ObjectID {
					isUsed = true
					break
				}
			}
			if !isUsed {
				candidate = append(candidate, npc)
			}
		}
		if len(candidate) > 0 {
			index := min(
				len(candidate)-1, int(random.Float64()*float64(len(candidate))),
			)
			return candidate[index].Plan.ObjectID,
				raknet.Vector3(candidate[index].Plan.Position)
		}
	}
	angle := random.Float64() * 2 * math.Pi
	distance := 3 + random.Float64()*15
	return 0, raknet.Vector3{
		X: e.sourcePosition.X + float32(math.Cos(angle)*distance),
		Y: e.sourcePosition.Y + float32(math.Sin(angle)*distance),
		Z: e.sourcePosition.Z,
	}
}

func (e heroBurstStep) produceRetargetImpact(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	schedule := e.schedule
	targetObjectID := schedule.shotTargetObjectIDs[e.index]
	impactPosition := schedule.shotTargetPositions[e.index]
	if targetObjectID == 0 {
		packets, err := schedule.run.ResolveCollision(
			context.Background(), e.index, e.deadline, true, false, 0,
			schedule.plan.Damage.Maximum, false, sim.Position(impactPosition),
			sim.Position{Z: -1},
		)
		schedule.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("heroBurstRetargetMiss[%d]: %w", e.index, err)
		}
		return packets, nil
	}
	liveNPC, isFound := peerSession.zone.NPCs().NPC(targetObjectID)
	if !isFound || liveNPC.IsDefeated || liveNPC.HitPoint <= 0 {
		packets, err := schedule.run.ResolveCollision(
			context.Background(), e.index, e.deadline, false, false, 0,
			schedule.plan.Damage.Maximum, false, sim.Position(impactPosition),
			sim.Position{Z: -1},
		)
		schedule.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("heroBurstRetargetLost[%d]: %w", e.index, err)
		}
		return packets, nil
	}
	shotSourcePosition := schedule.shotSourcePositions[e.index]
	shotGeometry := schedule.shotGeometries[e.index]
	launchPosition := campaignProjectileLaunchPosition(
		shotSourcePosition, game.Vec3(impactPosition),
		peerSession.deployedCampaignFootprintRadius(),
	)
	collision, err := zonenpc.ResolveProjectileCollision(
		launchPosition, game.Vec3(impactPosition), liveNPC.Plan.Position,
		schedule.shotTravelDistances[e.index], shotGeometry,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRetargetCollision[%d]: %w", e.index, err)
	}
	if !collision.IsDirectHit {
		packets, resolveErr := schedule.run.ResolveCollision(
			context.Background(), e.index, e.deadline, false, false, 0,
			schedule.plan.Damage.Maximum, false, collision.Position,
			sim.Position{Z: -1},
		)
		schedule.runtime.registry.mutex.Unlock()
		if resolveErr != nil {
			return nil, fmt.Errorf("heroBurstRetargetDodge[%d]: %w", e.index, resolveErr)
		}
		schedule.runtime.logger.Printf(
			"RakNet projectile trajectory resolved kind=hero-burst projectile=%d source=%d target=%d ability=%q shot=%d outcome=miss aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) endpoint=(%.3f,%.3f,%.3f)",
			schedule.firstProjectileObjectID+uint32(e.index),
			schedule.sourceObjectID, targetObjectID, schedule.definition.Name,
			e.index, impactPosition.X, impactPosition.Y, impactPosition.Z,
			liveNPC.Plan.Position.X, liveNPC.Plan.Position.Y,
			liveNPC.Plan.Position.Z, collision.Position.X,
			collision.Position.Y, collision.Position.Z,
		)
		return packets, nil
	}
	livePlan, err := zoneability.PlanBasic(zoneability.BasicCommand{
		NPCs: peerSession.zone.NPCs(), SourceObjectID: schedule.sourceObjectID,
		TargetObjectID: targetObjectID, AbilityID: schedule.plan.AbilityID,
		SourcePosition: schedule.sourcePosition, MaximumRange: schedule.admissionRange,
		Definition: schedule.definition, Damage: schedule.plan.Damage,
	})
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRetargetPlan[%d]: %w", e.index, err)
	}
	result, err := zoneability.CommitBasic(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), livePlan,
		schedule.creature, peerSession.binding.Difficulty,
		schedule.runtime.program.Critical,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRetargetCommit[%d]: %w", e.index, err)
	}
	transition, err := peerSession.applyCampaignDamageTransition(result.Damage)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRetargetTransition[%d]: %w", e.index, err)
	}
	impactPosition = raknet.Vector3(collision.Position)
	packets, err := schedule.run.ResolveCollision(
		context.Background(), e.index, e.deadline, true, true,
		result.Damage.PreviousHealth, result.Damage.Damage, result.IsCritical,
		sim.Position(impactPosition), sim.Position{Z: -1},
	)
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	schedule.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("heroBurstRetargetHit[%d]: %w", e.index, err)
	}
	schedule.runtime.logger.Printf(
		"RakNet projectile trajectory resolved kind=hero-burst projectile=%d source=%d target=%d ability=%q shot=%d outcome=hit aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) contact=(%.3f,%.3f,%.3f)",
		schedule.firstProjectileObjectID+uint32(e.index),
		schedule.sourceObjectID, targetObjectID, schedule.definition.Name,
		e.index, schedule.shotTargetPositions[e.index].X,
		schedule.shotTargetPositions[e.index].Y,
		schedule.shotTargetPositions[e.index].Z, liveNPC.Plan.Position.X,
		liveNPC.Plan.Position.Y, liveNPC.Plan.Position.Z,
		impactPosition.X, impactPosition.Y, impactPosition.Z,
	)
	packets = filterProjectileCombatPackets(packets)
	timestamp := schedule.packet.SourceTime + uint64(e.deadline/time.Millisecond)
	resultPackets, err := schedule.runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp, schedule.binding,
		[]zoneability.AreaResult{{
			Snapshot: liveNPC, Damage: result.Damage, IsCritical: result.IsCritical,
			Definition: schedule.definition,
		}}, []campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("heroBurstRetargetPublish[%d]: %w", e.index, err)
	}
	return append(packets, resultPackets...), nil
}

func (e heroBurstSchedule) planTargetPosition(
	peerSession gameplayPeerSession, index int,
) raknet.Vector3 {
	if e.definition.BurstTargeting == sim.ProjectileBurstTargetingArc {
		_, position := e.planArcTarget(peerSession, index)
		return position
	}
	if e.definition.BurstTargeting == sim.ProjectileBurstTargetingRadial {
		angle := 2 * math.Pi * float64(index) / float64(len(e.definition.HitDelays))
		return raknet.Vector3{
			X: peerSession.playerPosition.X + float32(math.Cos(angle))*e.definition.Distance,
			Y: peerSession.playerPosition.Y + float32(math.Sin(angle))*e.definition.Distance,
			Z: peerSession.playerPosition.Z,
		}
	}
	if e.definition.BurstTargeting == sim.ProjectileBurstTargetingCursorArea {
		random := peerSession.zone.Population().Random()
		angle := random.Float64() * 2 * math.Pi
		scatterRadius := e.definition.Radius
		if e.definition.Name == "MissileTempestActive" {
			scatterRadius = missileTempestScatterRadius
		}
		distance := float32(random.Float64()) * scatterRadius
		return raknet.Vector3{
			X: e.targetPosition.X + float32(math.Cos(angle))*distance,
			Y: e.targetPosition.Y + float32(math.Sin(angle))*distance,
			Z: e.targetPosition.Z,
		}
	}
	npc, isFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if isFound && !npc.IsDefeated && npc.HitPoint > 0 {
		return raknet.Vector3(npc.Plan.Position)
	}
	return e.targetPosition
}

func (e heroBurstSchedule) planArcTarget(
	peerSession gameplayPeerSession, index int,
) (uint32, raknet.Vector3) {
	shotCount := len(e.definition.HitDelays)
	centerAngle := 2 * math.Pi * float64(index) / float64(shotCount)
	halfArc := math.Pi / float64(shotCount)
	selectedObjectID := uint32(0)
	selected := game.Vec3{}
	selectedDistance := e.definition.Distance
	isSelected := false
	for _, npc := range peerSession.zone.NPCs().LiveSnapshots() {
		if npc.Faction != zonenpc.FactionNonPlayerAligned || npc.Plan.IsFixture {
			continue
		}
		delta := npc.Plan.Position.Sub(game.Vec3(peerSession.playerPosition))
		distance := delta.Length()
		if distance > selectedDistance {
			continue
		}
		angle := math.Atan2(float64(delta.Y), float64(delta.X))
		if angle < 0 {
			angle += 2 * math.Pi
		}
		difference := math.Abs(angle - centerAngle)
		difference = min(difference, 2*math.Pi-difference)
		if difference > halfArc {
			continue
		}
		selected = npc.Plan.Position
		selectedObjectID = npc.Plan.ObjectID
		selectedDistance = distance
		isSelected = true
	}
	if isSelected {
		return selectedObjectID, raknet.Vector3(selected)
	}
	return 0, raknet.Vector3{
		X: peerSession.playerPosition.X +
			float32(math.Cos(centerAngle))*e.definition.Distance,
		Y: peerSession.playerPosition.Y +
			float32(math.Sin(centerAngle))*e.definition.Distance,
		Z: peerSession.playerPosition.Z,
	}
}

func (e heroBurstSchedule) webbedTargetObjectIDs(
	peerSession gameplayPeerSession,
) []uint32 {
	if e.definition.Name != "PlasmaRandom_WebbedLightning" {
		return nil
	}
	targetObjectIDs := make([]uint32, 0, len(e.webbedStatusTargets))
	for targetObjectID := range e.webbedStatusTargets {
		npc, isFound := peerSession.zone.NPCs().NPC(targetObjectID)
		if !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
			continue
		}
		targetObjectIDs = append(targetObjectIDs, targetObjectID)
	}
	return targetObjectIDs
}

func (e heroBurstStep) applyWebbedStatus(
	targetObjectIDs []uint32,
) ([][]byte, error) {
	if e.index+1 != len(e.schedule.definition.HitDelays) ||
		len(targetObjectIDs) == 0 {
		return nil, nil
	}
	schedule := e.schedule
	plan := zoneability.AreaPlan{
		SourceObjectID: schedule.sourceObjectID,
		AbilityID:      schedule.plan.AbilityID,
		Definition:     schedule.definition,
	}
	packets, err := schedule.runtime.applyTargetStatus(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond),
		plan, targetObjectIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("heroBurstWebbedStatus: %w", err)
	}
	return packets, nil
}

func (e heroBurstStep) produceRadialImpact(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	schedule := e.schedule
	impactPosition := schedule.shotTargetPositions[e.index]
	targetObjectID := schedule.shotTargetObjectIDs[e.index]
	if schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingRadial {
		impactPosition = schedule.planTargetPosition(peerSession, e.index)
	}
	if schedule.definition.BurstTargeting != sim.ProjectileBurstTargetingArc {
		targetObjectID = 0
		minimumDistance := schedule.definition.Radius
		for _, npc := range peerSession.zone.NPCs().LiveSnapshots() {
			if npc.Faction != zonenpc.FactionNonPlayerAligned ||
				schedule.hitCounts[npc.Plan.ObjectID] >= 4 {
				continue
			}
			distance := geometryraknet.Distance(
				impactPosition, raknet.Vector3(npc.Plan.Position),
			)
			if distance > minimumDistance {
				continue
			}
			minimumDistance = distance
			targetObjectID = npc.Plan.ObjectID
		}
	}
	liveNPC, isLiveFound := peerSession.zone.NPCs().NPC(targetObjectID)
	if targetObjectID == 0 || !isLiveFound || liveNPC.IsDefeated || !liveNPC.IsPublished ||
		liveNPC.HitPoint <= 0 {
		webbedTargetObjectIDs := schedule.webbedTargetObjectIDs(peerSession)
		isGroundImpact := schedule.definition.BurstTargeting ==
			sim.ProjectileBurstTargetingCursorArea
		impactFacing := sim.Position{X: 1}
		if schedule.definition.Name == "MissileTempestActive" {
			impactFacing = sim.Position{Z: -1}
		}
		packets, err := schedule.run.ResolveCollision(
			context.Background(), e.index, e.deadline, isGroundImpact, false, 0,
			schedule.plan.Damage.Maximum, false, sim.Position(impactPosition),
			impactFacing,
		)
		schedule.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("heroBurstRadialMiss[%d]: %w", e.index, err)
		}
		statusPackets, err := e.applyWebbedStatus(webbedTargetObjectIDs)
		if err != nil {
			return nil, err
		}
		return append(packets, statusPackets...), nil
	}
	maximumRange := schedule.admissionRange
	if schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingArc {
		maximumRange = schedule.definition.Distance
	} else if schedule.definition.BurstTargeting == sim.ProjectileBurstTargetingCursorArea {
		maximumRange += schedule.definition.Radius
		if schedule.definition.Name == "MissileTempestActive" {
			maximumRange += missileTempestScatterRadius
		}
	}
	livePlan, err := zoneability.PlanBasic(zoneability.BasicCommand{
		NPCs: peerSession.zone.NPCs(), SourceObjectID: schedule.sourceObjectID,
		TargetObjectID: targetObjectID, AbilityID: schedule.plan.AbilityID,
		SourcePosition: game.Vec3(peerSession.playerPosition),
		MaximumRange:   maximumRange, Definition: schedule.definition,
		Damage: schedule.plan.Damage,
	})
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRadialPlan[%d]: %w", e.index, err)
	}
	result, err := zoneability.CommitBasic(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), livePlan,
		schedule.creature, peerSession.binding.Difficulty,
		schedule.runtime.program.Critical,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRadialCommit[%d]: %w", e.index, err)
	}
	transition, err := peerSession.applyCampaignDamageTransition(result.Damage)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRadialTransition[%d]: %w", e.index, err)
	}
	schedule.hitCounts[targetObjectID]++
	if schedule.definition.Name == "PlasmaRandom_WebbedLightning" &&
		!result.Damage.IsDamageImmune && !result.Damage.IsDefeated &&
		result.Damage.Damage > 0 {
		schedule.webbedStatusTargets[targetObjectID] = true
	}
	webbedTargetObjectIDs := schedule.webbedTargetObjectIDs(peerSession)
	impactPosition = raknet.Vector3(liveNPC.Plan.Position)
	facing := zoneability.ProjectileDirection(peerSession.playerPosition, impactPosition)
	if schedule.definition.Name == "MissileTempestActive" {
		facing = raknet.Vector3{Z: -1}
	}
	packets, err := schedule.run.ResolveCollision(
		context.Background(), e.index, e.deadline, true, true,
		result.Damage.PreviousHealth, result.Damage.Damage, result.IsCritical,
		sim.Position(impactPosition), sim.Position(facing),
	)
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	schedule.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("heroBurstRadialHit[%d]: %w", e.index, err)
	}
	packets = filterProjectileCombatPackets(packets)
	timestamp := schedule.packet.SourceTime + uint64(e.deadline/time.Millisecond)
	resultPackets, err := schedule.runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp, schedule.binding,
		[]zoneability.AreaResult{{
			Snapshot: liveNPC, Damage: result.Damage, IsCritical: result.IsCritical,
			Definition: schedule.definition,
		}}, []campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("heroBurstRadialPublish[%d]: %w", e.index, err)
	}
	statusPackets, err := e.applyWebbedStatus(webbedTargetObjectIDs)
	if err != nil {
		return nil, err
	}
	packets = append(packets, resultPackets...)
	return append(packets, statusPackets...), nil
}

func (e heroBurstSchedule) produceRelease() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	packets, err := e.run.Advance(context.Background(), e.definition.ReleaseDelay)
	e.runtime.registry.mutex.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("heroBurstReleaseAdvance: %w", err)
	}
	if e.definition.Name == "LightningTempest_Active" ||
		e.definition.Name == "PlasmaRandom_WebbedLightning" {
		resetPackets, resetErr := e.run.ResetActorAnimation(context.Background())
		if resetErr != nil {
			return nil, fmt.Errorf("heroBurstReleaseReset: %w", resetErr)
		}
		packets = append(packets, resetPackets...)
	}
	return append(packets, e.releaseResponse), nil
}

func (e heroBurstSchedule) produceFinish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroBurstAttacks, e.firstProjectileObjectID)
		e.run.SetCancel(nil)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	e.run.Stop()
	return nil, nil
}

func (e heroBurstSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroBurstAttacks, e.firstProjectileObjectID)
		peerSession.restoreCampaignProjectileID(e.previousProjectileObjectID)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.restorePassiveKill(e.creatureIndex, e.passiveKillStack)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.run.Stop()
		e.runtime.logger.Printf(
			"RakNet hero projectile burst stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (r campaignAbilityCommandRuntime) handleHeroProjectileBurst(
	request campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, targetObjectID uint32, sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 {
		r.registry.mutex.Unlock()
		return request.reject("projectile burst definition unavailable")
	}
	command := request.command
	packet := request.packet
	targetPosition := command.Ability.TargetPosition
	if !isReportedZonePosition(targetPosition) {
		targetPosition = command.Ability.CursorPosition
	}
	if definition.BurstTargeting == sim.ProjectileBurstTargetingCursorArea {
		targetObjectID = 0
	}
	if targetObjectID == 0 &&
		definition.BurstTargeting != sim.ProjectileBurstTargetingRadial &&
		definition.BurstTargeting != sim.ProjectileBurstTargetingCursorArea {
		targetObjectID = zoneability.CursorTarget(
			peerSession.zone.NPCs(), command.Common.ObjectID,
			game.Vec3(command.Ability.CursorPosition),
			game.Vec3(targetPosition), definition.Radius,
		)
	}
	if definition.BurstTargeting == sim.ProjectileBurstTargetingRadial ||
		definition.BurstTargeting == sim.ProjectileBurstTargetingArc {
		targetPosition = raknet.Vector3(peerSession.playerPosition)
	}
	if targetObjectID == 0 &&
		definition.BurstTargeting != sim.ProjectileBurstTargetingRadial &&
		definition.BurstTargeting != sim.ProjectileBurstTargetingArc &&
		(!isReportedZonePosition(targetPosition) ||
			!isFiniteZonePosition(targetPosition)) {
		r.registry.mutex.Unlock()
		return request.reject("projectile burst cursor unavailable")
	}
	actorPosition := game.Vec3(peerSession.playerPosition)
	creature, definition, passiveKillStack :=
		peerSession.projectSoulRavagerBurst(creature, definition)
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstTiming: %w", err)
	}
	if projected.IsAreaDurationScaled {
		projected.ShotCount, err = game.ResolveAreaDurationCount(
			projected.ShotCount, creature.AreaDurationIncrease,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroBurstAreaDuration: %w", err)
		}
		if projected.Name != "LightningTempest_Active" {
			projected.HitDelays = make([]time.Duration, projected.ShotCount)
			for shotIndex := range projected.HitDelays {
				projected.HitDelays[shotIndex] =
					time.Duration(shotIndex) * projected.FiringRate
			}
		}
	}
	if projected.Name == "LightningTempest_Active" {
		projected.HitDelays, err = projectRetargetBurstCadence(
			peerSession, creature, projected,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroBurstCadence: %w", err)
		}
		projected.ReleaseDelay, err = zoneability.ProjectDuration(
			creature, definition, 2*time.Second,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroBurstReleaseTiming: %w", err)
		}
	}
	if len(projected.HitDelays) == 0 {
		r.registry.mutex.Unlock()
		return request.reject("projectile burst definition unavailable")
	}
	damage, err := zoneability.ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstDamage: %w", err)
	}
	plan := zoneability.BasicPlan{
		SourceObjectID: command.Common.ObjectID, TargetObjectID: targetObjectID,
		AbilityID: activeAbilityID, Definition: projected, Damage: damage,
	}
	admissionRange := heroAbilityAdmissionRange(creature, projected)
	if targetObjectID != 0 {
		plan, err = zoneability.PlanBasic(zoneability.BasicCommand{
			NPCs: peerSession.zone.NPCs(), SourceObjectID: command.Common.ObjectID,
			TargetObjectID: targetObjectID, AbilityID: activeAbilityID,
			SourcePosition: actorPosition, MaximumRange: admissionRange,
			Definition: projected, Damage: damage,
		})
		if err != nil {
			r.registry.mutex.Unlock()
			return request.reject("projectile burst target unavailable or out of range")
		}
	} else if definition.BurstTargeting != sim.ProjectileBurstTargetingRadial &&
		definition.BurstTargeting != sim.ProjectileBurstTargetingArc &&
		zoneability.Distance(actorPosition, game.Vec3(targetPosition)) > admissionRange {
		r.registry.mutex.Unlock()
		return request.reject("projectile burst cursor out of range")
	}
	definition = projected
	definition.MinimumDamage = damage.Minimum
	definition.MaximumDamage = damage.Maximum
	manaCost, err := game.ResolveAbilityManaCost(
		definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
		definition.ManaCoefficient, peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstManaProjection: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return request.reject("power unavailable")
	}
	definition.Speed, err = zoneability.ProjectProjectileSpeed(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstSpeed: %w", err)
	}
	previousProjectileObjectID := peerSession.nextProjectileObjectID
	firstProjectileObjectID, err := peerSession.reserveCampaignProjectileIDs(
		uint32(len(definition.HitDelays)), 1000,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstObjectID: %w", err)
	}
	projectileObjectID := make([]uint32, len(definition.HitDelays))
	for index := range projectileObjectID {
		projectileObjectID[index] = firstProjectileObjectID + uint32(index)
	}
	targetNPC, isTargetFound := peerSession.zone.NPCs().NPC(targetObjectID)
	targetHitPoint := float32(0)
	runTargetObjectID := command.Common.ObjectID
	projectileGeometry := zonenpc.ProjectileGeometry{}
	if isTargetFound {
		if targetNPC.TargetObjectID == 0 && !targetNPC.Plan.IsFixture {
			acquired, isAcquired, acquireErr := peerSession.zone.NPCs().AcquireTarget(
				targetObjectID, command.Common.ObjectID,
			)
			if acquireErr != nil {
				peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
				r.registry.mutex.Unlock()
				return request.reject(acquireErr.Error())
			}
			if isAcquired {
				targetNPC = acquired
			}
		}
		projectileGeometry, _, err = campaignTargetProjectileGeometry(r.program, targetNPC)
		if err != nil {
			peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroBurstGeometry: %w", err)
		}
		targetPosition = raknet.Vector3(campaignProjectileAimPosition(
			targetNPC.Plan.Position, targetNPC.Plan.NPCProfile.FootprintRadius,
			projectileGeometry,
		))
		targetHitPoint = targetNPC.HitPoint
		runTargetObjectID = targetObjectID
	}
	actorFootprint := peerSession.deployedCampaignFootprintRadius()
	actorHeight := actorFootprint
	actorPhysics, isActorPhysicsFound := r.program.NounPhysicsByID[creature.Noun]
	if isActorPhysicsFound {
		actorHeight = max(
			actorHeight,
			(actorPhysics.BoundMinimum.Z+actorPhysics.BoundMaximum.Z)*0.5,
		)
	}
	actorPosition.Z += max(float32(0), actorHeight)
	launchPosition := campaignProjectileLaunchPosition(
		actorPosition, game.Vec3(targetPosition), actorFootprint,
	)
	travelDistance := zoneability.Distance(launchPosition, game.Vec3(targetPosition))
	if definition.Name == "LightningTempest_Active" {
		travelDistance = max(float32(0.01), 10-actorFootprint)
	} else if definition.Name == "MissileTempestActive" {
		travelDistance = max(float32(0.01), 15-actorFootprint)
	}
	if definition.BurstTargeting == sim.ProjectileBurstTargetingRadial ||
		definition.BurstTargeting == sim.ProjectileBurstTargetingArc {
		travelDistance = definition.Distance
	}
	travelDelay := max(
		abilityraknet.ProjectileCollisionTick,
		time.Duration(float64(travelDistance)/float64(definition.Speed)*float64(time.Second)),
	)
	lastLaunch := definition.HitDelays[len(definition.HitDelays)-1]
	finishDelay := max(definition.ReleaseDelay, lastLaunch+travelDelay)
	plan.Definition = definition
	facing := zoneability.ProjectileDirection(raknet.Vector3(actorPosition), targetPosition)
	if definition.BurstTargeting == sim.ProjectileBurstTargetingRadial ||
		definition.BurstTargeting == sim.ProjectileBurstTargetingArc {
		facing = raknet.Vector3{X: 1}
	}
	simulationDefinition := definition
	if definition.Name == "LightningTempest_Active" {
		simulationDefinition.ReleaseDelay = finishDelay + time.Millisecond
	}
	run, immediatePackets, err := abilityraknet.NewBurstRun(abilityraknet.BurstInput{
		Ability: simulationDefinition, ActorObjectID: command.Common.ObjectID,
		TargetObjectID: runTargetObjectID, ProjectileObjectIDs: projectileObjectID,
		ActorPosition:  sim.Position(actorPosition),
		TargetPosition: sim.Position(targetPosition), ActorFacing: sim.Position(facing),
		FootprintRadius: actorFootprint,
		Damage:          definition.MaximumDamage, TargetHitPoint: targetHitPoint,
		ActorTeam: 1, SourceTime: packet.SourceTime, StartedAt: abilityStartTime,
		IsImpactFacingOmitted: definition.Name == "LightningTempest_Active",
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRun: %w", err)
	}
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: activeAbilityID, AbilityIndex: command.Ability.Index,
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime,
		SourceEndMilliseconds: packet.SourceTime +
			uint64(definition.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		run.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstAck: %w", err)
	}
	releaseResponse, err := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], activeAbilityID, command.Ability.Index,
		packet.SourceTime, 0, definition.ReleaseDelay,
	)
	if err != nil {
		run.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstRelease: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	manaPacket, err := abilityraknet.Mana(
		command.Common.ObjectID, remainingManaPoint,
	)
	if err != nil {
		run.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstMana: %w", err)
	}
	previousManaPoint := peerSession.deployedManaPoint()
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID),
			abilityStartTime, definition.Cooldown,
		)
	if !isCooldownReserved {
		run.Stop()
		r.registry.mutex.Unlock()
		return request.reject("cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, definition.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		run.Stop()
		r.registry.mutex.Unlock()
		return request.reject("release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		run.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroBurstCommit: %w", err)
	}
	if peerSession.heroBurstAttacks == nil {
		peerSession.heroBurstAttacks = make(map[uint32]*abilityraknet.BurstRun)
	}
	peerSession.heroBurstAttacks[firstProjectileObjectID] = run
	generation := peerSession.generation
	creatureIndex := peerSession.deployedCreatureIndex
	if !peerSession.consumePassiveKill(creatureIndex, passiveKillStack) {
		delete(peerSession.heroBurstAttacks, firstProjectileObjectID)
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		_ = peerSession.setCampaignCharacterManaPoints(
			creatureIndex, previousManaPoint,
		)
		run.Stop()
		r.registry.mutex.Unlock()
		return request.reject("passive soul state unavailable")
	}
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroBurstSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: command.Common.ObjectID,
		targetObjectID:             targetObjectID,
		targetPosition:             targetPosition,
		sourcePosition:             actorPosition,
		firstProjectileObjectID:    firstProjectileObjectID,
		previousProjectileObjectID: previousProjectileObjectID,
		creatureIndex:              creatureIndex, previousManaPoint: previousManaPoint,
		passiveKillStack: passiveKillStack,
		creature:         creature, definition: definition, admissionRange: admissionRange,
		plan:    plan,
		binding: binding, run: run, cooldownReservation: cooldownReservation,
		releaseReservation: releaseReservation, releaseResponse: releaseResponse,
		hitCounts:           make(map[uint32]uint32),
		webbedStatusTargets: make(map[uint32]bool),
		shotTargetObjectIDs: make(map[int]uint32),
		shotSourcePositions: make(map[int]game.Vec3),
		shotTargetPositions: make(map[int]raknet.Vector3),
		shotTravelDistances: make(map[int]float32),
		shotGeometries:      make(map[int]zonenpc.ProjectileGeometry),
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(definition.HitDelays)*2+2)
	for index, launchDelay := range definition.HitDelays {
		impactDelay := launchDelay + travelDelay
		producers = append(producers,
			schedule.producer(index, launchDelay, true),
			schedule.producer(index, impactDelay, false),
		)
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: definition.ReleaseDelay, Produce: schedule.produceRelease,
	})
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: finishDelay, Produce: schedule.produceFinish,
	})
	sortScheduledPacketProducersByDelay(producers)
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, schedule.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("heroBurstSchedule: %w", err)
	}
	run.SetCancel(cancel)
	r.logger.Printf(
		"RakNet hero projectile burst accepted ability=%s source=%d target=%d shots=%d",
		definition.Name, command.Common.ObjectID, targetObjectID,
		len(definition.HitDelays),
	)
	return append([][]byte{ackPacket, manaPacket}, immediatePackets...), nil
}

func projectRetargetBurstCadence(
	peerSession gameplayPeerSession, creature game.GameplayCreature,
	definition sim.AbilityDefinition,
) ([]time.Duration, error) {
	if definition.ShotCount == 0 || definition.FiringRate <= 0 {
		return nil, fmt.Errorf("invalid retarget cadence")
	}
	random := peerSession.zone.Population().Random()
	deadline := time.Duration(0)
	delays := make([]time.Duration, definition.ShotCount)
	for index := range delays {
		interval := definition.FiringRate
		if definition.FiringRateRandomness > 0 {
			interval += time.Duration(
				random.Float64() * float64(definition.FiringRateRandomness),
			)
		}
		projected, err := zoneability.ProjectDuration(
			creature, definition, interval,
		)
		if err != nil {
			return nil, fmt.Errorf("retargetInterval[%d]: %w", index, err)
		}
		deadline += max(50*time.Millisecond, projected)
		delays[index] = deadline
	}
	return delays, nil
}
