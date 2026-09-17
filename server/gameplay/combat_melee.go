package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

var grapplingPulsarFastSwipeHitDelays = [...]time.Duration{
	0,
	100 * time.Millisecond,
	230 * time.Millisecond,
	360 * time.Millisecond,
	490 * time.Millisecond,
	620 * time.Millisecond,
	750 * time.Millisecond,
	880 * time.Millisecond,
	1010 * time.Millisecond,
}

const noctBasicMeleeDogDistractionChance = 0.009999999776482582
const noctBasicMeleeDogDistractionRetry = 100 * time.Millisecond

func campaignScaldronDogApproachPosition(
	source game.Vec3, target game.Vec3, objectID uint32,
) game.Vec3 {
	deltaX := target.X - source.X
	deltaY := target.Y - source.Y
	length := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if length <= 0 {
		return target
	}
	direction := float32(1)
	if objectID%2 != 0 {
		direction = -1
	}
	return game.Vec3{
		X: target.X - deltaY/length*1.5*direction,
		Y: target.Y + deltaX/length*1.5*direction,
		Z: target.Z,
	}
}

type campaignNPCDogDistractionSchedule struct {
	request campaignNPCAttackRequest
}

func (e campaignNPCDogDistractionSchedule) retry() ([][]byte, error) {
	timestamp := e.request.timestamp + uint64(
		noctBasicMeleeDogDistractionRetry/time.Millisecond,
	)
	return e.request.resume(timestamp)
}

func (r campaignNPCActionRuntime) deferNoctBasicMeleeDogDistraction(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) (bool, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return false, nil
	}
	npc, isNPCFound := peerSession.zone.NPCs().NPC(objectID)
	family := campaignDifficultyNounFamily(npc.Plan.NounName)
	isDog := isNPCFound && (family == "noctbasicmeleedog" ||
		family == "noctbasicmeleedog_captain")
	random := peerSession.zone.NPCRandom()
	if !isDog || random == nil {
		r.registry.mutex.RUnlock()
		return false, nil
	}
	isDistracted := random.Float64() < noctBasicMeleeDogDistractionChance
	r.registry.mutex.RUnlock()
	if !isDistracted {
		return false, nil
	}
	schedule := campaignNPCDogDistractionSchedule{request: campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackMelee,
	}}
	_, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: noctBasicMeleeDogDistractionRetry, Produce: schedule.retry,
	}})
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return true, fmt.Errorf("dogDistractionSchedule: %w", err)
	}
	return true, nil
}

func (r campaignNPCActionRuntime) produceEnemyMelee(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	return r.produceEnemyMeleeWithPull(
		packet, sessionKey, generation, objectID, timestamp, true,
	)
}

func (r campaignNPCActionRuntime) produceEnemyMeleeWithPull(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, isPullAllowed bool,
) ([][]byte, error) {
	isSilenced := r.isSilenced(sessionKey, generation, objectID)
	if !isSilenced {
		isDistracted, err := r.deferNoctBasicMeleeDogDistraction(
			packet, sessionKey, generation, objectID, timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("enemyMeleeDogDistraction: %w", err)
		}
		if isDistracted {
			return nil, nil
		}
	}
	if !isSilenced {
		voltroidPackets, isVoltroidHandled, voltroidErr :=
			r.produceCitadelSpecificOne(
				packet, sessionKey, generation, objectID, timestamp,
			)
		if voltroidErr != nil {
			return nil, fmt.Errorf("enemyMeleeVoltroid: %w", voltroidErr)
		}
		if isVoltroidHandled {
			return voltroidPackets, nil
		}
	}
	if !isSilenced {
		cowerPackets, isCowerHandled, cowerErr := r.producePackMeleeCower(
			packet, sessionKey, generation, objectID, timestamp,
		)
		if cowerErr != nil {
			return nil, fmt.Errorf("enemyMeleePackCower: %w", cowerErr)
		}
		if isCowerHandled {
			return cowerPackets, nil
		}
		if isPullAllowed {
			pullPackets, isPullHandled, pullErr := r.produceGrapplingPulsarPull(
				packet, sessionKey, generation, objectID, timestamp,
			)
			if pullErr != nil {
				return nil, fmt.Errorf("enemyMeleePuller: %w", pullErr)
			}
			if isPullHandled {
				return pullPackets, nil
			}
		}
		fleePackets, isFleeHandled, fleeErr := r.produceChronoStrikerFlee(
			packet, sessionKey, generation, objectID, timestamp,
		)
		if fleeErr != nil {
			return nil, fmt.Errorf("enemyMeleeFlee: %w", fleeErr)
		}
		if isFleeHandled {
			return fleePackets, nil
		}
		shieldPackets, isShieldHandled, shieldErr := r.produceNomadShielderShield(
			packet, sessionKey, generation, objectID, timestamp,
		)
		if shieldErr != nil {
			return nil, fmt.Errorf("enemyMeleeShielderShield: %w", shieldErr)
		}
		if isShieldHandled {
			return shieldPackets, nil
		}
		if r.isNomadRuptionMagmaReady(sessionKey, generation, objectID, timestamp) {
			packets, err := r.produceEnemyLob(
				packet, sessionKey, generation, objectID, timestamp,
			)
			if err != nil {
				return nil, fmt.Errorf("enemyMeleeRuptionMagma: %w", err)
			}
			return packets, nil
		}
		if r.isNomadShielderGrenadeReady(sessionKey, generation, objectID, timestamp) {
			packets, err := r.produceEnemyLob(
				packet, sessionKey, generation, objectID, timestamp,
			)
			if err != nil {
				return nil, fmt.Errorf("enemyMeleeShielderGrenade: %w", err)
			}
			return packets, nil
		}
		phasePackets, isPhaseHandled, phaseErr := r.produceCitadelSpecialFour(
			packet, sessionKey, generation, objectID, timestamp,
		)
		if phaseErr != nil {
			return nil, fmt.Errorf("enemyMeleeCitadelSpecialFour: %w", phaseErr)
		}
		if isPhaseHandled {
			return phasePackets, nil
		}
	}
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		isPullSuppressed: !isPullAllowed, kind: campaignNPCAttackMelee,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyMeleeStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	if !isSilenced {
		packets, isHandled, panicErr := r.produceNashiraCombatPanic(packet, sessionKey, generation, objectID, timestamp)
		if panicErr != nil {
			return nil, fmt.Errorf("meleePanic: %w", panicErr)
		}
		if isHandled {
			return packets, nil
		}
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
		enemy.Plan, target.Position, target.FootprintRadius,
	)
	drainerMeleeProfile, isDrainerMeleeFound :=
		zonenpc.NoctMinionDrainerMeleeProfile(enemy.Plan.NounName)
	if isDrainerMeleeFound {
		profile = drainerMeleeProfile
		isProfileFound = true
	}
	grapplingSwipeProfile, isGrapplingPulsar := zonenpc.VerdanthSpecialThreeFastSwipeProfile(
		enemy.Plan.NounName,
	)
	if isGrapplingPulsar {
		profile = grapplingSwipeProfile
		isProfileFound = true
	}
	_, isHomerFamily := zonenpc.NocturnaSpecialHomerMeleeProfile(enemy.Plan.NounName)
	if isHomerFamily {
		profile = scaleNocturnaSpecialHomerCooldown(
			profile, uint32(len(peerSession.zone.Snapshot().Members)),
		)
	}
	if isProfileFound && profile.AbilityName == "PackflyAttack" &&
		peerSession.zone.NPCs().HasSameSpeciesAlly(objectID, 10) {
		profile.Cooldown = time.Duration(float64(profile.Cooldown) * 0.75)
		profile.MovementSpeed *= 1.25
	}
	_, slowAttackScale := peerSession.zone.NPCs().SlowProfile(objectID, r.now())
	profile = applyNPCSlowTiming(profile, slowAttackScale)
	if profile.AbilityName == "ShadowBossSwipe" {
		profile.AnimationName = zonenpc.NashiraSwipeAnimation(
			timestamp / uint64(time.Second/time.Millisecond),
		)
	}
	chargeupState := peerSession.campaignNPCChargeups[objectID]
	r.registry.mutex.RUnlock()
	if isEnemyFound && isTargetFound && isProfileFound &&
		profile.AbilityName == "NocturnaSpecialHomer" {
		return r.produceZelemShot(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if isEnemyFound && isTargetFound && isProfileFound &&
		profile.AbilityName == "ZelemBasicHybridProjectile" {
		return r.produceZelemShot(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if isEnemyFound && isTargetFound && isProfileFound &&
		profile.AbilityName == "ShadowToss" {
		return r.produceZelemShot(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if isEnemyFound && isTargetFound && isProfileFound &&
		profile.Family == zonenpc.ActionCone &&
		profile.AbilityName == "ChainLightningBolt" {
		return r.produceEnemyCone(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if !isEnemyFound || !isTargetFound || !isProfileFound ||
		profile.Family != zonenpc.ActionMelee {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	if profile.AbilityName == "ScaldronBasicCopter_Passive" {
		return r.produceScaldronBasicCopter(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if profile.AbilityName == "ScaldronBasicBlink_AttackBlink" {
		return r.produceScaldronBasicBlink(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	wanderPackets, isWanderHandled, wanderErr :=
		r.produceScaldronBasicNestleWander(
			packet, sessionKey, generation, objectID, timestamp,
		)
	if wanderErr != nil {
		return nil, fmt.Errorf("enemyMeleeNestleWander: %w", wanderErr)
	}
	if isWanderHandled {
		return wanderPackets, nil
	}
	growthPackets, isGrowthHandled, err := r.produceVerdanthBasicOozeGrowth(
		packet, sessionKey, generation, enemy, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyMeleeOozeGrowth: %w", err)
	}
	if isGrowthHandled {
		return growthPackets, nil
	}
	if profile.AbilityName == "ZelemChargeupStandardAttack" && !isSilenced {
		if chargeupState.isCharged {
			profile = zonenpc.ZelemChargeupDischargeProfile()
		} else if chargeupState.nextBuildTimestamp == 0 ||
			timestamp >= chargeupState.nextBuildTimestamp {
			return r.produceZelemChargeupBuild(
				packet, sessionKey, generation, objectID, timestamp,
			)
		}
	}
	healPackets, isHealHandled, err := r.produceVerdanthSpecialTwoHeal(
		packet, sessionKey, generation, enemy, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyMeleeHeal: %w", err)
	}
	if isHealHandled {
		return healPackets, nil
	}
	fleePackets, isFleeHandled, err := r.produceMendingTanglidFlee(
		packet, sessionKey, generation, enemy, target, profile, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyMeleeMendingFlee: %w", err)
	}
	if isFleeHandled {
		return fleePackets, nil
	}
	attackPlan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		pursuitPosition := target.Position
		if profile.AbilityName == "ScaldronBasicDog_Attack" {
			pursuitPosition = campaignScaldronDogApproachPosition(
				enemy.Plan.Position, target.Position, objectID,
			)
		}
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, pursuitPosition, profile,
			target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemyMeleePursuitPlan: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyMeleePursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, request.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyMeleePursuitSchedule: %w", scheduleErr)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, attackPlan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyMeleeStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(attackPlan)
	if err != nil {
		return nil, fmt.Errorf("enemyMeleeTimeline: %w", err)
	}
	request.isPullSuppressed = false
	isChargeupDischarge := profile.AbilityName == "ZelemChargeupDischargeAttack"
	if isChargeupDischarge {
		removePacket, removeErr := npcraknet.ChargeupEffect(
			objectID, "zelem_chargeup_charge.ServerEventDef", true,
		)
		if removeErr != nil {
			return nil, fmt.Errorf("enemyChargeupRemove: %w", removeErr)
		}
		r.registry.mutex.Lock()
		latest, isLatestFound := r.registry.sessions[sessionKey]
		latestState := latest.campaignNPCChargeups[objectID]
		if !isLatestFound || latest.generation != generation ||
			!latestState.isCharged {
			r.registry.mutex.Unlock()
			return nil, nil
		}
		latestState.isCharged = false
		latest.campaignNPCChargeups[objectID] = latestState
		r.registry.sessions[sessionKey] = latest
		r.registry.mutex.Unlock()
		startPackets = append([][]byte{removePacket}, startPackets...)
	}
	schedule := campaignNPCAttackSchedule{request: request, plan: attackPlan}
	if profile.AbilityName == "FastSwipe" {
		producers := make(
			[]raknet.ScheduledPacketProducer,
			0, len(grapplingPulsarFastSwipeHitDelays)+1,
		)
		for _, hitDelay := range grapplingPulsarFastSwipeHitDelays {
			hitSchedule := schedule
			hitSchedule.request.timestamp += uint64(hitDelay / time.Millisecond)
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: hitDelay, Produce: hitSchedule.hit,
			})
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: timeline.NextDelay, Produce: schedule.next,
		})
		_, scheduleErr := packet.ScheduleProducers(producers)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyFastSwipeSchedule: %w", scheduleErr)
		}
		return startPackets, nil
	}
	_, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: timeline.HitDelay, Produce: schedule.hit},
		{Delay: timeline.NextDelay, Produce: schedule.next},
	})
	if scheduleErr != nil {
		if isChargeupDischarge {
			r.registry.mutex.Lock()
			latest, isLatestFound := r.registry.sessions[sessionKey]
			if isLatestFound && latest.generation == generation {
				latestState := latest.campaignNPCChargeups[objectID]
				latestState.isCharged = true
				latest.campaignNPCChargeups[objectID] = latestState
				r.registry.sessions[sessionKey] = latest
			}
			r.registry.mutex.Unlock()
		}
		return nil, fmt.Errorf("enemyMeleeSchedule: %w", scheduleErr)
	}
	return startPackets, nil
}

func (r campaignNPCActionRuntime) produceCryosRezMelee(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackCryosRezMelee,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyRezMeleeStun: %w", err)
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
	profile, isProfileFound := zonenpc.CryosSpecialThreeRezMeleeProfile(
		enemy.Plan.NounName,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound || !isProfileFound {
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
			return nil, fmt.Errorf("enemyRezMeleePursuitPlan: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyRezMeleePursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, request.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("enemyRezMeleePursuitSchedule: %w", scheduleErr)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyRezMeleeStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(plan)
	if err != nil {
		return nil, fmt.Errorf("enemyRezMeleeTimeline: %w", err)
	}
	schedule := campaignNPCAttackSchedule{request: request, plan: plan}
	_, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: timeline.HitDelay, Produce: schedule.hit},
		{Delay: timeline.NextDelay, Produce: schedule.next},
	})
	if scheduleErr != nil {
		return nil, fmt.Errorf("enemyRezMeleeSchedule: %w", scheduleErr)
	}
	return startPackets, nil
}
