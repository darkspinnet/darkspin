package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

const fireTempestSupportName = "FireTempestSupport"
const energySentinelBasicName = "EnergySentinelBasic"
const heroHealingReduction = float32(0.5)

type heroHealingReductionKey struct {
	targetObjectID uint32
	modifierID     uint32
}

type heroHealingReductionRun struct {
	modifier       *campaignNPCModifierRun
	key            heroHealingReductionKey
	sourceObjectID uint32
	targetObjectID uint32
	revision       uint64
	expiresAt      time.Time
	cancel         raknet.CancelSchedule
}

type heroHealingReductionExpiry struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	revision   uint64
	run        *heroHealingReductionRun
}

// Generic debuff retirement must also retire its feature owner. Otherwise a
// refresh can reuse a released instance and an obsolete expiry can remove a
// later allocation with the same instance ID. The caller holds the registry lock.
func (e *gameplayPeerSession) forgetHeroHealingReduction(modifier *campaignNPCModifierRun) {
	for key, run := range e.heroHealingReductions {
		if run == nil || run.modifier != modifier {
			continue
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		if e.zone != nil && e.zone.NPCs() != nil {
			e.zone.NPCs().ClearHealingReduction(run.targetObjectID, run.expiresAt)
		}
		delete(e.heroHealingReductions, key)
	}
}

func (e campaignDamageRuntime) stopHeroHealingReductions(
	sessionKey string, generation uint64, targetObjectID uint32,
) ([][]byte, error) {
	if sessionKey == "" || generation == 0 || targetObjectID == 0 {
		return nil, errors.New("healing reduction cleanup invalid")
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
	runs := make([]*heroHealingReductionRun, 0)
	for key, run := range peerSession.heroHealingReductions {
		if key.targetObjectID != targetObjectID || run == nil {
			continue
		}
		peerSession.zone.Effect().Remove(run.modifier.instanceID)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		runs = append(runs, run)
	}
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(runs))
	for index, run := range runs {
		isCreated, err := run.modifier.release(e.npc.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("healingReductionRelease[%d]: %w", index, err)
		}
		if !isCreated {
			continue
		}
		packet, err := effectraknet.ModifierDelete(
			targetObjectID, run.modifier.instanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("healingReductionDelete[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroHealingReductionExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.heroHealingReductions[e.run.key] == e.run &&
		e.run.revision == e.revision
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	// This expiry owns the deletion publication; do not cancel its own group.
	e.run.cancel = nil
	peerSession.zone.Effect().Remove(e.run.modifier.instanceID)
	peerSession.untrackCampaignNPCModifier(e.run.modifier)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	isCreated, err := e.run.modifier.release(e.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("healingReductionRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.run.targetObjectID, e.run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("healingReductionDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignDamageRuntime) applyHeroHealingReduction(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, definition sim.AbilityDefinition,
	targetObjectID uint32,
) ([][]byte, error) {
	isSupportedAbility := definition.Name == fireTempestSupportName ||
		definition.Name == energySentinelBasicName
	if !isSupportedAbility || definition.RootModifierID == 0 ||
		definition.StatusDuration <= 0 || targetObjectID == 0 {
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
	if peerSession.heroHealingReductions == nil {
		peerSession.heroHealingReductions = make(
			map[heroHealingReductionKey]*heroHealingReductionRun,
		)
	}
	key := heroHealingReductionKey{
		targetObjectID: targetObjectID,
		modifierID:     definition.RootModifierID,
	}
	run := peerSession.heroHealingReductions[key]
	isNew := run == nil
	if isNew {
		modifier, err := newCampaignNPCModifierRun(e.npc.modifierPool)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("healingReductionRun: %w", err)
		}
		run = &heroHealingReductionRun{
			modifier: modifier, key: key, targetObjectID: targetObjectID,
		}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			e.registry.mutex.Unlock()
			isCreated, releaseErr := modifier.release(e.npc.modifierPool)
			if isCreated {
				err = errors.Join(err, errors.New("unpublished healing modifier was created"))
			}
			return nil, fmt.Errorf(
				"healingReductionTrack: %w", errors.Join(err, releaseErr),
			)
		}
	}
	previousRecord := run.modifier.record
	previousRevision := run.revision
	previousExpiresAt := run.expiresAt
	previousCancel := run.cancel
	revision := previousRevision + 1
	expiresAt := e.npc.now().Add(definition.StatusDuration)
	err := peerSession.zone.NPCs().ApplyHealingReduction(
		targetObjectID, heroHealingReduction, expiresAt,
	)
	if err != nil {
		if isNew {
			peerSession.untrackCampaignNPCModifier(run.modifier)
			isCreated, releaseErr := run.modifier.release(e.npc.modifierPool)
			if isCreated {
				err = errors.Join(err, errors.New("unpublished healing modifier was created"))
			}
			err = errors.Join(err, releaseErr)
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingReductionApply: %w", err)
	}
	record := zoneeffect.Modifier{
		InstanceID: run.modifier.instanceID, GUID: definition.RootModifierID,
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		Rank: 1, Duration: definition.StatusDuration, ExpiresAt: expiresAt,
		Kind: zoneeffect.ModifierKindDebuff, InitiatorObject: sourceObjectID,
		StackCount: 1, HealingReduction: heroHealingReduction,
	}
	if isNew {
		err = peerSession.zone.Effect().Put(record)
	} else {
		err = peerSession.zone.Effect().Update(record)
	}
	if err != nil {
		peerSession.zone.NPCs().ClearHealingReduction(targetObjectID, expiresAt)
		if isNew {
			peerSession.untrackCampaignNPCModifier(run.modifier)
			isCreated, releaseErr := run.modifier.release(e.npc.modifierPool)
			if isCreated {
				err = errors.Join(err, errors.New("unpublished healing modifier was created"))
			}
			err = errors.Join(err, releaseErr)
		} else {
			restoreErr := peerSession.zone.NPCs().ApplyHealingReduction(
				targetObjectID, heroHealingReduction, previousExpiresAt,
			)
			if restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("healingReductionRestore: %w", restoreErr))
			}
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingReductionInventory: %w", err)
	}
	run.sourceObjectID = sourceObjectID
	run.revision = revision
	run.expiresAt = expiresAt
	run.modifier.record = record
	peerSession.heroHealingReductions[key] = run
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		ModifierID: definition.RootModifierID, InstanceID: run.modifier.instanceID,
		StackCount: 1, Duration: definition.StatusDuration, Timestamp: timestamp,
	})
	if err != nil {
		rollbackErr := e.rollbackHeroHealingReduction(
			sessionKey, generation, run, revision, isNew, previousRecord,
			previousRevision, previousExpiresAt, previousCancel,
		)
		return nil, fmt.Errorf("healingReductionCreate: %w", errors.Join(err, rollbackErr))
	}
	expiry := heroHealingReductionExpiry{
		runtime: e, sessionKey: sessionKey, generation: generation,
		revision: revision, run: run,
	}
	cancel, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: definition.StatusDuration, Produce: expiry.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		rollbackErr := e.rollbackHeroHealingReduction(
			sessionKey, generation, run, revision, isNew, previousRecord,
			previousRevision, previousExpiresAt, previousCancel,
		)
		return nil, fmt.Errorf("healingReductionSchedule: %w", errors.Join(scheduleErr, rollbackErr))
	}
	e.registry.mutex.Lock()
	latest, isLatestFound := e.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.heroHealingReductions[key] == run &&
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

func (e campaignDamageRuntime) rollbackHeroHealingReduction(
	sessionKey string, generation uint64, run *heroHealingReductionRun,
	revision uint64, isNew bool, previousRecord zoneeffect.Modifier, previousRevision uint64,
	previousExpiresAt time.Time, previousCancel raknet.CancelSchedule,
) error {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.heroHealingReductions[run.key] == run && run.revision == revision
	var restoreErr error
	if isCurrent {
		peerSession.zone.NPCs().ClearHealingReduction(
			run.targetObjectID, run.expiresAt,
		)
	}
	if isCurrent && isNew {
		peerSession.zone.Effect().Remove(run.modifier.instanceID)
		peerSession.untrackCampaignNPCModifier(run.modifier)
	}
	if isCurrent && !isNew {
		run.modifier.record = previousRecord
		run.sourceObjectID = previousRecord.SourceObjectID
		run.revision = previousRevision
		run.expiresAt = previousExpiresAt
		run.cancel = previousCancel
		healingErr := peerSession.zone.NPCs().ApplyHealingReduction(
			run.targetObjectID, heroHealingReduction, previousExpiresAt,
		)
		if healingErr != nil {
			restoreErr = fmt.Errorf("healingReductionRestore: %w", healingErr)
		}
		inventoryErr := peerSession.zone.Effect().Update(previousRecord)
		if inventoryErr != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("healingReductionRestoreInventory: %w", inventoryErr))
		}
	}
	if isCurrent {
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	if isNew && isCurrent {
		isCreated, releaseErr := run.modifier.release(e.npc.modifierPool)
		if releaseErr != nil {
			return fmt.Errorf("healingReductionRollbackRelease: %w", releaseErr)
		}
		if isCreated {
			return errors.New("unpublished healing modifier was created")
		}
	}
	if restoreErr != nil {
		return fmt.Errorf("healingReductionRollback: %w", restoreErr)
	}
	return nil
}
