package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type heroQuantumBlinkRun struct {
	mutex     sync.Mutex
	cancel    raknet.CancelSchedule
	isStopped bool
}

func (e *heroQuantumBlinkRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	if e.isStopped {
		e.mutex.Unlock()
		return
	}
	e.isStopped = true
	cancel := e.cancel
	e.cancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
}

type heroQuantumBlinkSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	returnPosition      raknet.Vector3
	initialDestination  raknet.Vector3
	targetObjectIDs     []uint32
	initialTargetIndex  int
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	damage              game.DamageRange
	binding             game.GameplayBinding
	run                 *heroQuantumBlinkRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
	releaseDelay        time.Duration
}

type heroQuantumBlinkStrike struct {
	schedule heroQuantumBlinkSchedule
	index    uint32
	deadline time.Duration
}

func (e heroQuantumBlinkStrike) produce() ([][]byte, error) {
	return e.schedule.strike(e.index, e.deadline)
}

type heroQuantumBlinkAnimation struct {
	schedule      heroQuantumBlinkSchedule
	animationName string
	deadline      time.Duration
}

func (e heroQuantumBlinkAnimation) produce() ([][]byte, error) {
	return e.schedule.animate(e.animationName, e.deadline)
}

func (e heroQuantumBlinkSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID &&
		peerSession.heroQuantumBlink == e.run
}

func (e heroQuantumBlinkSchedule) selectTarget(
	npc *zonenpc.Session, strikeIndex uint32,
) (zonenpc.Snapshot, bool) {
	if npc == nil || len(e.targetObjectIDs) == 0 || e.initialTargetIndex < 0 {
		return zonenpc.Snapshot{}, false
	}
	start := (e.initialTargetIndex + int(strikeIndex)) % len(e.targetObjectIDs)
	for offset := range len(e.targetObjectIDs) {
		index := (start + offset) % len(e.targetObjectIDs)
		target, isFound := npc.NPC(e.targetObjectIDs[index])
		if isFound && !target.IsDefeated && target.HitPoint > 0 &&
			target.Faction == zonenpc.FactionNonPlayerAligned &&
			!target.Plan.IsFixture {
			return target, true
		}
	}
	return zonenpc.Snapshot{}, false
}

func (e heroQuantumBlinkSchedule) animate(
	animationName string, deadline time.Duration,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.runtime.registry.mutex.Unlock()
	packet, err := npcraknet.AnimationState(
		e.sourceObjectID, animationName,
		e.packet.SourceTime+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkAnimate: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e heroQuantumBlinkSchedule) strike(
	strikeIndex uint32, deadline time.Duration,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := e.selectTarget(peerSession.zone.NPCs(), strikeIndex)
	random, err := peerSession.abilityRandom()
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkRandom[%d]: %w", strikeIndex, err)
	}
	animationIndex, err := random.Index(uint32(len(e.definition.AnimationNames)))
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkPose[%d]: %w", strikeIndex, err)
	}
	animationName := e.definition.AnimationNames[animationIndex]
	destination := e.initialDestination
	if isTargetFound && strikeIndex > 0 {
		destination = raknet.Vector3(target.Plan.Position)
	}
	err = peerSession.teleportPlayer(e.runtime.now(), destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkPosition[%d]: %w", strikeIndex, err)
	}
	var result zoneability.AreaResult
	var transition campaignDamageTransition
	if isTargetFound {
		results, damageErr := zoneability.CommitArea(
			peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
			zoneability.AreaPlan{
				SourceObjectID: e.sourceObjectID,
				AbilityID:      util.HashID(e.definition.Name), Definition: e.definition,
				Damage: e.damage, Center: game.Vec3(destination),
				Target: []zonenpc.Snapshot{target},
			}, e.creature, peerSession.binding.Difficulty,
			e.runtime.program.Critical,
		)
		if damageErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("quantumBlinkDamage[%d]: %w", strikeIndex, damageErr)
		}
		if len(results) > 0 {
			result = results[0]
			transition, err = peerSession.applyCampaignDamageTransition(result.Damage)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("quantumBlinkTransition[%d]: %w", strikeIndex, err)
			}
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: e.sourceObjectID, Position: game.Vec3(destination),
	})
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkTeleport[%d]: %w", strikeIndex, err)
	}
	animationPacket, err := npcraknet.AnimationState(
		e.sourceObjectID, animationName,
		e.packet.SourceTime+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkAnimation[%d]: %w", strikeIndex, err)
	}
	packets = append(packets, animationPacket)
	if result.Damage.ObjectID == 0 {
		return packets, nil
	}
	damagePackets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond), e.binding,
		[]zoneability.AreaResult{result}, []campaignDamageTransition{transition},
		func(result zoneability.AreaResult) ([]byte, error) {
			effectPacket, marshalErr := raknet.MarshalApplication(
				raknet.ServerEventMessage{
					Asset:    util.HashID(e.definition.HitEffectName),
					ObjectID: result.Damage.ObjectID,
					Position: raknet.Vector3(result.Snapshot.Plan.Position),
				},
			)
			if marshalErr != nil {
				return nil, fmt.Errorf("quantumBlinkHitEffect: %w", marshalErr)
			}
			return effectPacket, nil
		}, false,
	)
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkPublish[%d]: %w", strikeIndex, err)
	}
	return append(packets, damagePackets...), nil
}

func (e heroQuantumBlinkSchedule) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err := peerSession.teleportPlayer(e.runtime.now(), e.returnPosition)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkReturn: %w", err)
	}
	peerSession.heroQuantumBlink = nil
	e.run.mutex.Lock()
	e.run.cancel = nil
	e.run.isStopped = true
	e.run.mutex.Unlock()
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: e.sourceObjectID, Position: game.Vec3(e.returnPosition),
	})
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkReturnMarshal: %w", err)
	}
	resetPacket, err := npcraknet.ResetAnimation(
		e.sourceObjectID,
		e.packet.SourceTime+uint64(e.releaseDelay/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkReset: %w", err)
	}
	packets = append(packets, resetPacket, e.releasePacket)
	return packets, nil
}

func (e heroQuantumBlinkSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.heroQuantumBlink = nil
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	e.run.Stop()
	e.runtime.logger.Printf(
		"RakNet Quantum Blink stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleHeroQuantumBlink(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindQuantumBlink ||
		definition.Name != "QuantumBlink" || definition.Range <= 0 ||
		definition.Radius <= 0 || definition.HitDelay <= 0 ||
		definition.ReleaseDelay <= definition.HitDelay ||
		definition.ShotCount != 5 || definition.TickDuration <= 0 ||
		definition.MinimumDamage <= 0 ||
		definition.MaximumDamage < definition.MinimumDamage ||
		len(definition.AnimationNames) != 4 || definition.SlideAnimationName == "" ||
		definition.AnimationName == "" || definition.SecondaryAnimationName == "" ||
		definition.HitEffectName == "" {
		r.registry.mutex.Unlock()
		return req.reject("Quantum Blink definition unavailable")
	}
	cursor := req.command.Ability.TargetPosition
	if !isReportedZonePosition(cursor) {
		cursor = req.command.Ability.CursorPosition
	}
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	if !isReportedZonePosition(cursor) || !isFiniteZonePosition(cursor) ||
		!isInsideZoneTrigger(peerSession.playerPosition, cursor, admissionRange) {
		r.registry.mutex.Unlock()
		return req.reject("Quantum Blink position unavailable")
	}
	if peerSession.heroQuantumBlink != nil {
		r.registry.mutex.Unlock()
		return req.reject("Quantum Blink already active")
	}
	targetObjectIDs := make([]uint32, 0)
	initialTargetIndex := -1
	selectedDistance := definition.Radius
	for _, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Faction != zonenpc.FactionNonPlayerAligned ||
			target.Plan.IsFixture ||
			zonegeometry.Distance(game.Vec3(cursor), target.Plan.Position) >
				definition.Radius {
			continue
		}
		targetObjectIDs = append(targetObjectIDs, target.Plan.ObjectID)
		distance := zonegeometry.Distance(game.Vec3(cursor), target.Plan.Position)
		if initialTargetIndex < 0 || distance < selectedDistance {
			initialTargetIndex = len(targetObjectIDs) - 1
			selectedDistance = distance
		}
	}
	initialDestination := cursor
	if initialTargetIndex >= 0 {
		target, isFound := peerSession.zone.NPCs().NPC(
			targetObjectIDs[initialTargetIndex],
		)
		if isFound {
			initialDestination = quantumBlinkSlideDestination(
				peerSession.playerPosition, raknet.Vector3(target.Plan.Position),
				peerSession.deployedCampaignFootprintRadius(),
				target.Plan.NPCProfile.FootprintRadius,
			)
		}
	}
	err := zonenavigation.ValidateMovement(
		peerSession.zone.Navigation(), game.Vec3(peerSession.playerPosition),
		game.Vec3(initialDestination), peerSession.deployedCampaignFootprintRadius(),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return req.reject("Quantum Blink destination unavailable")
	}
	damage, err := zoneability.ProjectDamage(
		creature, definition, definition.MinimumDamage, definition.MaximumDamage,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkDamage: %w", err)
	}
	cooldown, err := zoneability.ProjectCooldown(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkCooldown: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		definition.ManaCost, creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	slideDistance := zonegeometry.Distance(
		game.Vec3(peerSession.playerPosition), game.Vec3(initialDestination),
	)
	slideDelay := definition.HitDelay + time.Duration(
		float64(slideDistance)/100*float64(time.Second),
	)
	releaseDelay := slideDelay +
		time.Duration(definition.ShotCount)*definition.TickDuration +
		definition.FinalWaitDelay
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds: req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime +
			uint64(slideDelay/time.Millisecond),
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(releaseDelay/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], activeAbilityID, req.command.Ability.Index,
		req.packet.SourceTime, slideDelay, releaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkRelease: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	startPackets, err := abilityraknet.StartSpendPresentation(
		req.command.Common.ObjectID, activeAbilityID, definition.AnimationName,
		cooldown, req.packet.SourceTime, remainingManaPoint,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkStart: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID), abilityStartTime, cooldown,
		)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("Quantum Blink cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, releaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("Quantum Blink release unavailable")
	}
	previousManaPoint := peerSession.deployedManaPoint()
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkCommit: %w", err)
	}
	run := &heroQuantumBlinkRun{}
	peerSession.heroQuantumBlink = run
	generation := peerSession.generation
	creatureIndex := peerSession.deployedCreatureIndex
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	schedule := heroQuantumBlinkSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
		returnPosition: initialDestination, initialDestination: initialDestination,
		targetObjectIDs: targetObjectIDs, initialTargetIndex: initialTargetIndex,
		creature: creature, definition: definition, damage: damage, binding: binding,
		run: run, cooldownReservation: cooldownReservation,
		releaseReservation: releaseReservation, releasePacket: releasePacket,
		releaseDelay: releaseDelay,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, definition.ShotCount+3)
	slide := heroQuantumBlinkAnimation{
		schedule: schedule, animationName: definition.SlideAnimationName,
		deadline: definition.HitDelay,
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: definition.HitDelay, Produce: slide.produce,
	})
	for index := uint32(0); index < definition.ShotCount; index++ {
		deadline := slideDelay + time.Duration(index)*definition.TickDuration
		step := heroQuantumBlinkStrike{
			schedule: schedule, index: index, deadline: deadline,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	relaxDelay := slideDelay +
		time.Duration(definition.ShotCount)*definition.TickDuration
	relax := heroQuantumBlinkAnimation{
		schedule: schedule, animationName: definition.SecondaryAnimationName,
		deadline: relaxDelay,
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: relaxDelay, Produce: relax.produce,
	})
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: releaseDelay, Produce: schedule.finish,
	})
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else if req.packet.ScheduleGroup != nil {
		cancel, err = req.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("schedule unavailable")
	}
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("quantumBlinkSchedule: %w", err)
	}
	run.mutex.Lock()
	run.cancel = cancel
	run.mutex.Unlock()
	r.logger.Printf(
		"RakNet Quantum Blink accepted source=%d targets=%d destination=(%g,%g,%g)",
		req.command.Common.ObjectID, len(targetObjectIDs),
		initialDestination.X, initialDestination.Y, initialDestination.Z,
	)
	return append([][]byte{ackPacket}, startPackets...), nil
}

func quantumBlinkSlideDestination(
	start raknet.Vector3, target raknet.Vector3,
	sourceFootprint float32, targetFootprint float32,
) raknet.Vector3 {
	stopDistance := max(float32(0), sourceFootprint) + max(float32(0), targetFootprint)
	distance := chargeDistance(start, target)
	if distance <= stopDistance || distance == 0 {
		return target
	}
	ratio := stopDistance / distance
	return raknet.Vector3{
		X: target.X + (start.X-target.X)*ratio,
		Y: target.Y + (start.Y-target.Y)*ratio,
		Z: target.Z + (start.Z-target.Z)*ratio,
	}
}
