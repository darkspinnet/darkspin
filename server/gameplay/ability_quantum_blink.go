package gameplay

import (
	"errors"
	"fmt"
	"math"
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
	registry        *gameplaySessionRegistry
	mutex           sync.Mutex
	cancel          raknet.CancelSchedule
	isStopped       bool
	returnPosition  raknet.Vector3
	orientation     game.Quaternion
	sourceObjectID  uint32
	startedAt       time.Time
	sourceTime      uint64
	isPositionSaved bool
	nextTargetIndex int
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
	syncStamp           uint8
	activeAbilityID     uint32
	abilityIndex        uint32
	slideDelay          time.Duration
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
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID &&
		peerSession.heroQuantumBlink == e.run
	if !isCurrent {
		return false
	}
	e.run.mutex.Lock()
	isStopped := e.run.isStopped
	e.run.mutex.Unlock()
	return !isStopped
}

func (e heroQuantumBlinkSchedule) selectTarget(
	npc *zonenpc.Session, strikeIndex uint32,
) (zonenpc.Snapshot, bool) {
	if npc == nil || len(e.targetObjectIDs) == 0 || e.initialTargetIndex < 0 {
		return zonenpc.Snapshot{}, false
	}
	start := e.run.nextTargetIndex
	count := len(e.targetObjectIDs)
	if strikeIndex == 0 {
		start = e.initialTargetIndex
		count = 1
	}
	for offset := range count {
		index := (start + offset) % len(e.targetObjectIDs)
		target, isFound := npc.NPC(e.targetObjectIDs[index])
		if isFound && !target.IsDefeated && target.HitPoint > 0 &&
			target.Faction == zonenpc.FactionNonPlayerAligned &&
			!target.Plan.IsFixture {
			if strikeIndex > 0 {
				e.run.nextTargetIndex = (index + 1) % len(e.targetObjectIDs)
			}
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
	packet, err := npcraknet.AnimationState(
		e.sourceObjectID, animationName,
		e.packet.SourceTime+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkAnimate: %w", err)
	}
	err = e.runtime.registry.queueQuantumBlinkPoseLocked(&peerSession, [][]byte{packet})
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkAnimateQueue: %w", err)
	}
	return nil, nil
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
	if strikeIndex > 0 && !isTargetFound {
		packets, err := e.finishLocked(peerSession, deadline)
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return packets, fmt.Errorf("blinkEarlyFinish: %w", err)
		}
		return packets, nil
	}
	orientation := e.run.orientation
	if strikeIndex > 0 {
		random, randomErr := peerSession.abilityRandom()
		if randomErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("blinkFacingRandom: %w", randomErr)
		}
		// RandomDir runs before PlayStrikeAnimation. Native TeleportAndFace
		// constructs a world-up quaternion whose local Y is that direction.
		yaw := random.Float64()*2*math.Pi - math.Pi/2
		orientation = game.Quaternion{Z: float32(math.Sin(yaw / 2)), W: float32(math.Cos(yaw / 2))}
	}
	animationPacket, err := e.strikeAnimation(strikeIndex, deadline, peerSession)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkPose[%d]: %w", strikeIndex, err)
	}
	destination := e.initialDestination
	if isTargetFound && strikeIndex > 0 {
		destination = raknet.Vector3(target.Plan.Position)
	}
	err = peerSession.teleportPlayer(e.runtime.now(), destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkPosition[%d]: %w", strikeIndex, err)
	}
	if strikeIndex == 0 {
		e.run.isPositionSaved = true
	}
	err = peerSession.syncZoneHeroPose()
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkPoseSync[%d]: %w", strikeIndex, err)
	}
	err = peerSession.zone.Hero().SetOrientation(peerSession.binding.UserID, peerSession.generation, e.sourceObjectID, orientation)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("blinkFacing[%d]: %w", strikeIndex, err)
	}
	e.run.orientation = orientation
	// SavePosition follows the initial slide, before any later strike teleport.
	// Cancellation during the windup must not move the caster to the destination.
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
	packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: e.sourceObjectID, Position: game.Vec3(destination), Orientation: orientation,
	})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("quantumBlinkTeleport[%d]: %w", strikeIndex, err)
	}
	if len(animationPacket) > 0 {
		packets = append(packets, animationPacket)
	}
	err = e.runtime.registry.queueQuantumBlinkPoseLocked(&peerSession, packets)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkStrikeQueue[%d]: %w", strikeIndex, err)
	}
	if result.Damage.ObjectID == 0 {
		if strikeIndex == 0 && e.initialTargetIndex < 0 {
			packets, finishErr := e.finishAt(deadline)
			if finishErr != nil {
				return packets, fmt.Errorf("blinkEmptyFinish: %w", finishErr)
			}
			return packets, nil
		}
		return nil, nil
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
	return damagePackets, nil
}

func (e heroQuantumBlinkSchedule) strikeAnimation(
	strikeIndex uint32, deadline time.Duration, peerSession gameplayPeerSession,
) ([]byte, error) {
	// Activate deals the slide hit before DoStrikes begins. Only the five
	// subsequent StrikeEnemy calls choose a strike animation.
	if strikeIndex == 0 {
		return nil, nil
	}
	random, err := peerSession.abilityRandom()
	if err != nil {
		return nil, fmt.Errorf("blinkRandom: %w", err)
	}
	animationIndex, err := random.Index(uint32(len(e.definition.AnimationNames)))
	if err != nil {
		return nil, fmt.Errorf("blinkPose: %w", err)
	}
	packet, err := npcraknet.AnimationState(
		e.sourceObjectID, e.definition.AnimationNames[animationIndex],
		e.packet.SourceTime+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("blinkAnimation: %w", err)
	}
	return packet, nil
}

func (e heroQuantumBlinkSchedule) finish() ([][]byte, error) {
	packets, err := e.finishAt(e.releaseDelay)
	if err != nil {
		return packets, fmt.Errorf("blinkFinish: %w", err)
	}
	return packets, nil
}

func (e heroQuantumBlinkSchedule) finishAt(deadline time.Duration) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		return nil, nil
	}
	packets, err := e.finishLocked(peerSession, deadline)
	if err != nil {
		return packets, fmt.Errorf("blinkFinishState: %w", err)
	}
	return packets, nil
}

func (e heroQuantumBlinkSchedule) finishLocked(peerSession gameplayPeerSession, deadline time.Duration) ([][]byte, error) {
	if !peerSession.abilityReleaseSession().IsCurrent(e.releaseReservation) {
		peerSession.heroQuantumBlink = nil
		e.run.Stop()
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		return nil, nil
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		e.syncStamp, e.activeAbilityID, e.abilityIndex,
		e.packet.SourceTime, e.slideDelay, deadline,
	)
	if err != nil {
		return nil, fmt.Errorf("blinkRelease: %w", err)
	}
	err = peerSession.teleportPlayer(e.runtime.now(), e.returnPosition)
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkReturn: %w", err)
	}
	err = peerSession.syncZoneHeroPose()
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkReturnPose: %w", err)
	}
	packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: e.sourceObjectID, Position: game.Vec3(e.returnPosition), Orientation: e.run.orientation,
	})
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkReturnMarshal: %w", err)
	}
	resetPacket, err := npcraknet.ResetAnimation(
		e.sourceObjectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkReset: %w", err)
	}
	packets = append(packets, resetPacket)
	err = e.runtime.registry.queueQuantumBlinkPoseLocked(&peerSession, packets)
	if err != nil {
		return nil, fmt.Errorf("quantumBlinkFinishQueue: %w", err)
	}
	isCompleted := peerSession.abilityReleaseSession().Complete(
		e.releaseReservation, e.run.startedAt.Add(deadline),
	)
	if isCompleted {
		peerSession.queuePackets([][]byte{releasePacket})
	}
	peerSession.heroQuantumBlink = nil
	e.run.mutex.Lock()
	e.run.cancel = nil
	e.run.isStopped = true
	e.run.mutex.Unlock()
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	return nil, nil
}

func (e heroQuantumBlinkSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		cleanupErr := peerSession.retireQuantumBlinkLocked(e.runtime.now())
		if cleanupErr != nil {
			e.runtime.logger.Printf("RakNet Quantum Blink failure cleanup user=%d: %v", peerSession.binding.UserID, cleanupErr)
		}
		manaErr := peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		if manaErr != nil {
			e.runtime.logger.Printf("RakNet Quantum Blink mana rollback user=%d: %v", peerSession.binding.UserID, manaErr)
		}
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
		definition.AnimationName == "" ||
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
		game.Vec3(initialDestination), peerSession.deployedCampaignNavigationRadius(),
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
	releaseDelay := slideDelay + time.Duration(definition.ShotCount)*definition.TickDuration
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
	run := &heroQuantumBlinkRun{
		registry:       r.registry,
		returnPosition: initialDestination, sourceObjectID: req.command.Common.ObjectID,
		startedAt: abilityStartTime, sourceTime: req.packet.SourceTime,
		orientation: normalizedHeroOrientation(req.command.Common.Orientation),
	}
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
		releaseReservation: releaseReservation,
		syncStamp:          req.command.Common.Unknown[0], activeAbilityID: activeAbilityID,
		abilityIndex: req.command.Ability.Index, slideDelay: slideDelay,
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
	for index := uint32(0); index <= definition.ShotCount; index++ {
		deadline := slideDelay
		if index > 0 {
			deadline += time.Duration(index-1) * definition.TickDuration
		}
		step := heroQuantumBlinkStrike{
			schedule: schedule, index: index, deadline: deadline,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
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
	isStopped := run.isStopped
	if !isStopped {
		run.cancel = cancel
	}
	run.mutex.Unlock()
	if isStopped && cancel != nil {
		cancel()
	}
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
	if distance < stopDistance || distance == 0 {
		return start
	}
	ratio := stopDistance / distance
	return raknet.Vector3{
		X: target.X + (start.X-target.X)*ratio,
		Y: target.Y + (start.Y-target.Y)*ratio,
		Z: target.Z + (start.Z-target.Z)*ratio,
	}
}
