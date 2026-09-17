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
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const fireTempestSpawnDelay = 700 * time.Millisecond
const fireTempestSpawnAnimationDuration = 2 * time.Second
const fireTempestOwnerPoll = 250 * time.Millisecond
const fireTempestEnrageWarmup = 1250 * time.Millisecond
const fireTempestEnrageDuration = 15 * time.Second
const fireTempestEnrageDamageBuff = float32(0.50)
const fireTempestBeamOutDuration = 800 * time.Millisecond
const fireTempestFallbackHitPoint = float32(50)
const fireTempestFallbackFootprint = float32(0.75)

type fireTempestActiveRun struct {
	runtime              campaignAbilityCommandRuntime
	packet               raknet.Packet
	sessionKey           string
	generation           uint64
	ownerObjectID        uint32
	petObjectID          uint32
	modifierInstanceID   uint32
	enrageInstanceID     uint32
	isEnrageBuffApplied  bool
	stackCount           uint32
	cancel               raknet.CancelSchedule
	enrageCancel         raknet.CancelSchedule
	monitorCancel        raknet.CancelSchedule
	replacementCancel    raknet.CancelSchedule
	attack               *abilityraknet.ProjectileRun
	attackTargetObjectID uint32
	orientation          raknet.Quaternion
}

func (e *fireTempestActiveRun) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	if e == nil || !isFound || peerSession.generation != e.generation {
		return false
	}
	if peerSession.fireTempestActive == e {
		return true
	}
	return false
}

type fireTempestStep struct {
	run        *fireTempestActiveRun
	definition sim.AbilityDefinition
	creature   game.GameplayCreature
	binding    game.GameplayBinding
	timestamp  uint64
	position   game.Vec3
	deadline   time.Duration
	kind       uint8
}

const (
	fireTempestStepOwner uint8 = iota + 1
	fireTempestStepSpawn
	fireTempestStepSpawnRelease
	fireTempestStepExpire
	fireTempestStepEnrageStart
	fireTempestStepEnragePulse
	fireTempestStepEnrageExpire
	fireTempestStepMonitor
)

func (e fireTempestStep) produce() ([][]byte, error) {
	switch e.kind {
	case fireTempestStepOwner:
		return e.owner()
	case fireTempestStepSpawn:
		return e.spawn()
	case fireTempestStepSpawnRelease:
		return e.spawnRelease()
	case fireTempestStepExpire:
		return e.expire()
	case fireTempestStepEnrageStart:
		return e.enrageStart()
	case fireTempestStepEnragePulse:
		return e.enragePulse()
	case fireTempestStepEnrageExpire:
		return e.enrageExpire()
	case fireTempestStepMonitor:
		return e.monitor()
	default:
		return nil, errors.New("fire tempest step invalid")
	}
}

func (e fireTempestStep) monitor() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	peerSession, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	if !e.run.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.run.monitorCancel = nil
	pet, isPetFound := peerSession.zone.Companion().Snapshot(e.run.petObjectID)
	if !isPetFound || pet.HitPoint <= 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	ownerPosition := game.Vec3(peerSession.playerPosition)
	if pet.Position.Sub(ownerPosition).Length() <= 50 {
		e.run.runtime.registry.mutex.Unlock()
		err := e.scheduleMonitor()
		if err != nil {
			return nil, fmt.Errorf("fireTempestMonitorContinue: %w", err)
		}
		return nil, nil
	}
	oldPetObjectID := e.run.petObjectID
	if e.run.attack != nil {
		e.run.attack.Stop()
		peerSession.zone.Companion().ReleaseAttack(
			oldPetObjectID, e.run.attackTargetObjectID,
		)
		e.run.attack = nil
		e.run.attackTargetObjectID = 0
	}
	newPetObjectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestReplacementObject: %w", err)
	}
	position, err := fireTempestSpawnPosition(peerSession, e.run.orientation)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestReplacementPosition: %w", err)
	}
	peerSession.zone.Companion().Remove(oldPetObjectID)
	enrageInstanceID := e.run.enrageInstanceID
	if e.run.enrageCancel != nil {
		e.run.enrageCancel()
		e.run.enrageCancel = nil
	}
	e.run.enrageInstanceID = 0
	e.run.isEnrageBuffApplied = false
	e.run.stackCount = 1
	e.run.petObjectID = newPetObjectID
	e.run.runtime.registry.sessions[e.run.sessionKey] = peerSession
	e.run.runtime.registry.mutex.Unlock()

	packets := make([][]byte, 0, 3)
	if enrageInstanceID != 0 {
		_ = e.run.runtime.modifierPool.Release(enrageInstanceID)
		packet, marshalErr := effectraknet.ModifierDelete(
			oldPetObjectID, enrageInstanceID,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("fireTempestReplacementEnrage: %w", marshalErr)
		}
		packets = append(packets, packet)
	}
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{oldPetObjectID},
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestReplacementDelete: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(e.definition.MuzzleEffectName), ObjectID: e.run.ownerObjectID,
		Position: raknet.Vector3{X: ownerPosition.X, Y: ownerPosition.Y, Z: ownerPosition.Z},
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestReplacementEffect: %w", err)
	}
	packets = append(packets, deletePacket, effectPacket)
	err = e.scheduleReplacement(position)
	if err != nil {
		return nil, fmt.Errorf("fireTempestReplacementSchedule: %w", err)
	}
	return packets, nil
}

func (e fireTempestStep) scheduleMonitor() error {
	producer := raknet.ScheduledPacketProducer{
		Delay: fireTempestOwnerPoll,
		Produce: fireTempestStep{
			run: e.run, definition: e.definition, creature: e.creature,
			binding: e.binding, timestamp: e.timestamp,
			kind: fireTempestStepMonitor,
		}.produce,
	}
	cancel, err := e.run.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{producer},
	)
	if err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	e.run.monitorCancel = cancel
	return nil
}

func (e fireTempestStep) scheduleReplacement(position game.Vec3) error {
	steps := []raknet.ScheduledPacketProducer{
		{Delay: fireTempestSpawnDelay, Produce: fireTempestStep{
			run: e.run, definition: e.definition, creature: e.creature,
			binding: e.binding, timestamp: e.timestamp, position: position,
			kind: fireTempestStepSpawn,
		}.produce},
		{Delay: fireTempestSpawnDelay + fireTempestSpawnAnimationDuration,
			Produce: fireTempestStep{
				run: e.run, definition: e.definition, creature: e.creature,
				binding: e.binding, timestamp: e.timestamp, position: position,
				kind: fireTempestStepSpawnRelease,
			}.produce},
		{Delay: fireTempestSpawnDelay + fireTempestSpawnAnimationDuration,
			Produce: fireTempestStep{
				run: e.run, definition: e.definition, creature: e.creature,
				binding: e.binding, timestamp: e.timestamp,
				kind: fireTempestStepMonitor,
			}.produce},
	}
	cancel, err := e.run.packet.ScheduleProducers(steps)
	if err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	e.run.replacementCancel = cancel
	return nil
}

func (e fireTempestStep) current() (gameplayPeerSession, bool) {
	e.run.runtime.registry.mutex.RLock()
	defer e.run.runtime.registry.mutex.RUnlock()
	peerSession, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	return peerSession, e.run.isCurrent(peerSession, isFound)
}

func (e fireTempestStep) owner() ([][]byte, error) {
	_, isCurrent := e.current()
	if !isCurrent {
		return nil, nil
	}
	modifierPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.run.ownerObjectID,
		TargetObjectID: e.run.ownerObjectID,
		ModifierID:     e.definition.RootModifierID,
		InstanceID:     e.run.modifierInstanceID,
		Duration:       e.definition.Duration,
		Timestamp:      e.timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestOwnerModifier: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(e.definition.MuzzleEffectName), ObjectID: e.run.ownerObjectID,
		Position: raknet.Vector3{X: e.position.X, Y: e.position.Y, Z: e.position.Z},
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestSummonEffect: %w", err)
	}
	return [][]byte{modifierPacket, effectPacket}, nil
}

func (e fireTempestStep) spawn() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	peerSession, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	if !e.run.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	hitPoint := e.run.runtime.program.NonPlayerHitPoint[util.HashID("RangedElementalPet")]
	if hitPoint <= 0 {
		hitPoint = fireTempestFallbackHitPoint
	}
	hitPoint = peerSession.petHitPoint(hitPoint)
	footprint, err := e.run.runtime.program.FootprintRadius(e.definition.SpawnNoun)
	if err != nil || footprint <= 0 {
		footprint = fireTempestFallbackFootprint
	}
	err = peerSession.zone.Companion().Put(zonecompanion.Actor{
		UserID: peerSession.binding.UserID, PeerGeneration: peerSession.generation,
		ObjectID: e.run.petObjectID, OwnerObjectID: e.run.ownerObjectID,
		Noun:     util.HashID(e.definition.SpawnNoun),
		Position: e.position, FootprintRadius: footprint,
		HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		IsTargetable: true, IsCombatant: true,
	})
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestCompanion: %w", err)
	}
	e.run.runtime.registry.sessions[e.run.sessionKey] = peerSession
	e.run.runtime.registry.mutex.Unlock()
	return marshalFireTempestSpawn(e.run, e.definition, e.position)
}

func (e fireTempestStep) spawnRelease() ([][]byte, error) {
	peerSession, isCurrent := e.current()
	if !isCurrent || peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return nil, nil
	}
	pet, isFound := peerSession.zone.Companion().Snapshot(e.run.petObjectID)
	if !isFound || pet.HitPoint <= 0 {
		return nil, nil
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.run.petObjectID,
		Value: map[uint8]float32{
			11: zonecompanion.CompatibilityMovementSpeed,
			12: zonecompanion.CompatibilityMovementSpeed,
			48: 0,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestSpawnReleaseAttribute: %w", err)
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.run.petObjectID, State: util.HashID("relaxed"), Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestSpawnReleaseAnimation: %w", err)
	}
	attackPackets, err := e.run.runtime.damage.startFireTempestPetAttack(
		e.run.packet, e.run.sessionKey, e.run.generation,
		e.timestamp+uint64(e.deadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("fireTempestSpawnReleaseAttack: %w", err)
	}
	packets := [][]byte{attributePacket, animationPacket}
	return append(packets, attackPackets...), nil
}

func (e fireTempestStep) expire() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	peerSession, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	if !e.run.isCurrent(peerSession, isFound) {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.fireTempestActive = nil
	packets, err := peerSession.stopFireTempestRun(e.run)
	e.run.runtime.registry.sessions[e.run.sessionKey] = peerSession
	e.run.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("fireTempestExpire: %w", err)
	}
	return packets, nil
}

func (e fireTempestStep) enrageStart() ([][]byte, error) {
	peerSession, isCurrent := e.current()
	if !isCurrent || peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return nil, nil
	}
	pet, isFound := peerSession.zone.Companion().Snapshot(e.run.petObjectID)
	if !isFound || pet.HitPoint <= 0 {
		return nil, nil
	}
	modifierPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.run.ownerObjectID, TargetObjectID: e.run.petObjectID,
		ModifierID: e.definition.SecondaryModifierID,
		InstanceID: e.run.enrageInstanceID, Duration: fireTempestEnrageDuration,
		Timestamp: e.timestamp + uint64(e.deadline/time.Millisecond),
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestEnrageModifier: %w", err)
	}
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.run.petObjectID, State: util.HashID("firetempest_pet_enraged"), Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestEnrageAnimation: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(e.definition.ActivationEffectName), ObjectID: e.run.petObjectID,
		Position: raknet.Vector3{X: pet.Position.X, Y: pet.Position.Y, Z: pet.Position.Z},
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestEnrageEffect: %w", err)
	}
	return [][]byte{modifierPacket, animationPacket, effectPacket}, nil
}

func (e fireTempestStep) enragePulse() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	peerSession, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	if !e.run.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil || peerSession.zone.NPCs() == nil ||
		peerSession.zone.Population() == nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	pet, isPetFound := peerSession.zone.Companion().Snapshot(e.run.petObjectID)
	if !isPetFound || pet.HitPoint <= 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	var attributePacket []byte
	if !e.run.isEnrageBuffApplied {
		err := peerSession.zone.Companion().AddBuff(
			e.run.petObjectID,
			zonecompanion.Buff{DamageBuff: fireTempestEnrageDamageBuff},
		)
		if err != nil {
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("fireTempestEnrageDamageBuff: %w", err)
		}
		attributePacket, err = raknet.MarshalApplication(
			raknet.AttributeDataUpdateMessage{
				ObjectID: e.run.petObjectID,
				Value:    map[uint8]float32{113: 0.20},
			},
		)
		if err != nil {
			peerSession.zone.Companion().RemoveBuff(
				e.run.petObjectID,
				zonecompanion.Buff{DamageBuff: fireTempestEnrageDamageBuff},
			)
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("fireTempestEnrageScale: %w", err)
		}
		e.run.isEnrageBuffApplied = true
	}
	creature := e.creature
	creature.DamageProfile.PrimaryAttribute = creature.PetDamage
	creature.DamageProfile.IsPrimaryAttributeFound = true
	creature.DamageProfile.DamageBuff += fireTempestEnrageDamageBuff
	damageDefinition := e.definition
	damageDefinition.Kind = sim.AbilityKindAuraArea
	plan, err := zoneability.PlanFixedArea(
		peerSession.zone.NPCs(), e.run.petObjectID, pet.Position,
		creature, damageDefinition,
	)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestEnragePlan: %w", err)
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), plan,
		creature, peerSession.binding.Difficulty, e.run.runtime.program.Critical,
	)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestEnrageDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("fireTempestEnrageTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	e.run.runtime.registry.sessions[e.run.sessionKey] = peerSession
	e.run.runtime.registry.mutex.Unlock()
	effect := func(result zoneability.AreaResult) ([]byte, error) {
		packet, marshalErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset:    util.HashID(e.definition.ImpactEffectName),
			ObjectID: result.Snapshot.Plan.ObjectID,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("fireTempestEnrageImpact: %w", marshalErr)
		}
		return packet, nil
	}
	packets, err := e.run.runtime.damage.publishAreaResults(
		e.run.packet, e.run.sessionKey, e.run.generation, e.run.petObjectID,
		e.timestamp+uint64(e.deadline/time.Millisecond), e.binding,
		results, transitions, effect, true,
	)
	if err != nil {
		return nil, fmt.Errorf("fireTempestEnragePublish: %w", err)
	}
	if len(attributePacket) != 0 {
		packets = append([][]byte{attributePacket}, packets...)
	}
	return packets, nil
}

func (e fireTempestStep) enrageExpire() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	peerSession, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	if !e.run.isCurrent(peerSession, isFound) || e.run.enrageInstanceID == 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	instanceID := e.run.enrageInstanceID
	e.run.enrageInstanceID = 0
	e.run.enrageCancel = nil
	isEnrageBuffApplied := e.run.isEnrageBuffApplied
	e.run.runtime.registry.sessions[e.run.sessionKey] = peerSession
	e.run.runtime.registry.mutex.Unlock()
	if isEnrageBuffApplied && peerSession.zone != nil &&
		peerSession.zone.Companion() != nil {
		peerSession.zone.Companion().RemoveBuff(
			e.run.petObjectID,
			zonecompanion.Buff{DamageBuff: fireTempestEnrageDamageBuff},
		)
	}
	e.run.isEnrageBuffApplied = false
	_ = e.run.runtime.modifierPool.Release(instanceID)
	modifierPacket, err := effectraknet.ModifierDelete(e.run.petObjectID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("fireTempestEnrageDelete: %w", err)
	}
	if !isEnrageBuffApplied {
		return [][]byte{modifierPacket}, nil
	}
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.run.petObjectID,
		Value:    map[uint8]float32{113: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestEnrageScaleReset: %w", err)
	}
	return [][]byte{modifierPacket, attributePacket}, nil
}

func marshalFireTempestSpawn(
	run *fireTempestActiveRun, definition sim.AbilityDefinition, position game.Vec3,
) ([][]byte, error) {
	messages := []raknet.ApplicationMessage{
		raknet.ObjectCreateMessage{
			ObjectID: run.petObjectID, Noun: util.HashID(definition.SpawnNoun),
			PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
			Scale: 1, Team: 1, OwnerID: run.ownerObjectID, IsCollisionEnabled: true,
		},
		raknet.AttributeDataUpdateMessage{
			ObjectID: run.petObjectID,
			Value:    map[uint8]float32{11: 0, 12: 0, 48: 0},
		},
		raknet.SetAnimationStateMessage{
			ObjectID: run.petObjectID, State: util.HashID("cast_firetempest_pet_spawn"), Scale: 1,
		},
		raknet.ServerEventMessage{Asset: util.HashID(definition.HitEffectName), ObjectID: run.petObjectID},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("fireTempestSpawnMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func fireTempestSpawnPosition(
	peerSession gameplayPeerSession, orientation raknet.Quaternion,
) (game.Vec3, error) {
	facing, err := geometryraknet.Forward(orientation)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("fireTempestFacing: %w", err)
	}
	angle := math.Atan2(float64(facing.Y), float64(facing.X))
	angle += (peerSession.zone.Population().Random().Float64() - 0.5) * math.Pi
	desired := game.Vec3(peerSession.playerPosition).Add(game.Vec3{
		X: float32(math.Cos(angle)) * 5,
		Y: float32(math.Sin(angle)) * 5,
	})
	position, isProjected, err := zonenavigation.DirectMovementDestination(
		peerSession.zone.Navigation(), game.Vec3(peerSession.playerPosition),
		desired, fireTempestFallbackFootprint,
	)
	if err != nil {
		return game.Vec3{}, fmt.Errorf("fireTempestProjection: %w", err)
	}
	if isProjected {
		return position, nil
	}
	return desired, nil
}

func (e *gameplayPeerSession) stopFireTempestRun(
	run *fireTempestActiveRun,
) ([][]byte, error) {
	if run == nil {
		return nil, nil
	}
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
		if e.zone != nil && e.zone.Companion() != nil {
			e.zone.Companion().ReleaseAttack(
				run.petObjectID, run.attackTargetObjectID,
			)
		}
		run.attack = nil
		run.attackTargetObjectID = 0
	}
	_ = run.runtime.modifierPool.Release(run.modifierInstanceID)
	if run.enrageInstanceID != 0 {
		_ = run.runtime.modifierPool.Release(run.enrageInstanceID)
	}
	if e.zone != nil && e.zone.Companion() != nil {
		e.zone.Companion().Remove(run.petObjectID)
	}
	packets := make([][]byte, 0, 4)
	if run.enrageInstanceID != 0 {
		packet, err := effectraknet.ModifierDelete(run.petObjectID, run.enrageInstanceID)
		if err != nil {
			return nil, fmt.Errorf("fireTempestStopEnrage: %w", err)
		}
		packets = append(packets, packet)
		run.enrageInstanceID = 0
	}
	ownerPacket, err := effectraknet.ModifierDelete(
		run.ownerObjectID, run.modifierInstanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("fireTempestStopOwner: %w", err)
	}
	beamPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: run.petObjectID, State: util.HashID("firetempest_pet_beamout"), Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestStopBeam: %w", err)
	}
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{run.petObjectID},
	})
	if err != nil {
		return nil, fmt.Errorf("fireTempestStopDelete: %w", err)
	}
	packets = append(packets, ownerPacket, beamPacket)
	_, scheduleErr := run.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay:   fireTempestBeamOutDuration,
		Produce: fireTempestStaticProducer{packet: deletePacket}.produce,
	}})
	if scheduleErr != nil {
		packets = append(packets, deletePacket)
	}
	return packets, nil
}

type fireTempestStaticProducer struct{ packet []byte }

func (e fireTempestStaticProducer) produce() ([][]byte, error) {
	return [][]byte{e.packet}, nil
}

func (e *gameplayPeerSession) stopFireTempestActive() ([][]byte, error) {
	if e == nil || e.fireTempestActive == nil {
		return nil, nil
	}
	run := e.fireTempestActive
	e.fireTempestActive = nil
	return e.stopFireTempestRun(run)
}

func (r campaignAbilityCommandRuntime) handleFireTempestActive(
	req campaignCharacterAbilityRequest,
	peerSession gameplayPeerSession,
	creature game.GameplayCreature,
	ability zonecontent.HeroAbility,
	sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	definition := ability.Definition
	if ability.ID == 0 || definition.Name != "FireTempestActive" ||
		definition.Kind != sim.AbilityKindSummonBuff || definition.SpawnNoun == "" ||
		definition.Duration <= 0 || definition.PlacementDistance <= 0 {
		r.registry.mutex.Unlock()
		return req.reject("fire tempest definition unavailable")
	}
	definition, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestTiming: %w", err)
	}
	isExisting := peerSession.fireTempestActive != nil
	manaCost := float32(20)
	animationName := definition.AnimationName
	if isExisting {
		manaCost = max(float32(0), float32(10)-creature.PetDamage*0.05)
		animationName = definition.SecondaryAnimationName
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	cooldownReservation, isCooldownReserved := peerSession.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(ability.ID), abilityStartTime, definition.Cooldown,
	)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("fire tempest cooldown unavailable")
	}
	releaseReservation, isReleaseReserved := peerSession.abilityReleaseSession().Reserve(
		abilityStartTime, definition.ReleaseDelay,
	)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("fire tempest release unavailable")
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: ability.ID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime + uint64(definition.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    req.packet.SourceTime + uint64(definition.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestAck: %w", err)
	}
	startPackets, err := abilityraknet.StartSpendPresentation(
		req.command.Common.ObjectID, ability.ID, animationName,
		definition.Cooldown, req.packet.SourceTime, remainingManaPoint,
	)
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestStart: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], ability.ID, req.command.Ability.Index,
		req.packet.SourceTime, definition.HitDelay, definition.ReleaseDelay,
	)
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestRelease: %w", err)
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
		return nil, fmt.Errorf("fireTempestCommit: %w", err)
	}
	if isExisting {
		return r.startFireTempestEnrage(
			req, peerSession, creature, definition, sessionKey, previousManaPoint,
			cooldownReservation, releaseReservation, ackPacket, releasePacket,
			startPackets,
		)
	}
	return r.startFireTempestSummon(
		req, peerSession, creature, definition, sessionKey, previousManaPoint,
		cooldownReservation, releaseReservation, ackPacket, releasePacket,
		startPackets,
	)
}

func (r campaignAbilityCommandRuntime) startFireTempestSummon(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	sessionKey string, previousManaPoint float32,
	cooldownReservation zoneability.CooldownReservation,
	releaseReservation zoneaction.ReleaseReservation,
	ackPacket []byte, releasePacket []byte, startPackets [][]byte,
) ([][]byte, error) {
	objectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		return r.rollbackFireTempest(
			req, peerSession, sessionKey, previousManaPoint,
			cooldownReservation, releaseReservation, "fireTempestObjectID", err,
		)
	}
	instanceID, err := r.modifierPool.Allocate()
	if err != nil {
		return r.rollbackFireTempest(
			req, peerSession, sessionKey, previousManaPoint,
			cooldownReservation, releaseReservation, "fireTempestModifier", err,
		)
	}
	position, err := fireTempestSpawnPosition(peerSession, req.command.Common.Orientation)
	if err != nil {
		_ = r.modifierPool.Release(instanceID)
		return r.rollbackFireTempest(
			req, peerSession, sessionKey, previousManaPoint,
			cooldownReservation, releaseReservation, "fireTempestPosition", err,
		)
	}
	run := &fireTempestActiveRun{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: peerSession.generation, ownerObjectID: req.command.Common.ObjectID,
		petObjectID: objectID, modifierInstanceID: instanceID, stackCount: 1,
		orientation: req.command.Common.Orientation,
	}
	peerSession.fireTempestActive = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	steps := []raknet.ScheduledPacketProducer{
		{Delay: definition.HitDelay, Produce: fireTempestStep{
			run: run, definition: definition, creature: creature,
			binding: peerSession.binding, timestamp: req.packet.SourceTime,
			position: position, deadline: definition.HitDelay,
			kind: fireTempestStepOwner,
		}.produce},
		{Delay: definition.HitDelay + fireTempestSpawnDelay, Produce: fireTempestStep{
			run: run, definition: definition, creature: creature,
			binding: peerSession.binding, timestamp: req.packet.SourceTime,
			position: position, deadline: definition.HitDelay + fireTempestSpawnDelay,
			kind: fireTempestStepSpawn,
		}.produce},
		{Delay: definition.HitDelay + fireTempestSpawnDelay + fireTempestSpawnAnimationDuration,
			Produce: fireTempestStep{
				run: run, definition: definition, creature: creature,
				binding: peerSession.binding, timestamp: req.packet.SourceTime,
				position: position,
				deadline: definition.HitDelay + fireTempestSpawnDelay +
					fireTempestSpawnAnimationDuration,
				kind: fireTempestStepSpawnRelease,
			}.produce},
		{Delay: definition.HitDelay + fireTempestSpawnDelay + fireTempestSpawnAnimationDuration,
			Produce: fireTempestStep{
				run: run, definition: definition, creature: creature,
				binding: peerSession.binding, timestamp: req.packet.SourceTime,
				kind: fireTempestStepMonitor,
			}.produce},
		{Delay: definition.ReleaseDelay, Produce: fireTempestStaticProducer{
			packet: releasePacket,
		}.produce},
		{Delay: definition.HitDelay + definition.Duration, Produce: fireTempestStep{
			run: run, definition: definition, creature: creature,
			binding: peerSession.binding, timestamp: req.packet.SourceTime,
			position: position, deadline: definition.HitDelay + definition.Duration,
			kind: fireTempestStepExpire,
		}.produce},
	}
	cancel, err := req.packet.ScheduleProducers(steps)
	if err != nil {
		r.registry.mutex.Lock()
		latest, isFound := r.registry.sessions[sessionKey]
		if run.isCurrent(latest, isFound) {
			latest.fireTempestActive = nil
			_ = latest.setDeployedManaPoints(previousManaPoint)
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fireTempestSchedule: %w", err)
	}
	run.cancel = cancel
	packets := [][]byte{ackPacket}
	return append(packets, startPackets...), nil
}

func (r campaignAbilityCommandRuntime) startFireTempestEnrage(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	sessionKey string, previousManaPoint float32,
	cooldownReservation zoneability.CooldownReservation,
	releaseReservation zoneaction.ReleaseReservation,
	ackPacket []byte, releasePacket []byte, startPackets [][]byte,
) ([][]byte, error) {
	run := peerSession.fireTempestActive
	if run.stackCount >= 2 || run.enrageInstanceID != 0 {
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		_, err := req.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
			Delay:   definition.ReleaseDelay,
			Produce: fireTempestStaticProducer{packet: releasePacket}.produce,
		}})
		if err != nil {
			r.registry.mutex.Lock()
			latest, isFound := r.registry.sessions[sessionKey]
			if run.isCurrent(latest, isFound) {
				_ = latest.setDeployedManaPoints(previousManaPoint)
				latest.abilityCooldownSession().Rollback(cooldownReservation)
				latest.abilityReleaseSession().Rollback(releaseReservation)
				r.registry.sessions[sessionKey] = latest
			}
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("fireTempestMaxStackSchedule: %w", err)
		}
		packets := [][]byte{ackPacket}
		return append(packets, startPackets...), nil
	}
	instanceID, err := r.modifierPool.Allocate()
	if err != nil {
		return r.rollbackFireTempest(
			req, peerSession, sessionKey, previousManaPoint,
			cooldownReservation, releaseReservation, "fireTempestEnrageModifier", err,
		)
	}
	run.stackCount = 2
	run.enrageInstanceID = instanceID
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	startDelay := definition.HitDelay
	pulseDelay := startDelay + fireTempestEnrageWarmup
	steps := []raknet.ScheduledPacketProducer{{
		Delay: startDelay, Produce: fireTempestStep{
			run: run, definition: definition, creature: creature,
			binding: peerSession.binding, timestamp: req.packet.SourceTime,
			deadline: startDelay, kind: fireTempestStepEnrageStart,
		}.produce,
	}}
	for delay := pulseDelay; delay < definition.HitDelay+fireTempestEnrageDuration; delay += definition.TickDuration {
		steps = append(steps, raknet.ScheduledPacketProducer{
			Delay: delay, Produce: fireTempestStep{
				run: run, definition: definition, creature: creature,
				binding: peerSession.binding, timestamp: req.packet.SourceTime,
				deadline: delay, kind: fireTempestStepEnragePulse,
			}.produce,
		})
	}
	steps = append(steps,
		raknet.ScheduledPacketProducer{Delay: definition.ReleaseDelay, Produce: fireTempestStaticProducer{
			packet: releasePacket,
		}.produce},
		raknet.ScheduledPacketProducer{
			Delay: definition.HitDelay + fireTempestEnrageDuration,
			Produce: fireTempestStep{
				run: run, definition: definition, creature: creature,
				binding: peerSession.binding, timestamp: req.packet.SourceTime,
				deadline: definition.HitDelay + fireTempestEnrageDuration,
				kind:     fireTempestStepEnrageExpire,
			}.produce,
		},
	)
	cancel, err := req.packet.ScheduleProducers(steps)
	if err != nil {
		r.registry.mutex.Lock()
		latest, isFound := r.registry.sessions[sessionKey]
		if run.isCurrent(latest, isFound) {
			run.stackCount = 1
			run.enrageInstanceID = 0
			_ = latest.setDeployedManaPoints(previousManaPoint)
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fireTempestEnrageSchedule: %w", err)
	}
	run.enrageCancel = cancel
	packets := [][]byte{ackPacket}
	return append(packets, startPackets...), nil
}

func (r campaignAbilityCommandRuntime) rollbackFireTempest(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	sessionKey string, previousManaPoint float32,
	cooldownReservation zoneability.CooldownReservation,
	releaseReservation zoneaction.ReleaseReservation,
	step string, cause error,
) ([][]byte, error) {
	_ = peerSession.setDeployedManaPoints(previousManaPoint)
	peerSession.abilityCooldownSession().Rollback(cooldownReservation)
	peerSession.abilityReleaseSession().Rollback(releaseReservation)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	return nil, fmt.Errorf("%s: %w", step, cause)
}
