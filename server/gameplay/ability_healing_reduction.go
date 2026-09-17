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
		if run.cancel != nil {
			run.cancel()
		}
		peerSession.zone.NPCs().ClearHealingReduction(
			targetObjectID, run.expiresAt,
		)
		peerSession.zone.Effect().Remove(run.modifier.instanceID)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		delete(peerSession.heroHealingReductions, key)
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
	peerSession.zone.NPCs().ClearHealingReduction(
		e.run.targetObjectID, e.run.expiresAt,
	)
	peerSession.zone.Effect().Remove(e.run.modifier.instanceID)
	peerSession.untrackCampaignNPCModifier(e.run.modifier)
	delete(peerSession.heroHealingReductions, e.run.key)
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
			_, releaseErr := modifier.release(e.npc.modifierPool)
			return nil, fmt.Errorf(
				"healingReductionTrack: %w", errors.Join(err, releaseErr),
			)
		}
	}
	previousRecord := run.modifier.record
	previousRevision := run.revision
	previousExpiresAt := run.expiresAt
	previousCancel := run.cancel
	run.sourceObjectID = sourceObjectID
	run.revision++
	run.expiresAt = e.npc.now().Add(definition.StatusDuration)
	err := peerSession.zone.NPCs().ApplyHealingReduction(
		targetObjectID, heroHealingReduction, run.expiresAt,
	)
	if err != nil {
		if isNew {
			peerSession.untrackCampaignNPCModifier(run.modifier)
			_, _ = run.modifier.release(e.npc.modifierPool)
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingReductionApply: %w", err)
	}
	run.modifier.record = zoneeffect.Modifier{
		InstanceID: run.modifier.instanceID, GUID: definition.RootModifierID,
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		Rank: 1, Duration: definition.StatusDuration,
		Kind: zoneeffect.ModifierKindDebuff, InitiatorObject: sourceObjectID,
		StackCount: 1, HealingReduction: heroHealingReduction,
	}
	if isNew {
		err = peerSession.zone.Effect().Put(run.modifier.record)
	} else {
		err = peerSession.zone.Effect().Update(run.modifier.record)
	}
	if err != nil {
		peerSession.zone.NPCs().ClearHealingReduction(targetObjectID, run.expiresAt)
		if isNew {
			peerSession.untrackCampaignNPCModifier(run.modifier)
			_, _ = run.modifier.release(e.npc.modifierPool)
		} else {
			run.modifier.record = previousRecord
		}
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("healingReductionInventory: %w", err)
	}
	peerSession.heroHealingReductions[key] = run
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		ModifierID: definition.RootModifierID, InstanceID: run.modifier.instanceID,
		StackCount: 1, Duration: definition.StatusDuration, Timestamp: timestamp,
	})
	if err != nil {
		e.rollbackHeroHealingReduction(
			sessionKey, generation, run, isNew, previousRecord,
			previousRevision, previousExpiresAt, previousCancel,
		)
		return nil, fmt.Errorf("healingReductionCreate: %w", err)
	}
	expiry := heroHealingReductionExpiry{
		runtime: e, sessionKey: sessionKey, generation: generation,
		revision: run.revision, run: run,
	}
	cancel, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: definition.StatusDuration, Produce: expiry.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		e.rollbackHeroHealingReduction(
			sessionKey, generation, run, isNew, previousRecord,
			previousRevision, previousExpiresAt, previousCancel,
		)
		return nil, fmt.Errorf("healingReductionSchedule: %w", scheduleErr)
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
	isNew bool, previousRecord zoneeffect.Modifier, previousRevision uint64,
	previousExpiresAt time.Time, previousCancel raknet.CancelSchedule,
) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.heroHealingReductions[run.key] == run
	if isCurrent {
		peerSession.zone.NPCs().ClearHealingReduction(
			run.targetObjectID, run.expiresAt,
		)
	}
	if isCurrent && isNew {
		peerSession.zone.Effect().Remove(run.modifier.instanceID)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		delete(peerSession.heroHealingReductions, run.key)
	}
	if isCurrent && !isNew {
		run.modifier.record = previousRecord
		run.revision = previousRevision
		run.expiresAt = previousExpiresAt
		run.cancel = previousCancel
		_ = peerSession.zone.NPCs().ApplyHealingReduction(
			run.targetObjectID, heroHealingReduction, previousExpiresAt,
		)
		_ = peerSession.zone.Effect().Update(previousRecord)
	}
	if isCurrent {
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	if isNew && isCurrent {
		_, _ = run.modifier.release(e.npc.modifierPool)
	}
}
