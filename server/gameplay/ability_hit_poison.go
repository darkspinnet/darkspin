package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

const poisonNovaName = "LFPoisonRavager_PoisonNova"

type heroHitPoisonKey struct {
	abilityID      uint32
	targetObjectID uint32
}

type heroHitPoisonRun struct {
	modifier       *campaignNPCModifierRun
	binding        game.GameplayBinding
	creature       game.GameplayCreature
	definition     sim.AbilityDefinition
	sourceObjectID uint32
	targetObjectID uint32
	completedTick  uint32
	revision       uint64
	cancel         raknet.CancelSchedule
}

type heroHitPoisonStep struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	tickIndex  uint32
	revision   uint64
	key        heroHitPoisonKey
	run        *heroHitPoisonRun
}

func (e heroHitPoisonStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.heroHitPoisons[e.key] == e.run &&
		e.run.revision == e.revision && peerSession.zone != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.run.targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		peerSession.removeHeroHitPoison(e.key, e.run)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return e.release()
	}
	tickDefinition := e.run.definition
	tickDefinition.Name = poisonNovaName + "Poison"
	tickDefinition.MinimumDamage = tickDefinition.MinimumDamagePerTick
	tickDefinition.MaximumDamage = tickDefinition.MaximumDamagePerTick
	tickDefinition.DescriptorMask = 36
	tickDefinition.DamageType = 2
	tickDefinition.DamageSource = 1
	tickDefinition.IsDescriptorFound = true
	tickDefinition.IsDamageTypeFound = true
	tickDefinition.IsDamageSourceFound = true
	damage, err := zoneability.ProjectDamage(
		e.run.creature, tickDefinition,
		tickDefinition.MinimumDamage, tickDefinition.MaximumDamage,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("hitPoisonDamage: %w", err)
	}
	damageResult, err := peerSession.zone.NPCs().DamageOverTime(
		e.run.sourceObjectID, e.run.targetObjectID, damage.Minimum,
		tickDefinition.DamageSource,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("hitPoisonApply: %w", err)
	}
	e.run.completedTick = e.tickIndex
	transition, err := peerSession.applyCampaignDamageTransition(damageResult)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("hitPoisonTransition: %w", err)
	}
	isFinal := e.tickIndex == e.run.definition.NumberOfTicks ||
		damageResult.IsDefeated
	if isFinal {
		peerSession.removeHeroHitPoison(e.key, e.run)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets, err := e.runtime.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.run.sourceObjectID,
		e.timestamp, e.run.binding,
		[]zoneability.AreaResult{{
			Snapshot: target, Damage: damageResult, Definition: tickDefinition,
		}}, []campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("hitPoisonPublish: %w", err)
	}
	if !isFinal {
		return packets, nil
	}
	releasePackets, err := e.release()
	if err != nil {
		return nil, err
	}
	return append(packets, releasePackets...), nil
}

func (e heroHitPoisonStep) release() ([][]byte, error) {
	isCreated, err := e.run.modifier.release(e.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("hitPoisonRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.run.targetObjectID, e.run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("hitPoisonDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *gameplayPeerSession) removeHeroHitPoison(
	key heroHitPoisonKey, run *heroHitPoisonRun,
) {
	if e == nil || run == nil || e.heroHitPoisons[key] != run {
		return
	}
	delete(e.heroHitPoisons, key)
	e.untrackCampaignNPCModifier(run.modifier)
}

func (e campaignDamageRuntime) applyHeroHitPoison(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, result zoneability.AreaResult,
) ([][]byte, error) {
	definition := result.Definition
	if definition.Name != poisonNovaName || definition.RootModifierID == 0 ||
		definition.StatusDuration <= 0 || definition.TickDuration <= 0 ||
		definition.NumberOfTicks == 0 ||
		definition.MinimumDamagePerTick <= 0 ||
		definition.MaximumDamagePerTick < definition.MinimumDamagePerTick ||
		result.Damage.IsDamageImmune || result.Damage.IsDefeated ||
		result.Damage.Damage <= 0 {
		return nil, nil
	}
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.deployedObjectID == sourceObjectID && peerSession.zone != nil &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures))
	if !isCurrent {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	abilityID := util.HashID(definition.Name)
	key := heroHitPoisonKey{
		abilityID: abilityID, targetObjectID: result.Damage.ObjectID,
	}
	if peerSession.heroHitPoisons == nil {
		peerSession.heroHitPoisons = make(map[heroHitPoisonKey]*heroHitPoisonRun)
	}
	run := peerSession.heroHitPoisons[key]
	isNew := run == nil
	if isNew {
		modifier, err := newCampaignNPCModifierRun(e.npc.modifierPool)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("hitPoisonRun: %w", err)
		}
		run = &heroHitPoisonRun{modifier: modifier}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			e.registry.mutex.Unlock()
			_, releaseErr := modifier.release(e.npc.modifierPool)
			return nil, fmt.Errorf(
				"hitPoisonTrack: %w", errors.Join(err, releaseErr),
			)
		}
	}
	previousRevision := run.revision
	previousCompletedTick := run.completedTick
	previousCancel := run.cancel
	run.binding = peerSession.binding
	run.creature = peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	run.definition = definition
	run.sourceObjectID = sourceObjectID
	run.targetObjectID = result.Damage.ObjectID
	run.revision++
	run.completedTick = 0
	revision := run.revision
	peerSession.heroHitPoisons[key] = run
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: sourceObjectID, TargetObjectID: run.targetObjectID,
			ModifierID: definition.RootModifierID, InstanceID: run.modifier.instanceID,
			StackCount: 1, Duration: definition.StatusDuration, Timestamp: timestamp,
		},
	)
	if err != nil {
		e.rollbackHeroHitPoison(
			sessionKey, generation, key, run, isNew,
			previousRevision, revision, previousCancel,
			previousCompletedTick,
		)
		return nil, fmt.Errorf("hitPoisonCreate: %w", err)
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, definition.NumberOfTicks)
	for tickIndex := uint32(1); tickIndex <= definition.NumberOfTicks; tickIndex++ {
		delay := time.Duration(tickIndex) * definition.TickDuration
		step := heroHitPoisonStep{
			runtime: e, packet: packet, sessionKey: sessionKey,
			generation: generation, timestamp: timestamp + uint64(delay/time.Millisecond),
			tickIndex: tickIndex, revision: revision, key: key, run: run,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: delay, Produce: step.produce,
		})
	}
	cancel, scheduleErr := packet.ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		e.rollbackHeroHitPoison(
			sessionKey, generation, key, run, isNew,
			previousRevision, revision, previousCancel,
			previousCompletedTick,
		)
		return nil, fmt.Errorf("hitPoisonSchedule: %w", scheduleErr)
	}
	e.registry.mutex.Lock()
	latest, isLatestFound := e.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.heroHitPoisons[key] == run && run.revision == revision
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

func (e campaignDamageRuntime) rollbackHeroHitPoison(
	sessionKey string, generation uint64, key heroHitPoisonKey,
	run *heroHitPoisonRun, isNew bool,
	previousRevision uint64, revision uint64,
	previousCancel raknet.CancelSchedule,
	previousCompletedTick uint32,
) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.heroHitPoisons[key] == run && run.revision == revision
	if isCurrent {
		if isNew {
			peerSession.removeHeroHitPoison(key, run)
		} else {
			run.revision = previousRevision
			run.cancel = previousCancel
			run.completedTick = previousCompletedTick
		}
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	if isNew && isCurrent {
		_, _ = run.modifier.release(e.npc.modifierPool)
	}
}
