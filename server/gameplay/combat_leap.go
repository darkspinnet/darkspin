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
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignHopperAirTime = 666667 * time.Microsecond

type campaignLeapSchedule struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	objectID         uint32
	timestamp        uint64
	plan             zonenpc.AttackPlan
	origin           game.Vec3
	destination      game.Vec3
	isHopperNormal   bool
	isHopperRetreat  bool
	isHopperJumped   bool
	hopperDistance   float32
	flightDuration   time.Duration
	flightElapsed    time.Duration
}

func (e campaignLeapSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	if e.actionGeneration != 0 {
		e.runtime.releaseActionGeneration(
			e.sessionKey, e.generation, e.objectID, e.actionGeneration,
		)
	} else {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	}
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignLeapSchedule) resume(timestamp uint64) ([][]byte, error) {
	packets, err := e.runtime.produceEnemyLeap(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return e.fail("enemyLeapResume", err)
	}
	return packets, nil
}

func (e campaignLeapSchedule) launch() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCAttackGenerationActiveAt(
		e.generation, e.objectID, e.plan.TargetObjectID,
		e.actionGeneration, e.runtime.now(),
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
	if !isEnemyFound || !isTargetFound || enemy.ActionGeneration != e.actionGeneration {
		e.runtime.releaseActionGeneration(
			e.sessionKey, e.generation, e.objectID, e.actionGeneration,
		)
		return nil, nil
	}
	animationPacket, err := npcraknet.AnimationState(
		e.objectID, e.plan.Profile.LoopAnimationName,
		e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond),
	)
	if err != nil {
		return e.fail("enemyLeapAirAnimation", err)
	}
	if e.plan.Profile.AbilityName == "NoctHopperJump" {
		return e.launchHopper(enemy, target.Position, animationPacket)
	}
	deltaX := target.Position.X - enemy.Plan.Position.X
	deltaY := target.Position.Y - enemy.Plan.Position.Y
	centerDistance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if centerDistance < e.plan.Profile.MinimumRange {
		cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet,
			[]raknet.ScheduledPacketProducer{{
				Delay: campaignHopperAirTime, Produce: e.landAtCloseRange,
			}},
		)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr != nil {
			return e.fail("enemyLeapCloseSchedule", scheduleErr)
		}
		return [][]byte{animationPacket}, nil
	}
	movementProfile := e.plan.Profile
	movementProfile.Range = 0.1
	movementProfile.MovementSpeed = e.plan.Profile.ForcedMovementSpeed
	action, err := campaignNPCActionWithProfile(
		enemy.Plan, target.ObjectID, target.Position, movementProfile,
		target.ActorFootprintRadius,
	)
	if err != nil {
		return e.fail("enemyLeapMovePlan", err)
	}
	if !action.IsPursuitNeeded {
		landingPackets, landingErr := e.land(
			e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond), true,
		)
		if landingErr != nil {
			return nil, landingErr
		}
		return append([][]byte{animationPacket}, landingPackets...), nil
	}
	var pursuitPackets [][]byte
	if e.plan.Profile.AbilityName == "NomadBioSpecialTwoJumpAttack" {
		// This leap is server-driven; ordinary pursuit cannot move its jump root.
		pursuitPackets, err = npcraknet.StopAtPose(e.objectID, enemy.Plan.Position, enemy.Facing)
	} else {
		pursuitPackets, err = npcraknet.Pursuit(action)
	}
	if err != nil {
		return e.fail("enemyLeapMoveMarshal", err)
	}
	arrivalTimestamp := e.timestamp + uint64(e.plan.Profile.HitDelay/time.Millisecond)
	err = e.runtime.pursuit.schedule(
		e.packet, e.sessionKey, e.generation, e.objectID,
		arrivalTimestamp, action.TargetPosition, action.Profile, e.landWithDamage,
	)
	if err != nil {
		return e.fail("enemyLeapMoveSchedule", err)
	}
	return append([][]byte{animationPacket}, pursuitPackets...), nil
}

func (e campaignLeapSchedule) landWithDamage(
	timestamp uint64,
) ([][]byte, error) {
	return e.land(timestamp, true)
}

func (e campaignLeapSchedule) landHopper() ([][]byte, error) {
	timestamp := e.timestamp +
		uint64((e.plan.Profile.HitDelay+e.flightDuration)/time.Millisecond)
	return e.land(timestamp, e.isHopperJumped && !e.isHopperNormal)
}

func (e campaignLeapSchedule) landAtCloseRange() ([][]byte, error) {
	timestamp := e.timestamp +
		uint64((e.plan.Profile.HitDelay+campaignHopperAirTime)/time.Millisecond)
	return e.land(timestamp, true)
}

func isCampaignLeapTarget(
	source game.Vec3, targetPosition game.Vec3,
	targetFootprintRadius float32, radius float32,
) bool {
	deltaX := targetPosition.X - source.X
	deltaY := targetPosition.Y - source.Y
	distance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	return distance <= radius+targetFootprintRadius
}

func (e campaignLeapSchedule) land(
	timestamp uint64, isDamageEnabled bool,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := false
	if isFound {
		if e.plan.Profile.AbilityName == "NoctHopperJump" {
			isCurrent = peerSession.isCampaignNPCSourceGenerationActive(
				e.generation, e.objectID, e.actionGeneration,
			)
		} else {
			isCurrent = peerSession.isCampaignNPCAttackGenerationActiveAt(
				e.generation, e.objectID, e.plan.TargetObjectID,
				e.actionGeneration, e.runtime.now(),
			)
		}
	}
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || source.ActionGeneration != e.actionGeneration ||
		peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseActionGeneration(
			e.sessionKey, e.generation, e.objectID, e.actionGeneration,
		)
		return nil, nil
	}
	if e.plan.Profile.AbilityName == "NoctHopperJump" &&
		e.destination != source.Plan.Position {
		err := peerSession.zone.NPCs().SetPosition(e.objectID, e.destination)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("hopperJumpPosition", err)
		}
		source, isSourceFound = peerSession.zone.NPCs().NPC(e.objectID)
		if !isSourceFound {
			e.runtime.registry.mutex.Unlock()
			return e.fail("hopperJumpSource", errors.New("source unavailable after landing"))
		}
	}
	packets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	if isDamageEnabled {
		damageProfile := e.plan.Profile
		damageProfile.Range = damageProfile.Radius
		for _, target := range peerSession.zone.LiveNPCTargets() {
			if e.plan.Profile.AbilityName == "NomadBioSpecialTwoJumpAttack" &&
				target.ObjectID != e.plan.TargetObjectID {
				continue
			}
			if !isCampaignLeapTarget(
				source.Plan.Position, target.Position,
				target.FootprintRadius, damageProfile.Radius,
			) {
				continue
			}
			plan, err := zonenpc.PlanAttackWithProfile(
				source, target.ObjectID, target.Position, damageProfile,
				target.ActorFootprintRadius,
			)
			if err != nil {
				continue
			}
			result, err := zonenpc.CommitAttack(
				peerSession.zone.NPCRandom(), plan,
				source.EffectiveCriticalRating(), e.runtime.program.Critical,
			)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail("enemyLeapCommit", err)
			}
			hitPackets, targetStatDelta, _, err :=
				e.runtime.applyEnemyAreaAttackDamage(
					&peerSession, e.generation, plan, result, timestamp,
				)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail("enemyLeapDamage", err)
			}
			packets = append(packets, hitPackets...)
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		}
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	presentationPackets := make([][]byte, 0, 2)
	if e.plan.Profile.AbilityName == "NomadBioSpecialTwoJumpAttack" ||
		e.plan.Profile.AbilityName == "NoctHopperJump" {
		positionPacket, positionErr := npcraknet.LeapPosition(
			e.objectID, source.Plan.Position, e.plan.TargetPosition,
		)
		if positionErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign NPC leap landing position omitted object=%d: %v",
				e.objectID, positionErr,
			)
		} else {
			presentationPackets = append(presentationPackets, positionPacket)
		}
	}
	landingPacket, err := npcraknet.AnimationState(
		e.objectID, e.plan.Profile.EndAnimationName, timestamp,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet campaign NPC leap landing presentation omitted object=%d: %v",
			e.objectID, err,
		)
	} else {
		presentationPackets = append(presentationPackets, landingPacket)
	}
	packets = append(presentationPackets, packets...)
	nextDelay := max(e.plan.Profile.EndAnimationDelay, e.plan.Profile.Cooldown)
	nextSchedule := e
	nextSchedule.timestamp = timestamp
	cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet,
		[]raknet.ScheduledPacketProducer{{Delay: nextDelay, Produce: nextSchedule.next}},
	)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		return e.fail("enemyLeapNextSchedule", scheduleErr)
	}
	err = e.runtime.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		e.runtime.logger.Printf(
			"campaign NPC leap stat persistence omitted object=%d: %v",
			e.objectID, err,
		)
	}
	return packets, nil
}

func (e campaignLeapSchedule) next() ([][]byte, error) {
	nextDelay := max(e.plan.Profile.EndAnimationDelay, e.plan.Profile.Cooldown)
	timestamp := e.timestamp + uint64(nextDelay/time.Millisecond)
	step := campaignNPCFirstActionStep{
		runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.objectID, timestamp: timestamp,
		actionGeneration: e.actionGeneration,
	}
	packets, err := step.produce()
	if err != nil {
		return e.fail("enemyLeapNext", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceEnemyLeap(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	resume := campaignLeapSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, resume.resume,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyLeapStun: %w", err)
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
	profile, isProfileFound := campaignNPCActionProfile(
		enemy.Plan, target.Position, target.ActorFootprintRadius,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound || !isProfileFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	resume.actionGeneration = enemy.ActionGeneration
	if profile.AbilityName == "StealthAttack" {
		return r.startStealtherStealth(
			packet, sessionKey, generation, enemy, target, profile, timestamp,
		)
	}
	if profile.Family == zonenpc.ActionCone &&
		profile.AbilityName == "NomadBioSpecialTwoSwipe" {
		return r.produceEnemyCone(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if profile.Family != zonenpc.ActionLeap {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	if profile.AbilityName == "NoctHopperJump" {
		delta := target.Position.Sub(enemy.Plan.Position)
		centerDistance := delta.Length()
		surfaceDistance := max(float32(0), centerDistance-enemy.Plan.ActorFootprintRadius())
		resume.isHopperRetreat = surfaceDistance < profile.MinimumRange
		resume.isHopperNormal = resume.isHopperRetreat ||
			surfaceDistance > campaignHopperMaxJumpDistance ||
			peerSession.zone.NPCs().SilenceRemaining(objectID, r.now()) > 0
		if surfaceDistance > campaignHopperMaxJumpDistance {
			resume.hopperDistance = campaignHopperMaxJumpDistance * 0.4
		} else if !resume.isHopperRetreat {
			resume.hopperDistance = surfaceDistance - profile.Radius*0.5
		}
		if resume.isHopperNormal {
			profile.AnimationName = "nct_minn_su_04_jump_intro"
			profile.LoopAnimationName = "nct_minn_su_04_jump_in_air"
			profile.EndAnimationName = "nct_minn_su_04_jump_landing"
		}
	}
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.ActorFootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.ActorFootprintRadius,
		)
		if actionErr != nil || !action.IsPursuitNeeded {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, fmt.Errorf("enemyLeapPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, resume.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, fmt.Errorf("enemyLeapPursuitSchedule: %w", scheduleErr)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyLeapStart: %w", err)
	}
	resume.plan = plan
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: profile.HitDelay, Produce: resume.launch,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyLeapSchedule: %w", err)
	}
	return startPackets, nil
}
