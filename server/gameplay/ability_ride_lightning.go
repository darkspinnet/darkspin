package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const rideLightningShockDuration = 3 * time.Second

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
	destination         raknet.Vector3
	arrivalPackets      [][]byte
	strikeRange         float32
	releaseResponse     []byte
}

// LightningRogueActive teleports and plays cast_blinkstrike_end at timetohit,
// after its wind-up; timetorelease only releases the ability lock.
func (e rideLightningSchedule) produceArrival() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		peerSession.deployedObjectID != e.sourceObjectID ||
		peerSession.deployedHitPoint() <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	previousMotion := peerSession.playerMotionSnapshot()
	err := peerSession.teleportPlayer(e.runtime.now(), e.destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("rideArrivalTeleport: %w", err)
	}
	err = peerSession.syncZoneHero()
	if err != nil {
		peerSession.restorePlayerMotion(previousMotion, peerSession.playerMotionRevision())
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("rideArrivalHero: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets := clonePendingPackets(e.arrivalPackets)
	if e.plan.TargetObjectID == 0 {
		return packets, nil
	}
	impactPackets, err := e.produceImpact()
	if err != nil {
		// Arrival already committed. Keep its correction deliverable even if
		// damage publication fails, rather than stranding the client at takeoff.
		e.runtime.logger.Printf("RakNet Ride the Lightning impact failed source=%d: %v", e.sourceObjectID, err)
		return packets, nil
	}
	return append(packets, impactPackets...), nil
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
	if !isTargetFound || liveNPC.IsDefeated || liveNPC.HitPoint <= 0 ||
		liveNPC.Plan.Position.Sub(game.Vec3(e.destination)).Length() > e.strikeRange {
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
	return [][]byte{e.releaseResponse}, nil
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

func (r campaignAbilityCommandRuntime) handleRideLightning(
	req campaignCharacterAbilityRequest, specialTwoAbility zonecontent.HeroAbility,
) ([][]byte, error) {
	var err error
	packet := req.packet
	command := req.command
	commandSession := req.commandSession
	abilityStartTime := req.startTime
	targetObjectID := req.targetObjectID
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
		return req.reject("session unavailable")
	}
	creatureIndex := peerSession.deployedCreatureIndex
	creature := r.registry.projectPassiveCreature(
		peerSession, creatureIndex, abilityStartTime,
	)
	destination := command.Ability.CursorPosition
	if !isReportedZonePosition(destination) {
		destination = command.Ability.TargetPosition
	}
	if !isFiniteZonePosition(destination) || !isReportedZonePosition(destination) ||
		peerSession.zone == nil || peerSession.zone.Navigation() == nil {
		r.registry.mutex.Unlock()
		return req.reject("cursor or navigation unavailable")
	}
	// A blink needs a walkable landing surface, not a walking path from the
	// caster or the melee target's range. Preserve cursor-directed ground casts.
	landing, isLandingFound, projectionErr := zonenavigation.ProjectPosition(
		peerSession.zone.Navigation(), game.Vec3(destination),
		peerSession.deployedCampaignFootprintRadius(),
	)
	if projectionErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("rideLandingProject: %w", projectionErr)
	}
	if !isLandingFound {
		r.registry.mutex.Unlock()
		return req.reject("cursor destination is not walkable")
	}
	destination = raknet.Vector3(landing)
	err = peerSession.advancePlayerPosition(abilityStartTime, command.Common.Position)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignRidePosition: %w", err)
	}
	definition := specialTwoAbility.Definition
	cooldownKey := zoneability.HeroAbilityCooldown(specialTwoAbility.ID)
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
	if !isCooldownReady || peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("destination, cooldown, or power unavailable")
	}
	ridePlan := zoneability.BasicPlan{}
	strikeRange := peerSession.deployedCampaignFootprintRadius() +
		zonenavigation.ProjectionDistance
	if targetObjectID != 0 && peerSession.zone.NPCs() != nil {
		target, isTargetFound := peerSession.zone.NPCs().LiveNPC(targetObjectID)
		if isTargetFound && target.Faction == zonenpc.FactionNonPlayerAligned &&
			!target.Plan.IsFixture {
			strikeRange += target.Plan.NPCProfile.FootprintRadius
			if target.Plan.Position.Sub(landing).Length() <= strikeRange {
				ridePlan, err = zoneability.PlanProjected(
					peerSession.zone.NPCs(), command.Common.ObjectID, targetObjectID,
					landing, creature, definition, strikeRange,
				)
				if err != nil {
					r.registry.mutex.Unlock()
					return nil, fmt.Errorf("rideStrikePlan: %w", err)
				}
			}
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
	arrivalAnimationPacket, marshalErr := abilityraknet.Animation(
		command.Common.ObjectID, definition.OutAnimationName,
		packet.SourceTime+uint64(definition.HitDelay/time.Millisecond),
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
		return req.reject("cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, definition.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.restorePlayerMotion(previousMotion, peerSession.playerMotionRevision())
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
		destination:         destination,
		arrivalPackets:      append(teleportPackets, arrivalAnimationPacket),
		strikeRange:         strikeRange,
		releaseResponse:     releaseResponsePacket,
	}
	var cancel raknet.CancelSchedule
	producers := []raknet.ScheduledPacketProducer{
		{Delay: definition.HitDelay, Produce: schedule.produceArrival},
		{Delay: definition.ReleaseDelay, Produce: schedule.produceRelease},
	}
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
	if cancel == nil {
		rollbackErr := schedule.rollbackAdmission()
		return nil, fmt.Errorf("rideScheduleCancel: %w", errors.Join(
			errors.New("cancellation unavailable"), rollbackErr,
		))
	}
	schedule.bindCancel(cancel)
	r.logger.Printf(
		"RakNet campaign teleport strike accepted ability=%s source=%d target=%d destination=(%g,%g,%g) mana=%g cooldown=%s arrival=%s release=%s",
		definition.Name, command.Common.ObjectID, ridePlan.TargetObjectID,
		destination.X, destination.Y, destination.Z, remainingManaPoint,
		cooldown, definition.HitDelay, definition.ReleaseDelay,
	)
	packets := make([][]byte, 0, 1+len(startPackets))
	packets = append(packets, ackPacket)
	// Teammates do not run the caster's predicted ability animation.
	// Publish its wind-up now; teleport and arrival are delivered at timetohit.
	packets = append(packets, startPackets...)
	if len(abilityLessonCompletePacket) != 0 {
		packets = append(packets, abilityLessonCompletePacket)
	}
	return packets, nil
}
