package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCLobRun struct {
	isLaunched bool
	cancel     raknet.CancelSchedule
}

type campaignNPCLobSchedule struct {
	runtime            campaignNPCActionRuntime
	packet             raknet.Packet
	sessionKey         string
	generation         uint64
	objectID           uint32
	projectileObjectID uint32
	retainedObjectID   uint32
	timestamp          uint64
	plan               zonenpc.AttackPlan
	toss               zoneability.TossPlan
	result             zonenpc.AttackResult
	source             zonenpc.Snapshot
	submunitions       []campaignNPCSubmunition
	binding            game.GameplayBinding
	run                *campaignNPCLobRun
}

type campaignNPCSubmunition struct {
	objectID uint32
	toss     zoneability.TossPlan
	profile  zonenpc.ActionProfile
}

func (r campaignNPCActionRuntime) isNomadRuptionMagmaReady(
	sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) bool {
	r.registry.mutex.RLock()
	defer r.registry.mutex.RUnlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceActive(generation, objectID) {
		return false
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound || enemy.IsDefeated {
		return false
	}
	_, isFamilyFound := zonenpc.NomadRuptionHurlMagmaProfile(enemy.Plan.NounName)
	return isFamilyFound && timestamp >= peerSession.campaignNPCRuptionNextMagmas[objectID]
}

func (r campaignNPCActionRuntime) isNomadShielderGrenadeReady(
	sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) bool {
	r.registry.mutex.RLock()
	defer r.registry.mutex.RUnlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceActive(generation, objectID) {
		return false
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound || enemy.IsDefeated {
		return false
	}
	_, isFamilyFound := zonenpc.NomadShielderGrenadeProfile(enemy.Plan.NounName)
	return isFamilyFound && timestamp >= peerSession.campaignNPCShielderNextGrenades[objectID]
}

func (e campaignNPCLobSchedule) resume(timestamp uint64) ([][]byte, error) {
	if e.plan.Profile.AbilityName == "HurlMagma" ||
		e.plan.Profile.AbilityName == "NomadShielderGrenade" {
		return e.runtime.produceEnemyMelee(
			e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
		)
	}
	return e.runtime.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (e campaignNPCLobSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCLobs[e.projectileObjectID] == e.run
}

func (e campaignNPCLobSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(peerSession, isFound) {
		delete(peerSession.campaignNPCLobs, e.projectileObjectID)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignNPCLobSchedule) terminate() {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(peerSession, isFound) {
		delete(peerSession.campaignNPCLobs, e.projectileObjectID)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
}

func (e campaignNPCLobSchedule) launch() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound) &&
		peerSession.isCampaignNPCSourceActive(e.generation, e.objectID)
	if isCurrent {
		e.run.isLaunched = true
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := abilityraknet.TossLaunchForTeam(
		e.toss, e.projectileObjectID, e.timestamp, 0,
	)
	if err != nil {
		return e.fail("enemyLobLaunch", err)
	}
	return packets, nil
}

func (e campaignNPCLobSchedule) landing() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isSleepMushroom := e.plan.Profile.AbilityName == "SleepMushroom"
	isGrenade := e.plan.Profile.AbilityName == "CitadelGrenadeRoll"
	isMagma := e.plan.Profile.AbilityName == "HurlMagma"
	isShielderGrenade := e.plan.Profile.AbilityName == "NomadShielderGrenade"
	if !isSleepMushroom && !isMagma &&
		(!isShielderGrenade || len(e.submunitions) == 0) {
		delete(peerSession.campaignNPCLobs, e.projectileObjectID)
		e.run.cancel = nil
	}
	if !e.run.isLaunched {
		delete(peerSession.campaignNPCLobs, e.projectileObjectID)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if isGrenade || isShielderGrenade {
		source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
		if !isSourceFound || peerSession.zone.NPCRandom() == nil {
			e.runtime.registry.mutex.Unlock()
			e.terminate()
			return nil, nil
		}
		damagePackets := make([][]byte, 0)
		statDelta := sporenet.PlayerStatDelta{}
		center := game.Vec3{
			X: e.toss.Destination.X, Y: e.toss.Destination.Y,
			Z: e.toss.Destination.Z,
		}
		for _, target := range campaignLobAreaTargets(
			peerSession.zone.LiveNPCTargets(), center, e.plan.Profile.Radius,
		) {
			plan, planErr := zonenpc.PlanAreaAttackWithProfile(
				source, target.ObjectID, target.Position, e.plan.Profile,
			)
			if planErr != nil {
				continue
			}
			result, commitErr := zonenpc.CommitAttack(
				peerSession.zone.NPCRandom(), plan,
				source.Plan.NPCProfile.CriticalRating, e.runtime.program.Critical,
			)
			if commitErr != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail("enemyGrenadeCommit", commitErr)
			}
			hitPackets, targetStatDelta, _, damageErr :=
				e.runtime.applyEnemyAreaAttackDamage(
					&peerSession, e.generation, plan, result,
					e.landingTimestamp(),
				)
			if damageErr != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail("enemyGrenadeDamage", damageErr)
			}
			damagePackets = append(damagePackets, hitPackets...)
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		landingPackets, err := abilityraknet.TossDirectLanding(
			e.toss, e.projectileObjectID, damagePackets,
		)
		if err != nil {
			return e.fail("enemyGrenadeLanding", err)
		}
		if isShielderGrenade {
			for _, submunition := range e.submunitions {
				launchPackets, launchErr := abilityraknet.TossLaunchForTeam(
					submunition.toss, submunition.objectID,
					e.landingTimestamp(), 0,
				)
				if launchErr != nil {
					return e.fail("enemySubmunitionLaunch", launchErr)
				}
				landingPackets = append(landingPackets, launchPackets...)
			}
		}
		err = e.runtime.stats.Record(context.Background(), e.binding, statDelta)
		if err != nil {
			e.runtime.logger.Printf(
				"RakNet campaign NPC grenade stats omitted object=%d: %v",
				e.objectID, err,
			)
		}
		return landingPackets, nil
	}
	if isSleepMushroom {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		landingPackets, err := abilityraknet.TossDirectLanding(
			e.toss, e.projectileObjectID, nil,
		)
		if err != nil {
			return e.fail("enemySleepMushroomLanding", err)
		}
		spawnPacket, err := abilityraknet.TrapSpawnForTeam(
			e.retainedObjectID, e.objectID,
			raknet.Vector3{
				X: e.toss.Destination.X, Y: e.toss.Destination.Y,
				Z: e.toss.Destination.Z,
			},
			e.toss.Definition, 0,
		)
		if err != nil {
			return e.fail("enemySleepMushroomSpawn", err)
		}
		sleepPackets, err := e.sleep(false, e.landingTimestamp())
		if err != nil {
			return e.fail("enemySleepMushroomFirstTick", err)
		}
		packets := append(landingPackets, spawnPacket)
		return append(packets, sleepPackets...), nil
	}
	if isMagma {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		landingPackets, err := abilityraknet.TossDirectLanding(
			e.toss, e.projectileObjectID, nil,
		)
		if err != nil {
			return e.fail("enemyMagmaLanding", err)
		}
		spawnDefinition := e.toss.Definition
		spawnDefinition.SpawnNoun = e.plan.Profile.RetainedObjectNoun
		spawnPacket, err := abilityraknet.TrapSpawnForTeam(
			e.retainedObjectID, e.objectID,
			raknet.Vector3{X: e.toss.Destination.X, Y: e.toss.Destination.Y, Z: e.toss.Destination.Z},
			spawnDefinition, 0,
		)
		if err != nil {
			return e.fail("enemyMagmaSpawn", err)
		}
		effectPacket, err := abilityraknet.TrapObjectEffect(
			e.plan.Profile.RetainedEffectName, e.retainedObjectID, e.objectID,
		)
		if err != nil {
			return e.fail("enemyMagmaEffect", err)
		}
		tickPackets, err := e.magmaTick(e.landingTimestamp())
		if err != nil {
			return e.fail("enemyMagmaFirstTick", err)
		}
		packets := append(landingPackets, spawnPacket, effectPacket)
		return append(packets, tickPackets...), nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, e.plan.TargetObjectID,
	)
	isCollision := isTargetFound && zonegeometry.Distance(
		target.Position,
		game.Vec3{X: e.toss.Destination.X, Y: e.toss.Destination.Y, Z: e.toss.Destination.Z},
	) <= target.FootprintRadius+e.plan.Profile.Radius
	damagePackets := make([][]byte, 0)
	nashiraPlans := make([]zonenpc.SpawnPlan, 0, 1)
	nashiraPackets := make([][]byte, 0)
	reflection := thornBarkReflection{}
	isStatusEligible := false
	statDelta := sporenet.PlayerStatDelta{}
	if isCollision {
		isLocalTarget := target.IsHero &&
			target.UserID == peerSession.binding.UserID &&
			target.PeerGeneration == e.generation
		isDamageImmune := isLocalTarget &&
			((peerSession.ghostFormRun != nil &&
				peerSession.ghostFormRun.IsActive(
					peerSession.deployedCreatureIndex,
					peerSession.deployedHitPoint() > 0,
				)) || peerSession.heroModifierRun.IsDamageImmune())
		var isApplied bool
		var err error
		damagePackets, statDelta, reflection, isApplied, err =
			e.runtime.applyEnemyAttackDamage(
				&peerSession, e.generation, e.plan, e.result,
				e.timestamp+uint64((e.plan.Profile.HitDelay+e.toss.Lob.Duration)/time.Millisecond),
			)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("enemyLobDamage", err)
		}
		liveTarget, isLiveTargetFound := peerSession.campaignNPCTarget(
			e.generation, e.plan.TargetObjectID,
		)
		isStatusEligible = isApplied && !isDamageImmune &&
			isLiveTargetFound && liveTarget.HitPoint > 0
	}
	if e.plan.Profile.AbilityName == "ShadowToss" {
		var spawnErr error
		nashiraPlans, nashiraPackets, spawnErr = e.runtime.planCampaignNashiraFiend(
			&peerSession, e.objectID,
			game.Vec3{
				X: e.toss.Destination.X, Y: e.toss.Destination.Y,
				Z: e.toss.Destination.Z,
			},
			e.landingTimestamp(),
		)
		if spawnErr != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("enemyNashiraFiend", spawnErr)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	reflectionPackets, reflectionErr := e.runtime.publishThornBarkReflection(
		e.packet, e.sessionKey, e.generation, e.landingTimestamp(),
		e.binding, reflection,
	)
	if reflectionErr != nil {
		e.runtime.logger.Printf(
			"RakNet campaign NPC lob reflection omitted object=%d: %v",
			e.objectID, reflectionErr,
		)
	} else {
		damagePackets = append(damagePackets, reflectionPackets...)
	}

	landingPackets, err := abilityraknet.TossDirectLanding(
		e.toss, e.projectileObjectID, damagePackets,
	)
	if err != nil {
		return e.fail("enemyLobLanding", err)
	}
	landingPackets = append(landingPackets, nashiraPackets...)
	if len(nashiraPlans) != 0 {
		actionPackets, actionErr := e.runtime.scheduleFirstActions(
			e.packet, e.sessionKey, e.generation, nashiraPlans,
			e.landingTimestamp(),
		)
		if actionErr != nil {
			return e.fail("enemyNashiraFiendAction", actionErr)
		}
		landingPackets = append(landingPackets, actionPackets...)
	}
	if isStatusEligible {
		poisonPackets, poisonErr := e.runtime.applyCampaignNPCPoison(
			e.packet, e.sessionKey, e.generation, e.plan,
			e.timestamp+uint64((e.plan.Profile.HitDelay+e.toss.Lob.Duration)/time.Millisecond),
		)
		if poisonErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign NPC lob poison omitted object=%d: %v",
				e.objectID, poisonErr,
			)
		} else {
			landingPackets = append(landingPackets, poisonPackets...)
		}
	}
	err = e.runtime.stats.Record(context.Background(), e.binding, statDelta)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet campaign NPC lob stats omitted object=%d: %v",
			e.objectID, err,
		)
	}
	return landingPackets, nil
}

func (e campaignNPCLobSchedule) landingTimestamp() uint64 {
	return e.timestamp +
		uint64((e.plan.Profile.HitDelay+e.toss.Lob.Duration)/time.Millisecond)
}

type campaignNPCSleepMushroomStep struct {
	schedule campaignNPCLobSchedule
	delay    time.Duration
	isFinal  bool
}

func (e campaignNPCSleepMushroomStep) produce() ([][]byte, error) {
	timestamp := e.schedule.landingTimestamp() + uint64(e.delay/time.Millisecond)
	return e.schedule.sleep(e.isFinal, timestamp)
}

func campaignLobAreaTargets(
	targets []zone.NPCTarget, center game.Vec3, radius float32,
) []zone.NPCTarget {
	if radius <= 0 {
		return nil
	}
	result := make([]zone.NPCTarget, 0, len(targets))
	for _, target := range targets {
		if target.ObjectID == 0 || target.HitPoint <= 0 ||
			zonegeometry.Distance(target.Position, center) >
				target.FootprintRadius+radius {
			continue
		}
		result = append(result, target)
	}
	return result
}

func (e campaignNPCLobSchedule) sleep(
	isFinal bool, timestamp uint64,
) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	targets := []zone.NPCTarget(nil)
	if isCurrent {
		targets = campaignLobAreaTargets(
			peerSession.zone.LiveNPCTargets(),
			game.Vec3{
				X: e.toss.Destination.X, Y: e.toss.Destination.Y,
				Z: e.toss.Destination.Z,
			},
			e.plan.Profile.Radius,
		)
	}
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, target := range targets {
		plan := e.plan
		plan.TargetObjectID = target.ObjectID
		plan.TargetPosition = target.Position
		modifierPackets, err := e.runtime.applyCampaignNPCTimedModifier(
			e.packet, e.sessionKey, e.generation, plan, timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("enemySleepMushroomModifier: %w", err)
		}
		packets = append(packets, modifierPackets...)
	}
	if !isFinal {
		return packets, nil
	}
	e.runtime.registry.mutex.Lock()
	latest, isLatestFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(latest, isLatestFound) {
		delete(latest.campaignNPCLobs, e.projectileObjectID)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = latest
	}
	e.runtime.registry.mutex.Unlock()
	deletePacket, err := abilityraknet.TrapDelete(e.retainedObjectID)
	if err != nil {
		return nil, fmt.Errorf("enemySleepMushroomDelete: %w", err)
	}
	return append(packets, deletePacket), nil
}

type campaignNPCMagmaStep struct {
	schedule        campaignNPCLobSchedule
	delay           time.Duration
	isEffectRemoval bool
	isFinal         bool
}

func (e campaignNPCMagmaStep) produce() ([][]byte, error) {
	timestamp := e.schedule.landingTimestamp() + uint64(e.delay/time.Millisecond)
	if e.isFinal {
		return e.schedule.finishMagma()
	}
	if e.isEffectRemoval {
		e.schedule.runtime.registry.mutex.RLock()
		peerSession, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
		isCurrent := e.schedule.isCurrent(peerSession, isFound)
		e.schedule.runtime.registry.mutex.RUnlock()
		if !isCurrent {
			return nil, nil
		}
		packet, err := abilityraknet.TrapRemoveEffect(e.schedule.retainedObjectID)
		if err != nil {
			return nil, fmt.Errorf("enemyMagmaEffectRemove: %w", err)
		}
		return [][]byte{packet}, nil
	}
	return e.schedule.magmaTick(timestamp)
}

func (e campaignNPCLobSchedule) magmaTick(timestamp uint64) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	center := game.Vec3{X: e.toss.Destination.X, Y: e.toss.Destination.Y, Z: e.toss.Destination.Z}
	packets := make([][]byte, 0)
	burnPlan := make([]zonenpc.AttackPlan, 0)
	statDelta := sporenet.PlayerStatDelta{}
	for _, target := range campaignLobAreaTargets(
		peerSession.zone.LiveNPCTargets(), center, e.plan.Profile.Radius,
	) {
		plan, planErr := zonenpc.PlanRetainedAreaAttackWithProfile(
			e.source, target.ObjectID, target.Position, e.plan.Profile,
		)
		if planErr != nil {
			continue
		}
		result, commitErr := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			e.source.Plan.NPCProfile.CriticalRating, e.runtime.program.Critical,
		)
		if commitErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyMagmaCommit: %w", commitErr)
		}
		hitPackets, targetStatDelta, isApplied, damageErr :=
			e.runtime.applyEnemyAreaAttackDamage(
				&peerSession, e.generation, plan, result, timestamp,
			)
		if damageErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyMagmaDamage: %w", damageErr)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		liveTarget, isLiveTargetFound := peerSession.campaignNPCTarget(
			e.generation, target.ObjectID,
		)
		if isApplied && isLiveTargetFound && liveTarget.HitPoint > 0 {
			burnPlan = append(burnPlan, plan)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	for _, plan := range burnPlan {
		modifierPackets, err := e.runtime.applyCampaignNPCPoison(
			e.packet, e.sessionKey, e.generation, plan, timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("enemyMagmaBurn: %w", err)
		}
		packets = append(packets, modifierPackets...)
	}
	err := e.runtime.stats.Record(context.Background(), e.binding, statDelta)
	if err != nil {
		return nil, fmt.Errorf("enemyMagmaStats: %w", err)
	}
	return packets, nil
}

func (e campaignNPCLobSchedule) finishMagma() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	delete(peerSession.campaignNPCLobs, e.projectileObjectID)
	e.run.cancel = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	deletePacket, err := abilityraknet.TrapDelete(e.retainedObjectID)
	if err != nil {
		return nil, fmt.Errorf("enemyMagmaDelete: %w", err)
	}
	return [][]byte{deletePacket}, nil
}

func (e campaignNPCLobSchedule) next() ([][]byte, error) {
	delay := e.plan.Profile.Cooldown
	if e.plan.Profile.AbilityName == "HurlMagma" ||
		e.plan.Profile.AbilityName == "NomadShielderGrenade" ||
		e.plan.Profile.AbilityName == "SleepMushroom" {
		delay = e.plan.Profile.ReleaseDelay
	}
	timestamp := e.timestamp + uint64(delay/time.Millisecond)
	packets, err := e.resume(timestamp)
	if err != nil {
		return e.fail("enemyLobNext", err)
	}
	return packets, nil
}

type campaignNPCSubmunitionStep struct {
	schedule    campaignNPCLobSchedule
	submunition campaignNPCSubmunition
	isFinal     bool
}

func buildCampaignNPCSubmunitions(
	profile zonenpc.ActionProfile, primary zoneability.TossPlan,
	firstObjectID uint32, random *sim.SimulatorRandom,
) ([]campaignNPCSubmunition, error) {
	if profile.SubmunitionCount == 0 {
		return nil, nil
	}
	if firstObjectID == 0 || random == nil ||
		profile.SubmunitionMinimumDistance <= 0 ||
		profile.SubmunitionMaximumDistance < profile.SubmunitionMinimumDistance ||
		profile.SubmunitionRadius <= 0 ||
		profile.SubmunitionMinimumDamage <= 0 ||
		profile.SubmunitionMaximumDamage < profile.SubmunitionMinimumDamage {
		return nil, errors.New("invalid submunitions profile")
	}
	submunitionProfile := profile
	submunitionProfile.AbilityName = "NomadShielderSubmunition"
	submunitionProfile.MinimumDamage = profile.SubmunitionMinimumDamage
	submunitionProfile.MaximumDamage = profile.SubmunitionMaximumDamage
	submunitionProfile.Radius = profile.SubmunitionRadius
	submunitionProfile.TrailEffectName = profile.SubmunitionTrailEffectName
	submunitionProfile.ImpactEffectName = profile.SubmunitionImpactEffectName
	submunitionProfile.ProjectileSpeed = profile.SubmunitionProjectileSpeed
	submunitionProfile.ProjectileHeight = profile.SubmunitionProjectileHeight
	definition := campaignNPCTossDefinition(submunitionProfile)
	definition.Toss.Offset = sim.Position{}
	start := primary.Destination
	baseAngle := math.Atan2(
		float64(primary.Destination.Y-primary.LaunchPosition.Y),
		float64(primary.Destination.X-primary.LaunchPosition.X),
	)
	sectorAngle := 2 * math.Pi / float64(profile.SubmunitionCount)
	result := make([]campaignNPCSubmunition, 0, profile.SubmunitionCount)
	for index := uint32(0); index < profile.SubmunitionCount; index++ {
		jitter := (random.Float64() - 0.5) * sectorAngle
		angle := baseAngle + float64(index+1)*sectorAngle + jitter
		distance := profile.SubmunitionMinimumDistance +
			float32(random.Float64())*(profile.SubmunitionMaximumDistance-profile.SubmunitionMinimumDistance)
		destination := sim.Position{
			X: start.X + float32(math.Cos(angle))*distance,
			Y: start.Y + float32(math.Sin(angle))*distance,
			Z: start.Z,
		}
		lob, err := sim.BuildTossLob(
			0, start, destination, definition.Toss.Height, 0,
			definition.Toss.Speed, 0, 0, true, false,
		)
		if err != nil {
			return nil, fmt.Errorf("submunitionTrajectory[%d]: %w", index, err)
		}
		cast := sim.TossCast{
			AnimationName: profile.AnimationName,
			Height:        definition.Toss.Height, Speed: definition.Toss.Speed,
		}
		result = append(result, campaignNPCSubmunition{
			objectID: firstObjectID + index, profile: submunitionProfile,
			toss: zoneability.TossPlan{
				SourceObjectID: primary.SourceObjectID,
				AbilityID:      util.HashID(submunitionProfile.AbilityName),
				Definition:     definition, Cast: cast,
				LaunchPosition: start, Destination: destination, Lob: lob,
			},
		})
	}
	sort.SliceStable(result, func(first int, second int) bool {
		if result[first].toss.Lob.Duration != result[second].toss.Lob.Duration {
			return result[first].toss.Lob.Duration < result[second].toss.Lob.Duration
		}
		return result[first].objectID < result[second].objectID
	})
	return result, nil
}

func (e campaignNPCSubmunitionStep) produce() ([][]byte, error) {
	e.schedule.runtime.registry.mutex.Lock()
	peerSession, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
	if !e.schedule.isCurrent(peerSession, isFound) || peerSession.zone.NPCRandom() == nil {
		e.schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	center := game.Vec3{
		X: e.submunition.toss.Destination.X,
		Y: e.submunition.toss.Destination.Y,
		Z: e.submunition.toss.Destination.Z,
	}
	damagePackets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	for _, target := range campaignLobAreaTargets(
		peerSession.zone.LiveNPCTargets(), center, e.submunition.profile.Radius,
	) {
		plan, planErr := zonenpc.PlanRetainedAreaAttackWithProfile(
			e.schedule.source, target.ObjectID, target.Position,
			e.submunition.profile,
		)
		if planErr != nil {
			continue
		}
		result, commitErr := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			e.schedule.source.Plan.NPCProfile.CriticalRating,
			e.schedule.runtime.program.Critical,
		)
		if commitErr != nil {
			e.schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemySubmunitionCommit: %w", commitErr)
		}
		hitPackets, targetStatDelta, _, damageErr :=
			e.schedule.runtime.applyEnemyAreaAttackDamage(
				&peerSession, e.schedule.generation, plan, result,
				e.schedule.landingTimestamp()+uint64(e.submunition.toss.Lob.Duration/time.Millisecond),
			)
		if damageErr != nil {
			e.schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemySubmunitionDamage: %w", damageErr)
		}
		damagePackets = append(damagePackets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
	}
	if e.isFinal {
		delete(peerSession.campaignNPCLobs, e.schedule.projectileObjectID)
		e.schedule.run.cancel = nil
	}
	e.schedule.runtime.registry.sessions[e.schedule.sessionKey] = peerSession
	e.schedule.runtime.registry.mutex.Unlock()
	packets, err := abilityraknet.TossDirectLanding(
		e.submunition.toss, e.submunition.objectID, damagePackets,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySubmunitionLanding: %w", err)
	}
	err = e.schedule.runtime.stats.Record(
		context.Background(), e.schedule.binding, statDelta,
	)
	if err != nil {
		return nil, fmt.Errorf("enemySubmunitionStats: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceEnemyLob(
	packet raknet.Packet,
	sessionKey string,
	generation uint64,
	objectID uint32,
	timestamp uint64,
) ([][]byte, error) {
	resume := campaignNPCLobSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyLobStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	if !r.isSilenced(sessionKey, generation, objectID) {
		packets, isHandled, panicErr := r.produceNashiraCombatPanic(packet, sessionKey, generation, objectID, timestamp)
		if panicErr != nil {
			return nil, fmt.Errorf("lobPanic: %w", panicErr)
		}
		if isHandled {
			return packets, nil
		}
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	magmaProfile, isMagmaFamily := zonenpc.NomadRuptionHurlMagmaProfile(
		enemy.Plan.NounName,
	)
	if isMagmaFamily {
		profile, isProfileFound = magmaProfile, true
	}
	shielderProfile, isShielderFamily := zonenpc.NomadShielderGrenadeProfile(
		enemy.Plan.NounName,
	)
	if isShielderFamily {
		profile, isProfileFound = shielderProfile, true
	}
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound ||
		!isProfileFound || !zonenpc.IsTossActionProfile(profile) {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	isSleepMushroom := profile.AbilityName == "SleepMushroom"
	isGrenade := profile.AbilityName == "CitadelGrenadeRoll"
	isMagma := profile.AbilityName == "HurlMagma"
	isShielderGrenade := profile.AbilityName == "NomadShielderGrenade"
	var plan zonenpc.AttackPlan
	var planErr error
	if isSleepMushroom {
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
	if planErr != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil || !action.IsPursuitNeeded {
			r.registry.mutex.Unlock()
			r.releaseAction(sessionKey, generation, objectID)
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyLobPursuitMarshal: %w", marshalErr)
		}
		r.registry.mutex.Unlock()
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, resume.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf(
				"RakNet campaign lob pursuit not scheduled object=%d: %v",
				objectID, scheduleErr,
			)
		}
		return pursuitPackets, nil
	}
	result := zonenpc.AttackResult{}
	if !isSleepMushroom && !isGrenade && !isMagma && !isShielderGrenade {
		if peerSession.zone.NPCRandom() == nil {
			r.registry.mutex.Unlock()
			return nil, errors.New("enemy lob random unavailable")
		}
		result, err = zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			enemy.Plan.NPCProfile.CriticalRating, r.program.Critical,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyLobCommit: %w", err)
		}
	}
	definition := campaignNPCTossDefinition(profile)
	distance := zonegeometry.Distance(enemy.Plan.Position, target.Position)
	cast, err := sim.ResolveTossCast(definition, distance)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyLobCast: %w", err)
	}
	plan.Profile.AnimationName = cast.AnimationName
	launchPosition, err := zoneability.TossLaunchPosition(
		enemy.Plan.Position, target.Position, cast.Offset,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyLobPosition: %w", err)
	}
	destination := sim.Position{
		X: target.Position.X, Y: target.Position.Y, Z: target.Position.Z,
	}
	lob, err := sim.BuildTossLob(
		profile.HitDelay, launchPosition, destination, cast.Height,
		definition.Toss.FlightTime, cast.Speed, 0, 0, true, false,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyLobTrajectory: %w", err)
	}
	objectIDCount := uint32(1) + profile.SubmunitionCount
	if isSleepMushroom || isMagma {
		objectIDCount = 2
	}
	projectileObjectID, err := peerSession.reserveCampaignProjectileIDs(
		objectIDCount, 1000,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyLobObjectID: %w", err)
	}
	tossPlan := zoneability.TossPlan{
		SourceObjectID: objectID, TargetObjectID: target.ObjectID,
		AbilityID: util.HashID(profile.AbilityName), Definition: definition,
		Cast: cast, LaunchPosition: launchPosition,
		Destination: destination, Lob: lob,
	}
	startPackets, err := marshalNPCAttack(peerSession.zone.NPCs(), plan, timestamp)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyLobStart: %w", err)
	}
	run := &campaignNPCLobRun{}
	submunitions := make([]campaignNPCSubmunition, 0, profile.SubmunitionCount)
	if isShielderGrenade && profile.SubmunitionCount > 0 {
		if peerSession.zone.NPCRandom() == nil {
			r.registry.mutex.Unlock()
			return nil, errors.New("enemy submunitions random unavailable")
		}
		submunitions, err = buildCampaignNPCSubmunitions(
			profile, tossPlan, projectileObjectID+1,
			peerSession.zone.NPCRandom(),
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemySubmunitionPlan: %w", err)
		}
	}
	if peerSession.campaignNPCLobs == nil {
		peerSession.campaignNPCLobs = make(map[uint32]*campaignNPCLobRun)
	}
	peerSession.campaignNPCLobs[projectileObjectID] = run
	previousSleepMushroomTimestamp := uint64(0)
	if isSleepMushroom {
		if peerSession.campaignNPCNextSleepMushrooms == nil {
			peerSession.campaignNPCNextSleepMushrooms = make(map[uint32]uint64)
		}
		previousSleepMushroomTimestamp = peerSession.campaignNPCNextSleepMushrooms[objectID]
		peerSession.campaignNPCNextSleepMushrooms[objectID] = timestamp +
			uint64(profile.Cooldown/time.Millisecond)
	}
	previousMagmaTimestamp := uint64(0)
	if isMagma {
		if peerSession.campaignNPCRuptionNextMagmas == nil {
			peerSession.campaignNPCRuptionNextMagmas = make(map[uint32]uint64)
		}
		previousMagmaTimestamp = peerSession.campaignNPCRuptionNextMagmas[objectID]
		peerSession.campaignNPCRuptionNextMagmas[objectID] = timestamp +
			uint64(profile.Cooldown/time.Millisecond)
	}
	previousGrenadeTimestamp := uint64(0)
	if isShielderGrenade {
		if peerSession.campaignNPCShielderNextGrenades == nil {
			peerSession.campaignNPCShielderNextGrenades = make(map[uint32]uint64)
		}
		previousGrenadeTimestamp = peerSession.campaignNPCShielderNextGrenades[objectID]
		peerSession.campaignNPCShielderNextGrenades[objectID] = timestamp +
			uint64(profile.Cooldown/time.Millisecond)
	}
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := campaignNPCLobSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		projectileObjectID: projectileObjectID, timestamp: timestamp,
		plan: plan, toss: tossPlan, result: result, source: enemy,
		submunitions: submunitions,
		binding:      binding, run: run,
	}
	if isSleepMushroom || isMagma {
		schedule.retainedObjectID = projectileObjectID + 1
	}
	landingDelay := profile.HitDelay + lob.Duration
	nextDelay := profile.Cooldown
	if isMagma || isShielderGrenade || isSleepMushroom {
		nextDelay = profile.ReleaseDelay
	}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: schedule.launch},
		{Delay: landingDelay, Produce: schedule.landing},
		{Delay: nextDelay, Produce: schedule.next},
	}
	if isShielderGrenade && len(submunitions) > 0 {
		for index, submunitionPlan := range submunitions {
			step := campaignNPCSubmunitionStep{
				schedule: schedule, submunition: submunitionPlan,
				isFinal: index+1 == len(submunitions),
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay:   landingDelay + submunitionPlan.toss.Lob.Duration,
				Produce: step.produce,
			})
		}
	}
	if isSleepMushroom {
		secondTick := campaignNPCSleepMushroomStep{
			schedule: schedule, delay: profile.TickDuration,
		}
		finalTick := campaignNPCSleepMushroomStep{
			schedule: schedule, delay: 2 * profile.TickDuration, isFinal: true,
		}
		producers = append(producers,
			raknet.ScheduledPacketProducer{
				Delay: landingDelay + secondTick.delay, Produce: secondTick.produce,
			},
			raknet.ScheduledPacketProducer{
				Delay: landingDelay + finalTick.delay, Produce: finalTick.produce,
			},
		)
	}
	if isMagma {
		for tick := uint32(1); tick < profile.NumberOfTicks; tick++ {
			delay := time.Duration(tick) * profile.TickDuration
			step := campaignNPCMagmaStep{schedule: schedule, delay: delay}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: landingDelay + delay, Produce: step.produce,
			})
		}
		effectDelay := time.Duration(profile.NumberOfTicks) * profile.TickDuration
		effectRemoval := campaignNPCMagmaStep{
			schedule: schedule, delay: effectDelay, isEffectRemoval: true,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: landingDelay + effectDelay, Produce: effectRemoval.produce,
		})
		cleanupDelay := effectDelay + 3*time.Second
		cleanup := campaignNPCMagmaStep{
			schedule: schedule, delay: cleanupDelay, isFinal: true,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: landingDelay + cleanupDelay, Produce: cleanup.produce,
		})
	}
	sortScheduledPacketProducersByDelay(producers)
	cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.registry.mutex.Lock()
		latest, isLatestFound := r.registry.sessions[sessionKey]
		if isLatestFound && latest.generation == generation &&
			latest.campaignNPCLobs[projectileObjectID] == run {
			delete(latest.campaignNPCLobs, projectileObjectID)
			if isSleepMushroom {
				latest.campaignNPCNextSleepMushrooms[objectID] = previousSleepMushroomTimestamp
			}
			if isMagma {
				latest.campaignNPCRuptionNextMagmas[objectID] = previousMagmaTimestamp
			}
			if isShielderGrenade {
				latest.campaignNPCShielderNextGrenades[objectID] = previousGrenadeTimestamp
			}
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyLobSchedule: %w", scheduleErr)
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	isTracked := isLatestFound && latest.generation == generation &&
		latest.campaignNPCLobs[projectileObjectID] == run
	if isTracked {
		run.cancel = cancel
		r.registry.sessions[sessionKey] = latest
	}
	r.registry.mutex.Unlock()
	if !isTracked {
		cancel()
		return nil, nil
	}
	return startPackets, nil
}

func campaignNPCTossDefinition(
	profile zonenpc.ActionProfile,
) sim.AbilityDefinition {
	offset := sim.Position{
		X: profile.ProjectileOffset.X,
		Y: profile.ProjectileOffset.Y,
		Z: profile.ProjectileOffset.Z,
	}
	nearAnimationName := profile.NearAnimationName
	if nearAnimationName == "" {
		nearAnimationName = profile.AnimationName
	}
	farAnimationName := profile.FarAnimationName
	if farAnimationName == "" {
		farAnimationName = profile.AnimationName
	}
	closeRange := profile.ProjectileCloseRange
	if closeRange <= 0 {
		closeRange = 2
	}
	definition := sim.AbilityDefinition{
		Name: profile.AbilityName, Kind: sim.AbilityKindToss,
		AnimationName: profile.AnimationName,
		HitDelay:      profile.HitDelay, ReleaseDelay: profile.ReleaseDelay,
		Cooldown: profile.Cooldown, Range: profile.Range,
		MinimumDamage: profile.MinimumDamage, MaximumDamage: profile.MaximumDamage,
		DamageCoefficient: profile.DamageCoefficient,
		Toss: sim.TossAbilityDefinition{
			NearAnimationName:    nearAnimationName,
			FarAnimationName:     farAnimationName,
			CloseRange:           closeRange,
			Height:               profile.ProjectileHeight,
			CloseHeight:          profile.ProjectileCloseHeight,
			Speed:                profile.ProjectileSpeed,
			CloseSpeed:           profile.ProjectileCloseSpeed,
			FlightTime:           profile.ProjectileFlightDuration,
			ProjectileNoun:       profile.ProjectileNoun,
			ProjectileEffectName: profile.TrailEffectName,
			ImpactEffectName:     profile.ImpactEffectName,
			Radius:               profile.Radius,
			Offset:               offset,
			CloseOffset: sim.Position{
				X: profile.ProjectileCloseOffset.X,
				Y: profile.ProjectileCloseOffset.Y,
				Z: profile.ProjectileCloseOffset.Z,
			},
			IsCloseOffsetXFound:       profile.IsProjectileCloseOffsetXFound,
			IsCloseOffsetYFound:       profile.IsProjectileCloseOffsetYFound,
			IsCloseOffsetZFound:       profile.IsProjectileCloseOffsetZFound,
			MaximumTargetCount:        1,
			IsMaximumTargetCountFound: true,
			IsStrikesGroundOnly:       true,
		},
	}
	if profile.AbilityName == "SleepMushroom" {
		definition.SpawnNoun = "Ability_Sprout_Shroom.Noun"
	}
	return definition
}
