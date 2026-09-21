package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const beastPetFallbackHitPoint = float32(50)
const beastPetFallbackFootprint = float32(0.75)
const beastPetEnrageDuration = 8 * time.Second
const beastPetEnrageDamageIncrease = float32(0.50)
const beastPetEnrageHealingPercent = float32(0.05)
const beastPetEnrageBodyScale = float32(0.20)
const beastPetSpawnReleaseDelay = 1400 * time.Millisecond

type beastPetEnrageRun struct {
	runtime    campaignAbilityCommandRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	instanceID uint32
	timestamp  uint64
	cancel     raknet.CancelSchedule
}

func (e *beastPetEnrageRun) stop() ([]byte, error) {
	if e == nil {
		return nil, nil
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	_ = e.runtime.modifierPool.Release(e.instanceID)
	packet, err := effectraknet.ModifierDelete(e.objectID, e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageDelete: %w", err)
	}
	return packet, nil
}

type beastSummonReleaseStep struct {
	packet []byte
}

func (e beastSummonReleaseStep) produce() ([][]byte, error) {
	if len(e.packet) == 0 {
		return nil, nil
	}
	return [][]byte{e.packet}, nil
}

type beastPetSpawnReleaseStep struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	sourceTime uint64
}

func (e beastPetSpawnReleaseStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.beastPetObjectID == e.objectID && peerSession.zone != nil &&
		peerSession.zone.Companion() != nil
	if isCurrent {
		pet, isPetFound := peerSession.zone.Companion().Snapshot(e.objectID)
		isCurrent = isPetFound && pet.HitPoint > 0
	}
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	speedPacket, err := heroChargeSpeedPacket(
		e.objectID, zonecompanion.CompatibilityMovementSpeed,
	)
	if err != nil {
		return nil, fmt.Errorf("beastPetSpawnSpeed: %w", err)
	}
	attackPackets, err := e.runtime.startBeastPetAttack(
		e.packet, e.sessionKey, e.generation, e.sourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("beastPetSpawnAttack: %w", err)
	}
	return append([][]byte{speedPacket}, attackPackets...), nil
}

func (e *beastPetEnrageRun) isCurrent(peerSession gameplayPeerSession, isFound bool) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.beastPetObjectID == e.objectID &&
		peerSession.beastPetEnrage == e
}

func (e *beastPetEnrageRun) activate() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.beastPetDamageIncrease = beastPetEnrageDamageIncrease
	healPacket, err := e.healLocked(&peerSession)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("beastPetEnrageInitialHeal: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	acquired, err := peerSession.zone.TauntCompanion(
		peerSession.binding.UserID, e.generation, e.objectID, 5,
	)
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageTaunt: %w", err)
	}
	modifierPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: peerSession.deployedObjectID,
		TargetObjectID: e.objectID,
		ModifierID:     util.HashID("BeastPetRage"),
		InstanceID:     e.instanceID,
		Duration:       beastPetEnrageDuration,
		Timestamp:      e.timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageCreate: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset:    util.HashID("beastSentinel_cast_enrage.ServerEventDef"),
		ObjectID: e.objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageEffect: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.objectID,
		Value:    map[uint8]float32{113: beastPetEnrageBodyScale},
	})
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageScale: %w", err)
	}
	plan := make([]zonenpc.SpawnPlan, 0, len(acquired))
	for _, current := range acquired {
		plan = append(plan, current.Plan)
	}
	actionPackets, err := e.runtime.npc.scheduleFirstActions(
		e.packet, e.sessionKey, e.generation, plan, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageTauntSchedule: %w", err)
	}
	packets := append([][]byte{modifierPacket, effectPacket, attributePacket}, healPacket...)
	return append(packets, actionPackets...), nil
}

func (e *beastPetEnrageRun) heal() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packet, err := e.healLocked(&peerSession)
	if err == nil {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageHeal: %w", err)
	}
	return packet, nil
}

func (e *beastPetEnrageRun) healLocked(
	peerSession *gameplayPeerSession,
) ([][]byte, error) {
	companion, isFound := peerSession.zone.Companion().Snapshot(e.objectID)
	if !isFound || companion.HitPoint <= 0 {
		return nil, nil
	}
	healing := companion.MaximumHitPoint * beastPetEnrageHealingPercent
	hitPoint := min(companion.MaximumHitPoint, companion.HitPoint+healing)
	if hitPoint <= companion.HitPoint {
		return nil, nil
	}
	_, _, err := peerSession.zone.Companion().SetHitPoint(e.objectID, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageResource: %w", err)
	}
	integerChange := int32(hitPoint) - int32(companion.HitPoint)
	messages := []raknet.ApplicationMessage{
		raknet.DamageCombatEventMessage{
			Flags: 0x0002, DeltaHealth: -(hitPoint - companion.HitPoint),
			TargetID: e.objectID, SourceID: peerSession.deployedObjectID,
			IntegerHPChange: -integerChange,
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: e.objectID, HitPoints: hitPoint, IsHitPointChanged: true,
		},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("beastPetEnrageResourceMarshal[%d]: %w", index, marshalErr)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *beastPetEnrageRun) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.beastPetDamageIncrease = 0
	peerSession.beastPetEnrage = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	e.cancel = nil
	_ = e.runtime.modifierPool.Release(e.instanceID)
	modifierPacket, err := effectraknet.ModifierDelete(e.objectID, e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageExpiry: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.objectID,
		Value:    map[uint8]float32{113: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageScaleReset: %w", err)
	}
	return [][]byte{modifierPacket, attributePacket}, nil
}

func (e *gameplayPeerSession) spawnBeastPet(
	program Programs, objectID uint32, ownerObjectID uint32,
	position game.Vec3, petDamage float32,
) ([][]byte, error) {
	if e == nil || e.zone == nil || e.zone.Companion() == nil ||
		objectID == 0 || ownerObjectID == 0 {
		return nil, errors.New("beast pet spawn unavailable")
	}
	hitPoint := program.NonPlayerHitPoint[util.HashID("BeastSentinelPet")]
	if hitPoint <= 0 {
		hitPoint = beastPetFallbackHitPoint
	}
	hitPoint = e.petHitPoint(hitPoint)
	footprintRadius, err := program.FootprintRadius("BeastSentinelPet.Noun")
	if err != nil || footprintRadius <= 0 {
		footprintRadius = beastPetFallbackFootprint
	}
	err = e.zone.Companion().Put(zonecompanion.Actor{
		UserID: e.binding.UserID, PeerGeneration: e.generation,
		ObjectID: objectID, OwnerObjectID: ownerObjectID,
		Noun:     util.HashID("BeastSentinelPet.Noun"),
		Position: position, FootprintRadius: footprintRadius,
		HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		IsTargetable: true, IsCombatant: true,
	})
	if err != nil {
		return nil, fmt.Errorf("beastPetPut: %w", err)
	}
	packets, err := companionraknet.Create(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID("BeastSentinelPet.Noun"),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: 1, OwnerID: ownerObjectID,
		IsCollisionEnabled: true,
	})
	if err != nil {
		e.zone.Companion().Remove(objectID)
		return nil, fmt.Errorf("beastPetCreate: %w", err)
	}
	message := []raknet.ApplicationMessage{
		raknet.AttributeDataUpdateMessage{
			ObjectID: objectID,
			Value: map[uint8]float32{
				11: 0,
				12: 0,
				48: 0,
			},
		},
		raknet.ServerEventMessage{
			Asset:    util.HashID("beastSentinel_summon_burrowIn.ServerEventDef"),
			ObjectID: objectID,
			Position: raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
		},
		raknet.SetAnimationStateMessage{
			ObjectID: objectID, State: util.HashID("beastsentinel_pet_spawn"),
			Scale: 1,
		},
	}
	for index, current := range message {
		encoded, marshalErr := raknet.MarshalApplication(current)
		if marshalErr != nil {
			e.zone.Companion().Remove(objectID)
			return nil, fmt.Errorf("beastPetMarshal[%d]: %w", index, marshalErr)
		}
		packets = append(packets, encoded)
	}
	e.beastPetObjectID = objectID
	e.beastPetDamage = petDamage
	return packets, nil
}

func (e *gameplayPeerSession) stopBeastPet() ([][]byte, error) {
	if e == nil || e.beastPetObjectID == 0 {
		return nil, nil
	}
	objectID := e.beastPetObjectID
	e.beastPetObjectID = 0
	e.beastPetDamage = 0
	e.beastPetDamageIncrease = 0
	packet := make([][]byte, 0, 2)
	if e.zone != nil && e.zone.Companion() != nil {
		e.zone.Companion().Remove(objectID)
	}
	if e.sagePassiveActivations != nil {
		activation, isFound := e.sagePassiveActivations[objectID]
		if isFound && activation.Attack != nil {
			activation.Attack.Stop()
		}
		delete(e.sagePassiveActivations, objectID)
	}
	if e.beastPetEnrage != nil {
		enrage := e.beastPetEnrage
		e.beastPetEnrage = nil
		modifierPacket, err := enrage.stop()
		if err != nil {
			return nil, fmt.Errorf("beastPetEnrageStop: %w", err)
		}
		if len(modifierPacket) != 0 {
			packet = append(packet, modifierPacket)
		}
	}
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{objectID},
	})
	if err != nil {
		return nil, fmt.Errorf("beastPetDelete: %w", err)
	}
	return append(packet, deletePacket), nil
}

func (r campaignAbilityCommandRuntime) handleSummonBeastEnrage(
	req campaignCharacterAbilityRequest,
	ability zonecontent.HeroAbility,
	peerSession gameplayPeerSession,
	definition sim.AbilityDefinition,
	manaCost float32,
) ([][]byte, error) {
	const activationDelay = 360 * time.Millisecond
	if peerSession.beastPetEnrage != nil {
		r.registry.mutex.Unlock()
		return req.reject("Summon Beast enrage already active")
	}
	instanceID, err := r.modifierPool.Allocate()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastEnrageAllocate: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: ability.ID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime + uint64(activationDelay/time.Millisecond),
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(definition.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		_ = r.modifierPool.Release(instanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastEnrageAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], ability.ID, req.command.Ability.Index,
		req.packet.SourceTime, activationDelay, definition.ReleaseDelay,
	)
	if err != nil {
		_ = r.modifierPool.Release(instanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastEnrageRelease: %w", err)
	}
	startPackets, err := abilityraknet.StartSpendPresentation(
		req.command.Common.ObjectID, ability.ID, definition.AnimationName,
		definition.Cooldown, req.packet.SourceTime, remainingManaPoint,
	)
	if err != nil {
		_ = r.modifierPool.Release(instanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastEnrageStart: %w", err)
	}
	cooldownReservation, isCooldownReserved := peerSession.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(ability.ID), req.startTime, definition.Cooldown,
	)
	if !isCooldownReserved {
		_ = r.modifierPool.Release(instanceID)
		r.registry.mutex.Unlock()
		return req.reject("Summon Beast enrage cooldown unavailable")
	}
	releaseReservation, isReleaseReserved := peerSession.abilityReleaseSession().Reserve(
		req.startTime, definition.ReleaseDelay,
	)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		_ = r.modifierPool.Release(instanceID)
		r.registry.mutex.Unlock()
		return req.reject("Summon Beast enrage release unavailable")
	}
	previousManaPoint := peerSession.deployedManaPoint()
	err = peerSession.stopPlayerMovement(req.startTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		_ = r.modifierPool.Release(instanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastEnrageCommit: %w", err)
	}
	run := &beastPetEnrageRun{
		runtime: r, packet: req.packet, sessionKey: req.packet.Address.String(),
		generation: peerSession.generation, objectID: peerSession.beastPetObjectID,
		instanceID: instanceID,
		timestamp:  req.packet.SourceTime + uint64(activationDelay/time.Millisecond),
	}
	peerSession.beastPetEnrage = run
	r.registry.sessions[run.sessionKey] = peerSession
	r.registry.mutex.Unlock()
	producer := make([]raknet.ScheduledPacketProducer, 0, 10)
	producer = append(producer, raknet.ScheduledPacketProducer{
		Delay: activationDelay, Produce: run.activate,
	})
	for index := 1; index < 8; index++ {
		producer = append(producer, raknet.ScheduledPacketProducer{
			Delay:   activationDelay + time.Duration(index)*time.Second,
			Produce: run.heal,
		})
	}
	producer = append(producer,
		raknet.ScheduledPacketProducer{
			Delay:   definition.ReleaseDelay,
			Produce: beastSummonReleaseStep{packet: releasePacket}.produce,
		},
		raknet.ScheduledPacketProducer{
			Delay:   activationDelay + beastPetEnrageDuration,
			Produce: run.expire,
		},
	)
	cancel, err := req.packet.ScheduleProducers(producer)
	if err != nil {
		r.registry.mutex.Lock()
		latest, isFound := r.registry.sessions[run.sessionKey]
		if run.isCurrent(latest, isFound) {
			latest.beastPetEnrage = nil
			latest.beastPetDamageIncrease = 0
			_ = latest.setDeployedManaPoints(previousManaPoint)
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.sessions[run.sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("summonBeastEnrageSchedule: %w", err)
	}
	run.cancel = cancel
	packets := [][]byte{ackPacket}
	packets = append(packets, startPackets...)
	return packets, nil
}

func (r campaignAbilityCommandRuntime) handleSummonBeast(
	req campaignCharacterAbilityRequest, ability zonecontent.HeroAbility,
	supportCreature game.GameplayCreature,
) ([][]byte, error) {
	definition := projectHeroAbilityRank(
		ability.Definition, req.command.Ability.Rank,
	)
	if ability.ID == 0 || definition.Kind != sim.AbilityKindSummonBuff ||
		definition.SpawnNoun != "BeastSentinelPet.Noun" {
		return req.reject("Summon Beast definition unavailable")
	}
	if req.packet.ScheduleFunc == nil {
		return req.reject("Summon Beast schedule unavailable")
	}
	sessionKey := req.packet.Address.String()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isAvailable := isFound && peerSession.generation == req.commandSession.generation &&
		req.command.Common.ObjectID == peerSession.deployedObjectID &&
		peerSession.zone != nil &&
		peerSession.zone.Companion() != nil &&
		peerSession.abilityCooldownSession().IsReady(
			zoneability.HeroAbilityCooldown(ability.ID), req.startTime,
		) && peerSession.isAbilityReleaseReady(req.startTime)
	if !isAvailable {
		r.registry.mutex.Unlock()
		return req.reject("Summon Beast session, cooldown, or tracker unavailable")
	}
	definition, err := zoneability.ProjectTiming(supportCreature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		definition.ManaCost, supportCreature.DamageProfile.PrimaryAttribute,
		definition.ManaCoefficient, peerSession.isOverdriveActiveAt(req.startTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastMana: %w", err)
	}
	pet, isPetFound := peerSession.zone.Companion().Snapshot(
		peerSession.beastPetObjectID,
	)
	isEnrage := isPetFound && pet.HitPoint > 0
	if peerSession.beastPetObjectID != 0 && !isEnrage {
		peerSession.beastPetObjectID = 0
		peerSession.beastPetDamage = 0
	}
	if isEnrage {
		manaCost *= 0.50
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	if isEnrage {
		return r.handleSummonBeastEnrage(
			req, ability, peerSession, definition, manaCost,
		)
	}
	objectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastObjectID: %w", err)
	}
	facing, err := geometryraknet.Forward(req.command.Common.Orientation)
	if err != nil {
		r.registry.mutex.Unlock()
		return req.reject("Summon Beast facing unavailable")
	}
	position := game.Vec3{
		X: peerSession.playerPosition.X,
		Y: peerSession.playerPosition.Y,
		Z: peerSession.playerPosition.Z,
	}.Add(
		game.Vec3(facing).Scale(
			definition.PlacementDistance + beastPetFallbackFootprint,
		),
	)
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp:    req.command.Common.Unknown[0],
		ResponseType: raknet.ActionResponseAccepted,
		ObjectID:     ability.ID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime,
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(definition.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], ability.ID,
		req.command.Ability.Index, req.packet.SourceTime,
		0, definition.ReleaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastRelease: %w", err)
	}
	startPackets, err := abilityraknet.StartSpendPresentation(
		req.command.Common.ObjectID, ability.ID, definition.AnimationName,
		definition.Cooldown, req.packet.SourceTime, remainingManaPoint,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastStart: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(ability.ID),
			req.startTime, definition.Cooldown,
		)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("Summon Beast cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			req.startTime, definition.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("Summon Beast release unavailable")
	}
	previousManaPoint := peerSession.deployedManaPoint()
	err = peerSession.stopPlayerMovement(req.startTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastCommit: %w", err)
	}
	spawnPackets, err := peerSession.spawnBeastPet(
		r.program, objectID, req.command.Common.ObjectID,
		position, supportCreature.PetDamage,
	)
	if err != nil {
		_ = peerSession.setDeployedManaPoints(previousManaPoint)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonBeastSpawn: %w", err)
	}
	if peerSession.sagePassiveActivations == nil {
		peerSession.sagePassiveActivations = make(map[uint32]summonCompanionActivation)
	}
	peerSession.sagePassiveActivations[objectID] = summonCompanionActivation{
		ObjectID: objectID, OwnerObjectID: req.command.Common.ObjectID,
		Position: raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
	}
	generation := peerSession.generation
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	releaseStep := beastSummonReleaseStep{packet: releasePacket}
	err = req.packet.ScheduleFunc(definition.ReleaseDelay, releaseStep.produce)
	if err != nil {
		r.logger.Printf("RakNet Summon Beast release schedule skipped for %s: %v", sessionKey, err)
	}
	packets := [][]byte{ackPacket}
	packets = append(packets, startPackets...)
	packets = append(packets, spawnPackets...)
	spawnReleaseStep := beastPetSpawnReleaseStep{
		runtime: r.damage, packet: req.packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		sourceTime: req.packet.SourceTime + uint64(beastPetSpawnReleaseDelay/time.Millisecond),
	}
	err = req.packet.ScheduleFunc(beastPetSpawnReleaseDelay, spawnReleaseStep.produce)
	if err != nil {
		r.logger.Printf("RakNet Summon Beast spawn release schedule skipped for %s: %v", sessionKey, err)
		fallbackPackets, fallbackErr := spawnReleaseStep.produce()
		if fallbackErr != nil {
			r.logger.Printf("RakNet Summon Beast fallback attack skipped for %s: %v", sessionKey, fallbackErr)
		} else {
			packets = append(packets, fallbackPackets...)
		}
	}
	return packets, nil
}

func (r campaignDamageRuntime) startBeastPetAttack(
	packet raknet.Packet, sessionKey string, generation uint64, sourceTime uint64,
) ([][]byte, error) {
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return nil, errors.New("beast pet schedule unavailable")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.beastPetObjectID != 0 && peerSession.zone != nil &&
		peerSession.zone.Companion() != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	activation, isActivationFound :=
		peerSession.sagePassiveActivations[peerSession.beastPetObjectID]
	if !isActivationFound || activation.Attack != nil {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	ability := r.npc.program.BeastPetBasic
	damageRange, err := game.ResolveAbilityDamageRange(
		game.AbilityDamage{
			Minimum: ability.MinimumDamage, Maximum: ability.MaximumDamage,
			Coefficient: ability.DamageCoefficient,
		},
		game.DamageProfile{
			PrimaryAttribute:        peerSession.beastPetDamage,
			IsPrimaryAttributeFound: true,
		},
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("beastPetDamageRange: %w", err)
	}
	ability.MinimumDamage = damageRange.Minimum
	ability.MaximumDamage = damageRange.Maximum
	if peerSession.beastPetDamageIncrease > 0 {
		ability.MinimumDamage *= 1 + peerSession.beastPetDamageIncrease
		ability.MaximumDamage *= 1 + peerSession.beastPetDamageIncrease
	}
	plan, isPlanFound, err := peerSession.zone.Companion().ReserveActorAttack(
		peerSession.beastPetObjectID,
		peerSession.zone.NPCs().LiveSnapshots(), ability.Range,
		r.npc.now(), ability.HitDelay+ability.Cooldown,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("beastPetReserve: %w", err)
	}
	if !isPlanFound {
		pursuit, isPursuitFound, pursuitErr :=
			peerSession.zone.Companion().ReserveActorPursuit(
				peerSession.beastPetObjectID,
				peerSession.zone.NPCs().LiveSnapshots(), ability.Range,
				zonecompanion.CompatibilityAggroRadius,
				zonecompanion.CompatibilityMovementSpeed,
			)
		r.registry.mutex.Unlock()
		if pursuitErr != nil {
			return nil, fmt.Errorf("beastPetPursuitReserve: %w", pursuitErr)
		}
		if !isPursuitFound {
			return nil, nil
		}
		pursuitPackets, marshalErr := companionraknet.Pursuit(pursuit)
		if marshalErr != nil {
			r.cancelCompanionPursuit(
				sessionKey, generation, pursuit.ObjectID,
				pursuit.TargetObjectID, pursuit.Position,
			)
			return nil, fmt.Errorf("beastPetPursuitMarshal: %w", marshalErr)
		}
		step := campaignCompanionPursuitStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceTime: sourceTime, plan: pursuit,
		}
		marshalErr = step.schedule()
		if marshalErr != nil {
			return nil, fmt.Errorf("beastPetPursuitSchedule: %w", marshalErr)
		}
		return pursuitPackets, nil
	}
	ability = applyCompanionAttackBuff(ability, plan)
	activation.Position = raknet.Vector3{
		X: plan.Position.X, Y: plan.Position.Y, Z: plan.Position.Z,
	}
	start, err := newSummonCompanionAttack(summonCompanionAttackInput{
		Ability: ability, CompanionID: plan.ObjectID,
		TargetID:       plan.TargetObjectID,
		Companion:      sim.Position(plan.Position),
		Target:         sim.Position(plan.TargetPosition),
		TargetHitPoint: plan.TargetHitPoint,
		Damage:         ability.MinimumDamage, SourceTime: sourceTime,
		CenterRange: plan.CenterRange, StartedAt: r.npc.now(),
	})
	if err != nil {
		peerSession.zone.Companion().ReleaseAttack(
			plan.ObjectID, plan.TargetObjectID,
		)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("beastPetStart: %w", err)
	}
	activation.Attack = start.Run
	activation.TargetObjectID = plan.TargetObjectID
	activation.CooldownEnd = plan.CooldownEnd
	peerSession.sagePassiveActivations[plan.ObjectID] = activation
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	step := campaignCompanionAttackStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceTime: sourceTime,
		plan: plan, start: start, ability: ability,
		criticalNounID: util.HashID("BeastSentinelPet"), isBeast: true,
	}
	err = step.schedule()
	if err != nil {
		return nil, fmt.Errorf("beastPetSchedule: %w", err)
	}
	return start.Packet, nil
}
