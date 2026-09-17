package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const turretFallbackHitPoint = float32(25)
const turretFallbackFootprint = float32(0.5)
const claymoreArmDelay = 400 * time.Millisecond
const claymoreTriggerInterval = 250 * time.Millisecond
const claymorePerceptionRadius = float32(20)

type heroTrapRun struct {
	assetName     string
	objectID      uint32
	ownerObjectID uint32
	isTriggered   bool
	cancel        raknet.CancelSchedule
}

func (e *heroTrapRun) Stop() {
	if e == nil || e.cancel == nil {
		return
	}
	e.cancel()
	e.cancel = nil
}

func (e *gameplayPeerSession) breakHeroTrap(
	objectID uint32,
) ([][]byte, bool, error) {
	if e == nil || objectID == 0 {
		return nil, false, nil
	}
	run := e.heroTraps[objectID]
	if run == nil || (run.assetName != "PipeBomb" && run.assetName != "TurretTrap") {
		return nil, false, nil
	}
	run.Stop()
	delete(e.heroTraps, objectID)
	if e.zone != nil && e.zone.Companion() != nil {
		e.zone.Companion().Remove(objectID)
	}
	effectPacket, err := abilityraknet.TrapObjectEffect(
		"cyber_trapper_trapBroken.ServerEventDef", objectID, run.ownerObjectID,
	)
	if err != nil {
		return nil, true, fmt.Errorf("heroTrapBrokenEffect: %w", err)
	}
	deletePacket, err := abilityraknet.TrapDelete(objectID)
	if err != nil {
		return nil, true, fmt.Errorf("heroTrapBrokenDelete: %w", err)
	}
	return [][]byte{effectPacket, deletePacket}, true, nil
}

type heroTrapSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	objectID            uint32
	creatureIndex       uint32
	previousManaPoint   float32
	position            raknet.Vector3
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	run                 *heroTrapRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
	detonationDelay     time.Duration
}

func (e heroTrapSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroTraps[e.objectID] == e.run
}

func (e heroTrapSchedule) spawn() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := abilityraknet.TrapSpawn(
		e.objectID, e.sourceObjectID, e.position, e.definition,
	)
	if err != nil {
		return nil, fmt.Errorf("heroTrapSpawn: %w", err)
	}
	packets := [][]byte{packet}
	if e.definition.ActivationEffectName != "" {
		effectPacket, effectErr := abilityraknet.TrapObjectEffect(
			e.definition.ActivationEffectName, e.objectID, e.sourceObjectID,
		)
		if effectErr != nil {
			return nil, fmt.Errorf("heroTrapActivation: %w", effectErr)
		}
		packets = append(packets, effectPacket)
	}
	if e.definition.Name == "PipeBomb" {
		tauntPackets, tauntErr := e.tauntPipeBomb()
		if tauntErr != nil {
			return nil, fmt.Errorf("heroPipeBombTaunt: %w", tauntErr)
		}
		packets = append(packets, tauntPackets...)
	}
	return packets, nil
}

func (e heroTrapSchedule) tauntPipeBomb() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.zone.NPCs() == nil {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	center := game.Vec3(e.position)
	targetObjectIDs := make([]uint32, 0)
	for _, npc := range peerSession.zone.NPCs().LiveSnapshots() {
		if npc.Faction != zonenpc.FactionNonPlayerAligned || npc.IsDefeated ||
			npc.HitPoint <= 0 || npc.Plan.IsFixture ||
			center.Sub(npc.Plan.Position).Length() > e.definition.Radius {
			continue
		}
		targetObjectIDs = append(targetObjectIDs, npc.Plan.ObjectID)
	}
	e.runtime.registry.mutex.RUnlock()
	taunt := e.definition
	taunt.StatusKind = sim.AbilityStatusKindTaunt
	taunt.RootModifierID = 0x658bb220
	taunt.StatusDuration = 3 * time.Second
	timestamp := e.packet.SourceTime + uint64(e.definition.HitDelay/time.Millisecond)
	packets := make([][]byte, 0, len(targetObjectIDs))
	for _, targetObjectID := range targetObjectIDs {
		targetPackets, err := e.runtime.damage.applyHeroTaunt(
			e.packet, e.sessionKey, e.generation, e.objectID,
			timestamp, taunt, targetObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("target[%d]: %w", targetObjectID, err)
		}
		packets = append(packets, targetPackets...)
	}
	return packets, nil
}

func (e heroTrapSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e heroTrapSchedule) trigger() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || e.definition.Name != "ClaymoreTrap" ||
		e.run.isTriggered || peerSession.zone == nil || peerSession.zone.NPCs() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	center := game.Vec3(e.position)
	isTriggered := false
	for _, npc := range peerSession.zone.NPCs().LiveSnapshots() {
		if npc.Faction != zonenpc.FactionNonPlayerAligned || npc.IsDefeated ||
			npc.HitPoint <= 0 || npc.Plan.IsFixture ||
			center.Sub(npc.Plan.Position).Length() >= claymorePerceptionRadius {
			continue
		}
		isTriggered = true
		break
	}
	if !isTriggered {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.run.isTriggered = true
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	detonation := e
	detonation.detonationDelay += e.definition.TickDuration
	producers := []raknet.ScheduledPacketProducer{{
		Delay: e.definition.TickDuration, Produce: detonation.detonate,
	}}
	producers = e.runtime.registry.producerGuard.scheduledProducers(
		e.sessionKey, producers,
	)
	_, err := e.packet.ScheduleProducers(producers)
	if err != nil {
		return nil, fmt.Errorf("heroTrapTriggerSchedule: %w", err)
	}
	return nil, nil
}

func (e heroTrapSchedule) detonate() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.definition.Name == "TurretTrap" {
		peerSession.zone.Companion().Remove(e.objectID)
		delete(peerSession.heroTraps, e.objectID)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		packets := make([][]byte, 0, 2)
		if e.definition.ImpactEffectName != "" {
			effectPacket, err := abilityraknet.TrapObjectEffect(
				e.definition.ImpactEffectName, e.objectID, e.sourceObjectID,
			)
			if err != nil {
				return nil, fmt.Errorf("heroTurretRepack: %w", err)
			}
			packets = append(packets, effectPacket)
		}
		deletePacket, err := abilityraknet.TrapDelete(e.objectID)
		if err != nil {
			return nil, fmt.Errorf("heroTurretDelete: %w", err)
		}
		return append(packets, deletePacket), nil
	}
	plan, err := zoneability.PlanTrapArea(
		peerSession.zone.NPCs(), e.sourceObjectID,
		game.Vec3(e.position), e.creature, e.definition,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapPlan: %w", err)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
		plan, e.creature, peerSession.binding.Difficulty,
		e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr :=
			peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroTrapTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	if e.definition.Name == "PipeBomb" {
		peerSession.zone.Companion().Remove(e.objectID)
	}
	delete(peerSession.heroTraps, e.objectID)
	e.run.cancel = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	timestamp := e.packet.SourceTime + uint64(e.detonationDelay/time.Millisecond)
	var effect func(zoneability.AreaResult) ([]byte, error)
	if e.definition.HitEffectName != "" {
		impact := campaignPointBlankImpact{
			assetName: e.definition.HitEffectName, attackerID: e.sourceObjectID,
			source: e.position,
		}
		effect = impact.marshal
	}
	statusPlan := plan
	statusPlan.AbilityID = e.objectID
	statusResults := results
	if e.definition.Name == "PipeBomb" {
		statusResults = append([]zoneability.AreaResult(nil), results...)
		for index := range statusResults {
			if statusResults[index].Damage.IsDefeated {
				continue
			}
			statusResults[index].Damage.IsDamageImmune = false
			statusResults[index].Damage.Damage = 1
		}
	}
	statusSourceObjectID := e.sourceObjectID
	if e.definition.Name == "PipeBomb" {
		statusSourceObjectID = e.objectID
	}
	statusPackets, err := e.runtime.applyAcceptedHitStatus(
		e.packet, e.sessionKey, e.generation, statusSourceObjectID,
		timestamp, statusPlan, statusResults,
	)
	if err != nil {
		return nil, fmt.Errorf("heroTrapStatus: %w", err)
	}
	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		timestamp, e.binding, results, transitions, effect, false,
	)
	if err != nil {
		return nil, fmt.Errorf("heroTrapPublish: %w", err)
	}
	if e.definition.Name == "PipeBomb" {
		packets = append(statusPackets, packets...)
	} else {
		packets = append(packets, statusPackets...)
	}
	if e.definition.ImpactEffectName != "" {
		detonationPacket, effectErr := abilityraknet.CursorAreaImpact(
			e.definition.ImpactEffectName, e.position,
		)
		if effectErr != nil {
			return nil, fmt.Errorf("heroTrapDetonation: %w", effectErr)
		}
		packets = append([][]byte{detonationPacket}, packets...)
	}
	deletePacket, err := abilityraknet.TrapDelete(e.objectID)
	if err != nil {
		return nil, fmt.Errorf("heroTrapDelete: %w", err)
	}
	return append(packets, deletePacket), nil
}

func (e heroTrapSchedule) fireTurret() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || e.definition.Name != "TurretTrap" {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	turret, isTurretFound := peerSession.zone.Companion().Snapshot(e.objectID)
	if !isTurretFound || turret.HitPoint <= 0 {
		peerSession.zone.Companion().Remove(e.objectID)
		delete(peerSession.heroTraps, e.objectID)
		cancel := e.run.cancel
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		if cancel != nil {
			cancel()
		}
		packets := make([][]byte, 0, 2)
		if e.definition.HitEffectName != "" {
			effectPacket, err := abilityraknet.TrapObjectEffect(
				e.definition.HitEffectName, e.objectID, e.sourceObjectID,
			)
			if err != nil {
				return nil, fmt.Errorf("heroTurretBroken: %w", err)
			}
			packets = append(packets, effectPacket)
		}
		deletePacket, err := abilityraknet.TrapDelete(e.objectID)
		if err != nil {
			return nil, fmt.Errorf("heroTurretBrokenDelete: %w", err)
		}
		return append(packets, deletePacket), nil
	}
	target := zonenpc.Snapshot{}
	targetDistance := float32(math.MaxFloat32)
	for _, current := range peerSession.zone.NPCs().LiveSnapshots() {
		if current.Faction != zonenpc.FactionNonPlayerAligned ||
			current.IsDefeated || current.HitPoint <= 0 || current.Plan.IsFixture {
			continue
		}
		distance := game.Vec3(e.position).Sub(current.Plan.Position).Length()
		maximumRange := e.definition.Range +
			max(float32(0), current.Plan.NPCProfile.FootprintRadius)
		if distance > maximumRange || distance > targetDistance {
			continue
		}
		if distance == targetDistance && target.Plan.ObjectID != 0 &&
			current.Plan.ObjectID > target.Plan.ObjectID {
			continue
		}
		target = current
		targetDistance = distance
	}
	if target.Plan.ObjectID == 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	damageRange, err := zoneability.ProjectDamage(
		e.creature, e.definition,
		e.definition.MinimumDamage, e.definition.MaximumDamage,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTurretDamage: %w", err)
	}
	damage, err := sim.SelectRankDamage(
		peerSession.zone.NPCRandom(),
		sim.DamageRange{Minimum: damageRange.Minimum, Maximum: damageRange.Maximum},
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTurretDamageSelect: %w", err)
	}
	critical, err := sim.ResolveCriticalDamage(
		peerSession.zone.NPCRandom(), damage, peerSession.binding.Difficulty,
		sim.CriticalProfile{
			Rating: e.creature.CriticalRating, AutoCrit: e.creature.AutoCrit,
			DamageIncrease: e.creature.CriticalDamageIncrease,
		},
		e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTurretCritical: %w", err)
	}
	result, err := peerSession.zone.NPCs().Hit(zonenpc.HitRequest{
		SourceObjectID: e.objectID, TargetObjectID: target.Plan.ObjectID, Damage: critical.Damage,
		SourcePosition: nil, Metadata: zoneability.NPCDamageMetadata(e.definition),
	})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTurretCommit: %w", err)
	}
	transition, err := peerSession.applyCampaignDamageTransition(result)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTurretTransition: %w", err)
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	presentationPackets, err := abilityraknet.TurretLaser(
		abilityraknet.TurretLaserRequest{
			SourceObjectID: e.objectID, TargetObjectID: target.Plan.ObjectID,
			SourceTime: e.packet.SourceTime,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("heroTurretPresentation: %w", err)
	}
	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.objectID,
		e.packet.SourceTime, binding,
		[]zoneability.AreaResult{{
			Snapshot: target, Damage: result, IsCritical: critical.IsCritical,
		}},
		[]campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("heroTurretPublish: %w", err)
	}
	return append(presentationPackets, packets...), nil
}

func (e heroTrapSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroTraps, e.objectID)
		if (e.definition.Name == "TurretTrap" || e.definition.Name == "PipeBomb") &&
			peerSession.zone != nil {
			peerSession.zone.Companion().Remove(e.objectID)
		}
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
			"RakNet hero trap stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (r campaignAbilityCommandRuntime) handleHeroTrap(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindTrap ||
		definition.SpawnNoun == "" || definition.Duration <= 0 ||
		definition.TickDuration < 0 ||
		definition.IsForwardPlacement && definition.PlacementDistance <= 0 {
		r.registry.mutex.Unlock()
		return req.reject("trap definition unavailable")
	}
	position := req.command.Ability.TargetPosition
	if definition.IsForwardPlacement {
		facing, facingErr := geometryraknet.Forward(req.command.Common.Orientation)
		if facingErr != nil {
			r.registry.mutex.Unlock()
			return req.reject("trap facing unavailable")
		}
		position = raknet.Vector3{
			X: peerSession.playerPosition.X + facing.X*definition.PlacementDistance,
			Y: peerSession.playerPosition.Y + facing.Y*definition.PlacementDistance,
			Z: peerSession.playerPosition.Z + facing.Z*definition.PlacementDistance,
		}
	} else if !isReportedZonePosition(position) {
		position = req.command.Ability.CursorPosition
	}
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	if !isReportedZonePosition(position) || !isFiniteZonePosition(position) ||
		!isInsideZoneTrigger(peerSession.playerPosition, position, admissionRange) {
		r.registry.mutex.Unlock()
		return req.reject("trap position unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	objectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapObjectID: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	previousManaPoint := peerSession.deployedManaPoint()
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp:    req.command.Common.Unknown[0],
		ResponseType: raknet.ActionResponseAccepted,
		ObjectID:     activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds: req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime +
			uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(projected.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapAck: %w", err)
	}
	cooldownPacket, err := raknet.MarshalApplication(raknet.CooldownUpdateMessage{
		ObjectID:                req.command.Common.ObjectID,
		AbilityKey:              uint64(activeAbilityID),
		DurationMilliseconds:    projected.Cooldown.Milliseconds(),
		SourceStartMilliseconds: int64(req.packet.SourceTime),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapCooldown: %w", err)
	}
	manaPacket, err := abilityraknet.Mana(
		req.command.Common.ObjectID, remainingManaPoint,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapManaPacket: %w", err)
	}
	var animationPacket []byte
	if projected.AnimationName != "" {
		animationPacket, err = raknet.MarshalApplication(raknet.SetAnimationStateMessage{
			ObjectID:  req.command.Common.ObjectID,
			State:     util.HashID(projected.AnimationName),
			Timestamp: req.packet.SourceTime, Scale: 1,
		})
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroTrapAnimation: %w", err)
		}
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], activeAbilityID,
		req.command.Ability.Index, req.packet.SourceTime,
		projected.HitDelay, projected.ReleaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapRelease: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID),
			abilityStartTime, projected.Cooldown,
		)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("trap cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("trap release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTrapCommit: %w", err)
	}
	if projected.Name == "TurretTrap" || projected.Name == "PipeBomb" {
		hitPoint := r.program.NonPlayerHitPoint[util.HashID(projected.Name)]
		if hitPoint <= 0 {
			hitPoint = turretFallbackHitPoint
		}
		footprint, footprintErr := r.program.FootprintRadius(projected.SpawnNoun)
		if footprintErr != nil || footprint <= 0 {
			footprint = turretFallbackFootprint
		}
		err = peerSession.zone.Companion().Put(zonecompanion.Actor{
			UserID:         peerSession.binding.UserID,
			PeerGeneration: peerSession.generation,
			ObjectID:       objectID, OwnerObjectID: req.command.Common.ObjectID,
			Noun:     util.HashID(projected.SpawnNoun),
			Position: game.Vec3(position), FootprintRadius: footprint,
			HitPoint: hitPoint, MaximumHitPoint: hitPoint,
			IsTargetable: true, IsCombatant: false,
		})
		if err != nil {
			_ = peerSession.setDeployedManaPoints(previousManaPoint)
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroTrapCompanion: %w", err)
		}
	}
	run := &heroTrapRun{
		assetName: projected.Name, objectID: objectID,
		ownerObjectID: req.command.Common.ObjectID,
	}
	if peerSession.heroTraps == nil {
		peerSession.heroTraps = make(map[uint32]*heroTrapRun)
	}
	peerSession.heroTraps[objectID] = run
	generation := peerSession.generation
	binding := peerSession.binding
	creatureIndex := peerSession.deployedCreatureIndex
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroTrapSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		objectID: objectID, creatureIndex: creatureIndex,
		previousManaPoint: previousManaPoint, position: position,
		creature: creature, definition: projected, binding: binding, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: releasePacket,
		detonationDelay: projected.Duration + projected.TickDuration,
	}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: projected.HitDelay, Produce: schedule.spawn},
		{Delay: projected.ReleaseDelay, Produce: schedule.release},
		{Delay: projected.Duration + projected.TickDuration, Produce: schedule.detonate},
	}
	if projected.Name == "TurretTrap" {
		for tick := time.Second; tick < projected.Duration; tick += time.Second {
			fire := schedule
			fire.packet.SourceTime += uint64(
				(projected.HitDelay + tick) / time.Millisecond,
			)
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: projected.HitDelay + tick, Produce: fire.fireTurret,
			})
		}
	} else if projected.Name == "ClaymoreTrap" {
		for delay := projected.HitDelay + claymoreArmDelay; delay < projected.Duration+projected.TickDuration; delay += claymoreTriggerInterval {
			trigger := schedule
			trigger.detonationDelay = delay
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: delay, Produce: trigger.trigger,
			})
		}
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else {
		cancel, err = req.packet.ScheduleGroup(producers)
	}
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("heroTrapSchedule: %w", err)
	}
	run.cancel = cancel
	r.logger.Printf(
		"RakNet hero trap accepted ability=%s source=%d trap=%d position=(%g,%g,%g)",
		projected.Name, req.command.Common.ObjectID, objectID,
		position.X, position.Y, position.Z,
	)
	packets := [][]byte{ackPacket, cooldownPacket, manaPacket}
	if len(animationPacket) != 0 {
		packets = append(packets, animationPacket)
	}
	return packets, nil
}
