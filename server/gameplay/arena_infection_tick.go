package gameplay

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *arenaInfectionRun) admitEntryLocked(objectID uint32, isPrimary bool, now time.Time, timestamp uint64) error {
	targets := arenaAbilityTargets{runtime: e.runtime, source: e.source}
	key, member, isFound := targets.member(objectID)
	if !isFound || member.isHeroDebuffImmune(objectID) || member.isOverdriveActiveAt(now) {
		return nil
	}
	if !isPrimary && (e.world.entries[objectID] != nil || now.Before(e.world.immunities[objectID])) {
		return nil
	}
	modifierID := e.definition.RootModifierID
	duration := e.definition.Duration
	tickLimit := e.definition.NumberOfTicks
	minimum := e.definition.MinimumDamagePerTick
	maximum := e.definition.MaximumDamagePerTick
	if !isPrimary {
		modifierID = e.definition.SpreadModifierID
		duration = rootingPlagueSpreadDuration
		tickLimit = uint32(duration / e.definition.TickDuration)
		minimum = e.definition.SecondaryMinimumDamage
		maximum = e.definition.SecondaryMaximumDamage
	}
	if previous := e.world.entries[objectID]; previous != nil {
		e.world.retireEntryLocked(previous, now)
	}
	status := &arenaHitStatus{runtime: e.runtime, targetKey: key, gameID: e.gameID,
		objectID: objectID, index: member.deployedCreatureIndex,
		noun:       member.binding.Creatures[member.deployedCreatureIndex].Noun,
		generation: member.generation, transportGeneration: member.transportGeneration,
		squad: member.squad, sourceObjectID: e.sourceObjectID,
		modifierID: modifierID, kind: arenaInfectionDisease, stackCount: 1,
		duration: duration, expiresAt: now.Add(duration), revision: 1}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("plagueModifierAllocate: %w", err)
	}
	status.instanceID = instanceID
	created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: objectID, ModifierGUID: modifierID, InstanceID: instanceID,
		DurationMilliseconds: uint32(duration.Milliseconds()), StackCount: 1,
		StartMilliseconds: timestamp, NPCAffixAssetID: 0,
	})
	if err != nil {
		return status.discard("plagueModifierCreate", err)
	}
	tickDefinition := e.definition
	tickDefinition.MinimumDamage = minimum
	tickDefinition.MaximumDamage = maximum
	damage, err := zoneability.ProjectDamage(e.creature, tickDefinition, minimum, maximum)
	if err != nil {
		return status.discard("plagueDamageProject", err)
	}
	periodic := &arenaBasicBurnRun{status: status, source: e.source, creature: e.creature,
		damage: damage, definition: tickDefinition, interval: e.definition.TickDuration,
		tickLimit: tickLimit, timestamp: timestamp}
	status.periodic = periodic
	if member.arenaHitStatuses == nil {
		member.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if old := member.arenaHitStatuses[status.key()]; old != nil {
		old.retire(&member)
	}
	member.arenaHitStatuses[status.key()] = status
	status.queue(&member, [][]byte{created})
	e.runtime.registry.sessions[key] = member
	entry := &arenaInfectionEntry{run: e, objectID: objectID, status: status,
		periodic: periodic, tickLimit: tickLimit, isPrimary: isPrimary, expiresAt: status.expiresAt}
	e.entries[objectID] = entry
	e.world.entries[objectID] = entry
	if !isPrimary {
		return nil
	}
	rootDefinition := e.definition
	rootDefinition.StatusKind = arenaChargeRoot
	rootDefinition.RootModifierID = e.definition.SecondaryModifierID
	if rootDefinition.StatusDuration <= 0 {
		rootDefinition.StatusDuration = e.definition.Duration
	}
	plan := zoneability.AreaPlan{SourceObjectID: e.sourceObjectID, AbilityID: e.abilityID,
		Definition: rootDefinition}
	_, err = e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation,
		e.sourceObjectID, timestamp, plan, []uint32{objectID})
	if err != nil {
		e.world.retireEntryLocked(entry, now)
		return fmt.Errorf("plagueRoot: %w", err)
	}
	return nil
}

func (e *arenaInfectionRun) tick() {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		e.retireLocked()
		e.runtime.registry.mutex.Unlock()
		return
	}
	e.tickCancel = nil
	now := e.runtime.now()
	objectIDs := make([]uint32, 0, len(e.entries))
	for objectID := range e.entries {
		objectIDs = append(objectIDs, objectID)
	}
	slices.Sort(objectIDs)
	finished := make([]*arenaAbilityTargets, 0, len(objectIDs))
	spreadIDs := make(map[uint32]struct{})
	for _, objectID := range objectIDs {
		entry := e.entries[objectID]
		if entry == nil {
			continue
		}
		if entry.status.isRetired {
			e.world.retireEntryLocked(entry, now)
			continue
		}
		if !entry.isPrimary && now.Before(e.world.immunities[objectID]) {
			entry.age++
			entry.periodic.tickCount++
			if entry.age >= entry.tickLimit {
				e.world.retireEntryLocked(entry, now)
			}
			continue
		}
		targets, result, err := e.damageEntryLocked(entry, now)
		if targets != nil {
			finished = append(finished, targets)
		}
		if err != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Rooting Plague tick failed target=%d: %v", objectID, err)
			}
			e.world.retireEntryLocked(entry, now)
			continue
		}
		entry.age++
		entry.periodic.tickCount++
		if result != nil && result.Damage.Damage > 0 && !result.Damage.IsDefeated {
			for _, candidate := range targets.LiveSnapshots() {
				if candidate.Plan.ObjectID == objectID || candidate.Plan.Position.Sub(result.Snapshot.Plan.Position).Length() > e.definition.Radius {
					continue
				}
				if _, _, isFound := targets.member(candidate.Plan.ObjectID); isFound {
					spreadIDs[candidate.Plan.ObjectID] = struct{}{}
				}
			}
		}
		if entry.age >= entry.tickLimit {
			e.world.retireEntryLocked(entry, now)
		}
	}
	spreadObjectIDs := make([]uint32, 0, len(spreadIDs))
	for objectID := range spreadIDs {
		spreadObjectIDs = append(spreadObjectIDs, objectID)
	}
	slices.Sort(spreadObjectIDs)
	for _, objectID := range spreadObjectIDs {
		if e.world.entries[objectID] != nil || now.Before(e.world.immunities[objectID]) {
			continue
		}
		timestamp := e.packet.SourceTime + uint64(now.Sub(e.startedAt)/time.Millisecond)
		err := e.admitEntryLocked(objectID, false, now, timestamp)
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Rooting Plague spread failed target=%d: %v", objectID, err)
		}
	}
	if len(e.entries) == 0 {
		e.retireLocked()
	} else {
		cancel, err := e.runtime.registry.timer.Schedule(e.definition.TickDuration, e.tick)
		if err == nil && cancel == nil {
			err = errors.New("Rooting Plague next tick cancellation unavailable")
		}
		if err != nil {
			if cancel != nil {
				cancel()
			}
			e.retireLocked()
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Rooting Plague reschedule failed owner=%d: %v", e.sourceObjectID, err)
			}
		} else {
			e.tickCancel = cancel
		}
	}
	e.runtime.registry.mutex.Unlock()
	for _, targets := range finished {
		targets.finish()
	}
}

func (e *arenaInfectionRun) damageEntryLocked(entry *arenaInfectionEntry,
	now time.Time,
) (*arenaAbilityTargets, *zoneability.AreaResult, error) {
	match := e.runtime.registry.arenaMatches[e.gameID]
	if match == nil || match.random == nil {
		return nil, nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(now.Sub(e.startedAt)/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, e.source, e.definition, timestamp).(*arenaAbilityTargets)
	snapshot, isFound := targets.NPC(entry.objectID)
	if !isFound {
		e.world.retireEntryLocked(entry, now)
		return targets, nil, nil
	}
	definition := entry.periodic.definition
	plan := zoneability.AreaPlan{SourceObjectID: e.sourceObjectID, AbilityID: e.abilityID,
		Definition: definition, Damage: entry.periodic.damage,
		Target: []zonenpc.Snapshot{snapshot}}
	results, err := zoneability.CommitArea(match.random, targets, plan, e.creature,
		e.source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		return targets, nil, fmt.Errorf("plagueDamage: %w", err)
	}
	packets, err := targets.packets(results)
	if err != nil {
		return targets, nil, fmt.Errorf("plaguePackets: %w", err)
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	if len(results) == 0 {
		return targets, nil, nil
	}
	return targets, &results[0], nil
}
