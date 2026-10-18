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
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
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
	lease      *abilityraknet.ProjectileTimeBubbleLease
}

type heroAuraAreaRun struct {
	mutex             sync.Mutex
	cast              *channelCast
	npc               *zonenpc.Session
	modifierPool      *modifierPool
	targets           map[uint32]heroAuraAreaTarget
	projectiles       map[uint32]heroAuraAreaProjectile
	statusKind        sim.AbilityStatusKind
	objectID          uint32
	cancel            raknet.CancelSchedule
	isCleaned         bool
	isTimeBubble      bool
	isStopping        bool
	isObjectPublished bool
	isSleepingCloud   bool
	expiresAt         time.Time
	runtime           campaignAbilityCommandRuntime
	originalZone      *zone.Zone
	stopContext       func() bool
}

func (e *heroAuraAreaRun) Stop() ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	if e.isTimeBubble {
		e.stopTimeBubble()
		return nil, nil
	}
	e.stopAura()
	return nil, nil
}

type heroAuraAreaTargetDelete struct {
	objectID   uint32
	instanceID uint32
}

func (r campaignDamageRuntime) breakSleepingCloudOnDamage(
	sessionKey string, generation uint64, objectID uint32,
	descriptorMask uint32,
) ([][]byte, error) {
	if objectID == 0 || descriptorMask&4 != 0 {
		return nil, nil
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.zone == nil {
		return nil, nil
	}
	// Keep each exact run locked until its deletions are queued and its handles
	// are retired. A scan or cleanup cannot replace a membership in between.
	runs := make(map[*heroAuraAreaRun]heroAuraAreaTarget)
	deletes := make([]heroAuraAreaTargetDelete, 0)
	for _, candidate := range r.registry.sessions {
		if candidate.zone != peerSession.zone {
			continue
		}
		for _, run := range candidate.heroAuraAreas {
			if run == nil || run.statusKind != sim.AbilityStatusKindSleep ||
				run.npc != peerSession.zone.NPCs() {
				continue
			}
			if _, isTracked := runs[run]; isTracked {
				continue
			}
			run.mutex.Lock()
			if run.isCleaned {
				run.mutex.Unlock()
				continue
			}
			target, isTracked := run.targets[objectID]
			if !isTracked {
				run.mutex.Unlock()
				continue
			}
			defer run.mutex.Unlock()
			runs[run] = target
			deletes = append(deletes, heroAuraAreaTargetDelete{
				objectID: objectID, instanceID: target.instanceID,
			})
		}
	}
	packets, err := marshalAuraAreaDeletes(deletes)
	if err != nil {
		return nil, fmt.Errorf("sleepBreakMarshal: %w", err)
	}
	for run, target := range runs {
		run.clearStatus(objectID, target.expiresAt)
		delete(run.targets, objectID)
	}
	// Include the attacker in the same queue boundary. Returning these packets
	// to the caller could publish a stale deletion after the next scan's create.
	for candidateKey, candidate := range r.registry.sessions {
		if candidate.zone != peerSession.zone {
			continue
		}
		candidate.queuePackets(packets)
		r.registry.sessions[candidateKey] = candidate
	}
	var releaseErr error
	for run, target := range runs {
		err = run.modifierPool.Release(target.instanceID)
		if err != nil && releaseErr == nil {
			releaseErr = fmt.Errorf("sleepBreakRelease[%d]: %w", target.instanceID, err)
		}
	}
	return nil, releaseErr
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
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	abilityID      uint32
	center         raknet.Vector3
	creature       game.GameplayCreature
	definition     sim.AbilityDefinition
	binding        game.GameplayBinding
	run            *heroAuraAreaRun
	releasePacket  []byte
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
	e.run.mutex.Lock()
	isActive := !e.run.isCleaned && !e.run.isStopping
	e.run.mutex.Unlock()
	return isActive && isFound && peerSession.generation == e.generation &&
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
	// A scan can commit field presentation, statuses or damage before a later
	// encoding/publication failure. Its paid activation is no longer rollbackable.
	e.run.cast.activateLocked(e.run.cast.revision)
	if e.run.isTimeBubble && index == 0 {
		spawnPackets, spawnErr := marshalAuraAreaSpawn(e.run.objectID, e.sourceObjectID, e.center, e.definition)
		if spawnErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("bubbleSpawn: %w", spawnErr)
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.queueTimeBubblePresentationLocked(e.run.originalZone, spawnPackets)
		peerSession = e.runtime.registry.sessions[e.sessionKey]
		e.run.mutex.Lock()
		e.run.isObjectPublished = true
		e.run.mutex.Unlock()
	}
	if index == 0 && e.definition.SpawnNoun != "" && !e.run.isTimeBubble {
		spawnPackets, marshalErr := marshalAuraAreaSpawn(
			e.run.objectID, e.sourceObjectID, e.center, e.definition,
		)
		if marshalErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("auraAreaSpawn: %w", marshalErr)
		}
		e.queueNPCMembershipLocked(&peerSession, spawnPackets, true)
		e.run.mutex.Lock()
		e.run.isObjectPublished = true
		e.run.mutex.Unlock()
	} else if index == 0 && e.definition.ActivationEffectName != "" && !e.run.isTimeBubble {
		effectPacket, marshalErr := raknet.MarshalApplication(raknet.DropPresentationMessage{
			Asset: util.HashID(e.definition.ActivationEffectName), Position: e.center,
		})
		if marshalErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("auraAreaEffect: %w", marshalErr)
		}
		e.queueNPCMembershipLocked(&peerSession, [][]byte{effectPacket}, true)
	}
	statusPackets, err := e.reconcile(&peerSession, deadline)
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
		e.run.mutex.Lock()
		e.run.cancel = nil
		e.run.mutex.Unlock()
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	if isFinal {
		cleanupErr := e.run.retireAuraLocked()
		peerSession = e.runtime.registry.sessions[e.sessionKey]
		if cleanupErr != nil {
			if !e.run.isTimeBubble {
				auraRetirement{run: e.run}.schedule(heroAuraAreaScanInterval)
			}
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("auraFinalRetire: %w", cleanupErr)
		}
	}
	e.runtime.registry.mutex.Unlock()

	packets := statusPackets
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
	if isFinal && e.run.isSleepingCloud {
		releasePackets, err := e.release()
		if err != nil {
			return nil, fmt.Errorf("sleepFinalRelease: %w", err)
		}
		packets = append(packets, releasePackets...)
	}
	return packets, nil
}

func (e heroAuraAreaSchedule) reconcile(
	peerSession *gameplayPeerSession, deadline time.Duration,
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
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isCleaned || e.run.isStopping {
		return nil, nil
	}
	// Queue each accepted membership before advancing to a fallible target.
	for objectID, tracked := range e.run.targets {
		if _, isInside := live[objectID]; isInside {
			continue
		}
		packets, err := marshalAuraAreaDeletes([]heroAuraAreaTargetDelete{{objectID: objectID, instanceID: tracked.instanceID}})
		if err != nil {
			return nil, fmt.Errorf("auraExitEncode: %w", err)
		}
		e.queueNPCMembershipLocked(peerSession, packets, false)
		e.run.clearStatus(objectID, tracked.expiresAt)
		delete(e.run.targets, objectID)
		e.run.releaseAuraInstance(tracked.instanceID)
	}
	for objectID := range live {
		if _, isTracked := e.run.targets[objectID]; isTracked {
			continue
		}
		instanceID, err := e.run.modifierPool.Allocate()
		if err != nil {
			return nil, fmt.Errorf("auraEntryAllocate: %w", err)
		}
		packets, err := e.prepareNPCMembership(objectID, instanceID, deadline)
		if err != nil {
			e.run.releaseAuraInstance(instanceID)
			return nil, fmt.Errorf("auraEntryEncode: %w", err)
		}
		err = e.applyStatus(peerSession.zone.NPCs(), objectID, expiresAt)
		if err != nil {
			e.run.releaseAuraInstance(instanceID)
			return nil, fmt.Errorf("auraEntryApply: %w", err)
		}
		if e.statusRemaining(peerSession.zone.NPCs(), objectID, now) == 0 {
			e.run.releaseAuraInstance(instanceID)
			continue
		}
		e.run.targets[objectID] = heroAuraAreaTarget{instanceID: instanceID, expiresAt: expiresAt}
		e.queueNPCMembershipLocked(peerSession, packets, true)
	}
	if e.run.isTimeBubble {
		err := e.reconcileTimeBubbleLocked(peerSession, deadline, now)
		if err != nil {
			// This scan owns only tentative additions. Keep accepted memberships
			// and retry on the next scan rather than failing the whole bubble.
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("RakNet Time Bubble membership scan deferred: %v", err)
			}
		}
		return nil, nil
	}
	return nil, nil
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
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets := [][]byte{e.releasePacket}
	if e.run.isSleepingCloud && e.definition.OutAnimationName != "" {
		packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
			ObjectID: e.sourceObjectID, State: util.HashID(e.definition.OutAnimationName),
			Timestamp: e.packet.SourceTime + uint64(e.definition.ReleaseDelay/time.Millisecond),
			Scale:     1,
		})
		if err != nil {
			return nil, fmt.Errorf("sleepRelax: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroAuraAreaSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroAuraAreas, e.abilityID)
	}
	refundPacket, refundErr := e.run.cast.failLocked(&peerSession, isFound, e.run.cast.revision)
	if isFound {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	if len(refundPacket) != 0 {
		e.runtime.registry.queueChannelPresentationLocked(e.run.originalZone, [][]byte{refundPacket})
		peerSession = e.runtime.registry.sessions[e.sessionKey]
	}
	if refundErr != nil {
		e.runtime.logger.Printf("RakNet hero aura refund failed: %v", refundErr)
	}
	cleanupErr := e.run.retireAuraLocked()
	if cleanupErr != nil {
		e.runtime.logger.Printf("RakNet hero aura cleanup deferred: %v", cleanupErr)
		if !e.run.isTimeBubble {
			auraRetirement{run: e.run}.schedule(heroAuraAreaScanInterval)
		}
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
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
	objectID := uint32(0)
	if projected.SpawnNoun != "" {
		objectID, err = peerSession.reserveCampaignObjectID()
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("auraAreaObjectID: %w", err)
		}
	}
	isSleepingCloud := projected.Name == "SleepingCloud"
	channelDuration := time.Duration(0)
	startHitDelay := projected.HitDelay
	if isSleepingCloud {
		// Packaged Lua waits until timetoendanimation from activation, not
		// from the first trigger scan. Its channel bar spans that entire cast.
		channelDuration = projected.ReleaseDelay
		startHitDelay = 0
		projected.Duration = projected.ReleaseDelay - projected.HitDelay
	}
	start, err := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: req.command.Common.Unknown[0], SourceID: req.command.Common.ObjectID,
		AbilityID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceTime: req.packet.SourceTime, AnimationName: projected.AnimationName,
		MuzzleEffectName: projected.MuzzleEffectName,
		HitDelay:         startHitDelay, ReleaseDelay: projected.ReleaseDelay,
		Cooldown: projected.Cooldown, ChannelDuration: channelDuration,
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
	cast := r.registry.reserveChannelCastLocked(peerSession, req.command.Common.ObjectID, cooldownReservation, releaseReservation)
	movementPackets := make([][]byte, 0)
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil && isSleepingCloud {
		movementPackets, err = marshalZonePlayerStop(req.command.Common.ObjectID, peerSession.playerPosition)
	}
	if err == nil {
		err = cast.debitLocked(&peerSession, manaCost)
	}
	if err != nil {
		refundPacket, refundErr := cast.failLocked(&peerSession, true, cast.revision)
		r.registry.sessions[sessionKey] = peerSession
		if len(refundPacket) != 0 {
			r.registry.queueChannelPresentationLocked(peerSession.zone, [][]byte{refundPacket})
		}
		if refundErr != nil {
			r.logger.Printf("RakNet hero aura admission refund failed: %v", refundErr)
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("auraAreaCommit: %w", err)
	}
	run := &heroAuraAreaRun{
		cast:            cast,
		isSleepingCloud: isSleepingCloud, expiresAt: abilityStartTime.Add(projected.ReleaseDelay),
		runtime: r, originalZone: peerSession.zone,
		isTimeBubble: definition.Name == timeBubbleAbilityName,
		npc:          peerSession.zone.NPCs(), modifierPool: r.modifierPool,
		targets: make(map[uint32]heroAuraAreaTarget), statusKind: projected.StatusKind,
		projectiles: make(map[uint32]heroAuraAreaProjectile), objectID: objectID,
	}
	if peerSession.heroAuraAreas == nil {
		peerSession.heroAuraAreas = make(map[uint32]*heroAuraAreaRun)
	}
	peerSession.heroAuraAreas[activeAbilityID] = run
	generation := peerSession.generation
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	if run.isTimeBubble {
		run.retainTimeBubbleLocked()
	} else {
		run.retainAuraLocked()
	}
	r.registry.mutex.Unlock()

	schedule := heroAuraAreaSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		abilityID: activeAbilityID, center: center, creature: creature,
		definition: projected, binding: binding, run: run,
		releasePacket: start.Release,
	}
	stepCount := uint32(projected.Duration/heroAuraAreaScanInterval) + 1
	producers := make([]raknet.ScheduledPacketProducer, 0, stepCount+1)
	for index := uint32(0); index < stepCount; index++ {
		deadline := projected.HitDelay + time.Duration(index)*heroAuraAreaScanInterval
		if isSleepingCloud && index+1 == stepCount {
			deadline = projected.ReleaseDelay
		}
		step := heroAuraAreaStep{
			schedule: schedule, deadline: deadline, index: index,
			isFinal: index+1 == stepCount,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	if !isSleepingCloud {
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: projected.ReleaseDelay, Produce: schedule.release,
		})
	}
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
	if err == nil && cancel == nil {
		err = errors.New("schedule handle unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		schedule.fail(err)
		return nil, fmt.Errorf("auraAreaSchedule: %w", err)
	}
	run.mutex.Lock()
	isRetired := run.isCleaned || run.isStopping
	if !isRetired {
		run.cancel = cancel
	}
	run.mutex.Unlock()
	if isRetired && cancel != nil {
		cancel()
	}
	admission := auraAreaAdmission{run: run, revision: cast.revision}
	err = req.packet.AfterResponseCommit(admission.commit)
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("auraAdmissionCommit: %w", err)
	}
	r.logger.Printf(
		"RakNet hero aura area accepted ability=%s source=%d object=%d noun=%q effect=%q center=(%g,%g,%g)",
		projected.Name, req.command.Common.ObjectID, objectID, projected.SpawnNoun,
		projected.ActivationEffectName, center.X, center.Y, center.Z,
	)
	packets := append([][]byte{start.Acknowledge, manaPacket}, movementPackets...)
	packets = append(packets, start.Presentation...)
	return packets, nil
}

type auraAreaAdmission struct {
	run      *heroAuraAreaRun
	revision uint64
}

func (e auraAreaAdmission) commit() {
	e.run.runtime.registry.mutex.Lock()
	defer e.run.runtime.registry.mutex.Unlock()
	// Response publication seals payment even if cancellation retired visuals.
	e.run.cast.activateLocked(e.revision)
}
