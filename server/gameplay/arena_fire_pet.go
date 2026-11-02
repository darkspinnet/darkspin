package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

func (r campaignAbilityCommandRuntime) arenaFireTempestSpawnPosition(
	member gameplayPeerSession, orientation raknet.Quaternion,
) (game.Vec3, error) {
	match := r.registry.arenaMatches[member.binding.GameID]
	if match == nil || match.random == nil {
		return game.Vec3{}, errors.New("Arena Fire Tempest match unavailable")
	}
	facing, err := geometryraknet.Forward(orientation)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("arenaFireFacing: %w", err)
	}
	angle := math.Atan2(float64(facing.Y), float64(facing.X))
	angle += (match.random.Float64() - 0.5) * math.Pi
	desired := game.Vec3(member.playerPosition).Add(game.Vec3{
		X: float32(math.Cos(angle)) * 5, Y: float32(math.Sin(angle)) * 5,
	})
	if match.navigation == nil {
		return desired, nil
	}
	position, isProjected, err := zonenavigation.DirectMovementDestination(
		match.navigation, game.Vec3(member.playerPosition), desired, fireTempestFallbackFootprint)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("arenaFireProjection: %w", err)
	}
	if isProjected {
		return position, nil
	}
	return desired, nil
}

func (e fireTempestStep) arenaCurrentLocked() (gameplayPeerSession, *arenaCombatPetRun, bool) {
	member, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	match := e.run.runtime.registry.arenaMatches[e.run.arenaGameID]
	if !e.run.isCurrent(member, isFound) || member.binding.Mode != game.ModeArena ||
		member.binding.GameID != e.run.arenaGameID || !member.isArenaCombatActive ||
		member.deployedObjectID != e.run.ownerObjectID || member.deployedHitPoint() <= 0 ||
		match == nil || !match.isCombatActive || match.abilityWorld != e.run.arenaWorld {
		return gameplayPeerSession{}, nil, false
	}
	return member, e.run.arenaWorld.combatPets[e.run.petObjectID], true
}

func (e fireTempestStep) arenaSpawn() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	member, _, isCurrent := e.arenaCurrentLocked()
	if !isCurrent {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	hitPoint := e.run.runtime.program.NonPlayerHitPoint[util.HashID("RangedElementalPet")]
	if hitPoint <= 0 {
		hitPoint = fireTempestFallbackHitPoint
	}
	hitPoint = member.petHitPoint(hitPoint)
	footprint, footprintErr := e.run.runtime.program.FootprintRadius(e.definition.SpawnNoun)
	if footprintErr != nil || footprint <= 0 {
		footprint = fireTempestFallbackFootprint
	}
	createPackets, err := companionraknet.Create(raknet.ObjectCreateMessage{
		ObjectID: e.run.petObjectID, Noun: util.HashID(e.definition.SpawnNoun),
		PositionX: e.position.X, PositionY: e.position.Y, PositionZ: e.position.Z,
		Scale: 1, Team: uint8(member.binding.Team), OwnerID: e.run.ownerObjectID,
		IsCollisionEnabled: true,
	})
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireCreate: %w", err)
	}
	messages := []raknet.ApplicationMessage{
		raknet.AttributeDataUpdateMessage{ObjectID: e.run.petObjectID,
			Value: map[uint8]float32{11: 0, 12: 0, 48: 0}},
		raknet.SetAnimationStateMessage{ObjectID: e.run.petObjectID,
			State: util.HashID("cast_firetempest_pet_spawn"), Scale: 1},
		raknet.ServerEventMessage{Asset: util.HashID(e.definition.HitEffectName), ObjectID: e.run.petObjectID},
	}
	for index, message := range messages {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFireSpawn[%d]: %w", index, marshalErr)
		}
		createPackets = append(createPackets, packet)
	}
	deletePacket, err := companionraknet.Defeat(e.run.petObjectID)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireDelete: %w", err)
	}
	err = e.run.arenaWorld.companion.Put(zonecompanion.Actor{
		UserID: member.binding.UserID, PeerGeneration: member.generation,
		ObjectID: e.run.petObjectID, OwnerObjectID: e.run.ownerObjectID,
		Noun: util.HashID(e.definition.SpawnNoun), Position: e.position,
		FootprintRadius: footprint, HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		IsTargetable: true, IsCombatant: true,
	})
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFirePut: %w", err)
	}
	pet := &arenaCombatPetRun{
		runtime: e.run.runtime, packet: e.run.packet, world: e.run.arenaWorld,
		sessionKey: e.run.sessionKey, gameID: e.run.arenaGameID,
		generation: e.run.generation, ownerObjectID: e.run.ownerObjectID,
		objectID: e.run.petObjectID, kind: arenaCombatPetFire,
		startedAt: e.run.runtime.now(), attackAbility: e.run.runtime.program.FireTempestPetBasic,
		petDamage:    member.binding.Creatures[member.deployedCreatureIndex].PetDamage + fireTempestPetPrimaryStatOffset,
		deletePacket: deletePacket, isPublished: true,
	}
	if e.run.arenaWorld.combatPets == nil {
		e.run.arenaWorld.combatPets = make(map[uint32]*arenaCombatPetRun)
	}
	e.run.arenaWorld.combatPets[pet.objectID] = pet
	e.run.runtime.registry.mutex.Unlock()
	return createPackets, nil
}

func (e fireTempestStep) arenaSpawnRelease() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	_, pet, isCurrent := e.arenaCurrentLocked()
	if !isCurrent || pet == nil || !pet.isPublished {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.run.runtime.registry.mutex.Unlock()
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.run.petObjectID, Value: map[uint8]float32{
			11: zonecompanion.CompatibilityMovementSpeed,
			12: zonecompanion.CompatibilityMovementSpeed, 48: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("arenaFireReleaseAttribute: %w", err)
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.run.petObjectID, State: util.HashID("relaxed"), Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaFireReleaseAnimation: %w", err)
	}
	attackPackets, err := e.run.runtime.damage.startArenaFireTempestPetAttack(
		e.run.packet, e.run.sessionKey, e.run.generation,
		e.timestamp+uint64(e.deadline/time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("arenaFireReleaseAttack: %w", err)
	}
	return append([][]byte{attributePacket, animationPacket}, attackPackets...), nil
}

func (e fireTempestStep) arenaExpire() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	member, _, isCurrent := e.arenaCurrentLocked()
	if !isCurrent {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	member.fireTempestActive = nil
	packets, err := member.stopArenaFireTempestRun(e.run)
	e.run.runtime.registry.sessions[e.run.sessionKey] = member
	e.run.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("arenaFireExpire: %w", err)
	}
	return packets, nil
}

func (e *gameplayPeerSession) stopArenaFireTempestRun(run *fireTempestActiveRun) ([][]byte, error) {
	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
	if run.enrageCancel != nil {
		run.enrageCancel()
		run.enrageCancel = nil
	}
	if run.monitorCancel != nil {
		run.monitorCancel()
		run.monitorCancel = nil
	}
	if run.replacementCancel != nil {
		run.replacementCancel()
		run.replacementCancel = nil
	}
	if run.attack != nil {
		run.attack.Stop()
		run.attack = nil
	}
	packets := make([][]byte, 0, 4)
	if run.enrageInstanceID != 0 {
		packet, err := effectraknet.ModifierDelete(run.petObjectID, run.enrageInstanceID)
		if err != nil {
			return nil, fmt.Errorf("arenaFireStopEnrage: %w", err)
		}
		packets = append(packets, packet)
		modErr := run.runtime.modifierPool.Release(run.enrageInstanceID)
		if modErr != nil && run.runtime.logger != nil {
			run.runtime.logger.Printf("Arena Fire Tempest enrage modifier release: %v", modErr)
		}
		run.enrageInstanceID = 0
	}
	ownerPacket, err := effectraknet.ModifierDelete(run.ownerObjectID, run.modifierInstanceID)
	if err != nil {
		return nil, fmt.Errorf("arenaFireStopOwner: %w", err)
	}
	modErr := run.runtime.modifierPool.Release(run.modifierInstanceID)
	if modErr != nil && run.runtime.logger != nil {
		run.runtime.logger.Printf("Arena Fire Tempest owner modifier release: %v", modErr)
	}
	packets = append(packets, ownerPacket)
	pet := run.arenaWorld.combatPets[run.petObjectID]
	if pet == nil {
		return packets, nil
	}
	pet.isPublished = false
	pet.retireLocked()
	beamPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: pet.objectID, State: util.HashID("firetempest_pet_beamout"), Scale: 1})
	if err != nil {
		return nil, fmt.Errorf("arenaFireStopBeam: %w", err)
	}
	packets = append(packets, beamPacket)
	producer := raknet.ScheduledPacketProducer{Delay: fireTempestBeamOutDuration,
		Produce: fireTempestStaticProducer{packet: pet.deletePacket}.produce}
	identity := gameplayProducerIdentityFromSession(run.sessionKey, *e, true)
	producers := run.runtime.registry.producerGuard.scheduledProducersForIdentity(
		identity, []raknet.ScheduledPacketProducer{producer})
	_, err = run.packet.Autonomous().ScheduleProducers(producers)
	if err != nil {
		packets = append(packets, pet.deletePacket)
	}
	return packets, nil
}

func (e fireTempestStep) arenaEnrageStart() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	_, pet, isCurrent := e.arenaCurrentLocked()
	if !isCurrent || pet == nil || e.run.enrageInstanceID == 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	actor, isFound := e.run.arenaWorld.companion.Snapshot(pet.objectID)
	e.run.runtime.registry.mutex.Unlock()
	if !isFound || actor.HitPoint <= 0 {
		return nil, nil
	}
	modifierPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.run.ownerObjectID, TargetObjectID: pet.objectID,
		ModifierID: e.definition.SecondaryModifierID, InstanceID: e.run.enrageInstanceID,
		Duration: fireTempestEnrageDuration, Timestamp: e.timestamp + uint64(e.deadline/time.Millisecond),
	})
	if err != nil {
		return nil, fmt.Errorf("arenaFireEnrageModifier: %w", err)
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: pet.objectID, State: util.HashID("firetempest_pet_enraged"), Scale: 1})
	if err != nil {
		return nil, fmt.Errorf("arenaFireEnrageAnimation: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(e.definition.ActivationEffectName), ObjectID: pet.objectID,
		Position: raknet.Vector3(actor.Position),
	})
	if err != nil {
		return nil, fmt.Errorf("arenaFireEnrageEffect: %w", err)
	}
	return [][]byte{modifierPacket, animationPacket, effectPacket}, nil
}

func (e fireTempestStep) arenaEnragePulse() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	member, pet, isCurrent := e.arenaCurrentLocked()
	if !isCurrent || pet == nil || e.run.enrageInstanceID == 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	actor, isFound := e.run.arenaWorld.companion.Snapshot(pet.objectID)
	if !isFound || actor.HitPoint <= 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	var attributePacket []byte
	if !e.run.isEnrageBuffApplied {
		var err error
		attributePacket, err = raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: pet.objectID, Value: map[uint8]float32{113: fireTempestEnrageBodyScale}})
		if err != nil {
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFireEnrageScale: %w", err)
		}
		err = e.run.arenaWorld.companion.AddBuff(pet.objectID,
			zonecompanion.Buff{DamageBuff: fireTempestEnrageDamageBuff})
		if err != nil {
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFireEnrageBuff: %w", err)
		}
		err = e.run.arenaWorld.companion.SetBodyScale(pet.objectID, fireTempestEnrageBodyScale)
		if err != nil {
			e.run.arenaWorld.companion.RemoveBuff(pet.objectID,
				zonecompanion.Buff{DamageBuff: fireTempestEnrageDamageBuff})
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFireEnrageBody: %w", err)
		}
		e.run.isEnrageBuffApplied = true
	}
	creature := e.creature
	creature.DamageProfile.PrimaryAttribute = creature.PetDamage
	creature.DamageProfile.IsPrimaryAttributeFound = true
	creature.DamageProfile.DamageBuff += fireTempestEnrageDamageBuff
	definition := e.definition
	definition.Kind = sim.AbilityKindAuraArea
	timestamp := e.timestamp + uint64(e.deadline/time.Millisecond)
	targets := e.run.runtime.registry.abilityTargetsLocked(e.run.runtime, member, definition, timestamp).(*arenaAbilityTargets)
	plan, err := zoneability.PlanFixedArea(targets, pet.objectID, actor.Position, creature, definition)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireEnragePlan: %w", err)
	}
	random, err := e.run.runtime.registry.abilityRandomLocked(member)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireEnrageRandom: %w", err)
	}
	results, err := zoneability.CommitArea(random, targets, plan, creature,
		member.binding.Difficulty, e.run.runtime.program.Critical)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaFireEnrageHit: %w", err)
	}
	packets, err := targets.packets(results)
	e.run.runtime.registry.mutex.Unlock()
	targets.finish()
	if err != nil {
		return nil, fmt.Errorf("arenaFireEnragePackets: %w", err)
	}
	for _, result := range results {
		impactPacket, impactErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(e.definition.ImpactEffectName), ObjectID: result.Snapshot.Plan.ObjectID})
		if impactErr != nil {
			return nil, fmt.Errorf("arenaFireEnrageImpact: %w", impactErr)
		}
		packets = append(packets, impactPacket)
	}
	if len(attributePacket) != 0 {
		packets = append([][]byte{attributePacket}, packets...)
	}
	return packets, nil
}

func (e fireTempestStep) arenaEnrageExpire() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	_, pet, isCurrent := e.arenaCurrentLocked()
	if !isCurrent || e.run.enrageInstanceID == 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	instanceID := e.run.enrageInstanceID
	e.run.enrageInstanceID = 0
	e.run.enrageCancel = nil
	isApplied := e.run.isEnrageBuffApplied
	e.run.isEnrageBuffApplied = false
	if isApplied && pet != nil {
		e.run.arenaWorld.companion.ClearBodyScale(pet.objectID)
		e.run.arenaWorld.companion.RemoveBuff(pet.objectID,
			zonecompanion.Buff{DamageBuff: fireTempestEnrageDamageBuff})
	}
	e.run.runtime.registry.mutex.Unlock()
	modErr := e.run.runtime.modifierPool.Release(instanceID)
	if modErr != nil && e.run.runtime.logger != nil {
		e.run.runtime.logger.Printf("Arena Fire Tempest enrage release: %v", modErr)
	}
	modifierPacket, err := effectraknet.ModifierDelete(e.run.petObjectID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("arenaFireEnrageDelete: %w", err)
	}
	if !isApplied {
		return [][]byte{modifierPacket}, nil
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.run.petObjectID, Value: map[uint8]float32{113: 0}})
	if err != nil {
		return nil, fmt.Errorf("arenaFireEnrageReset: %w", err)
	}
	return [][]byte{modifierPacket, attributePacket}, nil
}

func (e fireTempestStep) arenaMonitor() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	member, pet, isCurrent := e.arenaCurrentLocked()
	if !isCurrent || pet == nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.run.monitorCancel = nil
	actor, isFound := e.run.arenaWorld.companion.Snapshot(pet.objectID)
	if !isFound || actor.HitPoint <= 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	ownerPosition := game.Vec3(member.playerPosition)
	if actor.Position.Sub(ownerPosition).Length() <= 50 {
		isResumeNeeded := e.run.isAttackResumeNeeded
		e.run.runtime.registry.mutex.Unlock()
		err := e.scheduleMonitor()
		if err != nil {
			return nil, fmt.Errorf("arenaFireMonitor: %w", err)
		}
		if isResumeNeeded {
			return e.run.runtime.damage.startArenaFireTempestPetAttack(e.run.packet,
				e.run.sessionKey, e.run.generation,
				e.timestamp+uint64(e.deadline/time.Millisecond))
		}
		return nil, nil
	}
	oldObjectID := pet.objectID
	if e.run.attack != nil {
		e.run.attack.Stop()
		e.run.attack = nil
		e.run.attackTargetObjectID = 0
	}
	newObjectID, err := e.run.arenaWorld.objectIDSession.Reserve(1)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireReplacementID: %w", err)
	}
	position, err := e.run.runtime.arenaFireTempestSpawnPosition(member, e.run.orientation)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireReplacementPosition: %w", err)
	}
	enrageInstanceID := e.run.enrageInstanceID
	if e.run.enrageCancel != nil {
		e.run.enrageCancel()
		e.run.enrageCancel = nil
	}
	e.run.enrageInstanceID = 0
	e.run.isEnrageBuffApplied = false
	e.run.stackCount = 1
	e.run.petObjectID = newObjectID
	pet.isPublished = false
	pet.retireLocked()
	e.run.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, 3)
	if enrageInstanceID != 0 {
		modErr := e.run.runtime.modifierPool.Release(enrageInstanceID)
		if modErr != nil && e.run.runtime.logger != nil {
			e.run.runtime.logger.Printf("Arena Fire replacement modifier release: %v", modErr)
		}
		modifierPacket, marshalErr := effectraknet.ModifierDelete(oldObjectID, enrageInstanceID)
		if marshalErr != nil {
			return nil, fmt.Errorf("arenaFireReplacementModifier: %w", marshalErr)
		}
		packets = append(packets, modifierPacket)
	}
	packets = append(packets, pet.deletePacket)
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(e.definition.MuzzleEffectName), ObjectID: e.run.ownerObjectID,
		Position: raknet.Vector3(ownerPosition)})
	if err != nil {
		return nil, fmt.Errorf("arenaFireReplacementEffect: %w", err)
	}
	packets = append(packets, effectPacket)
	err = e.scheduleReplacement(position)
	if err != nil {
		return nil, fmt.Errorf("arenaFireReplacementSchedule: %w", err)
	}
	return packets, nil
}
