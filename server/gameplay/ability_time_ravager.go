package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

const timeRavagerBasicName = "TimeRavagerBasic"
const timeRavagerSlowDuration = 5 * time.Second
const timeRavagerSlowMaximumStack = uint32(5)
const timeRavagerMovementAdjustmentPerStack = float32(-0.10)
const timeRavagerAttackAdjustmentPerStack = float32(-0.05)

type heroTimeRavagerSlowRun struct {
	modifier       *campaignNPCModifierRun
	targetObjectID uint32
	stackCount     uint32
	revision       uint64
	expiresAt      time.Time
	cancel         raknet.CancelSchedule
}

type heroTimeRavagerSlowExpiry struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	revision   uint64
	run        *heroTimeRavagerSlowRun
}

func (e heroTimeRavagerSlowExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.heroTimeRavagerSlows[e.run.targetObjectID] == e.run &&
		e.run.revision == e.revision
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.zone.NPCs().ClearSlow(e.run.targetObjectID, e.run.expiresAt)
	peerSession.zone.Effect().Remove(e.run.modifier.instanceID)
	peerSession.untrackCampaignNPCModifier(e.run.modifier)
	delete(peerSession.heroTimeRavagerSlows, e.run.targetObjectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	isCreated, err := e.run.modifier.release(e.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("timeRavagerRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.run.targetObjectID, e.run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("timeRavagerDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignDamageRuntime) applyTimeRavagerSlow(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, result zoneability.AreaResult,
) ([][]byte, error) {
	if result.Definition.Name != timeRavagerBasicName ||
		result.Damage.IsDamageImmune || result.Damage.IsDefeated ||
		result.Damage.Damage <= 0 {
		return nil, nil
	}

	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.zone.Effect() != nil
	if !isCurrent {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.heroTimeRavagerSlows == nil {
		peerSession.heroTimeRavagerSlows = make(map[uint32]*heroTimeRavagerSlowRun)
	}
	run := peerSession.heroTimeRavagerSlows[result.Damage.ObjectID]
	isNew := run == nil
	if isNew {
		modifier, err := newCampaignNPCModifierRun(e.npc.modifierPool)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("timeRavagerRun: %w", err)
		}
		run = &heroTimeRavagerSlowRun{
			modifier: modifier, targetObjectID: result.Damage.ObjectID,
		}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			e.registry.mutex.Unlock()
			wasCreated, releaseErr := modifier.release(e.npc.modifierPool)
			if wasCreated && e.logger != nil {
				e.logger.Printf("Time Ravager untracked modifier was unexpectedly active")
			}
			return nil, fmt.Errorf("timeRavagerTrack: %w", errors.Join(err, releaseErr))
		}
	}

	previousStackCount := run.stackCount
	previousRevision := run.revision
	previousExpiresAt := run.expiresAt
	previousCancel := run.cancel
	run.stackCount = min(timeRavagerSlowMaximumStack, run.stackCount+1)
	run.revision++
	run.expiresAt = e.npc.now().Add(timeRavagerSlowDuration)
	movementAdjustment := float32(run.stackCount) * timeRavagerMovementAdjustmentPerStack
	attackAdjustment := float32(run.stackCount) * timeRavagerAttackAdjustmentPerStack
	err := peerSession.zone.NPCs().ApplySlow(
		run.targetObjectID, run.expiresAt,
		1+movementAdjustment, 1+attackAdjustment,
	)
	if err != nil {
		if isNew {
			peerSession.untrackCampaignNPCModifier(run.modifier)
		} else {
			run.stackCount = previousStackCount
			run.revision = previousRevision
			run.expiresAt = previousExpiresAt
			run.cancel = previousCancel
		}
		e.registry.mutex.Unlock()
		if isNew {
			wasCreated, releaseErr := run.modifier.release(e.npc.modifierPool)
			if releaseErr != nil && e.logger != nil {
				e.logger.Printf("Time Ravager apply rollback release failed: %v", releaseErr)
			}
			if wasCreated && e.logger != nil {
				e.logger.Printf("Time Ravager apply rollback released an active modifier")
			}
		}
		return nil, fmt.Errorf("timeRavagerApply: %w", err)
	}
	run.modifier.record = zoneeffect.Modifier{
		InstanceID:     run.modifier.instanceID,
		GUID:           util.HashID("TimeRavagerBasicModifier"),
		SourceObjectID: sourceObjectID, TargetObjectID: run.targetObjectID,
		Rank: 1, Duration: timeRavagerSlowDuration,
		Kind: zoneeffect.ModifierKindDebuff, InitiatorObject: sourceObjectID,
		StackCount: run.stackCount, AttackSpeed: attackAdjustment,
		MovementSpeedBuff: movementAdjustment,
	}
	if isNew {
		err = peerSession.zone.Effect().Put(run.modifier.record)
	} else {
		err = peerSession.zone.Effect().Update(run.modifier.record)
	}
	if err != nil {
		peerSession.zone.NPCs().ClearSlow(run.targetObjectID, run.expiresAt)
		if isNew {
			peerSession.untrackCampaignNPCModifier(run.modifier)
		} else {
			run.stackCount = previousStackCount
			run.revision = previousRevision
			run.expiresAt = previousExpiresAt
			run.cancel = previousCancel
			movementAdjustment = float32(previousStackCount) *
				timeRavagerMovementAdjustmentPerStack
			attackAdjustment = float32(previousStackCount) *
				timeRavagerAttackAdjustmentPerStack
			restoreErr := peerSession.zone.NPCs().ApplySlow(
				run.targetObjectID, previousExpiresAt,
				1+movementAdjustment, 1+attackAdjustment,
			)
			if restoreErr != nil && e.logger != nil {
				e.logger.Printf("Time Ravager inventory rollback slow restore failed: %v", restoreErr)
			}
			run.modifier.record.StackCount = previousStackCount
			run.modifier.record.AttackSpeed = attackAdjustment
			run.modifier.record.MovementSpeedBuff = movementAdjustment
			restoreErr = peerSession.zone.Effect().Update(run.modifier.record)
			if restoreErr != nil && e.logger != nil {
				e.logger.Printf("Time Ravager inventory rollback modifier restore failed: %v", restoreErr)
			}
		}
		e.registry.mutex.Unlock()
		if isNew {
			wasCreated, releaseErr := run.modifier.release(e.npc.modifierPool)
			if releaseErr != nil && e.logger != nil {
				e.logger.Printf("Time Ravager inventory rollback release failed: %v", releaseErr)
			}
			if wasCreated && e.logger != nil {
				e.logger.Printf("Time Ravager inventory rollback released an active modifier")
			}
		}
		return nil, fmt.Errorf("timeRavagerInventory: %w", err)
	}
	peerSession.heroTimeRavagerSlows[run.targetObjectID] = run
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: sourceObjectID, TargetObjectID: run.targetObjectID,
		ModifierID: run.modifier.record.GUID, InstanceID: run.modifier.instanceID,
		StackCount: run.stackCount, Duration: timeRavagerSlowDuration,
		Timestamp: timestamp,
	})
	if err != nil {
		e.rollbackTimeRavagerSlow(
			sessionKey, generation, run, isNew, previousStackCount,
			previousRevision, previousExpiresAt, previousCancel,
		)
		return nil, fmt.Errorf("timeRavagerCreate: %w", err)
	}
	expiry := heroTimeRavagerSlowExpiry{
		runtime: e, sessionKey: sessionKey, generation: generation,
		revision: run.revision, run: run,
	}
	cancel, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: timeRavagerSlowDuration, Produce: expiry.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		e.rollbackTimeRavagerSlow(
			sessionKey, generation, run, isNew, previousStackCount,
			previousRevision, previousExpiresAt, previousCancel,
		)
		return nil, fmt.Errorf("timeRavagerSchedule: %w", scheduleErr)
	}
	e.registry.mutex.Lock()
	latest, isLatestFound := e.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.heroTimeRavagerSlows[run.targetObjectID] == run &&
		run.revision == expiry.revision
	if isLatest {
		run.cancel = cancel
		e.registry.sessions[sessionKey] = latest
	}
	e.registry.mutex.Unlock()
	if !isLatest {
		cancel()
		return nil, nil
	}
	if previousCancel != nil {
		previousCancel()
	}
	if isNew {
		run.modifier.create()
	}
	return [][]byte{createPacket}, nil
}

func (e campaignDamageRuntime) rollbackTimeRavagerSlow(
	sessionKey string, generation uint64, run *heroTimeRavagerSlowRun,
	isNew bool, previousStackCount uint32, previousRevision uint64,
	previousExpiresAt time.Time, previousCancel raknet.CancelSchedule,
) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.heroTimeRavagerSlows[run.targetObjectID] == run
	if isCurrent && isNew {
		peerSession.zone.NPCs().ClearSlow(run.targetObjectID, run.expiresAt)
		peerSession.zone.Effect().Remove(run.modifier.instanceID)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		delete(peerSession.heroTimeRavagerSlows, run.targetObjectID)
	}
	if isCurrent && !isNew {
		peerSession.zone.NPCs().ClearSlow(run.targetObjectID, run.expiresAt)
		run.stackCount = previousStackCount
		run.revision = previousRevision
		run.expiresAt = previousExpiresAt
		run.cancel = previousCancel
		movementAdjustment := float32(previousStackCount) *
			timeRavagerMovementAdjustmentPerStack
		attackAdjustment := float32(previousStackCount) *
			timeRavagerAttackAdjustmentPerStack
		restoreErr := peerSession.zone.NPCs().ApplySlow(
			run.targetObjectID, previousExpiresAt,
			1+movementAdjustment, 1+attackAdjustment,
		)
		if restoreErr != nil && e.logger != nil {
			e.logger.Printf("Time Ravager packet rollback slow restore failed: %v", restoreErr)
		}
		run.modifier.record.StackCount = previousStackCount
		run.modifier.record.AttackSpeed = attackAdjustment
		run.modifier.record.MovementSpeedBuff = movementAdjustment
		restoreErr = peerSession.zone.Effect().Update(run.modifier.record)
		if restoreErr != nil && e.logger != nil {
			e.logger.Printf("Time Ravager packet rollback modifier restore failed: %v", restoreErr)
		}
	}
	if isCurrent {
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	if isNew && isCurrent {
		wasCreated, releaseErr := run.modifier.release(e.npc.modifierPool)
		if releaseErr != nil && e.logger != nil {
			e.logger.Printf("Time Ravager packet rollback release failed: %v", releaseErr)
		}
		if wasCreated && e.logger != nil {
			e.logger.Printf("Time Ravager packet rollback released an active modifier")
		}
	}
}
