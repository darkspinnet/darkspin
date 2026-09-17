package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type heroTauntRun struct {
	modifier       *campaignNPCModifierRun
	sourceObjectID uint32
	targetObjectID uint32
	revision       uint64
	expiresAt      time.Time
	cancel         raknet.CancelSchedule
}

type heroTauntExpiry struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	revision   uint64
	timestamp  uint64
	run        *heroTauntRun
}

func (e heroTauntExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.heroTaunts[e.run.targetObjectID] == e.run &&
		e.run.revision == e.revision
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	acquired, isExpired, err := peerSession.zone.ExpireHeroTaunt(
		e.run.targetObjectID, e.run.sourceObjectID, e.run.expiresAt,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTauntExpire: %w", err)
	}
	peerSession.zone.Effect().Remove(e.run.modifier.instanceID)
	peerSession.untrackCampaignNPCModifier(e.run.modifier)
	delete(peerSession.heroTaunts, e.run.targetObjectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	isCreated, err := e.run.modifier.release(e.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("heroTauntRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	deletePacket, err := effectraknet.ModifierDelete(
		e.run.targetObjectID, e.run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("heroTauntDelete: %w", err)
	}
	packets := [][]byte{deletePacket}
	if !isExpired || len(acquired) == 0 {
		return packets, nil
	}
	plans := make([]zonenpc.SpawnPlan, 0, len(acquired))
	for _, npc := range acquired {
		plans = append(plans, npc.Plan)
	}
	actionPackets, err := e.runtime.npc.scheduleFirstActions(
		e.packet, e.sessionKey, e.generation, plans,
		e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("heroTauntReacquire: %w", err)
	}
	return append(packets, actionPackets...), nil
}

func (e campaignDamageRuntime) applyAcceptedHitTaunts(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, definition sim.AbilityDefinition,
	results []zoneability.AreaResult,
) ([][]byte, error) {
	if definition.StatusKind != sim.AbilityStatusKindTaunt ||
		definition.RootModifierID == 0 || definition.StatusDuration <= 0 {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, result := range results {
		if result.Damage.IsDamageImmune || result.Damage.IsDefeated ||
			result.Damage.Damage <= 0 {
			continue
		}
		targetPackets, err := e.applyHeroTaunt(
			packet, sessionKey, generation, sourceObjectID, timestamp,
			definition, result.Damage.ObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("acceptedHitTaunt[%d]: %w", result.Damage.ObjectID, err)
		}
		packets = append(packets, targetPackets...)
	}
	return packets, nil
}

func (e campaignDamageRuntime) applyHeroTaunt(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, definition sim.AbilityDefinition,
	targetObjectID uint32,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.zone.Effect() != nil
	if !isCurrent {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.heroTaunts == nil {
		peerSession.heroTaunts = make(map[uint32]*heroTauntRun)
	}
	run := peerSession.heroTaunts[targetObjectID]
	isNew := run == nil
	if isNew {
		modifier, err := newCampaignNPCModifierRun(e.npc.modifierPool)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroTauntRun: %w", err)
		}
		run = &heroTauntRun{modifier: modifier, targetObjectID: targetObjectID}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			e.registry.mutex.Unlock()
			_, releaseErr := modifier.release(e.npc.modifierPool)
			return nil, fmt.Errorf("heroTauntTrack: %w", errors.Join(err, releaseErr))
		}
	}
	previousRecord := run.modifier.record
	previousSourceObjectID := run.sourceObjectID
	previousRevision := run.revision
	previousExpiresAt := run.expiresAt
	previousCancel := run.cancel
	run.sourceObjectID = sourceObjectID
	run.revision++
	run.expiresAt = e.npc.now().Add(definition.StatusDuration)
	var err error
	var acquired []zonenpc.Snapshot
	if sourceObjectID == peerSession.deployedObjectID {
		acquired, err = peerSession.zone.TauntHeroTargets(
			peerSession.binding.UserID, generation, sourceObjectID,
			[]uint32{targetObjectID}, run.expiresAt,
		)
	} else {
		acquired, err = peerSession.zone.TauntCompanionTargets(
			peerSession.binding.UserID, generation, sourceObjectID,
			[]uint32{targetObjectID}, run.expiresAt,
		)
	}
	if err != nil {
		e.rollbackHeroTauntLocked(
			&peerSession, run, isNew, previousRecord, previousSourceObjectID,
			previousRevision, previousExpiresAt, previousCancel,
		)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTauntApply: %w", err)
	}
	run.modifier.record = zoneeffect.Modifier{
		InstanceID: run.modifier.instanceID, GUID: definition.RootModifierID,
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		Rank: 1, Duration: definition.StatusDuration,
		Kind: zoneeffect.ModifierKindDebuff, InitiatorObject: sourceObjectID,
		StackCount: 1,
	}
	if isNew {
		err = peerSession.zone.Effect().Put(run.modifier.record)
	} else {
		err = peerSession.zone.Effect().Update(run.modifier.record)
	}
	if err != nil {
		peerSession.zone.NPCs().ClearTaunt(targetObjectID, sourceObjectID, run.expiresAt)
		e.rollbackHeroTauntLocked(
			&peerSession, run, isNew, previousRecord, previousSourceObjectID,
			previousRevision, previousExpiresAt, previousCancel,
		)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroTauntInventory: %w", err)
	}
	peerSession.heroTaunts[targetObjectID] = run
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		ModifierID: definition.RootModifierID, InstanceID: run.modifier.instanceID,
		StackCount: 1, Duration: definition.StatusDuration, Timestamp: timestamp,
	})
	if err != nil {
		e.rollbackHeroTaunt(
			sessionKey, generation, run, isNew, previousRecord,
			previousSourceObjectID, previousRevision, previousExpiresAt,
			previousCancel,
		)
		return nil, fmt.Errorf("heroTauntCreate: %w", err)
	}
	expiry := heroTauntExpiry{
		runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, revision: run.revision,
		timestamp: timestamp + uint64(definition.StatusDuration/time.Millisecond),
		run:       run,
	}
	cancel, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: definition.StatusDuration, Produce: expiry.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		e.rollbackHeroTaunt(
			sessionKey, generation, run, isNew, previousRecord,
			previousSourceObjectID, previousRevision, previousExpiresAt,
			previousCancel,
		)
		return nil, fmt.Errorf("heroTauntSchedule: %w", scheduleErr)
	}
	e.registry.mutex.Lock()
	latest, isLatestFound := e.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.heroTaunts[targetObjectID] == run && run.revision == expiry.revision
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
	plans := make([]zonenpc.SpawnPlan, 0, len(acquired))
	for _, npc := range acquired {
		plans = append(plans, npc.Plan)
	}
	actionPackets, err := e.npc.scheduleFirstActions(
		packet, sessionKey, generation, plans, timestamp,
	)
	if err != nil {
		e.npc.logger.Printf(
			"RakNet hero taunt action omitted target=%d: %v",
			targetObjectID, err,
		)
		return [][]byte{createPacket}, nil
	}
	return append([][]byte{createPacket}, actionPackets...), nil
}

func (e campaignDamageRuntime) rollbackHeroTaunt(
	sessionKey string, generation uint64, run *heroTauntRun, isNew bool,
	previousRecord zoneeffect.Modifier, previousSourceObjectID uint32,
	previousRevision uint64, previousExpiresAt time.Time,
	previousCancel raknet.CancelSchedule,
) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.heroTaunts[run.targetObjectID] == run
	if isCurrent {
		peerSession.zone.NPCs().ClearTaunt(
			run.targetObjectID, run.sourceObjectID, run.expiresAt,
		)
		if isNew {
			peerSession.zone.Effect().Remove(run.modifier.instanceID)
		}
		e.rollbackHeroTauntLocked(
			&peerSession, run, isNew, previousRecord, previousSourceObjectID,
			previousRevision, previousExpiresAt, previousCancel,
		)
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
}

func (e campaignDamageRuntime) rollbackHeroTauntLocked(
	peerSession *gameplayPeerSession, run *heroTauntRun, isNew bool,
	previousRecord zoneeffect.Modifier, previousSourceObjectID uint32,
	previousRevision uint64, previousExpiresAt time.Time,
	previousCancel raknet.CancelSchedule,
) {
	if isNew {
		peerSession.untrackCampaignNPCModifier(run.modifier)
		delete(peerSession.heroTaunts, run.targetObjectID)
		_, _ = run.modifier.release(e.npc.modifierPool)
		return
	}
	run.modifier.record = previousRecord
	run.sourceObjectID = previousSourceObjectID
	run.revision = previousRevision
	run.expiresAt = previousExpiresAt
	run.cancel = previousCancel
	_ = peerSession.zone.Effect().Update(previousRecord)
	if previousSourceObjectID != 0 && !previousExpiresAt.IsZero() {
		_, _ = peerSession.zone.TauntHeroTargets(
			peerSession.binding.UserID, peerSession.generation,
			previousSourceObjectID, []uint32{run.targetObjectID}, previousExpiresAt,
		)
	}
}
