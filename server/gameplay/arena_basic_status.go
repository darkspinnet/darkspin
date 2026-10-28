package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Basic-hit debuffs belong to the struck player, so swapping the caster does
// not retire them. Their effects are projected from the recipient's status.
func (e *arenaAbilityTargets) applyBasicStatusLocked(result zonenpc.DamageResult,
	sourceCreature game.GameplayCreature, rank uint32, timestamp uint64,
) error {
	if rank == 0 || result.IsDefeated || result.IsDamageImmune &&
		e.definition.Name != "LightspeedTempestBasic" && e.definition.Name != "EnergySentinelBasic" {
		return nil
	}
	name := ""
	kind := sim.AbilityStatusKind("")
	duration := time.Duration(0)
	switch e.definition.Name {
	case "LightningTempest_Basic":
		name, kind, duration = "Modifier_LightningTempest_Basic", arenaBasicEnergyVulnerability, 3*time.Second
	case "Weakness":
		name, kind, duration = "WeaknessDebuff", arenaBasicWeakness, 10*time.Second
	case "Pummel":
		return e.applyPummelStatusLocked(result, rank, timestamp)
	case "FireRavagerBasic":
		return e.applyBasicBurnLocked(result, sourceCreature, rank, timestamp)
	case "LightspeedTempestBasic":
		name, kind, duration = "LightSpeedHaste", arenaBasicHaste, 20*time.Second
	case "EnergySentinelBasic":
		name, kind, duration = "EnergySentinelHealDebuff", arenaBasicHealingReduction, e.definition.StatusDuration
	default:
		return nil
	}
	if e.runtime.registry.timer == nil {
		return errors.New("arena basic status timer unavailable")
	}
	key, target, isFound := e.member(result.ObjectID)
	if !isFound || target.binding.Mode != game.ModeArena {
		return nil
	}
	if kind != arenaBasicHaste && (target.isHeroDebuffImmune(result.ObjectID) ||
		target.isOverdriveActiveAt(e.runtime.now())) {
		return nil
	}
	modifierID := util.HashID(name)
	statusKey := arenaHitStatusKey{modifierID: modifierID}
	if kind == arenaBasicHealingReduction {
		statusKey.sourceObjectID = e.source.deployedObjectID
	}
	previous := target.arenaHitStatuses[statusKey]
	if (kind == arenaBasicEnergyVulnerability || kind == arenaBasicHaste) && previous != nil && previous.matches(target) &&
		previous.kind == kind && e.runtime.now().Before(previous.expiresAt) {
		if previous.stackCount >= 5 {
			return nil
		}
		err := previous.updateBasic(&target, timestamp, previous.stackCount+1)
		if err != nil {
			return fmt.Errorf("arenaBasicStack: %w", err)
		}
		e.runtime.registry.sessions[key] = target
		return nil
	}
	status, createPacket, err := e.prepareBasicStatusLocked(key, target, result.ObjectID,
		name, kind, rank, 1, duration, timestamp)
	if err != nil {
		return fmt.Errorf("arenaBasicPrepare: %w", err)
	}
	if kind == arenaBasicHealingReduction {
		status.isCasterUnique = true
	}
	if target.arenaHitStatuses == nil {
		target.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous != nil {
		previous.retire(&target)
	}
	target.arenaHitStatuses[statusKey] = status
	packets := [][]byte{createPacket}
	if kind == arenaBasicHaste || kind == arenaBasicHealingReduction {
		attributePackets, attributeErr := status.refresh(&target)
		if attributeErr != nil {
			delete(target.arenaHitStatuses, statusKey)
			rollbackErr := status.discard("arenaHasteRollback", attributeErr)
			e.runtime.registry.sessions[key] = target
			return fmt.Errorf("arenaHasteAttributes: %w", rollbackErr)
		}
		packets = append(packets, attributePackets...)
	}
	status.queue(&target, packets)
	e.runtime.registry.sessions[key] = target
	return nil
}

func (e *arenaAbilityTargets) prepareBasicStatusLocked(key string, target gameplayPeerSession,
	objectID uint32, name string, kind sim.AbilityStatusKind, rank uint32,
	stackCount uint32, duration time.Duration, timestamp uint64,
) (*arenaHitStatus, []byte, error) {
	status := &arenaHitStatus{runtime: e.runtime, targetKey: key, gameID: e.source.binding.GameID,
		objectID: objectID, index: target.deployedCreatureIndex,
		noun:       target.binding.Creatures[target.deployedCreatureIndex].Noun,
		generation: target.generation, transportGeneration: target.transportGeneration,
		squad: target.squad, sourceObjectID: e.source.deployedObjectID,
		modifierID: util.HashID(name), kind: kind, rank: rank, stackCount: stackCount,
		duration: duration, expiresAt: e.runtime.now().Add(duration), revision: 1}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return nil, nil, fmt.Errorf("arenaBasicAllocate: %w", err)
	}
	status.instanceID = instanceID
	createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: status.objectID, ModifierGUID: status.modifierID, InstanceID: instanceID,
		DurationMilliseconds: uint32(duration.Milliseconds()), StackCount: stackCount,
		StartMilliseconds: timestamp, SourceID: status.sourceObjectID})
	if err != nil {
		return nil, nil, status.discard("arenaBasicCreate", err)
	}
	expiry := arenaHitStatusExpiry{status: status, revision: status.revision}
	status.cancel, err = e.runtime.registry.timer.Schedule(duration, expiry.expire)
	if err == nil && status.cancel == nil {
		err = errors.New("basic status expiry cancellation unavailable")
	}
	if err != nil {
		return nil, nil, status.discard("arenaBasicSchedule", err)
	}
	return status, createPacket, nil
}

func (e *arenaAbilityTargets) applyPummelStatusLocked(result zonenpc.DamageResult, rank uint32, timestamp uint64) error {
	if e.runtime.registry.timer == nil {
		return errors.New("arena pummel timer unavailable")
	}
	key, target, isFound := e.member(result.ObjectID)
	if !isFound || target.isHeroDebuffImmune(result.ObjectID) || target.isOverdriveActiveAt(e.runtime.now()) {
		return nil
	}
	markerKey := arenaHitStatusKey{modifierID: util.HashID("PummelDebuff")}
	previous := target.arenaHitStatuses[markerKey]
	if previous != nil && previous.matches(target) && e.runtime.now().Before(previous.expiresAt) {
		return nil
	}
	marker, markerPacket, err := e.prepareBasicStatusLocked(key, target, result.ObjectID,
		"PummelDebuff", arenaBasicPummel, rank, 1, 10*time.Second, timestamp)
	if err != nil {
		return fmt.Errorf("pummelMarker: %w", err)
	}
	silenceDuration := 2500 * time.Millisecond
	if rank >= 2 {
		silenceDuration = 1500 * time.Millisecond
	}
	silence, silencePacket, err := e.prepareBasicStatusLocked(key, target, result.ObjectID,
		"PummelSilence", sim.AbilityStatusKindSilence, rank, 1, silenceDuration, timestamp)
	if err != nil {
		markerErr := marker.discard("pummelMarkerRollback", err)
		return fmt.Errorf("pummelSilence: %w", markerErr)
	}
	if target.arenaHitStatuses == nil {
		target.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous != nil {
		previous.retire(&target)
	}
	if previousSilence := target.arenaHitStatuses[silence.key()]; previousSilence != nil {
		previousSilence.retire(&target)
	}
	target.arenaHitStatuses[marker.key()] = marker
	target.arenaHitStatuses[silence.key()] = silence
	attributePackets, err := silence.refresh(&target)
	if err != nil {
		silence.retire(&target)
		marker.retire(&target)
		e.runtime.registry.sessions[key] = target
		return fmt.Errorf("pummelAttribute: %w", err)
	}
	packets := [][]byte{markerPacket, silencePacket}
	packets = append(packets, attributePackets...)
	marker.queue(&target, packets)
	e.runtime.registry.sessions[key] = target
	return nil
}

func (e *arenaHitStatus) updateBasic(target *gameplayPeerSession, timestamp uint64, stackCount uint32) error {
	updatePacket, err := raknet.MarshalApplication(raknet.ModifierUpdatedMessage{
		TargetID: e.objectID, InstanceID: e.instanceID,
		StartMilliseconds: int64(timestamp), StackCount: stackCount})
	if err != nil {
		return fmt.Errorf("basicUpdatePacket: %w", err)
	}
	nextRevision := e.revision + 1
	expiry := arenaHitStatusExpiry{status: e, revision: nextRevision}
	cancel, err := e.runtime.registry.timer.Schedule(e.duration, expiry.expire)
	if err == nil && cancel == nil {
		err = errors.New("basic status expiry cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("basicUpdateSchedule: %w", err)
	}
	previousCancel := e.cancel
	e.stackCount = stackCount
	e.expiresAt = e.runtime.now().Add(e.duration)
	e.revision = nextRevision
	e.cancel = cancel
	packets := [][]byte{updatePacket}
	if e.kind == arenaBasicHaste {
		attributePackets, attributeErr := e.refresh(target)
		if attributeErr != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena basic haste attribute update failed target=%d: %v", e.objectID, attributeErr)
			}
		} else {
			packets = append(packets, attributePackets...)
		}
	}
	e.queue(target, packets)
	if previousCancel != nil {
		previousCancel()
	}
	return nil
}
