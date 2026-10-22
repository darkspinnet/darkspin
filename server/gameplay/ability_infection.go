package gameplay

import (
	"errors"
	"fmt"
	"log"
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
	expiresAt          time.Time
	isConsumed         bool
}

type heroInfectionRun struct {
	activationID   uint64
	mutex          sync.Mutex
	npc            *zonenpc.Session
	modifierPool   *modifierPool
	infections     map[uint32]heroInfectionEntry
	now            func() time.Time
	cancel         raknet.CancelSchedule
	tickCancel     raknet.CancelSchedule
	isCleaned      bool
	isAccepted     bool
	creature       game.GameplayCreature
	definition     sim.AbilityDefinition
	sourceObjectID uint32
}

func (e *heroInfectionRun) Stop() []heroInfectionEntry {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	cancel := e.cancel
	tickCancel := e.tickCancel
	e.cancel = nil
	e.tickCancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	if tickCancel != nil {
		tickCancel()
	}
	return e.Cleanup()
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
	if !e.npc.AdmitInfection(entry.objectID, entry.modifierInstanceID, entry.isPrimary, e.now()) {
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
	if !entry.isConsumed {
		e.applyImmunity(entry, e.now())
	}
	if !entry.rootExpiresAt.IsZero() {
		e.npc.ClearBioEntangle(objectID, entry.rootInstanceID)
	}
	if entry.modifierInstanceID != 0 {
		err := e.modifierPool.Release(entry.modifierInstanceID)
		if err != nil {
			log.Printf("Infection expired modifier release failed target=%d: %v", objectID, err)
		}
	}
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
	return e.npc.IsInfectionImmune(objectID, e.now())
}

// expireRoot releases BioEntangle independently of the disease modifier.
// In particular, upgraded roots expire before LifePlague finishes ticking.
func (e *heroInfectionRun) expireRoot(objectID uint32) (heroInfectionEntry, bool) {
	return e.expireRootInstance(objectID, 0)
}

func (e *heroInfectionRun) expireRootInstance(objectID, instanceID uint32) (heroInfectionEntry, bool) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	entry, isFound := e.infections[objectID]
	if !isFound || entry.rootInstanceID == 0 || (instanceID != 0 && entry.rootInstanceID != instanceID) {
		return heroInfectionEntry{}, false
	}
	e.npc.ClearBioEntangle(objectID, entry.rootInstanceID)
	err := e.modifierPool.Release(entry.rootInstanceID)
	if err != nil {
		log.Printf("Infection root release failed target=%d: %v", objectID, err)
	}
	removed := heroInfectionEntry{objectID: objectID, rootInstanceID: entry.rootInstanceID}
	entry.rootInstanceID = 0
	entry.rootExpiresAt = time.Time{}
	e.infections[objectID] = entry
	return removed, true
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
		if !entry.isConsumed {
			if e.isAccepted {
				e.applyImmunity(entry, e.now())
			} else {
				e.npc.RollbackInfection(objectID, entry.modifierInstanceID)
			}
		}
		if !entry.rootExpiresAt.IsZero() {
			e.npc.ClearBioEntangle(objectID, entry.rootInstanceID)
		}
		if entry.modifierInstanceID != 0 {
			err := e.modifierPool.Release(entry.modifierInstanceID)
			if err != nil {
				log.Printf("Infection cleanup modifier release failed target=%d: %v", objectID, err)
			}
		}
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
	startedAt           time.Time
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
		isHeroInfectionCurrent(peerSession, e.run)
}

func (e heroInfectionSchedule) tick(
	deadline time.Duration,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || !e.run.isAccepted {
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
		isTargetDead := !isTargetFound || target.IsDefeated || target.HitPoint <= 0
		if entry.rootInstanceID != 0 && (isTargetDead || !e.runtime.now().Before(entry.rootExpiresAt)) {
			removed, isRemoved := e.run.expireRoot(entry.objectID)
			if isRemoved {
				expired = append(expired, removed)
				entry.rootInstanceID = 0
				entry.rootExpiresAt = time.Time{}
			}
		}
		if entry.isConsumed {
			// Expunge removes the disease, while Bio Entangle retains its
			// independent duration and never deals or spreads disease damage.
			if entry.rootInstanceID == 0 {
				removed, isRemoved := e.run.Expire(entry.objectID)
				if isRemoved {
					expired = append(expired, removed)
				}
			}
			continue
		}
		if isTargetDead {
			removed, isRemoved := e.run.Expire(entry.objectID)
			if isRemoved {
				expired = append(expired, removed)
			}
			continue
		}
		if !entry.isPrimary && e.run.npc.IsInfectionImmune(entry.objectID, e.runtime.now()) {
			if entry.age >= entry.tickCount {
				removed, isRemoved := e.run.Expire(entry.objectID)
				if isRemoved {
					expired = append(expired, removed)
				}
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
			result.ActivationID = e.run.activationID
			results = append(results, result)
			transitions = append(transitions, transition)
			if result.Damage.Damage <= 0 || result.Damage.IsDefeated {
				continue
			}
			for _, candidate := range peerSession.zone.NPCs().LiveSnapshots() {
				if candidate.Faction != zonenpc.FactionNonPlayerAligned ||
					candidate.Plan.ObjectID == entry.objectID ||
					e.run.npc.IsInfectionSpreadBlocked(candidate.Plan.ObjectID, e.runtime.now()) ||
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
		if e.run.npc.IsInfectionSpreadBlocked(objectID, e.runtime.now()) {
			continue
		}
		instanceID, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("infectionSpreadModifier: %w", err)
		}
		entry := heroInfectionEntry{
			objectID: objectID, modifierInstanceID: instanceID,
			tickCount: uint32(rootingPlagueSpreadDuration / e.definition.TickDuration),
			expiresAt: e.runtime.now().Add(rootingPlagueSpreadDuration),
		}
		if !e.run.Add(entry) {
			_ = e.runtime.modifierPool.Release(instanceID)
			continue
		}
		created = append(created, entry)
	}
	if e.run.IsEmpty() {
		delete(peerSession.heroInfections, e.run)
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
		if entry.modifierInstanceID != 0 {
			packet, marshalErr := effectraknet.ModifierDelete(entry.objectID, entry.modifierInstanceID)
			if marshalErr != nil {
				return nil, fmt.Errorf("infectionDelete: %w", marshalErr)
			}
			packets = append(packets, packet)
		}
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
	isCurrent := e.isCurrent(peerSession, isFound) && e.run.isAccepted
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e heroInfectionSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return
	}
	delete(peerSession.heroInfections, e.run)
	if !e.run.isAccepted {
		err := peerSession.setCampaignCharacterManaPoints(e.creatureIndex, e.previousManaPoint)
		if err != nil {
			e.runtime.logger.Printf("Infection mana rollback failed: %v", err)
		}
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	entries := e.run.Stop()
	if e.run.isAccepted {
		packets := infectionDeletionPackets(entries)
		for candidateKey, candidate := range e.runtime.registry.sessions {
			if candidate.zone == nil || candidate.zone.NPCs() != e.run.npc {
				continue
			}
			candidate.queuePackets(packets)
			e.runtime.registry.sessions[candidateKey] = candidate
		}
	}
	e.runtime.registry.mutex.Unlock()
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
	rootDuration = zonenpc.CrowdControlDuration(target.Plan, rootDuration)
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
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionStart: %w", err)
	}
	manaPacket, err := abilityraknet.Mana(
		req.command.Common.ObjectID, remainingManaPoint,
	)
	if err != nil {
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
		_ = r.modifierPool.Release(modifierInstanceID)
		_ = r.modifierPool.Release(rootInstanceID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("infectionCommit: %w", err)
	}
	run := &heroInfectionRun{
		activationID: r.damage.activationID,
		npc:          peerSession.zone.NPCs(), modifierPool: r.modifierPool,
		infections: make(map[uint32]heroInfectionEntry),
		now:        r.now,
		creature:   creature, definition: projected, sourceObjectID: req.command.Common.ObjectID,
	}
	entry := heroInfectionEntry{
		objectID: targetObjectID, modifierInstanceID: modifierInstanceID,
		rootInstanceID: rootInstanceID, rootExpiresAt: rootExpiresAt,
		tickCount: projected.NumberOfTicks, isPrimary: true,
		expiresAt: abilityStartTime.Add(projected.Duration),
	}
	run.infections[targetObjectID] = entry
	if peerSession.heroInfections == nil {
		peerSession.heroInfections = make(map[*heroInfectionRun]struct{})
	}
	peerSession.heroInfections[run] = struct{}{}
	generation := peerSession.generation
	binding := peerSession.binding
	creatureIndex := peerSession.deployedCreatureIndex
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroInfectionSchedule{
		runtime: r, packet: req.packet.Autonomous(), sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		startedAt:     abilityStartTime,
		creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
		creature: creature, definition: projected, binding: binding, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: start.Release,
	}
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
			schedule.fail(marshalErr)
			return nil, fmt.Errorf("infectionModifierMarshal[%d]: %w", index, marshalErr)
		}
		modifierPackets = append(modifierPackets, packet)
	}
	step := heroInfectionTickStep{schedule: schedule, deadline: projected.TickDuration}
	rootStep := heroInfectionRootStep{schedule: schedule, objectID: targetObjectID, instanceID: rootInstanceID}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: max(time.Duration(0), abilityStartTime.Add(projected.TickDuration).Sub(r.now())), Produce: step.produce, AfterCommit: step.commit},
		{Delay: projected.ReleaseDelay, Produce: schedule.release},
		{Delay: max(time.Duration(0), rootExpiresAt.Sub(r.now())), Produce: rootStep.produce},
	}
	sortScheduledPacketProducersByDelay(producers)
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if schedule.packet.ScheduleGroupResult != nil {
		cancel, err = schedule.packet.ScheduleGroupResult(producers, schedule.fail)
	} else if schedule.packet.ScheduleGroup != nil {
		cancel, err = schedule.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("schedule unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		schedule.fail(err)
		return nil, fmt.Errorf("infectionSchedule: %w", err)
	}
	if cancel == nil {
		err = errors.New("schedule cancellation unavailable")
		schedule.fail(err)
		return nil, fmt.Errorf("infectionCancellation: %w", err)
	}
	r.registry.mutex.Lock()
	current, isFound := r.registry.sessions[sessionKey]
	if !schedule.isCurrent(current, isFound) {
		r.registry.mutex.Unlock()
		cancel()
		run.Cleanup()
		return nil, errors.New("infection stopped before schedule acceptance")
	}
	replacedPackets, replaceErr := schedule.acceptPrimary(entry, cancel)
	r.registry.mutex.Unlock()
	if replaceErr != nil {
		cancel()
		schedule.fail(replaceErr)
		return nil, fmt.Errorf("infectionAccept: %w", replaceErr)
	}
	r.logger.Printf(
		"RakNet hero infections accepted ability=%s source=%d target=%d",
		projected.Name, req.command.Common.ObjectID, targetObjectID,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	packets = append(packets, replacedPackets...)
	packets = append(packets, modifierPackets...)
	return packets, nil
}
