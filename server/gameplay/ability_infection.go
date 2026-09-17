package gameplay

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const rootingPlagueSpreadDuration = 6 * time.Second

type heroInfectionEntry struct {
	objectID           uint32
	modifierInstanceID uint32
	rootInstanceID     uint32
	rootExpiresAt      time.Time
	age                uint32
	tickCount          uint32
	isPrimary          bool
}

type heroInfectionRun struct {
	mutex        sync.Mutex
	npc          *zonenpc.Session
	modifierPool *modifierPool
	infections   map[uint32]heroInfectionEntry
	immunities   map[uint32]struct{}
	cancel       raknet.CancelSchedule
	isCleaned    bool
}

func (e *heroInfectionRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	cancel := e.cancel
	e.cancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	e.Cleanup()
}

func (e *heroInfectionRun) Add(entry heroInfectionEntry) bool {
	if e == nil || entry.objectID == 0 || entry.modifierInstanceID == 0 ||
		entry.tickCount == 0 {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return false
	}
	if _, isFound := e.infections[entry.objectID]; isFound {
		return false
	}
	if _, isImmune := e.immunities[entry.objectID]; isImmune {
		return false
	}
	e.infections[entry.objectID] = entry
	return true
}

func (e *heroInfectionRun) Entries() []heroInfectionEntry {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	entries := make([]heroInfectionEntry, 0, len(e.infections))
	for _, entry := range e.infections {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left int, right int) bool {
		return entries[left].objectID < entries[right].objectID
	})
	return entries
}

func (e *heroInfectionRun) Advance(objectID uint32) (heroInfectionEntry, bool) {
	if e == nil || objectID == 0 {
		return heroInfectionEntry{}, false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	entry, isFound := e.infections[objectID]
	if !isFound {
		return heroInfectionEntry{}, false
	}
	entry.age++
	e.infections[objectID] = entry
	return entry, true
}

func (e *heroInfectionRun) Expire(objectID uint32) (heroInfectionEntry, bool) {
	if e == nil || objectID == 0 {
		return heroInfectionEntry{}, false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	entry, isFound := e.infections[objectID]
	if !isFound {
		return heroInfectionEntry{}, false
	}
	delete(e.infections, objectID)
	e.immunities[objectID] = struct{}{}
	if !entry.rootExpiresAt.IsZero() {
		e.npc.ClearRoot(objectID, entry.rootExpiresAt)
	}
	_ = e.modifierPool.Release(entry.modifierInstanceID)
	if entry.rootInstanceID != 0 {
		_ = e.modifierPool.Release(entry.rootInstanceID)
	}
	return entry, true
}

func (e *heroInfectionRun) IsImmune(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return true
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if _, isFound := e.infections[objectID]; isFound {
		return true
	}
	_, isFound := e.immunities[objectID]
	return isFound
}

func (e *heroInfectionRun) IsEmpty() bool {
	if e == nil {
		return true
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return len(e.infections) == 0
}

func (e *heroInfectionRun) Cleanup() []heroInfectionEntry {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return nil
	}
	e.isCleaned = true
	entries := make([]heroInfectionEntry, 0, len(e.infections))
	for objectID, entry := range e.infections {
		entries = append(entries, entry)
		if !entry.rootExpiresAt.IsZero() {
			e.npc.ClearRoot(objectID, entry.rootExpiresAt)
		}
		_ = e.modifierPool.Release(entry.modifierInstanceID)
		if entry.rootInstanceID != 0 {
			_ = e.modifierPool.Release(entry.rootInstanceID)
		}
	}
	e.infections = nil
	return entries
}

type heroInfectionSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	run                 *heroInfectionRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

type heroInfectionTickStep struct {
	schedule heroInfectionSchedule
	deadline time.Duration
}

func (e heroInfectionTickStep) produce() ([][]byte, error) {
	return e.schedule.tick(e.deadline)
}

func (e heroInfectionSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroInfection == e.run
}

func (e heroInfectionSchedule) tick(
	deadline time.Duration,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	results := make([]zoneability.AreaResult, 0)
	transitions := make([]campaignDamageTransition, 0)
	expired := make([]heroInfectionEntry, 0)
	spreadCandidate := make(map[uint32]zonenpc.Snapshot)
	for _, current := range e.run.Entries() {
		entry, isAdvanced := e.run.Advance(current.objectID)
		if !isAdvanced {
			continue
		}
		target, isTargetFound := peerSession.zone.NPCs().NPC(entry.objectID)
		if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
			if removed, isRemoved := e.run.Expire(entry.objectID); isRemoved {
				expired = append(expired, removed)
			}
			continue
		}
		tickDefinition := e.definition
		if entry.isPrimary {
			tickDefinition.MinimumDamage = e.definition.MinimumDamagePerTick
			tickDefinition.MaximumDamage = e.definition.MaximumDamagePerTick
		} else {
			tickDefinition.MinimumDamage = e.definition.SecondaryMinimumDamage
			tickDefinition.MaximumDamage = e.definition.SecondaryMaximumDamage
		}
		damage, err := zoneability.ProjectDamage(
			e.creature, tickDefinition,
			tickDefinition.MinimumDamage, tickDefinition.MaximumDamage,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("infectionDamageProjection: %w", err)
		}
		tickResults, err := zoneability.CommitArea(
			peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
			zoneability.AreaPlan{
				SourceObjectID: e.sourceObjectID,
				AbilityID:      util.HashID(e.definition.Name),
				Definition:     tickDefinition, Damage: damage,
				Target: []zonenpc.Snapshot{target},
			}, e.creature, peerSession.binding.Difficulty,
			e.runtime.program.Critical,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("infectionDamage: %w", err)
		}
		for _, result := range tickResults {
			transition, transitionErr :=
				peerSession.applyCampaignDamageTransition(result.Damage)
			if transitionErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("infectionTransition: %w", transitionErr)
			}
			results = append(results, result)
			transitions = append(transitions, transition)
			if result.Damage.Damage <= 0 || result.Damage.IsDefeated {
				continue
			}
			for _, candidate := range peerSession.zone.NPCs().LiveSnapshots() {
				if candidate.Faction != zonenpc.FactionNonPlayerAligned ||
					candidate.Plan.ObjectID == entry.objectID ||
					e.run.IsImmune(candidate.Plan.ObjectID) ||
					zonegeometry.Distance(
						target.Plan.Position, candidate.Plan.Position,
					) > e.definition.Radius {
					continue
				}
				spreadCandidate[candidate.Plan.ObjectID] = candidate
			}
		}
		if entry.age >= entry.tickCount {
			if removed, isRemoved := e.run.Expire(entry.objectID); isRemoved {
				expired = append(expired, removed)
			}
		}
	}
	created := make([]heroInfectionEntry, 0, len(spreadCandidate))
	for objectID := range spreadCandidate {
		instanceID, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("infectionSpreadModifier: %w", err)
		}
		entry := heroInfectionEntry{
			objectID: objectID, modifierInstanceID: instanceID,
			tickCount: uint32(rootingPlagueSpreadDuration / e.definition.TickDuration),
		}
		if !e.run.Add(entry) {
			_ = e.runtime.modifierPool.Release(instanceID)
			continue
		}
		created = append(created, entry)
	}
	if e.run.IsEmpty() {
		peerSession.heroInfection = nil
		e.run.cancel = nil
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond), e.binding,
		results, transitions, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("infectionPublish: %w", err)
	}
	for _, entry := range created {
		packet, marshalErr := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: entry.objectID, ModifierGUID: e.definition.SpreadModifierID,
			InstanceID:           entry.modifierInstanceID,
			DurationMilliseconds: uint32(rootingPlagueSpreadDuration.Milliseconds()),
			StackCount:           1,
			StartMilliseconds: e.packet.SourceTime +
				uint64(deadline/time.Millisecond),
			SourceID: e.sourceObjectID,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("infectionSpreadMarshal: %w", marshalErr)
		}
		packets = append(packets, packet)
	}
	for _, entry := range expired {
		packet, marshalErr := effectraknet.ModifierDelete(
			entry.objectID, entry.modifierInstanceID,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("infectionDelete: %w", marshalErr)
		}
		packets = append(packets, packet)
		if entry.rootInstanceID == 0 {
			continue
		}
		rootPacket, marshalErr := effectraknet.ModifierDelete(
			entry.objectID, entry.rootInstanceID,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("infectionRootDelete: %w", marshalErr)
		}
		packets = append(packets, rootPacket)
	}
	return packets, nil
}

func (e heroInfectionSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e heroInfectionSchedule) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.heroInfection = nil
	e.run.cancel = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	entries := e.run.Cleanup()
	packets := make([][]byte, 0, len(entries)*2)
	for _, entry := range entries {
		packet, err := effectraknet.ModifierDelete(
			entry.objectID, entry.modifierInstanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("infectionFinishDelete: %w", err)
		}
		packets = append(packets, packet)
		if entry.rootInstanceID == 0 {
			continue
		}
		packet, err = effectraknet.ModifierDelete(
			entry.objectID, entry.rootInstanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("infectionFinishRoot: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroInfectionSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.heroInfection = nil
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	e.run.Cleanup()
	e.runtime.logger.Printf(
		"RakNet hero infections stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleHeroInfection(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, targetObjectID uint32, sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindInfection ||
		definition.Range <= 0 || definition.Radius <= 0 ||
		definition.Duration <= 0 || definition.TickDuration <= 0 ||
		definition.NumberOfTicks == 0 || definition.MinimumDamagePerTick <= 0 ||
		definition.MaximumDamagePerTick < definition.MinimumDamagePerTick ||
		definition.SecondaryMinimumDamage <= 0 ||
		definition.SecondaryMaximumDamage < definition.SecondaryMinimumDamage ||
		definition.RootModifierID == 0 || definition.SecondaryModifierID == 0 ||
		definition.SpreadModifierID == 0 || definition.AnimationName == "" {
		r.registry.mutex.Unlock()
		return req.reject("infections definition unavailable")
	}
	if peerSession.heroInfection != nil {
		r.registry.mutex.Unlock()
		return req.reject("infections already active")
	}
	if targetObjectID == 0 {
		targetObjectID = zoneability.CursorTarget(
			peerSession.zone.NPCs(), req.command.Common.ObjectID,
			game.Vec3(req.command.Ability.CursorPosition),
			game.Vec3(req.command.Ability.TargetPosition),
			campaignHeldMeleeCursorRadius,
		)
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(targetObjectID)
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 ||
		target.Faction != zonenpc.FactionNonPlayerAligned ||
		!isInsideZoneTrigger(
			peerSession.playerPosition, raknet.Vector3(target.Plan.Position),
			admissionRange,
		) {
		r.registry.mutex.Unlock()
		return req.reject("infections target unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute,
		projected.ManaCoefficient, peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	rootDuration := projected.StatusDuration
	if rootDuration <= 0 {
		rootDuration = projected.Duration
	}
	modifierInstanceID, err := r.modifierPool.Allocate()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionModifier: %w", err)
	}
	rootInstanceID, err := r.modifierPool.Allocate()
	if err != nil {
		_ = r.modifierPool.Release(modifierInstanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionRootModifier: %w", err)
	}
	rootExpiresAt := abilityStartTime.Add(rootDuration)
	err = peerSession.zone.NPCs().ApplyRoot(targetObjectID, rootExpiresAt)
	if err != nil {
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return req.reject("infections root unavailable")
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	previousManaPoint := peerSession.deployedManaPoint()
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: req.command.Common.Unknown[0], SourceID: req.command.Common.ObjectID,
		AbilityID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceTime: req.packet.SourceTime, AnimationName: projected.AnimationName,
		MuzzleEffectName: projected.MuzzleEffectName,
		HitDelay:         projected.HitDelay, ReleaseDelay: projected.ReleaseDelay,
		Cooldown: projected.Cooldown,
	})
	if err != nil {
		peerSession.zone.NPCs().ClearRoot(targetObjectID, rootExpiresAt)
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionStart: %w", err)
	}
	manaPacket, err := abilityraknet.Mana(
		req.command.Common.ObjectID, remainingManaPoint,
	)
	if err != nil {
		peerSession.zone.NPCs().ClearRoot(targetObjectID, rootExpiresAt)
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionManaPacket: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID),
			abilityStartTime, projected.Cooldown,
		)
	if !isCooldownReserved {
		peerSession.zone.NPCs().ClearRoot(targetObjectID, rootExpiresAt)
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return req.reject("infections cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.zone.NPCs().ClearRoot(targetObjectID, rootExpiresAt)
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return req.reject("infections release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		peerSession.zone.NPCs().ClearRoot(targetObjectID, rootExpiresAt)
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionCommit: %w", err)
	}
	run := &heroInfectionRun{
		npc: peerSession.zone.NPCs(), modifierPool: r.modifierPool,
		infections: make(map[uint32]heroInfectionEntry),
		immunities: make(map[uint32]struct{}),
	}
	entry := heroInfectionEntry{
		objectID: targetObjectID, modifierInstanceID: modifierInstanceID,
		rootInstanceID: rootInstanceID, rootExpiresAt: rootExpiresAt,
		tickCount: projected.NumberOfTicks, isPrimary: true,
	}
	if !run.Add(entry) {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		_ = peerSession.setDeployedManaPoints(previousManaPoint)
		peerSession.zone.NPCs().ClearRoot(targetObjectID, rootExpiresAt)
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return nil, errors.New("infections primary unavailable")
	}
	peerSession.heroInfection = run
	generation := peerSession.generation
	binding := peerSession.binding
	creatureIndex := peerSession.deployedCreatureIndex
	liveCount := len(peerSession.zone.NPCs().LiveSnapshots())
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	modifierPackets := make([][]byte, 0, 2)
	for index, message := range []raknet.ApplicationMessage{
		raknet.ModifierCreatedMessage{
			TargetID: targetObjectID, ModifierGUID: projected.RootModifierID,
			InstanceID:           modifierInstanceID,
			DurationMilliseconds: uint32(projected.Duration.Milliseconds()),
			StackCount:           1, StartMilliseconds: req.packet.SourceTime,
			SourceID: req.command.Common.ObjectID,
		},
		raknet.ModifierCreatedMessage{
			TargetID: targetObjectID, ModifierGUID: projected.SecondaryModifierID,
			InstanceID:           rootInstanceID,
			DurationMilliseconds: uint32(rootDuration.Milliseconds()),
			StackCount:           1, StartMilliseconds: req.packet.SourceTime,
			SourceID: req.command.Common.ObjectID,
		},
	} {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, fmt.Errorf("infectionModifierMarshal[%d]: %w", index, marshalErr)
		}
		modifierPackets = append(modifierPackets, packet)
	}
	schedule := heroInfectionSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
		creature: creature, definition: projected, binding: binding, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: start.Release,
	}
	maximumTickCount := max(14, liveCount+14)
	producers := make([]raknet.ScheduledPacketProducer, 0, maximumTickCount+2)
	for tick := 1; tick <= maximumTickCount; tick++ {
		deadline := time.Duration(tick) * projected.TickDuration
		step := heroInfectionTickStep{schedule: schedule, deadline: deadline}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	producers = append(producers,
		raknet.ScheduledPacketProducer{
			Delay: projected.ReleaseDelay, Produce: schedule.release,
		},
		raknet.ScheduledPacketProducer{
			Delay:   time.Duration(maximumTickCount+1) * projected.TickDuration,
			Produce: schedule.finish,
		},
	)
	sortScheduledPacketProducersByDelay(producers)
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else if req.packet.ScheduleGroup != nil {
		cancel, err = req.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("schedule unavailable")
	}
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("infectionSchedule: %w", err)
	}
	run.cancel = cancel
	r.logger.Printf(
		"RakNet hero infections accepted ability=%s source=%d target=%d",
		projected.Name, req.command.Common.ObjectID, targetObjectID,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	packets = append(packets, modifierPackets...)
	return packets, nil
}
