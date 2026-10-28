package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

const missileFlakDuration = 10 * time.Second

type missileFlakRun struct {
	runtime             campaignAbilityCommandRuntime
	sessionKey          string
	generation          uint64
	transportGeneration uint64
	gameID              uint32
	squad               *squad.Session
	noun                uint32
	isReleased          bool
	effectSlot          uint8
	isEffectAllocated   bool
	creatureIndex       uint32
	objectID            uint32
	instanceID          uint32
	stackCount          uint32
	attackSpeed         float32
	cancel              raknet.CancelSchedule
}

func (e *missileFlakRun) matches(member gameplayPeerSession) bool {
	return e != nil && member.generation == e.generation && member.transportGeneration == e.transportGeneration &&
		member.binding.GameID == e.gameID && member.squad == e.squad && member.deployedObjectID == e.objectID &&
		member.deployedCreatureIndex == e.creatureIndex && e.creatureIndex < uint32(len(member.binding.Creatures)) &&
		member.binding.Creatures[e.creatureIndex].Noun == e.noun
}

func (e *missileFlakRun) release(pool *modifierPool) error {
	if e == nil {
		return nil
	}
	var effectErr error
	if e.isEffectAllocated && e.runtime.effectPool != nil {
		isReleased := e.runtime.effectPool.Release(e.objectID, e.effectSlot)
		e.isEffectAllocated = false
		if !isReleased {
			effectErr = errors.New("flak effect reservation unavailable")
		}
	}
	if e.isReleased || pool == nil {
		return effectErr
	}
	e.isReleased = true
	err := pool.Release(e.instanceID)
	if err != nil {
		return fmt.Errorf("flakRelease: %w", errors.Join(err, effectErr))
	}
	return effectErr
}

func (e *missileFlakRun) queue(member *gameplayPeerSession, packets [][]byte) {
	member.queuePackets(packets)
	for key, candidate := range e.runtime.registry.sessions {
		if key == e.sessionKey || candidate.isRejoinPending {
			continue
		}
		if member.binding.Mode == game.ModeArena {
			if candidate.binding.Mode != game.ModeArena || candidate.binding.GameID != e.gameID {
				continue
			}
		} else if member.zone == nil || candidate.zone != member.zone || candidate.binding.Mode == game.ModeArena {
			continue
		}
		candidate.queuePackets(packets)
		e.runtime.registry.sessions[key] = candidate
	}
}

func (e *missileFlakRun) attributes(member gameplayPeerSession) ([]byte, error) {
	creature := e.runtime.registry.projectPassiveCreature(member, member.deployedCreatureIndex, e.runtime.now())
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.objectID, Value: map[uint8]float32{23: creature.TimingProfile.AttackSpeed + member.enemyAttackSpeed() - member.arenaSphereAttackSpeed()},
	})
	if err != nil {
		return nil, fmt.Errorf("flakAttribute: %w", err)
	}
	return packet, nil
}

type missileFlakExpiry struct {
	runtime    campaignAbilityCommandRuntime
	sessionKey string
	generation uint64
	run        *missileFlakRun
}

func (e *missileFlakRun) cancelSchedule() {
	if e == nil || e.cancel == nil {
		return
	}
	e.cancel()
	e.cancel = nil
}

func (e *missileFlakRun) remove(peerSession *gameplayPeerSession) {
	if e == nil || peerSession == nil ||
		!e.matches(*peerSession) {
		return
	}
	creature := &peerSession.binding.Creatures[e.creatureIndex]
	creature.TimingProfile.AttackSpeed -= e.attackSpeed
	e.attackSpeed = 0
}

func (e missileFlakExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.missileFlakRun == e.run && e.run.matches(peerSession)
	if !isCurrent {
		e.run.cancel = nil
		err := e.run.release(e.runtime.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("flakStale: %w", err)
		}
		return nil, nil
	}
	e.run.cancel = nil
	packet, err := stopMissileFlak(&peerSession, e.runtime.modifierPool)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	if err != nil {
		return nil, fmt.Errorf("missileFlakExpire: %w", err)
	}
	if len(packet) == 0 {
		return nil, nil
	}
	return [][]byte{packet}, nil
}

func (r campaignAbilityCommandRuntime) applyMissileFlakLocked(
	packet raknet.Packet, peerSession *gameplayPeerSession, sessionKey string,
	generation uint64, rank int32, destroyedCount uint32, timestamp uint64,
) ([][]byte, error) {
	if rank != 7 && rank != 8 {
		return nil, nil
	}
	if peerSession == nil || r.modifierPool == nil || r.registry == nil ||
		(packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil) {
		return nil, errors.New("missile Flak runtime unavailable")
	}
	if peerSession.generation != generation || peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) || peerSession.deployedHitPoint() <= 0 {
		return nil, errors.New("missile Flak source unavailable")
	}
	packets := make([][]byte, 0, 2)
	maximumStack := uint32(10)
	attackSpeedPerStack := float32(0.05)
	if rank == 8 {
		maximumStack = 4
		attackSpeedPerStack = 0.12
	}
	stackCount := min(destroyedCount, maximumStack)
	if stackCount == 0 {
		deleted, err := stopMissileFlak(peerSession, r.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("flakEmpty: %w", err)
		}
		if len(deleted) > 0 {
			packets = append(packets, deleted)
		}
		return packets, nil
	}
	if r.effectPool == nil {
		return nil, errors.New("missile Flak effect pool unavailable")
	}
	instanceID, err := r.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("missileFlakAllocate: %w", err)
	}
	run := &missileFlakRun{
		runtime: r, sessionKey: sessionKey, generation: generation,
		transportGeneration: peerSession.transportGeneration, gameID: peerSession.binding.GameID,
		squad: peerSession.squad, noun: peerSession.binding.Creatures[peerSession.deployedCreatureIndex].Noun,
		creatureIndex: peerSession.deployedCreatureIndex,
		objectID:      peerSession.deployedObjectID,
		instanceID:    instanceID,
		stackCount:    stackCount,
		attackSpeed:   float32(stackCount) * attackSpeedPerStack,
	}
	run.effectSlot, run.isEffectAllocated = r.effectPool.Allocate(run.objectID)
	if !run.isEffectAllocated {
		return nil, fmt.Errorf("flakEffectAllocate: %w", errors.Join(errors.New("attachment slot unavailable"), run.release(r.modifierPool)))
	}
	createdPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: run.objectID,
		ModifierGUID: util.HashID(
			"MissileTempest_Support_FlakUpgradeModifier",
		),
		InstanceID: instanceID, DurationMilliseconds: uint32(missileFlakDuration.Milliseconds()),
		StackCount: stackCount, StartMilliseconds: timestamp, SourceID: run.objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("missileFlakCreate: %w", errors.Join(err, run.release(r.modifierPool)))
	}
	effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: run.effectSlot + 1, IsForceAttached: true,
		Asset: util.HashID("status_weapon_hasted.ServerEventDef"), ObjectID: run.objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("flakEffectCreate: %w", errors.Join(err, run.release(r.modifierPool)))
	}
	expiry := missileFlakExpiry{
		runtime: r, sessionKey: sessionKey, generation: generation, run: run,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: missileFlakDuration, Produce: expiry.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("flak cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("missileFlakSchedule: %w", errors.Join(err, run.release(r.modifierPool)))
	}
	run.cancel = cancel
	deleted, err := stopMissileFlak(peerSession, r.modifierPool)
	if err != nil {
		run.cancelSchedule()
		return nil, fmt.Errorf("flakPrevious: %w", errors.Join(err, run.release(r.modifierPool)))
	}
	if len(deleted) > 0 {
		packets = append(packets, deleted)
	}
	peerSession.binding.Creatures[run.creatureIndex].TimingProfile.AttackSpeed += run.attackSpeed
	peerSession.missileFlakRun = run
	packets = append(packets, createdPacket, effectPacket)
	if peerSession.binding.Mode == game.ModeArena {
		attribute, attributeErr := run.attributes(*peerSession)
		if attributeErr != nil {
			cleanup, cleanupErr := stopMissileFlak(peerSession, r.modifierPool)
			if len(cleanup) > 0 && r.logger != nil {
				r.logger.Printf("Flak rollback returned unexpected Arena packet")
			}
			return nil, fmt.Errorf("flakAttribute: %w", errors.Join(attributeErr, cleanupErr))
		}
		packets = append(packets, attribute)
		run.queue(peerSession, packets)
		return nil, nil
	}
	return packets, nil
}

func stopMissileFlak(
	peerSession *gameplayPeerSession, modifierPool *modifierPool,
) ([]byte, error) {
	if peerSession == nil || peerSession.missileFlakRun == nil {
		return nil, nil
	}
	run := peerSession.missileFlakRun
	isSameActor := run.matches(*peerSession)
	var effectPacket []byte
	var effectErr error
	if isSameActor && run.isEffectAllocated {
		effectPacket, effectErr = raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: run.effectSlot + 1, IsRemovalRequested: true, IsHardStop: true, ObjectID: run.objectID,
		})
	}
	run.cancelSchedule()
	run.remove(peerSession)
	peerSession.missileFlakRun = nil
	releaseErr := errors.Join(effectErr, run.release(modifierPool))
	if releaseErr != nil {
		releaseErr = fmt.Errorf("flakStopRelease: %w", releaseErr)
	}
	if !isSameActor {
		return nil, releaseErr
	}
	packet, err := effectraknet.ModifierDelete(run.objectID, run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("missileFlakStop: %w", errors.Join(err, releaseErr))
	}
	if peerSession.binding.Mode == game.ModeArena {
		attribute, attributeErr := run.attributes(*peerSession)
		if attributeErr != nil {
			return nil, fmt.Errorf("flakClearAttribute: %w", errors.Join(attributeErr, releaseErr))
		}
		packets := [][]byte{packet, attribute}
		if len(effectPacket) > 0 {
			packets = append(packets, effectPacket)
		}
		run.queue(peerSession, packets)
		return nil, releaseErr
	}
	if len(effectPacket) > 0 {
		run.queue(peerSession, [][]byte{effectPacket})
	}
	return packet, releaseErr
}
