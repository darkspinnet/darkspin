package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const plasmaSentinelPetDuration = 30 * time.Second
const plasmaSentinelMaximumPetCount = 2
const plasmaSentinelFallbackHitPoint = float32(50)
const plasmaSentinelFallbackFootprint = float32(0.75)

type plasmaSentinelPetRun struct {
	objectID           uint32
	modifierInstanceID uint32
	cancel             raknet.CancelSchedule
}

type plasmaSentinelActiveRun struct {
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	ownerObjectID  uint32
	orientation    raknet.Quaternion
	pets           [plasmaSentinelMaximumPetCount]plasmaSentinelPetRun
	effectSlot     uint8
	triggerCount   uint32
	isShieldActive bool
	cancel         raknet.CancelSchedule
}

type plasmaSentinelPetExpiryStep struct {
	run      *plasmaSentinelActiveRun
	objectID uint32
}

func (e plasmaSentinelPetExpiryStep) produce() ([][]byte, error) {
	return e.run.expirePet(e.objectID)
}

func (e *plasmaSentinelActiveRun) hasPets() bool {
	for _, pet := range e.pets {
		if pet.objectID != 0 {
			return true
		}
	}
	return false
}

func (e *plasmaSentinelActiveRun) hasPet(objectID uint32) bool {
	_, isFound := e.petIndex(objectID)
	return isFound
}

func (e *plasmaSentinelActiveRun) petIndex(objectID uint32) (int, bool) {
	if objectID == 0 {
		return 0, false
	}
	for index, pet := range e.pets {
		if pet.objectID == objectID {
			return index, true
		}
	}
	return 0, false
}

func (e *plasmaSentinelActiveRun) petObjectIDs() []uint32 {
	objectIDs := make([]uint32, 0, plasmaSentinelMaximumPetCount)
	for _, pet := range e.pets {
		if pet.objectID != 0 {
			objectIDs = append(objectIDs, pet.objectID)
		}
	}
	return objectIDs
}

func (e *plasmaSentinelActiveRun) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return e != nil && isFound && peerSession.generation == e.generation &&
		peerSession.plasmaSentinelActive == e
}

func (e *plasmaSentinelActiveRun) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if !e.isShieldActive {
		e.cancel = nil
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.isShieldActive = false
	if !e.hasPets() {
		peerSession.plasmaSentinelActive = nil
	}
	e.cancel = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	e.runtime.effectPool.Release(e.ownerObjectID, e.effectSlot)
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.effectSlot + 1, IsRemovalRequested: true, IsHardStop: true,
		ObjectID: e.ownerObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelActiveEffectDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *plasmaSentinelActiveRun) expirePet(objectID uint32) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	petIndex, isPetFound := e.petIndex(objectID)
	if !isFound || peerSession.generation != e.generation || !isPetFound ||
		peerSession.zone == nil ||
		peerSession.zone.Companion() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	pet := e.pets[petIndex]
	e.pets[petIndex] = plasmaSentinelPetRun{}
	if peerSession.plasmaSentinelActive == e && !e.isShieldActive && !e.hasPets() {
		peerSession.plasmaSentinelActive = nil
	}
	peerSession.zone.Companion().Remove(objectID)
	delete(peerSession.sagePassiveActivations, objectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	_ = e.runtime.modifierPool.Release(pet.modifierInstanceID)
	modifierPacket, err := effectraknet.ModifierDelete(
		e.ownerObjectID, pet.modifierInstanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelPetModifierDelete: %w", err)
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{objectID},
	})
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelPetDelete: %w", err)
	}
	return [][]byte{modifierPacket, packet}, nil
}

func (e *gameplayPeerSession) stopPlasmaSentinelActive() ([][]byte, error) {
	if e == nil || e.plasmaSentinelActive == nil {
		return nil, nil
	}
	run := e.plasmaSentinelActive
	e.plasmaSentinelActive = nil
	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
	packets := make([][]byte, 0, 5)
	if run.isShieldActive {
		run.isShieldActive = false
		run.runtime.effectPool.Release(run.ownerObjectID, run.effectSlot)
		effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: run.effectSlot + 1, IsRemovalRequested: true, IsHardStop: true,
			ObjectID: run.ownerObjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("plasmaSentinelStopEffect: %w", err)
		}
		packets = append(packets, effectPacket)
	}
	for index, pet := range run.pets {
		if pet.objectID == 0 {
			continue
		}
		if pet.cancel != nil {
			pet.cancel()
		}
		if e.zone != nil && e.zone.Companion() != nil {
			e.zone.Companion().Remove(pet.objectID)
		}
		delete(e.sagePassiveActivations, pet.objectID)
		_ = run.runtime.modifierPool.Release(pet.modifierInstanceID)
		modifierPacket, err := effectraknet.ModifierDelete(
			run.ownerObjectID, pet.modifierInstanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("plasmaSentinelStopModifier[%d]: %w", index, err)
		}
		deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
			ObjectID: []uint32{pet.objectID},
		})
		if err != nil {
			return nil, fmt.Errorf("plasmaSentinelStopPet[%d]: %w", index, err)
		}
		packets = append(packets, modifierPacket, deletePacket)
		run.pets[index] = plasmaSentinelPetRun{}
	}
	return packets, nil
}

func (r campaignAbilityCommandRuntime) handlePlasmaSentinelActive(
	req campaignCharacterAbilityRequest,
	peerSession gameplayPeerSession,
	creature game.GameplayCreature,
	ability zonecontent.HeroAbility,
	sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	_ = creature
	_ = abilityStartTime
	definition := ability.Definition
	if ability.ID == 0 || definition.Name != "PlasmaSentinelActive" ||
		definition.Kind != sim.AbilityKindReactiveSummon || definition.Duration <= 0 {
		r.registry.mutex.Unlock()
		return req.reject("plasma sentinel definition unavailable")
	}
	if peerSession.plasmaSentinelActive != nil {
		r.registry.mutex.Unlock()
		return req.reject("plasma sentinel active already running")
	}
	effectSlot, isEffectAllocated := r.effectPool.Allocate(req.command.Common.ObjectID)
	if !isEffectAllocated {
		r.registry.mutex.Unlock()
		return req.reject("plasma sentinel effect unavailable")
	}
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: ability.ID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime,
		SourceEndMilliseconds:    req.packet.SourceTime,
	})
	if err != nil {
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaSentinelActiveAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], ability.ID, req.command.Ability.Index,
		req.packet.SourceTime, 0, 0,
	)
	if err != nil {
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaSentinelActiveRelease: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: effectSlot + 1, IsForceAttached: true,
		Asset:    util.HashID(definition.ActivationEffectName),
		ObjectID: req.command.Common.ObjectID,
	})
	if err != nil {
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaSentinelActiveEffect: %w", err)
	}
	run := &plasmaSentinelActiveRun{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: peerSession.generation, ownerObjectID: req.command.Common.ObjectID,
		orientation: req.command.Common.Orientation, effectSlot: effectSlot,
		isShieldActive: true,
	}
	peerSession.plasmaSentinelActive = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	cancel, err := req.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: definition.Duration, Produce: run.expire,
	}})
	if err != nil {
		r.effectPool.Release(req.command.Common.ObjectID, effectSlot)
		r.registry.mutex.Lock()
		latest, isFound := r.registry.sessions[sessionKey]
		if run.isCurrent(latest, isFound) {
			latest.plasmaSentinelActive = nil
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaSentinelActiveSchedule: %w", err)
	}
	run.cancel = cancel
	return [][]byte{ackPacket, effectPacket, releasePacket}, nil
}

func (r campaignNPCActionRuntime) triggerPlasmaSentinelActive(
	peerSession *gameplayPeerSession, timestamp uint64,
) ([][]byte, error) {
	if peerSession == nil || peerSession.plasmaSentinelActive == nil ||
		peerSession.deployedObjectID == 0 || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil {
		return nil, nil
	}
	run := peerSession.plasmaSentinelActive
	if !run.isShieldActive || run.triggerCount >= plasmaSentinelMaximumPetCount {
		return nil, nil
	}
	petIndex := int(run.triggerCount)
	rockPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset:    util.HashID("plasmasentinel_got_hit_rocks.ServerEventDef"),
		ObjectID: run.ownerObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelRockEffect: %w", err)
	}
	var removePacket []byte
	if petIndex == plasmaSentinelMaximumPetCount-1 {
		removePacket, err = raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: run.effectSlot + 1, IsRemovalRequested: true, IsHardStop: true,
			ObjectID: run.ownerObjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("plasmaSentinelShieldRemove: %w", err)
		}
	}
	petPackets, err := r.spawnPlasmaSentinelPet(
		peerSession, run, timestamp, petIndex,
	)
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelPetSpawn[%d]: %w", petIndex, err)
	}
	run.triggerCount++
	packets := append([][]byte{rockPacket}, petPackets...)
	if run.triggerCount < plasmaSentinelMaximumPetCount {
		return packets, nil
	}
	run.isShieldActive = false
	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
	run.runtime.effectPool.Release(run.ownerObjectID, run.effectSlot)
	return append(packets, removePacket), nil
}

func (r campaignNPCActionRuntime) spawnPlasmaSentinelPet(
	peerSession *gameplayPeerSession,
	run *plasmaSentinelActiveRun,
	timestamp uint64,
	petIndex int,
) ([][]byte, error) {
	if petIndex < 0 || petIndex >= len(run.pets) || run.pets[petIndex].objectID != 0 {
		return nil, fmt.Errorf("petIndex: %d", petIndex)
	}
	objectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelPetObjectID: %w", err)
	}
	instanceID, err := r.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelPetModifier: %w", err)
	}
	hitPoint := r.program.NonPlayerHitPoint[util.HashID("PlasmaSentinelPet")]
	if hitPoint <= 0 {
		hitPoint = plasmaSentinelFallbackHitPoint
	}
	hitPoint = peerSession.petHitPoint(hitPoint)
	footprint, footprintErr := r.program.FootprintRadius("PlasmaSentinelPet.Noun")
	if footprintErr != nil || footprint <= 0 {
		footprint = plasmaSentinelFallbackFootprint
	}
	position, err := plasmaSentinelSpawnPosition(
		*peerSession, run.orientation, petIndex, footprint,
	)
	if err != nil {
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("plasmaSentinelPetPosition: %w", err)
	}
	err = peerSession.zone.Companion().Put(zonecompanion.Actor{
		UserID: peerSession.binding.UserID, PeerGeneration: peerSession.generation,
		ObjectID: objectID, OwnerObjectID: run.ownerObjectID,
		Noun:     util.HashID("PlasmaSentinelPet.Noun"),
		Position: position, FootprintRadius: footprint,
		HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		IsTargetable: true, IsCombatant: true,
	})
	if err != nil {
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("plasmaSentinelPetPut: %w", err)
	}
	run.pets[petIndex] = plasmaSentinelPetRun{
		objectID: objectID, modifierInstanceID: instanceID,
	}
	if peerSession.sagePassiveActivations == nil {
		peerSession.sagePassiveActivations = make(map[uint32]summonCompanionActivation)
	}
	peerSession.sagePassiveActivations[objectID] = summonCompanionActivation{
		Position: raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
	}
	modifierPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: run.ownerObjectID, TargetObjectID: run.ownerObjectID,
		ModifierID: 0x95dcd059, InstanceID: instanceID,
		Duration: plasmaSentinelPetDuration, Timestamp: timestamp,
	})
	if err != nil {
		rollbackPlasmaSentinelPet(peerSession, run, objectID)
		return nil, fmt.Errorf("plasmaSentinelPetModifierCreate: %w", err)
	}
	createPackets, err := companionraknet.Create(raknet.ObjectCreateMessage{
		ObjectID: objectID, Noun: util.HashID("PlasmaSentinelPet.Noun"),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: 1, OwnerID: run.ownerObjectID, IsCollisionEnabled: true,
	})
	if err != nil {
		rollbackPlasmaSentinelPet(peerSession, run, objectID)
		return nil, fmt.Errorf("plasmaSentinelPetCreate: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID,
		Value: map[uint8]float32{
			11: zonecompanion.CompatibilityMovementSpeed,
			12: zonecompanion.CompatibilityMovementSpeed,
			48: 0,
		},
	})
	if err != nil {
		rollbackPlasmaSentinelPet(peerSession, run, objectID)
		return nil, fmt.Errorf("plasmaSentinelPetAttribute: %w", err)
	}
	spawnPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID("PlasmaSentinelPetSpawn"), ObjectID: objectID,
		Position: raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
	})
	if err != nil {
		rollbackPlasmaSentinelPet(peerSession, run, objectID)
		return nil, fmt.Errorf("plasmaSentinelPetSpawn: %w", err)
	}
	expiry := plasmaSentinelPetExpiryStep{run: run, objectID: objectID}
	cancel, err := run.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: plasmaSentinelPetDuration, Produce: expiry.produce,
	}})
	if err != nil {
		rollbackPlasmaSentinelPet(peerSession, run, objectID)
		return nil, fmt.Errorf("plasmaSentinelPetSchedule: %w", err)
	}
	run.pets[petIndex].cancel = cancel
	packets := append([][]byte{modifierPacket}, createPackets...)
	packets = append(packets, attributePacket, spawnPacket)
	return packets, nil
}

func rollbackPlasmaSentinelPet(
	peerSession *gameplayPeerSession, run *plasmaSentinelActiveRun, objectID uint32,
) {
	if peerSession == nil || run == nil || objectID == 0 {
		return
	}
	petIndex, isFound := run.petIndex(objectID)
	if !isFound {
		return
	}
	pet := run.pets[petIndex]
	if peerSession.zone != nil && peerSession.zone.Companion() != nil {
		peerSession.zone.Companion().Remove(objectID)
	}
	delete(peerSession.sagePassiveActivations, objectID)
	_ = run.runtime.modifierPool.Release(pet.modifierInstanceID)
	run.pets[petIndex] = plasmaSentinelPetRun{}
}

func plasmaSentinelSpawnPosition(
	peerSession gameplayPeerSession,
	orientation raknet.Quaternion,
	petIndex int,
	footprint float32,
) (game.Vec3, error) {
	facing, err := geometryraknet.Forward(orientation)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("plasmaSentinelFacing: %w", err)
	}
	angle := math.Atan2(float64(facing.Y), float64(facing.X)) - math.Pi/3
	if petIndex%2 == 1 {
		angle += 2 * math.Pi / 3
	}
	desired := game.Vec3(peerSession.playerPosition).Add(game.Vec3{
		X: float32(math.Cos(angle)) * 5,
		Y: float32(math.Sin(angle)) * 5,
	})
	position, isProjected, err := zonenavigation.DirectMovementDestination(
		peerSession.zone.Navigation(), game.Vec3(peerSession.playerPosition),
		desired, footprint,
	)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("plasmaSentinelProjection: %w", err)
	}
	if isProjected {
		return position, nil
	}
	return desired, nil
}

func (r campaignDamageRuntime) startPlasmaSentinelPetAttack(
	packet raknet.Packet, sessionKey string, generation uint64, sourceTime uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.plasmaSentinelActive != nil &&
		peerSession.zone != nil && peerSession.zone.Companion() != nil &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	objectIDs := peerSession.plasmaSentinelActive.petObjectIDs()
	r.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		petPackets, err := r.startPlasmaSentinelPetActorAttack(
			packet, sessionKey, generation, sourceTime, objectID,
		)
		if err != nil {
			return nil, fmt.Errorf("plasmaSentinelPetAttack[%d]: %w", objectID, err)
		}
		packets = append(packets, petPackets...)
	}
	return packets, nil
}

func (r campaignDamageRuntime) startPlasmaSentinelPetActorAttack(
	packet raknet.Packet,
	sessionKey string,
	generation uint64,
	sourceTime uint64,
	objectID uint32,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.plasmaSentinelActive != nil &&
		peerSession.plasmaSentinelActive.hasPet(objectID) &&
		peerSession.zone != nil && peerSession.zone.Companion() != nil &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	activation, isActivationFound := peerSession.sagePassiveActivations[objectID]
	if !isActivationFound || activation.Attack != nil {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	ability := r.npc.program.PlasmaSentinelPetBasic
	petDamage := float32(0)
	if peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) {
		petDamage = peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PetDamage
	}
	damageRange, err := game.ResolveAbilityDamageRange(
		game.AbilityDamage{
			Minimum: ability.MinimumDamage, Maximum: ability.MaximumDamage,
			Coefficient: ability.DamageCoefficient,
		},
		game.DamageProfile{PrimaryAttribute: petDamage, IsPrimaryAttributeFound: true},
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaSentinelPetDamage: %w", err)
	}
	ability.MinimumDamage = damageRange.Minimum
	ability.MaximumDamage = damageRange.Maximum
	plan, isPlanFound, err := peerSession.zone.Companion().ReserveActorAttack(
		objectID, peerSession.zone.NPCs().LiveSnapshots(), ability.Range,
		r.npc.now(), ability.HitDelay+ability.Cooldown,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaSentinelPetReserve: %w", err)
	}
	if !isPlanFound {
		pursuit, isPursuitFound, pursuitErr :=
			peerSession.zone.Companion().ReserveActorPursuit(
				objectID, peerSession.zone.NPCs().LiveSnapshots(), ability.Range,
				zonecompanion.CompatibilityAggroRadius,
				zonecompanion.CompatibilityMovementSpeed,
			)
		r.registry.mutex.Unlock()
		if pursuitErr != nil {
			return nil, fmt.Errorf("plasmaSentinelPetPursuit: %w", pursuitErr)
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
			return nil, fmt.Errorf("plasmaSentinelPetPursuitMarshal: %w", marshalErr)
		}
		step := campaignCompanionPursuitStep{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceTime: sourceTime, plan: pursuit,
		}
		marshalErr = step.schedule()
		if marshalErr != nil {
			return nil, fmt.Errorf("plasmaSentinelPetPursuitSchedule: %w", marshalErr)
		}
		return pursuitPackets, nil
	}
	ability = applyCompanionAttackBuff(ability, plan)
	activation.Position = raknet.Vector3{
		X: plan.Position.X, Y: plan.Position.Y, Z: plan.Position.Z,
	}
	start, err := newSummonCompanionAttack(summonCompanionAttackInput{
		Ability: ability, CompanionID: plan.ObjectID, TargetID: plan.TargetObjectID,
		Companion: sim.Position(plan.Position), Target: sim.Position(plan.TargetPosition),
		TargetHitPoint: plan.TargetHitPoint, Damage: ability.MinimumDamage,
		SourceTime: sourceTime, CenterRange: plan.CenterRange, StartedAt: r.npc.now(),
	})
	if err != nil {
		peerSession.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("plasmaSentinelPetStart: %w", err)
	}
	activation.Attack = start.Run
	activation.TargetObjectID = plan.TargetObjectID
	activation.CooldownEnd = plan.CooldownEnd
	peerSession.sagePassiveActivations[plan.ObjectID] = activation
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	step := campaignCompanionAttackStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceTime: sourceTime, plan: plan,
		start: start, ability: ability,
		criticalNounID:   util.HashID("PlasmaSentinelPet"),
		isPlasmaSentinel: true,
	}
	err = step.schedule()
	if err != nil {
		return nil, fmt.Errorf("plasmaSentinelPetSchedule: %w", err)
	}
	return start.Packet, nil
}
