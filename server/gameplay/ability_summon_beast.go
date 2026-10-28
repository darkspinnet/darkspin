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
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	arenaWorld     *arenaAbilityWorld
	sessionKey     string
	generation     uint64
	objectID       uint32
	instanceID     uint32
	timestamp      uint64
	healingProfile game.HealingProfile
	criticalRating float32
	autoCrit       float32
	difficulty     uint32
	cancel         raknet.CancelSchedule
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
	if isFound && peerSession.binding.Mode == game.ModeArena {
		run := peerSession.beastPetResurrection
		isCurrent := peerSession.generation == e.generation && peerSession.beastPetObjectID == e.objectID &&
			run != nil && run.arenaWorld != nil && run.arenaWorld.combatPets[e.objectID] != nil
		e.runtime.registry.mutex.RUnlock()
		if !isCurrent {
			return nil, nil
		}
		speedPacket, err := heroChargeSpeedPacket(e.objectID, zonecompanion.CompatibilityMovementSpeed)
		if err != nil {
			return nil, fmt.Errorf("arenaBeastSpawnSpeed: %w", err)
		}
		pet := run.arenaWorld.combatPets[e.objectID]
		err = pet.scheduleMeleePoll(time.Millisecond)
		if err != nil {
			return nil, fmt.Errorf("arenaBeastSpawnAttack: %w", err)
		}
		return [][]byte{speedPacket}, nil
	}
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
	if e.arenaWorld != nil {
		return e.activateArena()
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	// RequestModifier captures its hero initiator at the authored hit time.
	// Every Rage tick uses that snapshot, including the healing-over-time bonus.
	creature := e.runtime.registry.projectPassiveCreature(
		peerSession, peerSession.deployedCreatureIndex, e.runtime.now(),
	)
	e.healingProfile = creature.HealingProfile
	e.criticalRating = creature.CriticalRating
	e.autoCrit = creature.AutoCrit
	e.difficulty = peerSession.binding.Difficulty
	peerSession.beastPetDamageIncrease = beastPetEnrageDamageIncrease
	healPackets, err := e.healLocked(&peerSession)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("beastPetEnrageInitialHeal: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	// The HP commit is accepted independently of later taunt/presentation work.
	// Publish through the original world's retained queues before unlocking.
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone != peerSession.zone || member.isZoneTerminal() {
			continue
		}
		member.queueCampaignPackets(healPackets)
		e.runtime.registry.sessions[sessionKey] = member
	}
	for _, retained := range e.runtime.registry.retainedSessions {
		if retained.peerSession.zone == peerSession.zone && !retained.peerSession.isZoneTerminal() {
			retained.peerSession.queueCampaignPackets(healPackets)
		}
	}
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
	e.runtime.registry.mutex.Lock()
	latest, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(latest, isFound) || latest.zone != peerSession.zone {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err = latest.zone.Companion().SetBodyScale(e.objectID, beastPetEnrageBodyScale)
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageScaleCommit: %w", err)
	}
	packets := [][]byte{modifierPacket, effectPacket, attributePacket}
	return append(packets, actionPackets...), nil
}

func (e *beastPetEnrageRun) heal() ([][]byte, error) {
	if e.arenaWorld != nil {
		return e.healArena()
	}
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
	multiplier := e.runtime.program.Critical.DamageBonus
	if multiplier <= 0 || math.IsNaN(float64(multiplier)) || math.IsInf(float64(multiplier), 0) {
		return nil, errors.New("invalid rage healing critical multiplier")
	}
	amount := companion.MaximumHitPoint * beastPetEnrageHealingPercent
	healingRange := sim.DamageRange{
		Minimum: amount, Maximum: amount,
	}
	random := peerSession.zone.Population().Random()
	if random == nil {
		return nil, errors.New("rage healing random unavailable")
	}
	// Native 9BCEF0 draws even for equal, potentially fractional endpoints.
	rangeDraw := float32(random.Float64())
	amount = healingRange.Minimum + (healingRange.Maximum-healingRange.Minimum)*rangeDraw
	healing, err := game.ResolveAbilityHealing(game.AbilityHealing{
		Amount:     amount,
		Descriptor: 4096, IsDescriptorFound: true,
	}, e.healingProfile)
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageHealing: %w", err)
	}
	isCritical, err := sim.RollCritical(peerSession.zone.Population().Random(),
		e.criticalRating, e.autoCrit, e.difficulty, e.runtime.program.Critical.RatingConversions,
	)
	if err != nil {
		return nil, fmt.Errorf("rageHealCritical: %w", err)
	}
	flags := uint16(0x0002)
	if isCritical {
		healing *= multiplier
		flags |= 0x0008
	}
	if math.IsNaN(float64(healing)) || math.IsInf(float64(healing), 0) {
		return nil, errors.New("invalid critical rage healing")
	}
	hitPoint := min(companion.MaximumHitPoint, companion.HitPoint+healing)
	if hitPoint <= companion.HitPoint {
		return nil, nil
	}
	integerChange := int32(hitPoint) - int32(companion.HitPoint)
	messages := []raknet.ApplicationMessage{
		raknet.DamageCombatEventMessage{
			Flags: flags, DeltaHealth: -healing,
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
	isRestored, err := peerSession.zone.Companion().RestoreHitPoint(companion, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("beastPetEnrageResource: %w", err)
	}
	if !isRestored {
		return nil, nil
	}
	return packets, nil
}

func (e *beastPetEnrageRun) expire() ([][]byte, error) {
	if e.arenaWorld != nil {
		return e.expireArena()
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.beastPetDamageIncrease = 0
	peerSession.beastPetEnrage = nil
	if peerSession.zone != nil && peerSession.zone.Companion() != nil {
		peerSession.zone.Companion().ClearBodyScale(e.objectID)
	}
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
	if e.beastPetResurrection != nil && e.beastPetResurrection.arenaWorld != nil {
		return e.stopArenaBeastPet()
	}
	objectID := e.beastPetObjectID
	var resurrectionPackets [][]byte
	if e.beastPetResurrection != nil {
		run := e.beastPetResurrection
		e.beastPetResurrection = nil
		var err error
		resurrectionPackets, err = run.stop(e)
		if err != nil {
			return nil, fmt.Errorf("beastResurrectionStop: %w", err)
		}
	}
	e.cancelPetPlasmaBurn(objectID)
	e.beastPetObjectID = 0
	e.beastPetDamage = 0
	e.beastPetDamageIncrease = 0
	packet := resurrectionPackets
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
	if peerSession.binding.Mode == game.ModeArena && peerSession.beastPetResurrection != nil {
		run.arenaWorld = peerSession.beastPetResurrection.arenaWorld
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
			if latest.zone != nil && latest.zone.Companion() != nil {
				latest.zone.Companion().ClearBodyScale(run.objectID)
			}
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
	var arenaWorld *arenaAbilityWorld
	if isFound && peerSession.binding.Mode == game.ModeArena {
		world, worldErr := r.registry.arenaAbilityWorldLocked(peerSession.binding.GameID)
		if worldErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("beastArenaWorld: %w", worldErr)
		}
		arenaWorld = world
	}
	isAvailable := isFound && peerSession.generation == req.commandSession.generation &&
		req.command.Common.ObjectID == peerSession.deployedObjectID &&
		(peerSession.zone != nil && peerSession.zone.Companion() != nil || arenaWorld != nil) &&
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
	pet := zonecompanion.Actor{}
	isPetFound := false
	if arenaWorld != nil {
		pet, isPetFound = arenaWorld.companion.Snapshot(peerSession.beastPetObjectID)
	} else {
		pet, isPetFound = peerSession.zone.Companion().Snapshot(peerSession.beastPetObjectID)
	}
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
	var objectID uint32
	if arenaWorld != nil {
		objectID, err = arenaWorld.objectIDSession.Reserve(1)
	} else {
		objectID, err = peerSession.reserveCampaignObjectID()
	}
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
	var spawnPackets [][]byte
	if arenaWorld != nil {
		spawnPackets, err = r.spawnArenaBeastPet(&peerSession, arenaWorld,
			req.packet, objectID, req.command.Common.ObjectID, position, supportCreature.PetDamage)
	} else {
		spawnPackets, err = peerSession.spawnBeastPet(r.program, objectID,
			req.command.Common.ObjectID, position, supportCreature.PetDamage)
	}
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
	if arenaWorld != nil {
		err = peerSession.startArenaBeastResurrection(r.damage, req.packet, sessionKey,
			arenaWorld, req.packet.SourceTime)
	} else {
		err = peerSession.startBeastResurrection(r.damage, req.packet, sessionKey, req.packet.SourceTime)
	}
	if err != nil {
		if arenaWorld != nil {
			pet := arenaWorld.combatPets[objectID]
			if pet != nil {
				pet.isPublished = false
				pet.retireLocked()
			}
			peerSession.beastPetObjectID = 0
			peerSession.beastPetDamage = 0
			delete(peerSession.sagePassiveActivations, objectID)
			paymentErr := peerSession.setDeployedManaPoints(previousManaPoint)
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.sessions[sessionKey] = peerSession
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaBeastResurrection: %w", errors.Join(err, paymentErr))
		}
		cleanupPackets, cleanupErr := peerSession.stopBeastPet()
		peerSession.queueCampaignPackets(cleanupPackets)
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("summonResurrection: %w", errors.Join(err, cleanupErr))
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
	packets, err := r.startBeastPetAttackWithResume(packet, sessionKey, generation, sourceTime, nil, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("beastAttack: %w", err)
	}
	return packets, nil
}

func (r campaignDamageRuntime) startBeastPetAttackWithResume(
	packet raknet.Packet, sessionKey string, generation uint64, sourceTime uint64,
	resurrection *beastPetResurrectionRun, resumeRevision uint64, transportGeneration uint64,
) ([][]byte, error) {
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return nil, errors.New("beast pet schedule unavailable")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.beastPetObjectID != 0 && peerSession.zone != nil &&
		peerSession.zone.Companion() != nil && peerSession.zone.NPCs() != nil
	if resurrection != nil {
		isCurrent = isCurrent && peerSession.beastPetResurrection == resurrection &&
			resurrection.resumeRevision == resumeRevision && peerSession.transportGeneration == transportGeneration &&
			!peerSession.isRejoinPending && peerSession.deployedHitPoint() > 0
	}
	if isCurrent && peerSession.beastPetResurrection != nil {
		state := peerSession.beastPetResurrection.state
		isCurrent = !state.IsCasting && !state.IsBurrowing
	}
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
				r.npc.now(),
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
				pursuit.TargetObjectID, pursuit.Position, pursuit.Revision,
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
