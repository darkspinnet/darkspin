package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
)

const spriteFallbackHitPoint = float32(25)
const spriteFallbackFootprint = float32(0.5)
const spriteFollowDistance = float32(2)
const spriteHealingFraction = float32(0.03)
const spriteHealingInterval = time.Second

type heroSummonRun struct {
	runtime    campaignAbilityCommandRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	ownerID    uint32
	definition sim.AbilityDefinition
	cancel     raknet.CancelSchedule
}

func (e *heroSummonRun) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroSummons[e.objectID] == e
}

func (e *heroSummonRun) release(packet []byte) func() ([][]byte, error) {
	return func() ([][]byte, error) {
		e.runtime.registry.mutex.RLock()
		peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
		isCurrent := e.isCurrent(peerSession, isFound)
		e.runtime.registry.mutex.RUnlock()
		if !isCurrent {
			return nil, nil
		}
		return [][]byte{packet}, nil
	}
}

func (e *heroSummonRun) tick() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil || peerSession.squad == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	companion, isCompanionFound := peerSession.zone.Companion().Snapshot(e.objectID)
	if !isCompanionFound || companion.HitPoint <= 0 ||
		peerSession.deployedObjectID != e.ownerID {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	ownerPosition := game.Vec3(peerSession.playerPosition)
	follows := make([]zonecompanion.Follow, 0, 1)
	delta := companion.Position.Sub(ownerPosition)
	distance := delta.Length()
	if distance > spriteFollowDistance {
		goal := ownerPosition.Add(delta.Scale(spriteFollowDistance / distance))
		err := peerSession.zone.Companion().SetPosition(e.objectID, goal)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroSummonFollow: %w", err)
		}
		follows = append(follows, zonecompanion.Follow{
			ObjectID: e.objectID, OwnerObjectID: e.ownerID,
			Position: companion.Position, Goal: ownerPosition,
			DesiredStopDistance: spriteFollowDistance,
		})
		companion.Position = goal
	}
	creatureIndex := peerSession.squad.DeployedIndex()
	character, isCharacterFound := peerSession.squad.Character(creatureIndex)
	maximumHitPoint := peerSession.characterHitPointMaximum(creatureIndex)
	healedAmount := float32(0)
	hitPoint := character.HitPoints
	if isCharacterFound && character.IsAvailable && character.HitPoints > 0 &&
		maximumHitPoint > character.HitPoints {
		healing := maximumHitPoint * spriteHealingFraction
		healing, err := game.ApplyTargetHealingReduction(
			healing, peerSession.binding.Creatures[creatureIndex].HealingTargetProfile,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroSummonHealingReduction: %w", err)
		}
		hitPoint = min(maximumHitPoint, character.HitPoints+healing)
		healedAmount = hitPoint - character.HitPoints
		if healedAmount > 0 {
			_, err = peerSession.setCampaignCharacterHitPoints(creatureIndex, hitPoint)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("heroSummonHealingCommit: %w", err)
			}
		}
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets, err := companionraknet.Follow(follows)
	if err != nil {
		return nil, fmt.Errorf("heroSummonFollowMarshal: %w", err)
	}
	if healedAmount <= 0 {
		return packets, nil
	}
	healingPackets, err := abilityraknet.MarshalTreeOfLifeHealing(
		e.definition, raknet.Vector3(companion.Position),
		[]abilityraknet.Healing{{
			SourceObjectID: e.objectID, ObjectID: e.ownerID,
			HitPoint: hitPoint, Amount: healedAmount,
		}}, false,
	)
	if err != nil {
		return nil, fmt.Errorf("heroSummonHealingMarshal: %w", err)
	}
	packets = append(packets, healingPackets...)
	err = e.runtime.stats.Record(
		context.Background(), binding,
		sporenet.PlayerStatDelta{
			PVEHealing:         float64(healedAmount),
			PVEHealingReceived: float64(healedAmount),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("heroSummonHealingStats: %w", err)
	}
	return packets, nil
}

func (e *heroSummonRun) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	companion, isCompanionFound := peerSession.zone.Companion().Snapshot(e.objectID)
	peerSession.zone.Companion().Remove(e.objectID)
	delete(peerSession.heroSummons, e.objectID)
	e.cancel = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	position := game.Vec3{}
	if isCompanionFound {
		position = companion.Position
	}
	packets := make([][]byte, 0, 2)
	if e.definition.ImpactEffectName != "" {
		effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(e.definition.ImpactEffectName), ObjectID: e.objectID,
			Position: raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
		})
		if err != nil {
			return nil, fmt.Errorf("heroSummonOutEffect: %w", err)
		}
		packets = append(packets, effectPacket)
	}
	deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{e.objectID},
	})
	if err != nil {
		return nil, fmt.Errorf("heroSummonDelete: %w", err)
	}
	return append(packets, deletePacket), nil
}

func (e *gameplayPeerSession) stopHeroSummons() ([][]byte, error) {
	if e == nil || len(e.heroSummons) == 0 {
		return nil, nil
	}
	runs := e.heroSummons
	e.heroSummons = nil
	packets := make([][]byte, 0, len(runs)*2)
	for objectID, run := range runs {
		if run != nil && run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		position := game.Vec3{}
		if e.zone != nil && e.zone.Companion() != nil {
			companion, isFound := e.zone.Companion().Snapshot(objectID)
			if isFound {
				position = companion.Position
			}
			e.zone.Companion().Remove(objectID)
		}
		if run != nil && run.definition.ImpactEffectName != "" {
			effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
				Asset: util.HashID(run.definition.ImpactEffectName), ObjectID: objectID,
				Position: raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
			})
			if err != nil {
				return nil, fmt.Errorf("heroSummonStopEffect: %w", err)
			}
			packets = append(packets, effectPacket)
		}
		deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
			ObjectID: []uint32{objectID},
		})
		if err != nil {
			return nil, fmt.Errorf("heroSummonStopDelete: %w", err)
		}
		packets = append(packets, deletePacket)
	}
	return packets, nil
}

func (r campaignAbilityCommandRuntime) handleHeroSummon(
	req campaignCharacterAbilityRequest,
	peerSession gameplayPeerSession,
	creature game.GameplayCreature,
	ability zonecontent.HeroAbility,
	sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	definition := ability.Definition
	if ability.ID == 0 || definition.Kind != sim.AbilityKindSummonBuff ||
		definition.Name != "SummonSprite" || definition.SpawnNoun != "SpritePet.Noun" ||
		definition.Duration <= 0 || definition.PlacementDistance <= 0 {
		r.registry.mutex.Unlock()
		return req.reject("summon definition unavailable")
	}
	definition, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroSummonTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		definition.ManaCost, creature.DamageProfile.PrimaryAttribute, definition.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroSummonMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	facing, err := geometryraknet.Forward(req.command.Common.Orientation)
	if err != nil {
		r.registry.mutex.Unlock()
		return req.reject("summon facing unavailable")
	}
	objectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroSummonObjectID: %w", err)
	}
	position := game.Vec3(peerSession.playerPosition).Add(
		game.Vec3(facing).Scale(definition.PlacementDistance + spriteFallbackFootprint),
	)
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: ability.ID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime,
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(definition.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroSummonAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], ability.ID, req.command.Ability.Index,
		req.packet.SourceTime, 0, definition.ReleaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroSummonRelease: %w", err)
	}
	startPackets, err := abilityraknet.StartSpendPresentation(
		req.command.Common.ObjectID, ability.ID, definition.AnimationName,
		definition.Cooldown, req.packet.SourceTime, remainingManaPoint,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroSummonStart: %w", err)
	}
	cooldownReservation, isCooldownReserved := peerSession.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(ability.ID), abilityStartTime, definition.Cooldown,
	)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("summon cooldown unavailable")
	}
	releaseReservation, isReleaseReserved := peerSession.abilityReleaseSession().Reserve(
		abilityStartTime, definition.ReleaseDelay,
	)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("summon release unavailable")
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
		return nil, fmt.Errorf("heroSummonCommit: %w", err)
	}
	hitPoint := r.program.NonPlayerHitPoint[util.HashID("SpritePet")]
	if hitPoint <= 0 {
		hitPoint = spriteFallbackHitPoint
	}
	hitPoint = peerSession.petHitPoint(hitPoint)
	footprintRadius, footprintErr := r.program.FootprintRadius(definition.SpawnNoun)
	if footprintErr != nil || footprintRadius <= 0 {
		footprintRadius = spriteFallbackFootprint
	}
	err = peerSession.zone.Companion().Put(zonecompanion.Actor{
		UserID: peerSession.binding.UserID, PeerGeneration: peerSession.generation,
		ObjectID: objectID, OwnerObjectID: req.command.Common.ObjectID,
		Noun:     util.HashID(definition.SpawnNoun),
		Position: position, FootprintRadius: footprintRadius,
		HitPoint: hitPoint, MaximumHitPoint: hitPoint,
		IsTargetable: true, IsCombatant: false,
	})
	if err != nil {
		_ = peerSession.setDeployedManaPoints(previousManaPoint)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroSummonCompanion: %w", err)
	}
	run := &heroSummonRun{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: peerSession.generation, objectID: objectID,
		ownerID: req.command.Common.ObjectID, definition: definition,
	}
	if peerSession.heroSummons == nil {
		peerSession.heroSummons = make(map[uint32]*heroSummonRun)
	}
	peerSession.heroSummons[objectID] = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	spawnPackets, err := marshalHeroSummonSpawn(run, position, footprintRadius)
	if err != nil {
		return nil, fmt.Errorf("heroSummonSpawn: %w", err)
	}
	producers := make([]raknet.ScheduledPacketProducer, 0,
		int(definition.Duration/spriteHealingInterval)+2)
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: definition.ReleaseDelay, Produce: run.release(releasePacket),
	})
	for delay := definition.ReleaseDelay + spriteHealingInterval; delay < definition.Duration; delay += spriteHealingInterval {
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: delay, Produce: run.tick,
		})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: definition.Duration, Produce: run.expire,
	})
	cancel, err := req.packet.ScheduleProducers(producers)
	if err != nil {
		r.registry.mutex.Lock()
		latest, isFound := r.registry.sessions[sessionKey]
		if run.isCurrent(latest, isFound) {
			latest.zone.Companion().Remove(objectID)
			delete(latest.heroSummons, objectID)
			_ = latest.setDeployedManaPoints(previousManaPoint)
			latest.abilityCooldownSession().Rollback(cooldownReservation)
			latest.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroSummonSchedule: %w", err)
	}
	run.cancel = cancel
	packets := [][]byte{ackPacket}
	packets = append(packets, startPackets...)
	return append(packets, spawnPackets...), nil
}

func (e gameplayPeerSession) petHitPoint(hitPoint float32) float32 {
	if hitPoint <= 0 || e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) {
		return hitPoint
	}
	if hitPoint <= 1 {
		hitPoint *= e.characterHitPointMaximum(e.deployedCreatureIndex)
	}
	petHealthIncrease := e.binding.Creatures[e.deployedCreatureIndex].PetHealthIncrease
	if petHealthIncrease == 0 {
		return hitPoint
	}
	return hitPoint * (1 + petHealthIncrease)
}

func marshalHeroSummonSpawn(
	run *heroSummonRun, position game.Vec3, footprintRadius float32,
) ([][]byte, error) {
	if run == nil || run.objectID == 0 || run.ownerID == 0 || footprintRadius <= 0 {
		return nil, errors.New("hero summon spawn invalid")
	}
	packets, err := companionraknet.Create(raknet.ObjectCreateMessage{
		ObjectID: run.objectID, Noun: util.HashID(run.definition.SpawnNoun),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, Team: 1, OwnerID: run.ownerID, IsCollisionEnabled: true,
	})
	if err != nil {
		return nil, fmt.Errorf("heroSummonCreate: %w", err)
	}
	message := []raknet.ApplicationMessage{
		raknet.AttributeDataUpdateMessage{
			ObjectID: run.objectID,
			Value: map[uint8]float32{
				11: zonecompanion.CompatibilityMovementSpeed,
				12: zonecompanion.CompatibilityMovementSpeed,
				48: 0,
			},
		},
		raknet.ServerEventMessage{
			Asset: util.HashID(run.definition.MuzzleEffectName), ObjectID: run.ownerID,
		},
		raknet.ServerEventMessage{
			Asset: util.HashID(run.definition.ActivationEffectName), ObjectID: run.objectID,
			Position: raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
		},
		raknet.SetAnimationStateMessage{
			ObjectID: run.objectID, State: util.HashID("LF_pet_healer_spawn"), Scale: 1,
		},
		raknet.ServerEventMessage{
			Asset: util.HashID(run.definition.HitEffectName), ObjectID: run.objectID,
		},
	}
	for index, current := range message {
		packet, err := raknet.MarshalApplication(current)
		if err != nil {
			return nil, fmt.Errorf("heroSummonSpawnMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
