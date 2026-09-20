package gameplay

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

var errCampaignAbilityNotSpecial = errors.New("campaign ability is not special")

const tutorialAbilityLessonObjectiveID = uint32(0xac4273f3)
const rideLightningShockDuration = 3 * time.Second

type plasmaWreathSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	sourceTime          uint64
	creatureIndex       uint32
	previousManaPoint   float32
	creature            game.GameplayCreature
	binding             game.GameplayBinding
	run                 *abilityraknet.PlasmaWreathRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

func (e plasmaWreathSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.plasmaWreathRun == e.run
}

func (e plasmaWreathSchedule) retire(reason string) [][]byte {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	packets := make([][]byte, 0)
	if isCurrent {
		packets, _ = e.run.Stop()
		peerSession.plasmaWreathRun = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.runtime.logger.Printf(
			"RakNet campaign Plasma Wreath retired source=%d reason=%s",
			e.sourceObjectID, reason,
		)
	}
	return packets
}

func (e plasmaWreathSchedule) rollback() {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(peerSession, isFound) {
		peerSession.plasmaWreathRun = nil
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	e.run.Abort()
}

func (e plasmaWreathSchedule) produceRelease() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e plasmaWreathSchedule) produceTick() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound) &&
		peerSession.deployedObjectID == e.sourceObjectID
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	tickCount := e.run.AdvanceTick()
	plan, err := zoneability.PlanPlasmaWreathHit(
		peerSession.zone.NPCs(), e.sourceObjectID,
		game.Vec3{
			X: peerSession.playerPosition.X,
			Y: peerSession.playerPosition.Y,
			Z: peerSession.playerPosition.Z,
		},
		e.creature,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignPlasmaWreathPlan: %w", err)
	}
	if plan.TargetObjectID == 0 {
		e.runtime.registry.mutex.Unlock()
		err = e.packet.ScheduleFunc(
			zoneability.PlasmaWreathCheckInterval, e.produceTick,
		)
		if err != nil {
			return e.retire("idle reschedule failed"), nil
		}
		return nil, nil
	}
	snapshot, isFound := peerSession.zone.NPCs().NPC(plan.TargetObjectID)
	if !isFound {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("campaignPlasmaWreathTarget: missing")
	}
	result, err := zoneability.CommitBasic(
		peerSession.zone.Population().Random(),
		peerSession.zone.NPCs(), plan, e.creature,
		peerSession.binding.Difficulty, e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignPlasmaWreathDamage: %w", err)
	}
	transition, err := peerSession.applyCampaignDamageTransition(result.Damage)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignPlasmaWreathTransition: %w", err)
	}
	orbPacket, isEmpty, err := e.run.ConsumeOrb()
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignPlasmaWreathOrb: %w", err)
	}
	cleanupPackets := make([][]byte, 0)
	if isEmpty {
		cleanupPackets, err = e.run.Stop()
		peerSession.plasmaWreathRun = nil
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignPlasmaWreathCleanup: %w", err)
	}
	timestamp := e.sourceTime + tickCount*uint64(
		zoneability.PlasmaWreathCheckInterval/time.Millisecond,
	)
	resultPackets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		timestamp, e.binding,
		[]zoneability.AreaResult{{
			Snapshot: snapshot, Damage: result.Damage,
			IsCritical: result.IsCritical, Definition: plan.Definition,
		}},
		[]campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignPlasmaWreathPublish: %w", err)
	}
	effectPackets, err := abilityraknet.PlasmaWreathHitPackets(
		e.sourceObjectID, plan.TargetObjectID,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignPlasmaWreathEffect: %w", err)
	}
	packets := [][]byte{orbPacket}
	packets = append(packets, resultPackets...)
	packets = append(packets, effectPackets...)
	packets = append(packets, cleanupPackets...)
	if !isEmpty {
		err = e.packet.ScheduleFunc(
			zoneability.PlasmaWreathCheckInterval, e.produceTick,
		)
		if err != nil {
			packets = append(
				packets, e.retire("hit reschedule failed")...,
			)
		}
	}
	return packets, nil
}

type areaSpecialSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	areaGeneration      uint64
	sourceObjectID      uint32
	targetObjectID      uint32
	targetPosition      game.Vec3
	creatureIndex       uint32
	previousManaPoint   float32
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	plan                zoneability.AreaPlan
	binding             game.GameplayBinding
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releaseAnimation    []byte
	releaseResponse     []byte
	isArcWeld           bool
	isZetawatt          bool
}

type areaSpecialHitStep struct {
	schedule areaSpecialSchedule
	index    int
}

type areaSpecialEffect struct {
	assetID uint32
}

func (e areaSpecialSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.areaBasicGeneration == e.areaGeneration
}

func (e areaSpecialSchedule) hitProducer(index int) raknet.ScheduledPacketProducer {
	step := areaSpecialHitStep{schedule: e, index: index}
	return raknet.ScheduledPacketProducer{
		Delay: e.plan.Definition.HitDelay +
			time.Duration(index)*zoneability.ArcWeldTimeBetweenChain,
		Produce: step.produce,
	}
}

func (e areaSpecialSchedule) releaseProducer() raknet.ScheduledPacketProducer {
	return raknet.ScheduledPacketProducer{
		Delay:   e.plan.Definition.ReleaseDelay,
		Produce: e.produceRelease,
	}
}

func (e areaSpecialSchedule) produceRelease() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound) &&
		peerSession.deployedObjectID == e.sourceObjectID
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releaseAnimation, e.releaseResponse}, nil
}

func (e areaSpecialSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.queuePackets([][]byte{e.releaseAnimation, e.releaseResponse})
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.runtime.logger.Printf(
			"RakNet campaign Shockwave stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (e areaSpecialEffect) produce(
	result zoneability.AreaResult,
) ([]byte, error) {
	packet, err := effectraknet.Event(effectraknet.EventRequest{
		AssetID: e.assetID, ObjectID: result.Damage.ObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("shockwaveEffect: %w", err)
	}
	return packet, nil
}

func (e areaSpecialHitStep) produce() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound := schedule.runtime.registry.sessions[schedule.sessionKey]
	if !schedule.isCurrent(peerSession, isFound) {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	livePlan := zoneability.AreaPlan{}
	var err error
	if schedule.isArcWeld {
		livePlan, err = zoneability.ArcWeldHitPlan(schedule.plan, e.index)
		if err != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("zoneability.ArcWeldHitPlan: %w", err)
		}
		liveTarget, isTargetFound := peerSession.zone.NPCs().NPC(
			livePlan.Target[0].Plan.ObjectID,
		)
		if !isTargetFound || liveTarget.IsDefeated || liveTarget.HitPoint <= 0 ||
			liveTarget.TargetObjectID != schedule.sourceObjectID {
			schedule.runtime.registry.mutex.Unlock()
			return nil, nil
		}
		livePlan.Target[0] = liveTarget
	} else {
		livePlan, err = zoneability.PlanShockwave(
			peerSession.zone.NPCs(), schedule.sourceObjectID,
			schedule.targetObjectID,
			game.Vec3{
				X: peerSession.playerPosition.X,
				Y: peerSession.playerPosition.Y,
				Z: peerSession.playerPosition.Z,
			},
			schedule.targetPosition, schedule.creature, schedule.definition,
		)
	}
	if schedule.isZetawatt {
		livePlan, err = zoneability.PlanZetawattBeam(
			peerSession.zone.NPCs(), schedule.sourceObjectID,
			game.Vec3{
				X: peerSession.playerPosition.X,
				Y: peerSession.playerPosition.Y,
				Z: peerSession.playerPosition.Z,
			},
			schedule.targetPosition, schedule.creature, schedule.definition,
		)
	}
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignShockwaveLivePlan: %w", err)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(),
		peerSession.zone.NPCs(), livePlan, schedule.creature,
		peerSession.binding.Difficulty, schedule.runtime.program.Critical,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignShockwaveDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for _, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(
			result.Damage,
		)
		if transitionErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignShockwaveTransition: %w", transitionErr,
			)
		}
		transitions = append(transitions, transition)
	}
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	schedule.runtime.registry.mutex.Unlock()
	hitTimestamp := schedule.packet.SourceTime + uint64(
		(schedule.plan.Definition.HitDelay+
			time.Duration(e.index)*zoneability.ArcWeldTimeBetweenChain)/
			time.Millisecond,
	)
	effect := areaSpecialEffect{
		assetID: util.HashID(livePlan.Definition.HitEffectName),
	}
	packets, err := schedule.runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, hitTimestamp, schedule.binding,
		results, transitions, effect.produce, !schedule.isZetawatt,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignShockwavePublish: %w", err)
	}
	if !schedule.isArcWeld && !schedule.isZetawatt {
		stunPackets, stunErr := schedule.runtime.damage.publishShockwaveStuns(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.sourceObjectID, hitTimestamp, results,
		)
		if stunErr != nil {
			return nil, fmt.Errorf("campaignShockwaveStun: %w", stunErr)
		}
		packets = append(packets, stunPackets...)
	}
	if schedule.isArcWeld && len(results) != 0 {
		previousObjectID := schedule.sourceObjectID
		if e.index > 0 {
			previousObjectID = schedule.plan.Target[e.index-1].Plan.ObjectID
		}
		beamPacket, beamErr := effectraknet.Chain(
			zoneability.ArcWeldBeamAsset(e.index), previousObjectID,
			results[0].Damage.ObjectID,
		)
		if beamErr != nil {
			return nil, fmt.Errorf("arcWeldBeamEffect: %w", beamErr)
		}
		packets = append([][]byte{beamPacket}, packets...)
	}
	if schedule.isZetawatt {
		beamPacket, beamErr := effectraknet.Beam(effectraknet.BeamRequest{
			AssetID:        util.HashID(livePlan.Definition.ImpactEffectName),
			SourceObjectID: schedule.sourceObjectID,
			TargetPoint:    livePlan.Center,
		})
		if beamErr != nil {
			return nil, fmt.Errorf("zetawattBeamEffect: %w", beamErr)
		}
		packets = append([][]byte{beamPacket}, packets...)
	}
	return packets, nil
}

type wraithActiveSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	activeGeneration    uint64
	sourceObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	creature            game.GameplayCreature
	plan                zoneability.AreaPlan
	binding             game.GameplayBinding
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releaseResponse     []byte
}

func (e wraithActiveSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.wraithActiveGeneration == e.activeGeneration
}

func (e wraithActiveSchedule) produceHit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	livePlan, err := zoneability.PlanArea(
		peerSession.zone.NPCs(), e.sourceObjectID,
		game.Vec3{
			X: peerSession.playerPosition.X,
			Y: peerSession.playerPosition.Y,
			Z: peerSession.playerPosition.Z,
		},
		e.creature, e.plan.Definition,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignWraithLivePlan: %w", err)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(),
		peerSession.zone.NPCs(), livePlan, e.creature,
		peerSession.binding.Difficulty, e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignWraithDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for _, result := range results {
		transition, transitionErr :=
			peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignWraithTransition: %w", transitionErr,
			)
		}
		transitions = append(transitions, transition)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	var effect func(zoneability.AreaResult) ([]byte, error)
	if e.plan.Definition.ImpactEffectName != "" {
		effect = areaSpecialEffect{
			assetID: util.HashID(
				e.plan.Definition.ImpactEffectName,
			),
		}.produce
	}
	timestamp := e.packet.SourceTime + uint64(
		e.plan.Definition.HitDelay/time.Millisecond,
	)
	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		timestamp, e.binding, results, transitions, effect, false,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignWraithPublish: %w", err)
	}
	if e.plan.Definition.StatusKind != sim.AbilityStatusKindFear ||
		e.plan.Definition.StatusDuration <= 0 {
		return packets, nil
	}
	for index, result := range results {
		isFeared := !result.Damage.IsDefeated &&
			!result.Damage.IsShieldStarted &&
			!result.Damage.IsTurtleStarted &&
			!result.Damage.IsDamageImmune
		if !isFeared {
			continue
		}
		fearPackets, fearErr := e.runtime.applyHeroNPCFear(
			e.packet, e.sessionKey, e.generation, result.Damage.ObjectID,
			e.sourceObjectID, e.plan.Definition.StatusDuration, timestamp,
		)
		if fearErr != nil {
			return nil, fmt.Errorf("campaignWraithFear[%d]: %w", index, fearErr)
		}
		packets = append(packets, fearPackets...)
	}
	return packets, nil
}

func (e wraithActiveSchedule) produceRelease() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releaseResponse}, nil
}

func (e wraithActiveSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(
			e.cooldownReservation,
		)
		peerSession.abilityReleaseSession().Rollback(
			e.releaseReservation,
		)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.runtime.logger.Printf(
			"RakNet campaign Death's Embrace stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

type treeOfLifeSchedule struct {
	runtime             campaignAbilityCommandRuntime
	sessionKey          string
	generation          uint64
	objectID            uint32
	actorObjectID       uint32
	creatureIndex       uint32
	previousManaPoint   float32
	position            raknet.Vector3
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	run                 *abilityraknet.AreaHealingRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releaseResponse     []byte
}

type treeOfLifeStep struct {
	schedule treeOfLifeSchedule
	deadline time.Duration
}

func (e treeOfLifeSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.treeOfLifeRun == e.run &&
		peerSession.treeOfLifeObjectID == e.objectID
}

func (e treeOfLifeSchedule) producer(
	deadline time.Duration,
) raknet.ScheduledPacketProducer {
	step := treeOfLifeStep{schedule: e, deadline: deadline}
	return raknet.ScheduledPacketProducer{
		Delay: deadline, Produce: step.produce,
	}
}

func (e treeOfLifeSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.treeOfLifeObjectID = 0
		peerSession.treeOfLifeRun = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.run.Stop()
		e.runtime.logger.Printf(
			"RakNet campaign Tree of Life stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (e treeOfLifeSchedule) rollbackAdmission() {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.generation == e.generation {
		peerSession.abilityCooldownSession().Rollback(
			e.cooldownReservation,
		)
		peerSession.abilityReleaseSession().Rollback(
			e.releaseReservation,
		)
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
}

func (e treeOfLifeStep) produce() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound :=
		schedule.runtime.registry.sessions[schedule.sessionKey]
	if !schedule.isCurrent(peerSession, isFound) {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	output, err := schedule.run.Advance(context.Background(), e.deadline)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTreeAdvance: %w", err)
	}
	healing := make([]zoneSquadHealing, 0)
	resourcePackets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	for _, pulse := range output.Pulse {
		selectedHealing := pulse.HealingMinimum
		if pulse.HealingMinimum != pulse.HealingMaximum {
			random, randomErr := peerSession.abilityRandom()
			if randomErr != nil {
				schedule.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"campaignTreeRandom: %w", randomErr,
				)
			}
			selectedHealing, err = sim.SelectRankDamage(
				random,
				sim.DamageRange{
					Minimum: pulse.HealingMinimum,
					Maximum: pulse.HealingMaximum,
				},
			)
			if err != nil {
				schedule.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignTreeSelect: %w", err)
			}
		}
		projectedHealing, projectionErr := zoneability.ProjectHealing(
			schedule.creature, schedule.definition, selectedHealing,
		)
		if projectionErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignTreeProjection: %w", projectionErr,
			)
		}
		pulseHealing, healErr :=
			peerSession.healLivingZoneSquad(projectedHealing)
		if healErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreeHeal: %w", healErr)
		}
		pulseStatDelta := zoneHealingStatDelta(pulseHealing)
		statDelta.PVEHealing += pulseStatDelta.PVEHealing
		statDelta.PVEHealingReceived +=
			pulseStatDelta.PVEHealingReceived
		healing = append(healing, pulseHealing...)
		companionHealing, companionHealErr :=
			peerSession.healLivingZoneCompanions(projectedHealing)
		if companionHealErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignTreeCompanionHeal: %w", companionHealErr,
			)
		}
		companionStatDelta := zoneHealingStatDelta(companionHealing)
		statDelta.PVEHealing += companionStatDelta.PVEHealing
		statDelta.PVEHealingReceived +=
			companionStatDelta.PVEHealingReceived
		healing = append(healing, companionHealing...)
	}
	for _, healedCharacter := range healing {
		if healedCharacter.objectID == 0 {
			continue
		}
		if healedCharacter.isCompanion {
			resourcePacket, resourceErr := raknet.MarshalApplication(
				raknet.CombatantDataDeltaMessage{
					ObjectID:          healedCharacter.objectID,
					HitPoints:         healedCharacter.hitPoint,
					IsHitPointChanged: true,
				},
			)
			if resourceErr != nil {
				schedule.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"campaignTreeCompanionResource: %w", resourceErr,
				)
			}
			resourcePackets = append(resourcePackets, resourcePacket)
			continue
		}
		resourcePacket, resourceErr :=
			peerSession.marshalCampaignCharacterResource(
				healedCharacter.objectID - 1,
			)
		if resourceErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignTreeResource: %w", resourceErr,
			)
		}
		resourcePackets = append(resourcePackets, resourcePacket)
	}
	isFinal := len(output.Cleanup) != 0
	if isFinal {
		peerSession.treeOfLifeObjectID = 0
		peerSession.treeOfLifeRun = nil
		schedule.run.ClearCancel()
	}
	binding := peerSession.binding
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	schedule.runtime.registry.mutex.Unlock()

	err = schedule.runtime.stats.Record(
		context.Background(), binding, statDelta,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignTreeStats: %w", err)
	}
	packets := output.Packet
	if e.deadline == schedule.definition.ReleaseDelay {
		packets = append(packets, schedule.releaseResponse)
	}
	if len(output.Pulse) != 0 {
		projectedHealing := make([]abilityraknet.Healing, 0, len(healing))
		for _, healedCharacter := range healing {
			projectedHealing = append(
				projectedHealing,
				abilityraknet.Healing{
					SourceObjectID: schedule.actorObjectID,
					ObjectID:       healedCharacter.objectID,
					HitPoint:       healedCharacter.hitPoint,
					Amount:         healedCharacter.amount,
				},
			)
		}
		healPackets, healErr := abilityraknet.MarshalTreeOfLifeHealing(
			schedule.definition, schedule.position,
			projectedHealing, isFinal,
		)
		if healErr != nil {
			return nil, fmt.Errorf("campaignTreeMarshal: %w", healErr)
		}
		packets = append(packets, healPackets...)
	}
	packets = append(packets, resourcePackets...)
	return append(packets, output.Cleanup...), nil
}

type enrageSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	instanceID          uint32
	creatureIndex       uint32
	previousManaPoint   float32
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	run                 *abilityraknet.EnrageRun
	previousRun         *abilityraknet.EnrageRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
	isReplacingApplied  bool
}

type enrageTickStep struct {
	schedule enrageSchedule
	index    int
	deadline time.Duration
}

func (e enrageSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.enrageRun == e.run
}

func (e enrageSchedule) tickProducer(
	index int, deadline time.Duration,
) raknet.ScheduledPacketProducer {
	step := enrageTickStep{
		schedule: e, index: index, deadline: deadline,
	}
	return raknet.ScheduledPacketProducer{
		Delay: deadline, Produce: step.produce,
	}
}

func (e enrageSchedule) produceRelease() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e enrageTickStep) produce() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound :=
		schedule.runtime.registry.sessions[schedule.sessionKey]
	if !schedule.isCurrent(peerSession, isFound) {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, healedAmount, err := schedule.run.Apply(
		campaignEnrageState{session: &peerSession},
		schedule.definition,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond),
	)
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	schedule.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignEnrageTick[%d]: %w", e.index, err)
	}
	if healedAmount <= 0 {
		return packets, nil
	}
	err = schedule.runtime.stats.Record(
		context.Background(), schedule.binding,
		sporenet.PlayerStatDelta{
			PVEHealing:         float64(healedAmount),
			PVEHealingReceived: float64(healedAmount),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("campaignEnrageStats: %w", err)
	}
	return packets, nil
}

func (e enrageSchedule) produceExpiry() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	expiryPackets, err := e.run.Expire(
		campaignEnrageState{session: &peerSession},
	)
	peerSession.enrageRun = nil
	e.run.ClearCancel()
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	_ = e.runtime.modifierPool.Release(e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("campaignEnrageExpire: %w", err)
	}
	return expiryPackets, nil
}

func (e enrageSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		if e.isReplacingApplied {
			e.run.ReleaseEffect()
			peerSession.enrageRun = e.previousRun
		} else {
			_, _ = e.run.Expire(
				campaignEnrageState{session: &peerSession},
			)
			peerSession.enrageRun = nil
		}
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(
			e.cooldownReservation,
		)
		peerSession.abilityReleaseSession().Rollback(
			e.releaseReservation,
		)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		_ = e.runtime.modifierPool.Release(e.instanceID)
		e.runtime.logger.Printf(
			"RakNet campaign Enrage stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

type rideLightningSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	abilityID           uint32
	cooldownKey         zoneability.CooldownKey
	creatureIndex       uint32
	previousManaPoint   float32
	motionRevision      uint64
	previousMotion      zoneaction.MotionSnapshot
	hitDelay            time.Duration
	creature            game.GameplayCreature
	binding             game.GameplayBinding
	plan                zoneability.BasicPlan
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releaseAnimation    []byte
	releaseResponse     []byte
}

func (e rideLightningSchedule) produceImpact() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.zone.Population() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	liveNPC, isTargetFound := peerSession.zone.NPCs().NPC(e.plan.TargetObjectID)
	if !isTargetFound || liveNPC.IsDefeated || liveNPC.HitPoint <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	result, err := zoneability.CommitBasic(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), e.plan,
		e.creature, peerSession.binding.Difficulty, e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignRideDamage: %w", err)
	}
	transition, err := peerSession.applyCampaignDamageTransition(result.Damage)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignRideTransition: %w", err)
	}
	isCooldownReset := false
	if result.Damage.IsDefeated {
		isCooldownReset = peerSession.abilityCooldownSession().Reset(e.cooldownKey)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	timestamp := e.packet.SourceTime + uint64(e.hitDelay/time.Millisecond)
	areaResult := zoneability.AreaResult{
		Snapshot: liveNPC, Damage: result.Damage, IsCritical: result.IsCritical,
		Definition: e.plan.Definition,
	}
	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
		e.binding, []zoneability.AreaResult{areaResult},
		[]campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignRidePublish: %w", err)
	}
	if !result.Damage.IsDefeated {
		shockPackets, shockErr := e.runtime.damage.publishNPCStuns(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
			[]zoneability.AreaResult{areaResult}, util.HashID("ShockModifier"),
			rideLightningShockDuration,
		)
		if shockErr != nil {
			return nil, fmt.Errorf("campaignRideShock: %w", shockErr)
		}
		packets = append(packets, shockPackets...)
	}
	if isCooldownReset {
		resetPacket, resetErr := abilityraknet.CooldownReset(
			e.sourceObjectID, e.abilityID,
		)
		if resetErr != nil {
			return nil, fmt.Errorf("campaignRideCooldownReset: %w", resetErr)
		}
		packets = append([][]byte{resetPacket}, packets...)
	}
	e.runtime.logger.Printf(
		"RakNet campaign Ride the Lightning landed source=%d target=%d damage=%g defeated=%t",
		e.sourceObjectID, e.plan.TargetObjectID, result.Damage.Damage,
		result.Damage.IsDefeated,
	)
	return packets, nil
}

func (e rideLightningSchedule) produceRelease() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID
	if isCurrent {
		peerSession.rideCancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releaseAnimation, e.releaseResponse}, nil
}

func (e rideLightningSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.generation == e.generation {
		peerSession.rideCancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	e.runtime.logger.Printf(
		"RakNet campaign Ride the Lightning release stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (e rideLightningSchedule) rollbackAdmission() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation {
		return nil
	}
	err := peerSession.setCampaignCharacterManaPoints(
		e.creatureIndex, e.previousManaPoint,
	)
	if err != nil {
		return fmt.Errorf("campaignRideRollback: %w", err)
	}
	peerSession.restorePlayerMotion(e.previousMotion, e.motionRevision)
	peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
	peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	return nil
}

func (e rideLightningSchedule) bindCancel(cancel raknet.CancelSchedule) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID
	if isCurrent {
		peerSession.rideCancel = cancel
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		cancel()
	}
}

type targetedAOESchedule struct {
	runtime              campaignAbilityCommandRuntime
	packet               raknet.Packet
	sessionKey           string
	generation           uint64
	sourceObjectID       uint32
	effectObjectID       uint32
	creatureIndex        uint32
	previousManaPoint    float32
	previousNextObjectID uint32
	finalDeadline        time.Duration
	creature             game.GameplayCreature
	definition           sim.AbilityDefinition
	binding              game.GameplayBinding
	run                  *abilityraknet.TargetedAOERun
	cooldownReservation  zoneability.CooldownReservation
	releaseReservation   zoneaction.ReleaseReservation
	releaseResponse      []byte
}

type targetedAOEStep struct {
	schedule targetedAOESchedule
	deadline time.Duration
}

const supportHealerSlowMovementScale = float32(0.60)
const supportHealerSlowAttackScale = float32(0.60)

func (e targetedAOESchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.sphereAttack == e.run &&
		peerSession.sphereObjectID == e.effectObjectID
}

func (e targetedAOESchedule) producer(
	deadline time.Duration,
) raknet.ScheduledPacketProducer {
	step := targetedAOEStep{schedule: e, deadline: deadline}
	return raknet.ScheduledPacketProducer{
		Delay: deadline, Produce: step.produce,
	}
}

func (e targetedAOEStep) produce() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound :=
		schedule.runtime.registry.sessions[schedule.sessionKey]
	if !schedule.isCurrent(peerSession, isFound) {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.zone == nil || peerSession.zone.NPCs() == nil ||
		peerSession.zone.Population() == nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, errors.New("targetedAOEZone: unavailable")
	}
	outputPackets, pulses, err := schedule.run.Advance(
		context.Background(), e.deadline,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportAdvance: %w", err)
	}
	results := make([]zoneability.AreaResult, 0)
	transitions := make([]campaignDamageTransition, 0)
	healingReductionTarget := make(map[uint32]struct{})
	for _, pulse := range pulses {
		plan, planErr := zoneability.PlanTargetedAOEPulse(
			peerSession.zone.NPCs(), schedule.sourceObjectID,
			game.Vec3{
				X: pulse.Position.X,
				Y: pulse.Position.Y,
				Z: pulse.Position.Z,
			},
			schedule.creature, schedule.definition,
			pulse.DamageMinimum, pulse.DamageMaximum,
			pulse.DamageCoefficient,
		)
		if planErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignSupportPlan: %w", planErr)
		}
		if schedule.definition.Name == fireTempestSupportName {
			for _, target := range plan.Target {
				healingReductionTarget[target.Plan.ObjectID] = struct{}{}
			}
		}
		if schedule.definition.Name == "SupportHealerSupport" {
			slowDuration := max(
				schedule.definition.TickDuration+250*time.Millisecond,
				time.Second,
			)
			slowExpiresAt := schedule.runtime.now().Add(slowDuration)
			for _, target := range plan.Target {
				slowErr := peerSession.zone.NPCs().ApplySlow(
					target.Plan.ObjectID, slowExpiresAt,
					supportHealerSlowMovementScale,
					supportHealerSlowAttackScale,
				)
				if slowErr != nil {
					schedule.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("campaignSupportSlow: %w", slowErr)
				}
			}
		}
		if pulse.IsFirstTargetOnly && len(plan.Target) > 1 {
			plan.Target = plan.Target[:1]
		}
		pulseResults, commitErr := zoneability.CommitArea(
			peerSession.zone.Population().Random(),
			peerSession.zone.NPCs(), plan, schedule.creature,
			peerSession.binding.Difficulty,
			schedule.runtime.program.Critical,
		)
		if commitErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignSupportDamage: %w", commitErr)
		}
		for _, result := range pulseResults {
			transition, transitionErr :=
				peerSession.applyCampaignDamageTransition(result.Damage)
			if transitionErr != nil {
				schedule.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"campaignSupportTransition: %w", transitionErr,
				)
			}
			results = append(results, result)
			transitions = append(transitions, transition)
		}
	}
	if e.deadline == schedule.finalDeadline {
		peerSession.sphereAttack = nil
		peerSession.sphereObjectID = 0
		schedule.run.ClearCancel()
	}
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	schedule.runtime.registry.mutex.Unlock()

	resultPackets, err := schedule.runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond),
		schedule.binding, results, transitions, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignSupportPublish: %w", err)
	}
	outputPackets = append(outputPackets, resultPackets...)
	for targetObjectID := range healingReductionTarget {
		isDefeated := false
		for _, result := range results {
			if result.Damage.ObjectID == targetObjectID && result.Damage.IsDefeated {
				isDefeated = true
				break
			}
		}
		if isDefeated {
			continue
		}
		modifierPackets, modifierErr :=
			schedule.runtime.damage.applyHeroHealingReduction(
				schedule.packet, schedule.sessionKey, schedule.generation,
				schedule.sourceObjectID,
				schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond),
				schedule.definition, targetObjectID,
			)
		if modifierErr != nil {
			return nil, fmt.Errorf("campaignSupportHealingReduction: %w", modifierErr)
		}
		outputPackets = append(outputPackets, modifierPackets...)
	}
	if e.deadline == schedule.definition.ReleaseDelay {
		if len(schedule.releaseResponse) != 0 {
			outputPackets = append(outputPackets, schedule.releaseResponse)
		}
	}
	return outputPackets, nil
}

func (e targetedAOESchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.sphereAttack = nil
		peerSession.sphereObjectID = 0
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.run.Stop()
		e.runtime.logger.Printf(
			"RakNet campaign support stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (e targetedAOESchedule) rollbackAdmission() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation {
		return nil
	}
	err := peerSession.setCampaignCharacterManaPoints(
		e.creatureIndex, e.previousManaPoint,
	)
	if err != nil {
		return fmt.Errorf("campaignSupportRollback: %w", err)
	}
	peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
	peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
	peerSession.restoreCampaignProjectileID(e.previousNextObjectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	return nil
}

func (r campaignAbilityCommandRuntime) handleSpecial(
	request campaignCharacterAbilityRequest,
) ([][]byte, error) {
	var err error
	packet := request.packet
	command := request.command
	commandSession := request.commandSession
	abilityStartTime := request.startTime
	targetObjectID := request.targetObjectID
	deployedCreature := request.deployedCreature
	specialTwoAbility, isSpecialTwoFound := r.program.HeroAbility(
		deployedCreature.Noun, zonecontent.HeroAbilitySpecialTwo,
	)
	isSupportCreatureFound := command.Ability.Index >= 6 && command.Ability.Index <= 8
	supportCreatureIndex := uint32(0)
	if isSupportCreatureFound {
		supportCreatureIndex = command.Ability.Index - 6
		isSupportCreatureFound = supportCreatureIndex <
			uint32(len(commandSession.binding.Creatures))
	}
	supportCreature := game.GameplayCreature{}
	if isSupportCreatureFound {
		supportCreature = commandSession.binding.Creatures[supportCreatureIndex]
	}
	supportAbility, isSupportAbilityFound := r.program.HeroAbility(
		supportCreature.Noun, zonecontent.HeroAbilitySpecialOne,
	)
	isPlasmaWreathRequest := isSupportCreatureFound && isSupportAbilityFound &&
		supportAbility.AssetName == "LightningRogueSupport"
	isArcWeldRequest := isSupportCreatureFound && isSupportAbilityFound &&
		supportAbility.AssetName == "EnergySentinelActive"
	isShockwaveRequest := command.Ability.Index == 2 && isSpecialTwoFound &&
		specialTwoAbility.AssetName == "EnergySentinelSupport"
	isTeleportStrikeRequest := command.Ability.Index == 2 &&
		isSpecialTwoFound && specialTwoAbility.ID != 0 &&
		specialTwoAbility.Definition.Kind == sim.AbilityKindTeleportStrike
	isWraithActiveRequest := command.Ability.Index == 2 && isSpecialTwoFound &&
		specialTwoAbility.AssetName == "DeathsEmbrace"
	isTreeOfLifeRequest := command.Ability.Index == 2 && isSpecialTwoFound &&
		specialTwoAbility.AssetName == "TreeOfLife"
	randomAbility, isRandomAbilityFound := r.program.HeroAbility(
		deployedCreature.Noun, zonecontent.HeroAbilityRandom,
	)
	isEnrageRequest := command.Ability.Index == 3 && isRandomAbilityFound &&
		randomAbility.AssetName == "CastEnrage"
	isZetawattRequest := command.Ability.Index == 3 && isRandomAbilityFound &&
		randomAbility.AssetName == "TechRandom"
	specialTwoCooldownKey := zoneability.HeroAbilityCooldown(specialTwoAbility.ID)
	supportCooldownKey := zoneability.HeroAbilityCooldown(supportAbility.ID)
	randomCooldownKey := zoneability.HeroAbilityCooldown(randomAbility.ID)
	isArborealMightRequest := isSupportCreatureFound && isSupportAbilityFound &&
		supportAbility.AssetName == "ArborealMight"
	isSummonBeastRequest := isSupportCreatureFound && isSupportAbilityFound &&
		supportAbility.AssetName == "SummonBeast"
	if isSummonBeastRequest {
		return r.handleSummonBeast(request, supportAbility, supportCreature)
	}
	if isArborealMightRequest {
		return r.handleHeroSelfModifier(
			request, supportAbility, supportCooldownKey,
		)
	}
	if isPlasmaWreathRequest {
		if packet.ScheduleFunc == nil {
			return request.reject("Plasma Wreath schedule unavailable")
		}
		sessionKey := packet.Address.String()
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[sessionKey]
		isFound = isFound && peerSession.generation == commandSession.generation &&
			command.Common.ObjectID == peerSession.deployedObjectID &&
			peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
			peerSession.zone.NPCs() != nil &&
			peerSession.zone.Population() != nil &&
			peerSession.plasmaWreathRun == nil &&
			peerSession.abilityCooldownSession().IsReady(
				supportCooldownKey, abilityStartTime,
			) &&
			peerSession.isAbilityReleaseReady(abilityStartTime)
		if !isFound {
			r.registry.mutex.Unlock()
			return request.reject("session, cooldown, release, or Plasma Wreath modifier unavailable")
		}
		creatureIndex := peerSession.deployedCreatureIndex
		creature := r.registry.projectPassiveCreature(
			peerSession, creatureIndex, abilityStartTime,
		)
		definition, projectionErr := zoneability.ProjectTiming(
			creature, supportAbility.Definition,
		)
		if projectionErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPlasmaWreathTiming: %w", projectionErr)
		}
		manaCost, manaErr := game.ResolveAbilityManaCost(
			definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
			definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPlasmaWreathMana: %w", manaErr)
		}
		if peerSession.deployedManaPoint() < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("power unavailable")
		}
		instanceID, allocateErr := r.modifierPool.Allocate()
		if allocateErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPlasmaWreathModifier: %w", allocateErr)
		}
		run, modifierPackets, runErr := abilityraknet.NewPlasmaWreathRun(
			instanceID, command.Common.ObjectID, r.effectPool, r.modifierPool,
		)
		if runErr != nil {
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPlasmaWreathRun: %w", runErr)
		}
		remainingManaPoint := peerSession.deployedManaPoint() - manaCost
		ackPacket, marshalErr := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
			SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
			ObjectID: util.HashID(definition.Name), AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds: packet.SourceTime,
			SourceEndMilliseconds: packet.SourceTime +
				uint64(definition.ReleaseDelay/time.Millisecond),
		})
		if marshalErr != nil {
			run.Abort()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPlasmaWreathAck: %w", marshalErr)
		}
		releasePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], util.HashID(definition.Name),
			command.Ability.Index, packet.SourceTime,
			definition.HitDelay, definition.ReleaseDelay,
		)
		if marshalErr != nil {
			run.Abort()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPlasmaWreathRelease: %w", marshalErr)
		}
		startPackets, marshalErr := abilityraknet.StartSpendPresentation(
			command.Common.ObjectID, util.HashID(definition.Name),
			definition.AnimationName, definition.Cooldown,
			packet.SourceTime, remainingManaPoint,
		)
		if marshalErr != nil {
			run.Abort()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPlasmaWreathStart: %w", marshalErr)
		}
		previousManaPoint := peerSession.deployedManaPoint()
		err = peerSession.stopPlayerMovement(abilityStartTime)
		if err == nil {
			err = peerSession.setDeployedManaPoints(remainingManaPoint)
		}
		if err != nil {
			run.Abort()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPlasmaWreathCommit: %w", err)
		}
		cooldownReservation, isCooldownReserved :=
			peerSession.abilityCooldownSession().Reserve(
				supportCooldownKey,
				abilityStartTime, definition.Cooldown,
			)
		if !isCooldownReserved {
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			run.Abort()
			r.registry.mutex.Unlock()
			return request.reject("Plasma Wreath cooldown unavailable")
		}
		releaseReservation, isReleaseReserved :=
			peerSession.abilityReleaseSession().Reserve(
				abilityStartTime, definition.ReleaseDelay,
			)
		if !isReleaseReserved {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			run.Abort()
			r.registry.mutex.Unlock()
			return request.reject("Plasma Wreath release unavailable")
		}
		peerSession.plasmaWreathRun = run
		generation := peerSession.generation
		binding := peerSession.binding
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()

		schedule := plasmaWreathSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceObjectID: command.Common.ObjectID,
			sourceTime: packet.SourceTime, creatureIndex: creatureIndex,
			previousManaPoint: previousManaPoint, creature: creature,
			binding: binding, run: run,
			cooldownReservation: cooldownReservation,
			releaseReservation:  releaseReservation,
			releasePacket:       releasePacket,
		}
		err = packet.ScheduleFunc(
			definition.ReleaseDelay, schedule.produceRelease,
		)
		if err != nil {
			schedule.rollback()
			return nil, fmt.Errorf("campaignPlasmaWreathReleaseSchedule: %w", err)
		}
		err = packet.ScheduleFunc(
			zoneability.PlasmaWreathCheckInterval, schedule.produceTick,
		)
		if err != nil {
			schedule.rollback()
			return nil, fmt.Errorf("campaignPlasmaWreathSchedule: %w", err)
		}
		r.logger.Printf(
			"RakNet campaign Plasma Wreath accepted source=%d index=%d orbs=%d",
			command.Common.ObjectID, command.Ability.Index, zoneability.PlasmaWreathOrbCount,
		)
		packets := append([][]byte{ackPacket}, startPackets...)
		return append(packets, modifierPackets...), nil
	}
	if isArcWeldRequest || isShockwaveRequest || isZetawattRequest {
		if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
			return request.reject("schedule unavailable")
		}
		sessionKey := packet.Address.String()
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[sessionKey]
		cooldownKey := specialTwoCooldownKey
		if isArcWeldRequest {
			cooldownKey = supportCooldownKey
		}
		if isZetawattRequest {
			cooldownKey = randomCooldownKey
		}
		isCooldownReady := isFound && peerSession.abilityCooldownSession().IsReady(
			cooldownKey, abilityStartTime,
		)
		isFound = isFound && peerSession.generation == commandSession.generation &&
			command.Common.ObjectID == peerSession.deployedObjectID &&
			peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
			peerSession.zone.NPCs() != nil && peerSession.zone.Population() != nil &&
			isCooldownReady &&
			peerSession.isAbilityReleaseReady(abilityStartTime)
		if !isFound {
			r.registry.mutex.Unlock()
			return request.reject("session, cooldown, or release unavailable")
		}
		creatureIndex := peerSession.deployedCreatureIndex
		creature := r.registry.projectPassiveCreature(
			peerSession, creatureIndex, abilityStartTime,
		)
		definition := specialTwoAbility.Definition
		if isArcWeldRequest {
			definition = supportAbility.Definition
		}
		if isZetawattRequest {
			definition = randomAbility.Definition
		}
		targetPosition := command.Ability.TargetPosition
		if !isReportedZonePosition(targetPosition) {
			targetPosition = command.Ability.CursorPosition
		}
		err = peerSession.advancePlayerPosition(abilityStartTime, command.Common.Position)
		if err != nil {
			r.registry.mutex.Unlock()
			return request.reject("position unavailable")
		}
		sourcePosition := game.Vec3{
			X: peerSession.playerPosition.X, Y: peerSession.playerPosition.Y, Z: peerSession.playerPosition.Z,
		}
		storedTargetPosition := game.Vec3{X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z}
		plan, planErr := zoneability.PlanShockwave(
			peerSession.zone.NPCs(), command.Common.ObjectID, targetObjectID,
			sourcePosition, storedTargetPosition, creature, definition,
		)
		if isArcWeldRequest {
			plan, planErr = zoneability.PlanArcWeld(
				peerSession.zone.NPCs(), command.Common.ObjectID, targetObjectID,
				sourcePosition, creature, definition,
			)
		}
		if isZetawattRequest {
			plan, planErr = zoneability.PlanZetawattBeam(
				peerSession.zone.NPCs(), command.Common.ObjectID,
				sourcePosition, storedTargetPosition, creature, definition,
			)
		}
		if planErr != nil {
			r.registry.mutex.Unlock()
			return request.reject(planErr.Error())
		}
		manaCost, manaErr := game.ResolveAbilityManaCost(
			plan.Definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
			plan.Definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return request.reject("power projection unavailable")
		}
		if peerSession.deployedManaPoint() < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("power unavailable")
		}
		remainingManaPoint := peerSession.deployedManaPoint() - manaCost
		ackPacket, marshalErr := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
			SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
			ObjectID: plan.AbilityID, AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds:  packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime + uint64(plan.Definition.HitDelay/time.Millisecond),
			SourceEndMilliseconds:    packet.SourceTime + uint64(plan.Definition.ReleaseDelay/time.Millisecond),
		})
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignShockwaveAck: %w", marshalErr)
		}
		releaseResponsePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], plan.AbilityID, command.Ability.Index,
			packet.SourceTime, plan.Definition.HitDelay, plan.Definition.ReleaseDelay,
		)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignShockwaveReleaseResponse: %w", marshalErr)
		}
		// Spend presentation does not start an animation. Shockwave needs the
		// same server start time as its hit; the scheduled reset and release
		// response below terminate that animation's authority at release.
		startPackets, marshalErr := abilityraknet.SpendPresentation(
			command.Common.ObjectID, plan.AbilityID, plan.Definition.Cooldown,
			packet.SourceTime, remainingManaPoint,
		)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignShockwaveStart: %w", marshalErr)
		}
		if isShockwaveRequest || isZetawattRequest {
			animationPacket, animationErr := abilityraknet.Animation(
				command.Common.ObjectID, plan.Definition.AnimationName, packet.SourceTime,
			)
			if animationErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("shockwaveAnimation: %w", animationErr)
			}
			startPackets = append(startPackets, animationPacket)
		}
		releaseAnimationPacket, marshalErr := abilityraknet.AnimationReset(
			command.Common.ObjectID,
			packet.SourceTime+uint64(plan.Definition.ReleaseDelay/time.Millisecond),
		)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignShockwaveRelease: %w", marshalErr)
		}
		previousManaPoint := peerSession.deployedManaPoint()
		err = peerSession.stopPlayerMovement(abilityStartTime)
		if err == nil {
			err = peerSession.setDeployedManaPoints(remainingManaPoint)
		}
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignShockwaveCommit: %w", err)
		}
		cooldownReservation, isCooldownReserved :=
			peerSession.abilityCooldownSession().Reserve(
				cooldownKey, abilityStartTime, plan.Definition.Cooldown,
			)
		if !isCooldownReserved {
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			r.registry.mutex.Unlock()
			return request.reject("cooldown unavailable")
		}
		releaseReservation, isReleaseReserved :=
			peerSession.abilityReleaseSession().Reserve(
				abilityStartTime, plan.Definition.ReleaseDelay,
			)
		if !isReleaseReserved {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			r.registry.mutex.Unlock()
			return request.reject("release unavailable")
		}
		peerSession.areaBasicGeneration++
		areaGeneration := peerSession.areaBasicGeneration
		generation := peerSession.generation
		binding := peerSession.binding
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()

		schedule := areaSpecialSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, areaGeneration: areaGeneration,
			sourceObjectID: command.Common.ObjectID,
			targetObjectID: targetObjectID, targetPosition: storedTargetPosition,
			creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
			creature: creature, definition: definition, plan: plan,
			binding: binding, cooldownReservation: cooldownReservation,
			releaseReservation: releaseReservation,
			releaseAnimation:   releaseAnimationPacket,
			releaseResponse:    releaseResponsePacket,
			isArcWeld:          isArcWeldRequest, isZetawatt: isZetawattRequest,
		}
		abilityProducers := []raknet.ScheduledPacketProducer{
			schedule.hitProducer(0),
		}
		if isArcWeldRequest {
			for hitIndex := 1; hitIndex < len(plan.Target); hitIndex++ {
				abilityProducers = append(
					abilityProducers, schedule.hitProducer(hitIndex),
				)
			}
		}
		abilityProducers = append(
			abilityProducers, schedule.releaseProducer(),
		)
		producers := r.registry.producerGuard.scheduledProducers(sessionKey, abilityProducers)
		if packet.ScheduleGroupResult != nil {
			_, err = packet.ScheduleGroupResult(producers, schedule.fail)
		} else {
			_, err = packet.ScheduleGroup(producers)
		}
		if err != nil {
			schedule.fail(err)
			return nil, fmt.Errorf("campaignShockwaveSchedule: %w", err)
		}
		r.logger.Printf("RakNet campaign %s accepted source=%d targets=%d cooldown=%s",
			plan.Definition.Name, command.Common.ObjectID, len(plan.Target), plan.Definition.Cooldown)
		return append([][]byte{ackPacket}, startPackets...), nil
	}
	if isWraithActiveRequest {
		if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
			return request.reject("schedule unavailable")
		}
		sessionKey := packet.Address.String()
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[sessionKey]
		isFound = isFound && peerSession.generation == commandSession.generation &&
			command.Common.ObjectID == peerSession.deployedObjectID &&
			peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
			peerSession.zone.NPCs() != nil && peerSession.zone.Population() != nil
		if !isFound {
			r.registry.mutex.Unlock()
			return request.reject("session unavailable")
		}
		creatureIndex := peerSession.deployedCreatureIndex
		creature := r.registry.projectPassiveCreature(
			peerSession, creatureIndex, abilityStartTime,
		)
		err = peerSession.advancePlayerPosition(abilityStartTime, command.Common.Position)
		if err != nil {
			r.registry.mutex.Unlock()
			return request.reject("position unavailable")
		}
		plan, planErr := zoneability.PlanArea(
			peerSession.zone.NPCs(), command.Common.ObjectID,
			game.Vec3{X: peerSession.playerPosition.X, Y: peerSession.playerPosition.Y, Z: peerSession.playerPosition.Z},
			creature, specialTwoAbility.Definition,
		)
		if planErr != nil {
			r.registry.mutex.Unlock()
			return request.reject(planErr.Error())
		}
		isCooldownReady := peerSession.abilityCooldownSession().IsReady(
			specialTwoCooldownKey, abilityStartTime,
		) &&
			peerSession.isAbilityReleaseReady(abilityStartTime)
		manaCost, manaErr := game.ResolveAbilityManaCost(
			plan.Definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
			plan.Definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return request.reject("power projection unavailable")
		}
		if !isCooldownReady || peerSession.deployedManaPoint() < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("cooldown or power unavailable")
		}
		remainingManaPoint := peerSession.deployedManaPoint() - manaCost
		ackPacket, marshalErr := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
			SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
			ObjectID: plan.AbilityID, AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds:  packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime + uint64(plan.Definition.HitDelay/time.Millisecond),
			SourceEndMilliseconds:    packet.SourceTime + uint64(plan.Definition.ReleaseDelay/time.Millisecond),
		})
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignWraithAck: %w", marshalErr)
		}
		releaseResponsePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], plan.AbilityID, command.Ability.Index,
			packet.SourceTime, plan.Definition.HitDelay, plan.Definition.ReleaseDelay,
		)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignWraithReleaseResponse: %w", marshalErr)
		}
		startPackets, marshalErr := abilityraknet.StartSpendPresentation(
			command.Common.ObjectID, plan.AbilityID, plan.Definition.AnimationName,
			plan.Definition.Cooldown, packet.SourceTime, remainingManaPoint,
		)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignWraithStart: %w", marshalErr)
		}
		previousManaPoint := peerSession.deployedManaPoint()
		err = peerSession.stopPlayerMovement(abilityStartTime)
		if err == nil {
			err = peerSession.setDeployedManaPoints(remainingManaPoint)
		}
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignWraithCommit: %w", err)
		}
		cooldownReservation, isCooldownReserved :=
			peerSession.abilityCooldownSession().Reserve(
				specialTwoCooldownKey,
				abilityStartTime, plan.Definition.Cooldown,
			)
		if !isCooldownReserved {
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			r.registry.mutex.Unlock()
			return request.reject("cooldown unavailable")
		}
		releaseReservation, isReleaseReserved :=
			peerSession.abilityReleaseSession().Reserve(
				abilityStartTime, plan.Definition.ReleaseDelay,
			)
		if !isReleaseReserved {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			r.registry.mutex.Unlock()
			return request.reject("release unavailable")
		}
		peerSession.wraithActiveGeneration++
		activeGeneration := peerSession.wraithActiveGeneration
		generation := peerSession.generation
		binding := peerSession.binding
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()

		schedule := wraithActiveSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, activeGeneration: activeGeneration,
			sourceObjectID: command.Common.ObjectID,
			creatureIndex:  creatureIndex, previousManaPoint: previousManaPoint,
			creature: creature, plan: plan,
			binding: binding, cooldownReservation: cooldownReservation,
			releaseReservation: releaseReservation,
			releaseResponse:    releaseResponsePacket,
		}
		producers := r.registry.producerGuard.scheduledProducers(
			sessionKey,
			[]raknet.ScheduledPacketProducer{
				{
					Delay:   plan.Definition.HitDelay,
					Produce: schedule.produceHit,
				},
				{
					Delay:   plan.Definition.ReleaseDelay,
					Produce: schedule.produceRelease,
				},
			},
		)
		var cancel raknet.CancelSchedule
		if packet.ScheduleGroupResult != nil {
			cancel, err = packet.ScheduleGroupResult(producers, schedule.fail)
		} else {
			cancel, err = packet.ScheduleGroup(producers)
		}
		if err != nil {
			schedule.fail(err)
			return nil, fmt.Errorf("campaignWraithSchedule: %w", err)
		}
		_ = cancel
		r.logger.Printf("RakNet campaign Wraith active accepted source=%d targets=%d cooldown=%s",
			command.Common.ObjectID, len(plan.Target), plan.Definition.Cooldown)
		return append([][]byte{ackPacket}, startPackets...), nil
	}
	if isTreeOfLifeRequest {
		if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
			return nil, errors.New("campaignTreeSchedule: unavailable")
		}
		sessionKey := packet.Address.String()
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[sessionKey]
		isFound = isFound && peerSession.generation == commandSession.generation &&
			command.Common.ObjectID == peerSession.deployedObjectID &&
			peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
			peerSession.treeOfLifeRun == nil
		if !isFound {
			r.registry.mutex.Unlock()
			return request.reject("session unavailable or busy")
		}
		creature := r.registry.projectPassiveCreature(peerSession,
			peerSession.deployedCreatureIndex, abilityStartTime,
		)
		position := command.Ability.TargetPosition
		if !isReportedZonePosition(position) {
			position = command.Ability.CursorPosition
		}
		err = peerSession.advancePlayerPosition(abilityStartTime, command.Common.Position)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreePosition: %w", err)
		}
		definition := specialTwoAbility.Definition
		admissionRange := heroAbilityAdmissionRange(creature, definition)
		isPositionAccepted := isReportedZonePosition(position) &&
			isFiniteZonePosition(position) && isInsideZoneTrigger(
			peerSession.playerPosition, position, admissionRange,
		)
		if !isPositionAccepted {
			r.registry.mutex.Unlock()
			return request.reject("position unavailable")
		}
		if !peerSession.abilityCooldownSession().IsReady(
			specialTwoCooldownKey, abilityStartTime,
		) {
			r.registry.mutex.Unlock()
			return request.reject("Tree of Life cooldown unavailable")
		}
		if !peerSession.isAbilityReleaseReady(abilityStartTime) {
			r.registry.mutex.Unlock()
			return request.reject("action release unavailable")
		}
		manaCost, manaErr := game.ResolveAbilityManaCost(
			definition.ManaCost, creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreeManaProjection: %w", manaErr)
		}
		if peerSession.deployedManaPoint() < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("power unavailable")
		}
		cooldown, cooldownErr := zoneability.ProjectCooldown(creature, definition)
		if cooldownErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreeCooldown: %w", cooldownErr)
		}
		definition.Cooldown = cooldown
		objectID, objectIDErr := peerSession.reserveCampaignProjectileIDs(1, 1000)
		if objectIDErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreeObjectID: %w", objectIDErr)
		}
		remainingManaPoint := peerSession.deployedManaPoint() - manaCost
		treeRun, immediatePackets, runErr := abilityraknet.NewAreaHealingRun(abilityraknet.AreaHealingInput{
			Ability: definition, ActorObjectID: command.Common.ObjectID, EffectObjectID: objectID,
			Position: toSimPosition(position), ManaPoint: remainingManaPoint,
			Team: 1, SourceTime: packet.SourceTime,
		})
		if runErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreeRun: %w", runErr)
		}
		ackPacket, marshalErr := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
			SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
			ObjectID: util.HashID(definition.Name), AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds:  packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime + uint64(definition.HitDelay/time.Millisecond),
			SourceEndMilliseconds:    packet.SourceTime + uint64(definition.ReleaseDelay/time.Millisecond),
		})
		if marshalErr != nil {
			treeRun.Stop()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreeAck: %w", marshalErr)
		}
		releaseResponsePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], util.HashID(definition.Name),
			command.Ability.Index, packet.SourceTime,
			definition.HitDelay, definition.ReleaseDelay,
		)
		if marshalErr != nil {
			treeRun.Stop()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreeReleaseResponse: %w", marshalErr)
		}
		previousManaPoint := peerSession.deployedManaPoint()
		cooldownReservation, isCooldownReserved :=
			peerSession.abilityCooldownSession().Reserve(
				specialTwoCooldownKey,
				abilityStartTime,
				cooldown,
			)
		if !isCooldownReserved {
			treeRun.Stop()
			r.registry.mutex.Unlock()
			return request.reject("cooldown unavailable")
		}
		releaseReservation, isReleaseReserved :=
			peerSession.abilityReleaseSession().Reserve(
				abilityStartTime, definition.ReleaseDelay,
			)
		if !isReleaseReserved {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			treeRun.Stop()
			r.registry.mutex.Unlock()
			return request.reject("release unavailable")
		}
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
		if err != nil {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			treeRun.Stop()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignTreeMana: %w", err)
		}
		peerSession.treeOfLifeObjectID = objectID
		peerSession.treeOfLifeRun = treeRun
		generation := peerSession.generation
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()

		deadlines := []time.Duration{definition.HitDelay, definition.ReleaseDelay}
		tickCount := int(definition.Duration / definition.TickDuration)
		for tickIndex := 1; tickIndex <= tickCount; tickIndex++ {
			deadlines = append(deadlines, definition.HitDelay+time.Duration(tickIndex)*definition.TickDuration)
		}
		slices.Sort(deadlines)
		deadlines = slices.Compact(deadlines)
		schedule := treeOfLifeSchedule{
			runtime: r, sessionKey: sessionKey,
			generation: generation, objectID: objectID,
			actorObjectID:     command.Common.ObjectID,
			creatureIndex:     peerSession.deployedCreatureIndex,
			previousManaPoint: previousManaPoint,
			position:          position, creature: creature, definition: definition,
			run: treeRun, cooldownReservation: cooldownReservation,
			releaseReservation: releaseReservation,
			releaseResponse:    releaseResponsePacket,
		}
		producers := make([]raknet.ScheduledPacketProducer, 0, len(deadlines))
		for _, deadline := range deadlines {
			producers = append(producers, schedule.producer(deadline))
		}
		producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
		var cancel raknet.CancelSchedule
		if packet.ScheduleGroupResult != nil {
			cancel, err = packet.ScheduleGroupResult(producers, schedule.fail)
		} else {
			cancel, err = packet.ScheduleGroup(producers)
		}
		if err != nil {
			schedule.fail(err)
			schedule.rollbackAdmission()
			return nil, fmt.Errorf("campaignTreeSchedule: %w", err)
		}
		treeRun.SetCancel(cancel)
		r.logger.Printf("RakNet campaign Tree accepted source=%d object=%d position=(%g,%g,%g)",
			command.Common.ObjectID, objectID, position.X, position.Y, position.Z)
		return append([][]byte{ackPacket}, immediatePackets...), nil
	}
	if isEnrageRequest {
		if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
			return nil, errors.New("campaignEnrageSchedule: unavailable")
		}
		sessionKey := packet.Address.String()
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[sessionKey]
		isFound = isFound && peerSession.generation == commandSession.generation &&
			command.Common.ObjectID == peerSession.deployedObjectID &&
			peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
			peerSession.abilityCooldownSession().IsReady(
				randomCooldownKey, abilityStartTime,
			) &&
			peerSession.isAbilityReleaseReady(abilityStartTime)
		if !isFound {
			r.registry.mutex.Unlock()
			return request.reject("session, cooldown, or Enrage modifier unavailable")
		}
		creatureIndex := peerSession.deployedCreatureIndex
		creature := r.registry.projectPassiveCreature(
			peerSession, creatureIndex, abilityStartTime,
		)
		targetObjectID := command.Ability.TargetID
		firstHeroObjectID := zonehero.ObjectID(peerSession.binding.Slot, 0)
		lastHeroObjectID := zonehero.ObjectID(peerSession.binding.Slot, squad.Size-1)
		// The native instant-target template does not reject an enemy or empty
		// client target. It searches for a friendly recipient around the stored
		// point and falls back to the caster when none is usable.
		targetCreatureIndex := creatureIndex
		isHeroTarget := targetObjectID >= firstHeroObjectID &&
			targetObjectID <= lastHeroObjectID
		if isHeroTarget {
			candidateCreatureIndex := targetObjectID - firstHeroObjectID
			candidateCharacter, isCandidateFound :=
				peerSession.squad.Character(candidateCreatureIndex)
			if isCandidateFound && candidateCharacter.IsAvailable &&
				candidateCharacter.HitPoints > 0 &&
				candidateCreatureIndex < uint32(len(peerSession.binding.Creatures)) {
				targetCreatureIndex = candidateCreatureIndex
			}
		}
		isCompanionTarget := false
		targetCreature := game.GameplayCreature{}
		if peerSession.zone != nil && peerSession.zone.Companion() != nil {
			companion, isCompanionFound :=
				peerSession.zone.Companion().Snapshot(targetObjectID)
			isCompanionTarget = isCompanionFound && companion.HitPoint > 0 &&
				companion.UserID == peerSession.binding.UserID &&
				companion.PeerGeneration == peerSession.generation
			if isCompanionTarget {
				targetCreature = creature
				targetCreature.HitPoint = companion.MaximumHitPoint
				targetCreature.DamageProfile.PrimaryAttribute = creature.PetDamage
				targetCreature.DamageProfile.IsPrimaryAttributeFound = true
			}
		}
		if !isCompanionTarget {
			targetObjectID = zonehero.ObjectID(
				peerSession.binding.Slot, targetCreatureIndex,
			)
			targetCharacter, isTargetFound :=
				peerSession.squad.Character(targetCreatureIndex)
			if !isTargetFound || !targetCharacter.IsAvailable ||
				targetCharacter.HitPoints <= 0 ||
				targetCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
				r.registry.mutex.Unlock()
				return request.reject("living allied Enrage target unavailable")
			}
			targetCreature = peerSession.binding.Creatures[targetCreatureIndex]
		}
		previousEnrageRun := peerSession.enrageRun
		if previousEnrageRun != nil &&
			previousEnrageRun.TargetObjectID() != targetObjectID {
			r.registry.mutex.Unlock()
			return request.reject("Enrage already active on another ally")
		}
		definition := randomAbility.Definition
		manaCost, manaErr := game.ResolveAbilityManaCost(
			definition.ManaCost, creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEnrageManaProjection: %w", manaErr)
		}
		if peerSession.deployedManaPoint() < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("power unavailable")
		}
		instanceID, allocateErr := r.modifierPool.Allocate()
		if allocateErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEnrageModifierAllocate: %w", allocateErr)
		}
		run, runErr := abilityraknet.NewEnrageRun(
			targetCreatureIndex, targetObjectID, isCompanionTarget,
			instanceID, command.Common.ObjectID,
			targetCreature, r.effectPool,
		)
		if runErr != nil {
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEnrageRun: %w", runErr)
		}
		isReplacingAppliedEnrage := run.Replace(previousEnrageRun)
		remainingManaPoint := peerSession.deployedManaPoint() - manaCost
		ackPacket, marshalErr := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
			SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
			ObjectID: util.HashID(definition.Name), AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds:  packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime + uint64(definition.HitDelay/time.Millisecond),
			SourceEndMilliseconds:    packet.SourceTime + uint64(definition.ReleaseDelay/time.Millisecond),
		})
		if marshalErr != nil {
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEnrageAck: %w", marshalErr)
		}
		releasePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], util.HashID(definition.Name),
			command.Ability.Index, packet.SourceTime,
			definition.HitDelay, definition.ReleaseDelay,
		)
		if marshalErr != nil {
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEnrageRelease: %w", marshalErr)
		}
		animationName := definition.AnimationName
		if targetCreatureIndex == creatureIndex {
			animationName = definition.OutAnimationName
		}
		startPackets, marshalErr := abilityraknet.StartSpendPresentation(
			command.Common.ObjectID, util.HashID(definition.Name),
			animationName, definition.Cooldown,
			packet.SourceTime, remainingManaPoint,
		)
		if marshalErr != nil {
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEnrageStart: %w", marshalErr)
		}
		previousManaPoint := peerSession.deployedManaPoint()
		err = peerSession.stopPlayerMovement(abilityStartTime)
		if err == nil {
			err = peerSession.setDeployedManaPoints(remainingManaPoint)
		}
		if err != nil {
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignEnrageCommit: %w", err)
		}
		cooldownReservation, isCooldownReserved :=
			peerSession.abilityCooldownSession().Reserve(
				randomCooldownKey,
				abilityStartTime, definition.Cooldown,
			)
		if !isCooldownReserved {
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return request.reject("Enrage cooldown unavailable")
		}
		releaseReservation, isReleaseReserved :=
			peerSession.abilityReleaseSession().Reserve(
				abilityStartTime, definition.ReleaseDelay,
			)
		if !isReleaseReserved {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return request.reject("Enrage release unavailable")
		}
		peerSession.enrageRun = run
		generation := peerSession.generation
		binding := peerSession.binding
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()

		schedule := enrageSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, instanceID: instanceID,
			creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
			definition: definition, binding: binding, run: run,
			previousRun:         previousEnrageRun,
			cooldownReservation: cooldownReservation,
			releaseReservation:  releaseReservation,
			releasePacket:       releasePacket,
			isReplacingApplied:  isReplacingAppliedEnrage,
		}
		producers := []raknet.ScheduledPacketProducer{{
			Delay: definition.ReleaseDelay, Produce: schedule.produceRelease,
		}}
		for tickIndex := 0; tickIndex < 10; tickIndex++ {
			deadline := definition.HitDelay +
				time.Duration(tickIndex)*abilityraknet.EnrageTick
			producers = append(
				producers, schedule.tickProducer(tickIndex, deadline),
			)
		}
		expiryDeadline := definition.HitDelay + abilityraknet.EnrageDuration
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: expiryDeadline, Produce: schedule.produceExpiry,
		})
		producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
		var cancel raknet.CancelSchedule
		if packet.ScheduleGroupResult != nil {
			cancel, err = packet.ScheduleGroupResult(producers, schedule.fail)
		} else {
			cancel, err = packet.ScheduleGroup(producers)
		}
		if err != nil {
			schedule.fail(err)
			return nil, fmt.Errorf("campaignEnrageSchedule: %w", err)
		}
		run.SetCancel(cancel)
		if previousEnrageRun != nil {
			previousEnrageRun.Cancel()
			_ = r.modifierPool.Release(previousEnrageRun.InstanceID())
		}
		r.logger.Printf("RakNet campaign Enrage accepted source=%d target=%d cooldown=%s",
			command.Common.ObjectID, targetObjectID, definition.Cooldown)
		return append([][]byte{ackPacket}, startPackets...), nil
	}
	if isTeleportStrikeRequest {
		if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
			return nil, errors.New("campaignRideSchedule: unavailable")
		}
		sessionKey := packet.Address.String()
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[sessionKey]
		isFound = isFound && peerSession.generation == commandSession.generation &&
			command.Common.ObjectID == peerSession.deployedObjectID &&
			peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures))
		if !isFound {
			r.registry.mutex.Unlock()
			return request.reject("session unavailable")
		}
		creatureIndex := peerSession.deployedCreatureIndex
		creature := r.registry.projectPassiveCreature(
			peerSession, creatureIndex, abilityStartTime,
		)
		destination := command.Ability.TargetPosition
		if !isReportedZonePosition(destination) {
			destination = command.Ability.CursorPosition
		}
		err = peerSession.advancePlayerPosition(abilityStartTime, command.Common.Position)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRidePosition: %w", err)
		}
		definition := specialTwoAbility.Definition
		admissionRange := heroAbilityAdmissionRange(creature, definition)
		cooldownKey := zoneability.HeroAbilityCooldown(specialTwoAbility.ID)
		isDestinationValid := isReportedZonePosition(destination) &&
			isFiniteZonePosition(destination)
		isDestinationInRange := isDestinationValid && isInsideZoneTrigger(
			peerSession.playerPosition, destination, admissionRange,
		)
		isCooldownReady := peerSession.abilityCooldownSession().IsReady(
			cooldownKey, abilityStartTime,
		) &&
			peerSession.isAbilityReleaseReady(abilityStartTime)
		manaCost, manaErr := game.ResolveAbilityManaCost(
			definition.ManaCost, creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRideManaProjection: %w", manaErr)
		}
		if !isDestinationInRange || !isCooldownReady || peerSession.deployedManaPoint() < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("destination, cooldown, or power unavailable")
		}
		ridePlan := zoneability.BasicPlan{}
		if targetObjectID != 0 {
			if peerSession.zone == nil || peerSession.zone.NPCs() == nil {
				r.registry.mutex.Unlock()
				return request.reject("target authority unavailable")
			}
			ridePlan, err = zoneability.PlanProjected(
				peerSession.zone.NPCs(), command.Common.ObjectID, targetObjectID,
				game.Vec3(peerSession.playerPosition), creature, definition, admissionRange,
			)
			if err != nil {
				r.registry.mutex.Unlock()
				return request.reject("target unavailable or out of range")
			}
			// The authored strike resolves damage from the melee landing point,
			// while admission range is measured from the pre-teleport position.
			ridePlan.SourcePosition = game.Vec3{
				X: destination.X, Y: destination.Y, Z: destination.Z,
			}
		}
		cooldown, cooldownErr := zoneability.ProjectCooldown(creature, definition)
		if cooldownErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRideCooldown: %w", cooldownErr)
		}
		remainingManaPoint := peerSession.deployedManaPoint() - manaCost
		abilityID := util.HashID(definition.Name)
		ackPacket, marshalErr := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
			SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
			ObjectID: abilityID, AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds:  packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime + uint64(definition.HitDelay/time.Millisecond),
			SourceEndMilliseconds:    packet.SourceTime + uint64(definition.ReleaseDelay/time.Millisecond),
		})
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRideAck: %w", marshalErr)
		}
		releaseResponsePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], abilityID, command.Ability.Index,
			packet.SourceTime, definition.HitDelay, definition.ReleaseDelay,
		)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRideReleaseResponse: %w", marshalErr)
		}
		teleportPackets, marshalErr := actionraknet.Teleport(actionraknet.TeleportRequest{
			ObjectID: command.Common.ObjectID,
			Position: game.Vec3{X: destination.X, Y: destination.Y, Z: destination.Z},
			Orientation: game.Quaternion{
				X: command.Common.Orientation.X, Y: command.Common.Orientation.Y,
				Z: command.Common.Orientation.Z, W: command.Common.Orientation.W,
			},
		})
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRideTeleport: %w", marshalErr)
		}
		startPackets, marshalErr := abilityraknet.StartSpendPresentation(
			command.Common.ObjectID, abilityID, definition.AnimationName, cooldown,
			packet.SourceTime, remainingManaPoint,
		)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRideStart: %w", marshalErr)
		}
		releaseAnimationPacket, marshalErr := abilityraknet.Animation(
			command.Common.ObjectID, definition.OutAnimationName,
			packet.SourceTime+uint64(definition.ReleaseDelay/time.Millisecond),
		)
		if marshalErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRideReleaseMarshal: %w", marshalErr)
		}
		abilityLessonCompletePacket := []byte(nil)
		isTutorialRide := peerSession.binding.Mode == game.ModeTutorial &&
			specialTwoAbility.AssetName == "LightningRogueActive"
		if isTutorialRide {
			abilityLessonCompletePacket, marshalErr = raknet.MarshalApplication(
				raknet.TutorialAbilityLessonCompleteMessage(
					tutorialAbilityLessonObjectiveID,
					uint8(peerSession.binding.Slot),
				),
			)
			if marshalErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignRideLessonComplete: %w", marshalErr)
			}
		}
		previousMotion := peerSession.playerMotionSnapshot()
		previousManaPoint := peerSession.deployedManaPoint()
		cooldownReservation, isCooldownReserved :=
			peerSession.abilityCooldownSession().Reserve(
				cooldownKey,
				abilityStartTime,
				cooldown,
			)
		if !isCooldownReserved {
			r.registry.mutex.Unlock()
			return request.reject("cooldown unavailable")
		}
		releaseReservation, isReleaseReserved :=
			peerSession.abilityReleaseSession().Reserve(
				abilityStartTime, definition.ReleaseDelay,
			)
		if !isReleaseReserved {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			r.registry.mutex.Unlock()
			return request.reject("release unavailable")
		}
		err = peerSession.teleportPlayer(abilityStartTime, destination)
		if err == nil {
			err = peerSession.setDeployedManaPoints(remainingManaPoint)
		}
		if err != nil {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignRideCommit: %w", err)
		}
		playerMotionRevision := peerSession.playerMotionRevision()
		generation := peerSession.generation
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		schedule := rideLightningSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceObjectID: command.Common.ObjectID,
			abilityID: abilityID, cooldownKey: cooldownKey,
			creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
			motionRevision:      playerMotionRevision,
			previousMotion:      previousMotion,
			hitDelay:            definition.HitDelay,
			creature:            creature,
			binding:             peerSession.binding,
			plan:                ridePlan,
			cooldownReservation: cooldownReservation,
			releaseReservation:  releaseReservation,
			releaseAnimation:    releaseAnimationPacket,
			releaseResponse:     releaseResponsePacket,
		}
		var cancel raknet.CancelSchedule
		producers := make([]raknet.ScheduledPacketProducer, 0, 2)
		if ridePlan.TargetObjectID != 0 {
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: definition.HitDelay, Produce: schedule.produceImpact,
			})
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: definition.ReleaseDelay, Produce: schedule.produceRelease,
		})
		producers = r.registry.producerGuard.scheduledProducers(
			sessionKey, producers,
		)
		if packet.ScheduleGroupResult != nil {
			cancel, err = packet.ScheduleGroupResult(producers, schedule.fail)
		} else {
			cancel, err = packet.ScheduleGroup(producers)
		}
		if err != nil {
			rollbackErr := schedule.rollbackAdmission()
			return nil, fmt.Errorf(
				"campaignRideSchedule: %w", errors.Join(err, rollbackErr),
			)
		}
		schedule.bindCancel(cancel)
		r.logger.Printf(
			"RakNet campaign teleport strike accepted ability=%s source=%d target=%d destination=(%g,%g,%g) mana=%g cooldown=%s",
			definition.Name, command.Common.ObjectID, targetObjectID,
			destination.X, destination.Y, destination.Z, remainingManaPoint,
			cooldown,
		)
		packets := make([][]byte, 0, 1+len(startPackets)+len(teleportPackets))
		packets = append(packets, ackPacket)
		// Teammates do not run the caster's predicted ability animation.
		// Publish its authored start explicitly before the teleport correction.
		packets = append(packets, startPackets...)
		packets = append(packets, teleportPackets...)
		if len(abilityLessonCompletePacket) != 0 {
			packets = append(packets, abilityLessonCompletePacket)
		}
		return packets, nil
	}
	return nil, errCampaignAbilityNotSpecial
}

type campaignGhostReleaseStep struct {
	runtime    campaignAbilityCommandRuntime
	sessionKey string
	generation uint64
	run        *abilityraknet.GhostFormRun
	packet     []byte
}

func (s campaignGhostReleaseStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.RLock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.ghostFormRun == s.run
	s.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{s.packet}, nil
}

type campaignGhostExpiryStep struct {
	runtime    campaignAbilityCommandRuntime
	sessionKey string
	generation uint64
	timestamp  uint64
	run        *abilityraknet.GhostFormRun
}

func (s campaignGhostExpiryStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.ghostFormRun == s.run
	if isCurrent {
		peerSession.ghostFormRun = nil
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	_ = s.runtime.modifierPool.Release(s.run.InstanceID())
	packets, err := s.run.ExpiryPackets(s.timestamp)
	if err != nil {
		return nil, fmt.Errorf("campaignGhostFormExpire: %w", err)
	}
	s.runtime.logger.Printf(
		"RakNet campaign Ghost Form expired object=%d instance=%d",
		s.run.ObjectID(), s.run.InstanceID(),
	)
	return packets, nil
}

type campaignGhostScheduleFailure struct {
	runtime             campaignAbilityCommandRuntime
	sessionKey          string
	generation          uint64
	run                 *abilityraknet.GhostFormRun
	creatureIndex       uint32
	previousManaPoint   float32
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	instanceID          uint32
}

func (f campaignGhostScheduleFailure) handle(scheduleErr error) {
	f.runtime.registry.mutex.Lock()
	peerSession, isFound := f.runtime.registry.sessions[f.sessionKey]
	isCurrent := isFound && peerSession.generation == f.generation &&
		peerSession.ghostFormRun == f.run
	if isCurrent {
		peerSession.ghostFormRun = nil
		_ = peerSession.setCampaignCharacterManaPoints(
			f.creatureIndex, f.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(f.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(f.releaseReservation)
		f.runtime.registry.sessions[f.sessionKey] = peerSession
	}
	f.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	f.run.ReleaseEffect()
	_ = f.runtime.modifierPool.Release(f.instanceID)
	f.runtime.logger.Printf(
		"RakNet campaign Ghost Form stopped after schedule failure for %s: %v",
		f.sessionKey, scheduleErr,
	)
}

func (e campaignAbilityCommandRuntime) rejectSquadAbility(
	command raknet.ActionCommandData, reason string,
) ([][]byte, error) {
	ackPacket, err := actionraknet.Reject(command)
	if err != nil {
		return nil, fmt.Errorf("campaignSupportReject: %w", err)
	}
	e.logger.Printf(
		"RakNet campaign support rejected source=%d index=%d reason=%s",
		command.Common.ObjectID, command.Ability.Index, reason,
	)
	return [][]byte{ackPacket}, nil
}

func (r campaignAbilityCommandRuntime) handleSquad(
	packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession,
) ([][]byte, error) {
	var err error
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return nil, errors.New("campaignSupportSchedule: unavailable")
	}
	abilityStartTime := r.now()
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isSessionAvailable := isFound &&
		peerSession.generation == commandSession.generation &&
		command.Common.ObjectID == peerSession.deployedObjectID &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil && peerSession.zone.Population() != nil
	if !isSessionAvailable {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "session unavailable")
	}
	if abilityStartTime.Before(peerSession.enemySilenceExpiresAt) {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "silenced")
	}
	if peerSession.isEnemySleepActive(abilityStartTime) {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "asleep")
	}
	if peerSession.isEnemyStunActive(abilityStartTime) {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "stunned")
	}
	if peerSession.isEnemyFearActive(abilityStartTime) {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "terrified")
	}
	if !peerSession.isAbilityReleaseReady(abilityStartTime) {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "action release unavailable")
	}
	creatureIndex := peerSession.deployedCreatureIndex
	creature := r.registry.projectPassiveCreature(
		peerSession, creatureIndex, abilityStartTime,
	)
	if command.Ability.Index < 6 || command.Ability.Index > 8 {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "support index unavailable")
	}
	supportCreatureIndex := command.Ability.Index - 6
	if supportCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "support creature unavailable")
	}
	supportCreature := peerSession.binding.Creatures[supportCreatureIndex]
	supportAbility, isSupportAbilityFound := r.program.HeroAbility(
		supportCreature.Noun, zonecontent.HeroAbilitySpecialOne,
	)
	supportCooldownKey := zoneability.HeroAbilityCooldown(supportAbility.ID)
	isSharedSelfModifier := isSupportAbilityFound &&
		supportAbility.Definition.Kind == sim.AbilityKindModifier &&
		supportAbility.AssetName != "Ghostform"
	if isSharedSelfModifier {
		r.registry.mutex.Unlock()
		return r.handleHeroSelfModifier(campaignCharacterAbilityRequest{
			runtime: r, packet: packet, command: command,
			commandSession: commandSession, startTime: abilityStartTime,
			deployedCreature: creature,
		}, supportAbility, supportCooldownKey)
	}
	isWraithGhostForm := isSupportAbilityFound &&
		supportAbility.AssetName == "Ghostform"
	if isWraithGhostForm {
		definition := supportAbility.Definition
		if !peerSession.abilityCooldownSession().IsReady(
			supportCooldownKey, abilityStartTime,
		) {
			r.registry.mutex.Unlock()
			return r.rejectSquadAbility(
				command, "Ghost Form cooldown unavailable",
			)
		}
		manaCost, manaErr := game.ResolveAbilityManaCost(
			definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
			definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignGhostFormMana: %w", manaErr)
		}
		if peerSession.deployedManaPoint() < manaCost {
			r.registry.mutex.Unlock()
			return r.rejectSquadAbility(command, "power unavailable")
		}
		instanceID, allocateErr := r.modifierPool.Allocate()
		if allocateErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignGhostFormModifier: %w", allocateErr)
		}
		run, runErr := abilityraknet.NewGhostFormRun(
			creatureIndex, instanceID, command.Common.ObjectID, r.effectPool,
		)
		if runErr != nil {
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignGhostFormRun: %w", runErr)
		}
		remainingManaPoint := peerSession.deployedManaPoint() - manaCost
		ackPacket, marshalErr := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
			SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
			ObjectID: util.HashID(definition.Name), AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds: packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime +
				uint64(definition.HitDelay/time.Millisecond),
			SourceEndMilliseconds: packet.SourceTime +
				uint64(definition.ReleaseDelay/time.Millisecond),
		})
		if marshalErr != nil {
			run.ReleaseEffect()
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignGhostFormAck: %w", marshalErr)
		}
		startPackets, marshalErr := abilityraknet.GhostFormStartPackets(
			command.Common.ObjectID, instanceID, definition,
			remainingManaPoint, packet.SourceTime, run.EffectSlot(),
		)
		if marshalErr != nil {
			run.ReleaseEffect()
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignGhostFormStart: %w", marshalErr)
		}
		releasePacket, marshalErr := abilityraknet.ReleaseResponse(
			command.Common.Unknown[0], util.HashID(definition.Name),
			command.Ability.Index, packet.SourceTime,
			definition.HitDelay, definition.ReleaseDelay,
		)
		if marshalErr != nil {
			run.ReleaseEffect()
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignGhostFormRelease: %w", marshalErr)
		}
		previousManaPoint := peerSession.deployedManaPoint()
		err = peerSession.stopPlayerMovement(abilityStartTime)
		if err == nil {
			err = peerSession.setDeployedManaPoints(remainingManaPoint)
		}
		if err != nil {
			run.ReleaseEffect()
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignGhostFormCommit: %w", err)
		}
		cooldownReservation, isCooldownReserved :=
			peerSession.abilityCooldownSession().Reserve(
				supportCooldownKey,
				abilityStartTime, definition.Cooldown,
			)
		if !isCooldownReserved {
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			run.ReleaseEffect()
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return r.rejectSquadAbility(
				command, "Ghost Form cooldown unavailable",
			)
		}
		releaseReservation, isReleaseReserved :=
			peerSession.abilityReleaseSession().Reserve(
				abilityStartTime, definition.ReleaseDelay,
			)
		if !isReleaseReserved {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			_ = peerSession.setCampaignCharacterManaPoints(
				creatureIndex, previousManaPoint,
			)
			run.ReleaseEffect()
			_ = r.modifierPool.Release(instanceID)
			r.registry.mutex.Unlock()
			return r.rejectSquadAbility(
				command, "Ghost Form release unavailable",
			)
		}
		peerSession.ghostFormRun = run
		generation := peerSession.generation
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()

		releaseStep := campaignGhostReleaseStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			run: run, packet: releasePacket,
		}
		expiryStep := campaignGhostExpiryStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			timestamp: packet.SourceTime + uint64(definition.Duration/time.Millisecond),
			run:       run,
		}
		producers := []raknet.ScheduledPacketProducer{
			{Delay: definition.ReleaseDelay, Produce: releaseStep.produce},
			{Delay: definition.Duration, Produce: expiryStep.produce},
		}
		scheduleFailure := campaignGhostScheduleFailure{
			runtime: r, sessionKey: sessionKey, generation: generation,
			run: run, creatureIndex: creatureIndex,
			previousManaPoint:   previousManaPoint,
			cooldownReservation: cooldownReservation,
			releaseReservation:  releaseReservation,
			instanceID:          instanceID,
		}
		producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
		var cancel raknet.CancelSchedule
		var scheduleErr error
		if packet.ScheduleGroupResult != nil {
			cancel, scheduleErr = packet.ScheduleGroupResult(
				producers, scheduleFailure.handle,
			)
		} else {
			cancel, scheduleErr = packet.ScheduleGroup(producers)
		}
		if scheduleErr != nil {
			scheduleFailure.handle(scheduleErr)
			return nil, fmt.Errorf("campaignGhostFormSchedule: %w", scheduleErr)
		}
		run.SetCancel(cancel)
		r.logger.Printf(
			"RakNet campaign Ghost Form accepted source=%d cooldown=%s duration=%s",
			command.Common.ObjectID, definition.Cooldown, definition.Duration,
		)
		return append([][]byte{ackPacket}, startPackets...), nil
	}
	isTargetedAOESupport := isSupportAbilityFound &&
		supportAbility.Definition.Kind == sim.AbilityKindTargetedAOE
	if !isTargetedAOESupport {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(
			command, "support definition unavailable",
		)
	}
	if !peerSession.abilityCooldownSession().IsReady(
		supportCooldownKey, abilityStartTime,
	) || peerSession.sphereAttack != nil {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(
			command, "targeted area cooldown unavailable",
		)
	}
	err = peerSession.advancePlayerPosition(abilityStartTime, command.Common.Position)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportPosition: %w", err)
	}
	position := command.Ability.TargetPosition
	if !isReportedZonePosition(position) {
		position = command.Ability.CursorPosition
	}
	if !isReportedZonePosition(position) {
		position = peerSession.playerPosition
	}
	definition, projectionErr := zoneability.ProjectTiming(
		creature, supportAbility.Definition,
	)
	if projectionErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportTiming: %w", projectionErr)
	}
	if definition.IsAreaDurationScaled {
		definition.NumberOfTicks, projectionErr = game.ResolveAreaDurationCount(
			definition.NumberOfTicks, creature.AreaDurationIncrease,
		)
		if projectionErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignSupportAreaDuration: %w", projectionErr)
		}
	}
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	if !isFiniteZonePosition(position) || !isInsideZoneTrigger(
		peerSession.playerPosition, position, admissionRange,
	) {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(
			command, "position unavailable or out of range",
		)
	}
	manaCost, manaErr := game.ResolveAbilityManaCost(
		definition.ManaCost, creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if manaErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportManaProjection: %w", manaErr)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(command, "power unavailable")
	}
	definition.ManaCost = manaCost
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	previousNextObjectID := peerSession.nextProjectileObjectID
	effectObjectID, err := peerSession.reserveCampaignProjectileIDs(1, 1000)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportObjectID: %w", err)
	}
	run, immediatePackets, runErr := abilityraknet.NewTargetedAOERun(abilityraknet.TargetedAOEInput{
		Ability: definition, ActorObjectID: command.Common.ObjectID,
		EffectObjectID: effectObjectID, Position: toSimPosition(position),
		ManaPoint: remainingManaPoint, Team: 1, SourceTime: packet.SourceTime,
	})
	if runErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportRun: %w", runErr)
	}
	ackPacket, marshalErr := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: util.HashID(definition.Name), AbilityIndex: command.Ability.Index,
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime + uint64(definition.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    packet.SourceTime + uint64(definition.ReleaseDelay/time.Millisecond),
	})
	if marshalErr != nil {
		run.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportAck: %w", marshalErr)
	}
	releaseResponsePacket, marshalErr := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], util.HashID(definition.Name),
		command.Ability.Index, packet.SourceTime,
		definition.HitDelay, definition.ReleaseDelay,
	)
	if marshalErr != nil {
		run.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportReleaseResponse: %w", marshalErr)
	}
	previousManaPoint := peerSession.deployedManaPoint()
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		run.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignSupportCommit: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			supportCooldownKey,
			abilityStartTime, definition.Cooldown,
		)
	if !isCooldownReserved {
		_ = peerSession.setCampaignCharacterManaPoints(
			creatureIndex, previousManaPoint,
		)
		peerSession.restoreCampaignProjectileID(previousNextObjectID)
		run.Stop()
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(
			command, "targeted area cooldown unavailable",
		)
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, definition.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		_ = peerSession.setCampaignCharacterManaPoints(
			creatureIndex, previousManaPoint,
		)
		peerSession.restoreCampaignProjectileID(previousNextObjectID)
		run.Stop()
		r.registry.mutex.Unlock()
		return r.rejectSquadAbility(
			command, "targeted area release unavailable",
		)
	}
	peerSession.sphereAttack = run
	peerSession.sphereObjectID = effectObjectID
	generation := peerSession.generation
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	deadlines := []time.Duration{definition.HitDelay, definition.ReleaseDelay}
	firstTick := uint32(1)
	if definition.IsInitialPulse {
		firstTick = 0
	}
	for tick := firstTick; tick <= definition.NumberOfTicks; tick++ {
		if definition.IsInitialPulse && tick == definition.NumberOfTicks {
			break
		}
		deadlines = append(deadlines, definition.HitDelay+time.Duration(tick)*definition.TickDuration)
	}
	slices.Sort(deadlines)
	deadlines = slices.Compact(deadlines)
	schedule := targetedAOESchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: command.Common.ObjectID,
		effectObjectID: effectObjectID, creatureIndex: creatureIndex,
		previousManaPoint:    previousManaPoint,
		previousNextObjectID: previousNextObjectID,
		finalDeadline:        deadlines[len(deadlines)-1],
		creature:             creature, definition: definition, binding: binding,
		run: run, cooldownReservation: cooldownReservation,
		releaseReservation: releaseReservation,
		releaseResponse:    releaseResponsePacket,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(deadlines))
	for _, deadline := range deadlines {
		producers = append(producers, schedule.producer(deadline))
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, schedule.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		schedule.fail(err)
		rollbackErr := schedule.rollbackAdmission()
		return nil, fmt.Errorf(
			"campaignSupportSchedule: %w", errors.Join(err, rollbackErr),
		)
	}
	run.SetCancel(cancel)
	r.logger.Printf("RakNet campaign %s accepted source=%d object=%d index=%d position=(%g,%g,%g)",
		definition.Name, command.Common.ObjectID, effectObjectID, command.Ability.Index,
		position.X, position.Y, position.Z)
	return append([][]byte{ackPacket}, immediatePackets...), nil
}
