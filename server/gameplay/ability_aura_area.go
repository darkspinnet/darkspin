package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const heroAuraAreaScanInterval = 250 * time.Millisecond
const timeBubbleAbilityName = "SpacetimeRandom2"

type heroAuraAreaTarget struct {
	instanceID uint32
	expiresAt  time.Time
}

type heroAuraAreaProjectile struct {
	instanceID uint32
	run        *abilityraknet.ProjectileRun
}

type heroAuraAreaRun struct {
	mutex        sync.Mutex
	npc          *zonenpc.Session
	modifierPool *modifierPool
	targets      map[uint32]heroAuraAreaTarget
	projectiles  map[uint32]heroAuraAreaProjectile
	statusKind   sim.AbilityStatusKind
	objectID     uint32
	cancel       raknet.CancelSchedule
	isCleaned    bool
}

func (e *heroAuraAreaRun) Stop() {
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

func (e *heroAuraAreaRun) Cleanup() []heroAuraAreaTargetDelete {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return nil
	}
	e.isCleaned = true
	deleted := make([]heroAuraAreaTargetDelete, 0, len(e.targets))
	for objectID, target := range e.targets {
		e.clearStatus(objectID, target.expiresAt)
		_ = e.modifierPool.Release(target.instanceID)
		deleted = append(deleted, heroAuraAreaTargetDelete{
			objectID: objectID, instanceID: target.instanceID,
		})
	}
	for objectID, projectile := range e.projectiles {
		projectile.run.SetSpeedScale(time.Now(), 1)
		_ = e.modifierPool.Release(projectile.instanceID)
		deleted = append(deleted, heroAuraAreaTargetDelete{
			objectID: objectID, instanceID: projectile.instanceID,
		})
	}
	e.targets = nil
	e.projectiles = nil
	return deleted
}

type heroAuraAreaTargetDelete struct {
	objectID   uint32
	instanceID uint32
}

func (e *heroAuraAreaRun) breakSleepOnDamage(
	objectID uint32,
) (heroAuraAreaTargetDelete, bool) {
	if e == nil || e.statusKind != sim.AbilityStatusKindSleep || objectID == 0 {
		return heroAuraAreaTargetDelete{}, false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return heroAuraAreaTargetDelete{}, false
	}
	target, isFound := e.targets[objectID]
	if !isFound {
		return heroAuraAreaTargetDelete{}, false
	}
	e.clearStatus(objectID, target.expiresAt)
	_ = e.modifierPool.Release(target.instanceID)
	delete(e.targets, objectID)
	return heroAuraAreaTargetDelete{
		objectID: objectID, instanceID: target.instanceID,
	}, true
}

func (r campaignDamageRuntime) breakSleepingCloudOnDamage(
	sessionKey string, generation uint64, objectID uint32,
	descriptorMask uint32,
) ([][]byte, error) {
	if objectID == 0 || descriptorMask&4 != 0 {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	runs := make([]*heroAuraAreaRun, 0, len(peerSession.heroAuraAreas))
	for _, run := range peerSession.heroAuraAreas {
		if run != nil && run.statusKind == sim.AbilityStatusKindSleep {
			runs = append(runs, run)
		}
	}
	r.registry.mutex.RUnlock()

	deletes := make([]heroAuraAreaTargetDelete, 0, len(runs))
	for _, run := range runs {
		deleted, isDeleted := run.breakSleepOnDamage(objectID)
		if isDeleted {
			deletes = append(deletes, deleted)
		}
	}
	packets, err := marshalAuraAreaDeletes(deletes)
	if err != nil {
		return nil, fmt.Errorf("sleepBreakMarshal: %w", err)
	}
	return packets, nil
}

func (e *heroAuraAreaRun) clearStatus(objectID uint32, expiresAt time.Time) {
	switch e.statusKind {
	case sim.AbilityStatusKindSilence:
		e.npc.ClearSilence(objectID, expiresAt)
	case sim.AbilityStatusKindSleep:
		e.npc.ClearSleep(objectID, expiresAt)
	case sim.AbilityStatusKindSlow:
		e.npc.ClearSlow(objectID, expiresAt)
	}
}

type heroAuraAreaSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	abilityID           uint32
	creatureIndex       uint32
	previousManaPoint   float32
	center              raknet.Vector3
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	run                 *heroAuraAreaRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

type heroAuraAreaStep struct {
	schedule heroAuraAreaSchedule
	deadline time.Duration
	index    uint32
	isFinal  bool
}

func (e heroAuraAreaStep) produce() ([][]byte, error) {
	return e.schedule.tick(e.deadline, e.index, e.isFinal)
}

func (e heroAuraAreaSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroAuraAreas[e.abilityID] == e.run
}

func (e heroAuraAreaSchedule) tick(
	deadline time.Duration, index uint32, isFinal bool,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	statusPackets, err := e.reconcile(peerSession, deadline)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("auraAreaReconcile: %w", err)
	}
	results := make([]zoneability.AreaResult, 0)
	transitions := make([]campaignDamageTransition, 0)
	isDamageTick := e.definition.MinimumDamage > 0 &&
		index%uint32(time.Second/heroAuraAreaScanInterval) == 0
	if isDamageTick {
		plan, planErr := zoneability.PlanFixedArea(
			peerSession.zone.NPCs(), e.sourceObjectID, game.Vec3(e.center),
			e.creature, e.definition,
		)
		if planErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("auraAreaPlan: %w", planErr)
		}
		results, err = zoneability.CommitArea(
			peerSession.zone.Population().Random(), peerSession.zone.NPCs(),
			plan, e.creature, peerSession.binding.Difficulty,
			e.runtime.program.Critical,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("auraAreaDamage: %w", err)
		}
		for resultIndex, result := range results {
			transition, transitionErr := peerSession.applyCampaignDamageTransition(
				result.Damage,
			)
			if transitionErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("auraAreaTransition[%d]: %w", resultIndex, transitionErr)
			}
			transitions = append(transitions, transition)
		}
	}
	if isFinal {
		delete(peerSession.heroAuraAreas, e.abilityID)
		e.run.cancel = nil
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets := statusPackets
	if index == 0 && e.definition.SpawnNoun != "" {
		spawnPackets, marshalErr := marshalAuraAreaSpawn(
			e.run.objectID, e.sourceObjectID, e.center, e.definition,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("auraAreaSpawn: %w", marshalErr)
		}
		packets = append(spawnPackets, packets...)
	} else if index == 0 && e.definition.ActivationEffectName != "" {
		effectPacket, marshalErr := raknet.MarshalApplication(raknet.DropPresentationMessage{
			Asset: util.HashID(e.definition.ActivationEffectName), Position: e.center,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("auraAreaEffect: %w", marshalErr)
		}
		packets = append([][]byte{effectPacket}, packets...)
	}
	if len(results) > 0 {
		damagePackets, publishErr := e.runtime.damage.publishAreaResults(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID,
			e.packet.SourceTime+uint64(deadline/time.Millisecond), e.binding,
			results, transitions, nil, false,
		)
		if publishErr != nil {
			return nil, fmt.Errorf("auraAreaPublish: %w", publishErr)
		}
		packets = append(packets, damagePackets...)
	}
	if isFinal {
		deletes := e.run.Cleanup()
		cleanupPackets, marshalErr := marshalAuraAreaDeletes(deletes)
		if marshalErr != nil {
			return nil, marshalErr
		}
		packets = append(packets, cleanupPackets...)
		if e.run.objectID != 0 {
			if e.definition.Name == timeBubbleAbilityName &&
				e.definition.ActivationEffectName != "" {
				effectPacket, effectErr := raknet.MarshalApplication(
					raknet.AttachedEffectMessage{
						Slot: 1, IsRemovalRequested: true, IsHardStop: true,
						ObjectID: e.run.objectID,
					},
				)
				if effectErr != nil {
					return nil, fmt.Errorf("auraAreaEffectRemove: %w", effectErr)
				}
				packets = append(packets, effectPacket)
			}
			deletePacket, deleteErr := raknet.MarshalApplication(
				raknet.ObjectDeleteMessage{ObjectID: []uint32{e.run.objectID}},
			)
			if deleteErr != nil {
				return nil, fmt.Errorf("auraAreaObjectDelete: %w", deleteErr)
			}
			packets = append(packets, deletePacket)
		}
	}
	return packets, nil
}

func (e heroAuraAreaSchedule) reconcile(
	peerSession gameplayPeerSession, deadline time.Duration,
) ([][]byte, error) {
	now := e.runtime.now()
	elapsed := max(time.Duration(0), deadline-e.definition.HitDelay)
	remaining := max(heroAuraAreaScanInterval, e.definition.Duration-elapsed)
	expiresAt := now.Add(remaining)
	live := make(map[uint32]zonenpc.Snapshot)
	for _, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Faction != zonenpc.FactionNonPlayerAligned ||
			zonegeometry.Distance(game.Vec3(e.center), target.Plan.Position) >
				e.definition.Radius {
			continue
		}
		live[target.Plan.ObjectID] = target
	}
	liveProjectile := make(map[uint32]*abilityraknet.ProjectileRun)
	if e.definition.Name == timeBubbleAbilityName {
		for objectID, run := range peerSession.campaignNPCProjectiles {
			snapshot := run.Snapshot(now)
			if !snapshot.IsActive || zonegeometry.Distance(
				game.Vec3(e.center), game.Vec3(snapshot.Position),
			) > e.definition.Radius {
				continue
			}
			liveProjectile[objectID] = run
		}
	}
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isCleaned {
		return nil, nil
	}
	messages := make([]raknet.ApplicationMessage, 0)
	for objectID, tracked := range e.run.targets {
		if _, isInside := live[objectID]; isInside {
			continue
		}
		e.run.clearStatus(objectID, tracked.expiresAt)
		_ = e.run.modifierPool.Release(tracked.instanceID)
		delete(e.run.targets, objectID)
		messages = append(messages, raknet.ModifierDeletedMessage{
			TargetID: objectID, InstanceID: tracked.instanceID,
		})
	}
	for objectID := range live {
		if _, isTracked := e.run.targets[objectID]; isTracked {
			continue
		}
		err := e.applyStatus(peerSession.zone.NPCs(), objectID, expiresAt)
		if err != nil {
			return nil, fmt.Errorf("status[%d]: %w", objectID, err)
		}
		if e.statusRemaining(peerSession.zone.NPCs(), objectID, now) == 0 {
			continue
		}
		instanceID, allocateErr := e.run.modifierPool.Allocate()
		if allocateErr != nil {
			e.run.clearStatus(objectID, expiresAt)
			return nil, fmt.Errorf("modifier[%d]: %w", objectID, allocateErr)
		}
		e.run.targets[objectID] = heroAuraAreaTarget{
			instanceID: instanceID, expiresAt: expiresAt,
		}
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: e.definition.RootModifierID,
			InstanceID:           instanceID,
			DurationMilliseconds: 0,
			StackCount:           1,
			StartMilliseconds:    e.packet.SourceTime + uint64(deadline/time.Millisecond),
			SourceID:             e.sourceObjectID,
		})
		if e.definition.HitEffectName != "" &&
			e.definition.Name != timeBubbleAbilityName {
			messages = append(messages, raknet.ServerEventMessage{
				Asset:    util.HashID(e.definition.HitEffectName),
				ObjectID: objectID,
			})
		}
	}
	for objectID, tracked := range e.run.projectiles {
		liveRun, isInside := liveProjectile[objectID]
		if isInside && liveRun == tracked.run {
			continue
		}
		tracked.run.SetSpeedScale(now, 1)
		_ = e.run.modifierPool.Release(tracked.instanceID)
		delete(e.run.projectiles, objectID)
		messages = append(messages, raknet.ModifierDeletedMessage{
			TargetID: objectID, InstanceID: tracked.instanceID,
		})
	}
	for objectID, run := range liveProjectile {
		if _, isTracked := e.run.projectiles[objectID]; isTracked ||
			!run.SetSpeedScale(now, 0.40) {
			continue
		}
		instanceID, allocateErr := e.run.modifierPool.Allocate()
		if allocateErr != nil {
			run.SetSpeedScale(now, 1)
			return nil, fmt.Errorf("projectileModifier[%d]: %w", objectID, allocateErr)
		}
		e.run.projectiles[objectID] = heroAuraAreaProjectile{
			instanceID: instanceID, run: run,
		}
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: e.definition.RootModifierID,
			InstanceID: instanceID, DurationMilliseconds: 0, StackCount: 1,
			StartMilliseconds: e.packet.SourceTime + uint64(deadline/time.Millisecond),
			SourceID:          e.sourceObjectID,
		})
		if e.definition.HitEffectName != "" &&
			e.definition.Name != timeBubbleAbilityName {
			messages = append(messages, raknet.ServerEventMessage{
				Asset: util.HashID(e.definition.HitEffectName), ObjectID: objectID,
			})
		}
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("marshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroAuraAreaSchedule) applyStatus(
	npc *zonenpc.Session, objectID uint32, expiresAt time.Time,
) error {
	switch e.definition.StatusKind {
	case sim.AbilityStatusKindSilence:
		return npc.ApplySilence(objectID, expiresAt)
	case sim.AbilityStatusKindSleep:
		return npc.ApplySleep(objectID, expiresAt)
	case sim.AbilityStatusKindSlow:
		return npc.ApplySlow(objectID, expiresAt, 0.40, 0.60)
	default:
		return errors.New("unsupported aura status")
	}
}

func (e heroAuraAreaSchedule) statusRemaining(
	npc *zonenpc.Session, objectID uint32, at time.Time,
) time.Duration {
	switch e.definition.StatusKind {
	case sim.AbilityStatusKindSilence:
		return npc.SilenceRemaining(objectID, at)
	case sim.AbilityStatusKindSleep:
		return npc.SleepRemaining(objectID, at)
	case sim.AbilityStatusKindSlow:
		movementScale := npc.SlowMovementScale(objectID, at)
		if movementScale < 1 {
			return time.Millisecond
		}
		return 0
	default:
		return 0
	}
}

func (e heroAuraAreaSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e heroAuraAreaSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroAuraAreas, e.abilityID)
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
	e.run.Stop()
	e.runtime.logger.Printf(
		"RakNet hero aura area stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func marshalAuraAreaDeletes(targets []heroAuraAreaTargetDelete) ([][]byte, error) {
	packets := make([][]byte, 0, len(targets))
	for index, target := range targets {
		packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
			TargetID: target.objectID, InstanceID: target.instanceID,
		})
		if err != nil {
			return nil, fmt.Errorf("auraAreaDelete[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func marshalAuraAreaSpawn(
	objectID uint32, ownerObjectID uint32, position raknet.Vector3,
	definition sim.AbilityDefinition,
) ([][]byte, error) {
	if objectID == 0 || ownerObjectID == 0 || definition.SpawnNoun == "" ||
		definition.Radius <= 0 {
		return nil, errors.New("aura area spawn invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.ObjectCreateMessage{
			ObjectID: objectID, Noun: util.HashID(definition.SpawnNoun),
			PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
			Scale: definition.Radius, Team: 1, OwnerID: ownerObjectID,
			IsCollisionEnabled: false,
		},
	}
	if definition.ActivationEffectName != "" {
		if definition.Name == timeBubbleAbilityName {
			messages = append(messages, raknet.AttachedEffectMessage{
				Slot: 1, IsForceAttached: true,
				Asset:    util.HashID(definition.ActivationEffectName),
				ObjectID: objectID,
			})
		} else {
			messages = append(messages, raknet.ServerEventMessage{
				Asset: util.HashID(definition.ActivationEffectName), ObjectID: objectID,
				Position: position,
			})
		}
	}
	packets := make([][]byte, 0, len(messages))
	for index, current := range messages {
		packet, err := raknet.MarshalApplication(current)
		if err != nil {
			return nil, fmt.Errorf("auraAreaSpawnMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (r campaignAbilityCommandRuntime) handleHeroAuraArea(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	isStatusSupported := definition.StatusKind == sim.AbilityStatusKindSilence ||
		definition.StatusKind == sim.AbilityStatusKindSleep ||
		definition.StatusKind == sim.AbilityStatusKindSlow
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindAuraArea ||
		!isStatusSupported || definition.Radius <= 0 || definition.Duration <= 0 ||
		definition.RootModifierID == 0 || definition.AnimationName == "" ||
		(definition.SpawnNoun == "" && definition.ActivationEffectName == "") {
		r.registry.mutex.Unlock()
		return req.reject("aura area definition unavailable")
	}
	center := raknet.Vector3(peerSession.playerPosition)
	if definition.Range > 0 {
		admissionRange := heroAbilityAdmissionRange(creature, definition)
		center = req.command.Ability.TargetPosition
		if !isReportedZonePosition(center) {
			center = req.command.Ability.CursorPosition
		}
		if !isReportedZonePosition(center) || !isFiniteZonePosition(center) ||
			!isInsideZoneTrigger(peerSession.playerPosition, center, admissionRange) {
			r.registry.mutex.Unlock()
			return req.reject("aura area position unavailable")
		}
	}
	if peerSession.heroAuraAreas[activeAbilityID] != nil {
		r.registry.mutex.Unlock()
		return req.reject("aura area already active")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("auraAreaTiming: %w", err)
	}
	if projected.IsAreaDurationScaled {
		projected.NumberOfTicks, err = game.ResolveAreaDurationCount(
			projected.NumberOfTicks, creature.AreaDurationIncrease,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("auraAreaDuration: %w", err)
		}
		projected.Duration = time.Duration(projected.NumberOfTicks-1) *
			projected.TickDuration
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute, projected.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("auraAreaMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	previousManaPoint := peerSession.deployedManaPoint()
	objectID := uint32(0)
	if projected.SpawnNoun != "" {
		objectID, err = peerSession.reserveCampaignObjectID()
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("auraAreaObjectID: %w", err)
		}
	}
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: req.command.Common.Unknown[0], SourceID: req.command.Common.ObjectID,
		AbilityID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceTime: req.packet.SourceTime, AnimationName: projected.AnimationName,
		MuzzleEffectName: projected.MuzzleEffectName,
		HitDelay:         projected.HitDelay, ReleaseDelay: projected.ReleaseDelay,
		Cooldown: projected.Cooldown,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("auraAreaStart: %w", err)
	}
	manaPacket, err := abilityraknet.Mana(req.command.Common.ObjectID, remainingManaPoint)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("auraAreaManaPacket: %w", err)
	}
	cooldownReservation, isCooldownReserved := peerSession.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(activeAbilityID), abilityStartTime,
		projected.Cooldown,
	)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("aura area cooldown unavailable")
	}
	releaseReservation, isReleaseReserved := peerSession.abilityReleaseSession().Reserve(
		abilityStartTime, projected.ReleaseDelay,
	)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("aura area release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("auraAreaCommit: %w", err)
	}
	run := &heroAuraAreaRun{
		npc: peerSession.zone.NPCs(), modifierPool: r.modifierPool,
		targets: make(map[uint32]heroAuraAreaTarget), statusKind: projected.StatusKind,
		projectiles: make(map[uint32]heroAuraAreaProjectile), objectID: objectID,
	}
	if peerSession.heroAuraAreas == nil {
		peerSession.heroAuraAreas = make(map[uint32]*heroAuraAreaRun)
	}
	peerSession.heroAuraAreas[activeAbilityID] = run
	generation := peerSession.generation
	creatureIndex := peerSession.deployedCreatureIndex
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroAuraAreaSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		abilityID: activeAbilityID, creatureIndex: creatureIndex,
		previousManaPoint: previousManaPoint, center: center, creature: creature,
		definition: projected, binding: binding, run: run,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: start.Release,
	}
	stepCount := uint32(projected.Duration/heroAuraAreaScanInterval) + 1
	producers := make([]raknet.ScheduledPacketProducer, 0, stepCount+1)
	for index := uint32(0); index < stepCount; index++ {
		deadline := projected.HitDelay + time.Duration(index)*heroAuraAreaScanInterval
		step := heroAuraAreaStep{
			schedule: schedule, deadline: deadline, index: index,
			isFinal: index+1 == stepCount,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: projected.ReleaseDelay, Produce: schedule.release,
	})
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
		return nil, fmt.Errorf("auraAreaSchedule: %w", err)
	}
	run.mutex.Lock()
	run.cancel = cancel
	run.mutex.Unlock()
	r.logger.Printf(
		"RakNet hero aura area accepted ability=%s source=%d object=%d noun=%q effect=%q center=(%g,%g,%g)",
		projected.Name, req.command.Common.ObjectID, objectID, projected.SpawnNoun,
		projected.ActivationEffectName, center.X, center.Y, center.Z,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, start.Presentation...)
	return packets, nil
}
