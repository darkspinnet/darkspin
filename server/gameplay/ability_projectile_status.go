package gameplay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type heroProjectileStatusRun struct {
	mutex             sync.Mutex
	projectile        *abilityraknet.ProjectileRun
	npc               *zonenpc.Session
	modifierPool      *modifierPool
	targetID          uint32
	instanceID        uint32
	expiresAt         time.Time
	statusKind        sim.AbilityStatusKind
	splashTargets     []heroProjectileStatusDelete
	cancel            raknet.CancelSchedule
	isApplied         bool
	completedTick     uint32
	definition        sim.AbilityDefinition
	creature          game.GameplayCreature
	sourceObjectID    uint32
	lastScanPosition  game.Vec3
	isScanPositionSet bool
	isCleaned         bool
}

func (e *heroProjectileStatusRun) advanceScanPosition(
	position game.Vec3,
) (game.Vec3, game.Vec3) {
	if e == nil {
		return position, position
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	previous := position
	if e.isScanPositionSet {
		previous = e.lastScanPosition
	}
	e.lastScanPosition = position
	e.isScanPositionSet = true
	return previous, position
}

func (e *heroProjectileStatusRun) Stop() {
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
	if e.projectile != nil {
		e.projectile.Finish()
	}
	e.Cleanup()
	e.CleanupSplash()
}

func (e *heroProjectileStatusRun) Apply(
	instanceID uint32, expiresAt time.Time,
) bool {
	if e == nil || instanceID == 0 || expiresAt.IsZero() {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned || e.isApplied {
		return false
	}
	e.instanceID = instanceID
	e.expiresAt = expiresAt
	e.isApplied = true
	return true
}

func (e *heroProjectileStatusRun) IsApplied() bool {
	if e == nil {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.isApplied && !e.isCleaned
}

func (e *heroProjectileStatusRun) ApplySplash(
	targetID uint32, instanceID uint32, expiresAt time.Time,
) bool {
	if e == nil || targetID == 0 || instanceID == 0 || expiresAt.IsZero() {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return false
	}
	for _, target := range e.splashTargets {
		if target.targetID == targetID {
			return false
		}
	}
	e.splashTargets = append(e.splashTargets, heroProjectileStatusDelete{
		targetID: targetID, instanceID: instanceID, expiresAt: expiresAt,
	})
	return true
}

func (e *heroProjectileStatusRun) HasSplash(targetID uint32) bool {
	if e == nil || targetID == 0 {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for _, target := range e.splashTargets {
		if target.targetID == targetID {
			return true
		}
	}
	return false
}

func (e *heroProjectileStatusRun) Splash(
	targetID uint32,
) (heroProjectileStatusDelete, bool) {
	if e == nil || targetID == 0 {
		return heroProjectileStatusDelete{}, false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for _, target := range e.splashTargets {
		if target.targetID == targetID {
			return target, true
		}
	}
	return heroProjectileStatusDelete{}, false
}

func (e *heroProjectileStatusRun) AdvanceSplashTick(targetID uint32) {
	if e == nil || targetID == 0 {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for index := range e.splashTargets {
		if e.splashTargets[index].targetID == targetID {
			e.splashTargets[index].completedTick++
			return
		}
	}
}

func (e *heroProjectileStatusRun) RemoveSplash(
	targetID uint32, expiresAt time.Time,
) heroProjectileStatusDelete {
	if e == nil || targetID == 0 || expiresAt.IsZero() {
		return heroProjectileStatusDelete{}
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for index, target := range e.splashTargets {
		if target.targetID != targetID || target.expiresAt != expiresAt {
			continue
		}
		e.clearSplashStatus(target)
		_ = e.modifierPool.Release(target.instanceID)
		e.splashTargets = append(
			e.splashTargets[:index], e.splashTargets[index+1:]...,
		)
		return target
	}
	return heroProjectileStatusDelete{}
}

func (e *heroProjectileStatusRun) Cleanup() heroProjectileStatusDelete {
	if e == nil {
		return heroProjectileStatusDelete{}
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return heroProjectileStatusDelete{}
	}
	e.isCleaned = true
	if e.isApplied {
		switch e.statusKind {
		case sim.AbilityStatusKindFear:
			e.npc.ClearFear(e.targetID, e.expiresAt)
		case sim.AbilityStatusKindCurse:
			e.npc.ClearCurse(e.targetID, e.expiresAt)
		case sim.AbilityStatusKindStun:
			e.npc.ClearStun(e.targetID, e.expiresAt)
		}
		_ = e.modifierPool.Release(e.instanceID)
	}
	return heroProjectileStatusDelete{
		targetID: e.targetID, instanceID: e.instanceID,
	}
}

func (e *heroProjectileStatusRun) CleanupSplash() []heroProjectileStatusDelete {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	targets := e.splashTargets
	e.splashTargets = nil
	e.mutex.Unlock()
	for _, target := range targets {
		e.clearSplashStatus(target)
		_ = e.modifierPool.Release(target.instanceID)
	}
	return targets
}

func (e *heroProjectileStatusRun) clearSplashStatus(
	target heroProjectileStatusDelete,
) {
	switch e.statusKind {
	case sim.AbilityStatusKindFear:
		e.npc.ClearFear(target.targetID, target.expiresAt)
	case sim.AbilityStatusKindCurse:
		e.npc.ClearCurse(target.targetID, target.expiresAt)
	case sim.AbilityStatusKindStun:
		e.npc.ClearStun(target.targetID, target.expiresAt)
	}
}

type heroProjectileStatusDelete struct {
	targetID      uint32
	instanceID    uint32
	expiresAt     time.Time
	completedTick uint32
}

type heroProjectileStatusSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	targetObjectID      uint32
	projectileObjectID  uint32
	creatureIndex       uint32
	previousManaPoint   float32
	impactDeadline      time.Duration
	sourcePosition      game.Vec3
	targetPosition      raknet.Vector3
	travelDistance      float32
	actorFootprint      float32
	geometry            zonenpc.ProjectileGeometry
	facing              raknet.Vector3
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	damage              game.DamageRange
	binding             game.GameplayBinding
	run                 *heroProjectileStatusRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

type heroProjectileStatusStep struct {
	schedule heroProjectileStatusSchedule
	deadline time.Duration
}

func (e heroProjectileStatusStep) advance() ([][]byte, error) {
	e.schedule.runtime.registry.mutex.RLock()
	peerSession, isFound :=
		e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
	isCurrent := e.schedule.isCurrent(peerSession, isFound)
	e.schedule.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.schedule.run.projectile.Advance(context.Background(), e.deadline)
	if err != nil {
		return nil, fmt.Errorf("projectileStatusAdvance: %w", err)
	}
	return packets, nil
}

func (e heroProjectileStatusStep) tick() ([][]byte, error) {
	return e.schedule.tick(e.deadline)
}

func (e heroProjectileStatusSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroProjectileRuns[e.projectileObjectID] == e.run
}

func (e heroProjectileStatusSchedule) impact() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	isTargetValid := isTargetFound && !target.IsDefeated && target.HitPoint > 0 &&
		target.Faction == zonenpc.FactionNonPlayerAligned
	if !isTargetValid {
		packets, err := e.run.projectile.ResolveCollision(
			context.Background(), e.impactDeadline, false, false, 0,
			e.damage.Maximum, false, sim.Position(e.targetPosition), sim.Position(e.facing),
		)
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("projectileStatusMiss: %w", err)
		}
		return packets, nil
	}
	launchPosition := campaignProjectileLaunchPosition(
		e.sourcePosition, game.Vec3(e.targetPosition), e.actorFootprint,
	)
	collision, err := zonenpc.ResolveProjectileCollision(
		launchPosition, game.Vec3(e.targetPosition), target.Plan.Position,
		e.travelDistance, e.geometry,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusCollision: %w", err)
	}
	if !collision.IsDirectHit {
		currentAimPosition := campaignProjectileAimPosition(
			target.Plan.Position, target.Plan.NPCProfile.FootprintRadius,
			e.geometry,
		)
		packets, resolveErr := e.run.projectile.ResolveCollision(
			context.Background(), e.impactDeadline, false, false, 0,
			e.damage.Maximum, false, collision.Position, sim.Position(e.facing),
		)
		e.runtime.registry.mutex.Unlock()
		if resolveErr != nil {
			return nil, fmt.Errorf("projectileStatusDodge: %w", resolveErr)
		}
		e.runtime.logger.Printf(
			"RakNet projectile trajectory resolved kind=hero-status projectile=%d source=%d target=%d ability=%q outcome=miss aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) endpoint=(%.3f,%.3f,%.3f) drift=%.3f",
			e.projectileObjectID, e.sourceObjectID, e.targetObjectID,
			e.definition.Name, e.targetPosition.X, e.targetPosition.Y,
			e.targetPosition.Z, currentAimPosition.X, currentAimPosition.Y,
			currentAimPosition.Z, collision.Position.X, collision.Position.Y,
			collision.Position.Z, zonegeometry.Distance(
				game.Vec3(e.targetPosition), currentAimPosition,
			),
		)
		return packets, nil
	}
	impactPosition := raknet.Vector3(collision.Position)
	currentAimPosition := campaignProjectileAimPosition(
		target.Plan.Position, target.Plan.NPCProfile.FootprintRadius,
		e.geometry,
	)
	facing := zoneability.ProjectileDirection(
		raknet.Vector3(e.sourcePosition), impactPosition,
	)
	packets, err := e.run.projectile.ResolveCollision(
		context.Background(), e.impactDeadline, true, true, target.HitPoint,
		e.damage.Maximum, false, sim.Position(impactPosition), sim.Position(facing),
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusImpact: %w", err)
	}
	e.runtime.logger.Printf(
		"RakNet projectile trajectory resolved kind=hero-status projectile=%d source=%d target=%d ability=%q outcome=hit aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) contact=(%.3f,%.3f,%.3f) drift=%.3f",
		e.projectileObjectID, e.sourceObjectID, e.targetObjectID,
		e.definition.Name, e.targetPosition.X, e.targetPosition.Y,
		e.targetPosition.Z, currentAimPosition.X, currentAimPosition.Y,
		currentAimPosition.Z, impactPosition.X, impactPosition.Y,
		impactPosition.Z, zonegeometry.Distance(
			game.Vec3(e.targetPosition), currentAimPosition,
		),
	)
	packets = filterProjectileCombatPackets(packets)
	expiresAt := e.runtime.now().Add(e.definition.StatusDuration)
	err = e.applyStatus(peerSession.zone.NPCs(), expiresAt)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusApply: %w", err)
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		e.clearStatus(peerSession.zone.NPCs(), expiresAt)
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusModifier: %w", err)
	}
	if !e.run.Apply(instanceID, expiresAt) {
		e.clearStatus(peerSession.zone.NPCs(), expiresAt)
		_ = e.runtime.modifierPool.Release(instanceID)
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	modifierPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: e.targetObjectID, ModifierGUID: e.definition.RootModifierID,
		InstanceID:           instanceID,
		DurationMilliseconds: uint32(e.definition.StatusDuration.Milliseconds()),
		StackCount:           1,
		StartMilliseconds:    e.packet.SourceTime + uint64(e.impactDeadline/time.Millisecond),
		SourceID:             e.sourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("projectileStatusModifierMarshal: %w", err)
	}
	packets = append(packets, modifierPacket)
	if e.definition.StatusKind != sim.AbilityStatusKindFear {
		return packets, nil
	}
	splashPackets, splashTargetIDs, err := e.applyTerrifySplash(
		impactPosition, expiresAt,
		e.packet.SourceTime+uint64(e.impactDeadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("projectileStatusSplash: %w", err)
	}
	packets = append(packets, splashPackets...)
	fleePackets, err := e.runtime.startTerrifyFlee(
		e.packet, e.sessionKey, e.generation, e.targetObjectID,
		e.sourceObjectID,
		e.packet.SourceTime+uint64(e.impactDeadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("projectileStatusFlee: %w", err)
	}
	packets = append(packets, fleePackets...)
	for _, targetObjectID := range splashTargetIDs {
		fleePackets, err = e.runtime.startTerrifyFlee(
			e.packet, e.sessionKey, e.generation, targetObjectID,
			e.sourceObjectID,
			e.packet.SourceTime+uint64(e.impactDeadline/time.Millisecond),
		)
		if err != nil {
			return nil, fmt.Errorf("projectileStatusSplashFlee: %w", err)
		}
		packets = append(packets, fleePackets...)
	}
	return packets, nil
}

func (e heroProjectileStatusSchedule) applyTerrifySplash(
	center raknet.Vector3, expiresAt time.Time, timestamp uint64,
) ([][]byte, []uint32, error) {
	if e.definition.SecondaryModifierID == 0 || e.definition.Radius <= 0 {
		return nil, nil, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil, nil
	}
	type splashModifier struct {
		targetID   uint32
		instanceID uint32
	}
	modifiers := make([]splashModifier, 0)
	for _, candidate := range peerSession.zone.NPCs().LiveSnapshots() {
		if candidate.Plan.ObjectID == e.targetObjectID || candidate.Plan.IsFixture ||
			!candidate.IsPublished || candidate.HitPoint <= 0 ||
			candidate.Faction != zonenpc.FactionNonPlayerAligned ||
			candidate.Plan.Position.Sub(game.Vec3(center)).Length() > e.definition.Radius {
			continue
		}
		err := peerSession.zone.NPCs().ApplyFear(candidate.Plan.ObjectID, expiresAt)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, nil, fmt.Errorf("splashFear: %w", err)
		}
		instanceID, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			peerSession.zone.NPCs().ClearFear(candidate.Plan.ObjectID, expiresAt)
			e.runtime.registry.mutex.Unlock()
			return nil, nil, fmt.Errorf("splashModifier: %w", err)
		}
		if !e.run.ApplySplash(candidate.Plan.ObjectID, instanceID, expiresAt) {
			peerSession.zone.NPCs().ClearFear(candidate.Plan.ObjectID, expiresAt)
			_ = e.runtime.modifierPool.Release(instanceID)
			continue
		}
		modifiers = append(modifiers, splashModifier{
			targetID: candidate.Plan.ObjectID, instanceID: instanceID,
		})
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(modifiers))
	targetObjectIDs := make([]uint32, 0, len(modifiers))
	for index, modifier := range modifiers {
		packet, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID:             modifier.targetID,
			ModifierGUID:         e.definition.SecondaryModifierID,
			InstanceID:           modifier.instanceID,
			DurationMilliseconds: uint32(e.definition.StatusDuration.Milliseconds()),
			StackCount:           1, StartMilliseconds: timestamp, SourceID: e.sourceObjectID,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("splashMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
		targetObjectIDs = append(targetObjectIDs, modifier.targetID)
	}
	return packets, targetObjectIDs, nil
}

func (e heroProjectileStatusSchedule) tick(
	deadline time.Duration,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || !e.run.IsApplied() {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	definition := e.definition
	definition.MinimumDamage = definition.MinimumDamagePerTick
	definition.MaximumDamage = definition.MaximumDamagePerTick
	plan := zoneability.AreaPlan{
		SourceObjectID: e.sourceObjectID, AbilityID: e.projectileObjectID,
		Definition: definition, Damage: e.damage,
		Center: target.Plan.Position, Target: []zonenpc.Snapshot{target},
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), plan,
		e.creature, peerSession.binding.Difficulty, e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusDamage: %w", err)
	}
	e.run.mutex.Lock()
	e.run.completedTick++
	e.run.mutex.Unlock()
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("projectileStatusTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		e.packet.SourceTime+uint64(deadline/time.Millisecond), e.binding,
		results, transitions, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("projectileStatusPublish: %w", err)
	}
	return packets, nil
}

func (e heroProjectileStatusSchedule) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroProjectileRuns, e.projectileObjectID)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	e.run.projectile.Finish()
	deleted := e.run.Cleanup()
	deletedTargets := e.run.CleanupSplash()
	if deleted.instanceID != 0 {
		deletedTargets = append([]heroProjectileStatusDelete{deleted}, deletedTargets...)
	}
	if len(deletedTargets) == 0 {
		return nil, nil
	}
	packets := make([][]byte, 0, len(deletedTargets))
	for index, target := range deletedTargets {
		packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
			TargetID: target.targetID, InstanceID: target.instanceID,
		})
		if err != nil {
			return nil, fmt.Errorf("projectileStatusDelete[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroProjectileStatusSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.run.projectile.Advance(
		context.Background(), e.definition.ReleaseDelay,
	)
	if err != nil {
		return nil, fmt.Errorf("projectileStatusRelease: %w", err)
	}
	return append(packets, e.releasePacket), nil
}

func (e heroProjectileStatusSchedule) applyStatus(
	npc *zonenpc.Session, expiresAt time.Time,
) error {
	switch e.definition.StatusKind {
	case sim.AbilityStatusKindFear:
		return npc.ApplyFear(e.targetObjectID, expiresAt)
	case sim.AbilityStatusKindCurse:
		return npc.ApplyCurse(e.targetObjectID, expiresAt)
	case sim.AbilityStatusKindStun:
		return npc.ApplyStun(e.targetObjectID, expiresAt)
	default:
		return errors.New("unsupported projectile status")
	}
}

func (e heroProjectileStatusSchedule) clearStatus(
	npc *zonenpc.Session, expiresAt time.Time,
) {
	switch e.definition.StatusKind {
	case sim.AbilityStatusKindFear:
		npc.ClearFear(e.targetObjectID, expiresAt)
	case sim.AbilityStatusKindCurse:
		npc.ClearCurse(e.targetObjectID, expiresAt)
	case sim.AbilityStatusKindStun:
		npc.ClearStun(e.targetObjectID, expiresAt)
	}
}

func (e heroProjectileStatusSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.heroProjectileRuns, e.projectileObjectID)
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
		"RakNet hero projectile status stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleHeroProjectileStatus(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, targetObjectID uint32, sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	isStatusSupported := definition.StatusKind == sim.AbilityStatusKindFear ||
		definition.StatusKind == sim.AbilityStatusKindCurse ||
		definition.StatusKind == sim.AbilityStatusKindStun
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindProjectileStatus ||
		!isStatusSupported || definition.Range <= 0 || definition.Speed <= 0 ||
		definition.Distance <= 0 || definition.StatusDuration <= 0 ||
		len(definition.HitDelays) == 0 || definition.MinimumDamagePerTick <= 0 ||
		definition.MaximumDamagePerTick < definition.MinimumDamagePerTick ||
		definition.ProjectileNoun == "" || definition.AnimationName == "" ||
		definition.RootModifierID == 0 {
		r.registry.mutex.Unlock()
		return req.reject("projectile status definition unavailable")
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(targetObjectID)
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 ||
		target.Faction != zonenpc.FactionNonPlayerAligned ||
		(definition.Name == "AfflictionBolt" &&
			(peerSession.zone.NPCs().CurseRemaining(targetObjectID, r.now()) > 0 ||
				peerSession.zone.NPCs().IsStealthed(targetObjectID))) ||
		!isInsideZoneTrigger(
			peerSession.playerPosition, raknet.Vector3(target.Plan.Position),
			admissionRange,
		) {
		r.registry.mutex.Unlock()
		return req.reject("projectile status target unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusTiming: %w", err)
	}
	damage, err := zoneability.ProjectDamage(
		creature, projected, projected.MinimumDamagePerTick,
		projected.MaximumDamagePerTick,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusDamage: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute, projected.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	projected.Speed, err = zoneability.ProjectProjectileSpeed(creature, projected)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusSpeed: %w", err)
	}
	previousProjectileObjectID := peerSession.nextProjectileObjectID
	projectileObjectID, err := peerSession.reserveCampaignProjectileIDs(1, 1000)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusObjectID: %w", err)
	}
	actorPosition := peerSession.playerPosition
	actorFootprint := peerSession.deployedCampaignFootprintRadius()
	geometry, _, err := campaignTargetProjectileGeometry(r.program, target)
	if err != nil {
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusGeometry: %w", err)
	}
	actorHeight := actorFootprint
	actorPhysics, isActorPhysicsFound := r.program.NounPhysicsByID[creature.Noun]
	if isActorPhysicsFound {
		actorHeight = max(
			actorHeight,
			(actorPhysics.BoundMinimum.Z+actorPhysics.BoundMaximum.Z)*0.5,
		)
	}
	actorPosition.Z += max(float32(0), actorHeight)
	targetPosition := raknet.Vector3(campaignProjectileAimPosition(
		target.Plan.Position, target.Plan.NPCProfile.FootprintRadius, geometry,
	))
	facing := zoneability.ProjectileDirection(actorPosition, targetPosition)
	launchPosition := campaignProjectileLaunchPosition(
		game.Vec3(actorPosition), game.Vec3(targetPosition), actorFootprint,
	)
	travelDistance := zoneability.Distance(
		launchPosition, game.Vec3(targetPosition),
	)
	travelDelay := max(
		abilityraknet.ProjectileCollisionTick,
		time.Duration(float64(travelDistance)/float64(projected.Speed)*float64(time.Second)),
	)
	impactDeadline := projected.HitDelay + travelDelay
	projectileRun, immediatePackets, err := abilityraknet.NewProjectileRun(
		abilityraknet.ProjectileInput{
			Ability: projected, ActorObjectID: req.command.Common.ObjectID,
			TargetObjectID: targetObjectID, ProjectileObjectID: projectileObjectID,
			ActorPosition: sim.Position(actorPosition), TargetPosition: sim.Position(targetPosition),
			ActorFacing: sim.Position(facing), ImpactPosition: sim.Position(targetPosition),
			FootprintRadius: actorFootprint,
			CollisionDelay:  travelDelay, IsDirectHit: true,
			IsCollisionExternallyDriven: true, Damage: damage.Maximum,
			TargetHitPoint: target.HitPoint, ActorTeam: 1,
			SourceTime: req.packet.SourceTime,
		},
	)
	if err != nil {
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusRun: %w", err)
	}
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime + uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    req.packet.SourceTime + uint64(projected.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], activeAbilityID, req.command.Ability.Index,
		req.packet.SourceTime, projected.HitDelay, projected.ReleaseDelay,
	)
	if err != nil {
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusReleasePacket: %w", err)
	}
	cooldownPacket, err := abilityraknet.Cooldown(abilityraknet.CooldownRequest{
		ObjectID: req.command.Common.ObjectID, AbilityID: activeAbilityID,
		Duration: projected.Cooldown, StartTime: req.packet.SourceTime,
	})
	if err != nil {
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusCooldownPacket: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	manaPacket, err := abilityraknet.Mana(req.command.Common.ObjectID, remainingManaPoint)
	if err != nil {
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusManaPacket: %w", err)
	}
	previousManaPoint := peerSession.deployedManaPoint()
	cooldownReservation, isCooldownReserved := peerSession.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(activeAbilityID), abilityStartTime,
		projected.Cooldown,
	)
	if !isCooldownReserved {
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return req.reject("projectile status cooldown unavailable")
	}
	releaseReservation, isReleaseReserved := peerSession.abilityReleaseSession().Reserve(
		abilityStartTime, projected.ReleaseDelay,
	)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return req.reject("projectile status release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileStatusCommit: %w", err)
	}
	run := &heroProjectileStatusRun{
		projectile: projectileRun, npc: peerSession.zone.NPCs(),
		modifierPool: r.modifierPool, targetID: targetObjectID,
		statusKind: projected.StatusKind,
		definition: projected, creature: creature,
		sourceObjectID: req.command.Common.ObjectID,
	}
	if peerSession.heroProjectileRuns == nil {
		peerSession.heroProjectileRuns = make(map[uint32]*heroProjectileStatusRun)
	}
	peerSession.heroProjectileRuns[projectileObjectID] = run
	generation := peerSession.generation
	creatureIndex := peerSession.deployedCreatureIndex
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroProjectileStatusSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		targetObjectID: targetObjectID, projectileObjectID: projectileObjectID,
		creatureIndex: creatureIndex, previousManaPoint: previousManaPoint,
		impactDeadline: impactDeadline, sourcePosition: game.Vec3(actorPosition),
		targetPosition: targetPosition, travelDistance: travelDistance,
		actorFootprint: actorFootprint, geometry: geometry, facing: facing,
		creature: creature, definition: projected, damage: damage, binding: binding,
		run: run, cooldownReservation: cooldownReservation,
		releaseReservation: releaseReservation, releasePacket: releasePacket,
	}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: projected.HitDelay, Produce: heroProjectileStatusStep{
			schedule: schedule, deadline: projected.HitDelay,
		}.advance},
		{Delay: projected.ReleaseDelay, Produce: schedule.release},
	}
	if projected.Name == "AfflictionBolt" {
		flightDuration := afflictionBoltFlightDuration(projected)
		if flightDuration <= 0 {
			schedule.fail(errors.New("affliction flight unavailable"))
			return nil, errors.New("projectileStatusAfflictionFlight: unavailable")
		}
		flightDeadline := projected.HitDelay + flightDuration
		for deadline := projected.HitDelay; deadline <= flightDeadline; deadline += afflictionBoltScanInterval {
			step := heroAfflictionBoltStep{schedule: schedule, deadline: deadline}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: deadline, Produce: step.scan,
			})
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: flightDeadline, Produce: schedule.finishAfflictionFlight,
		}, raknet.ScheduledPacketProducer{
			Delay:   flightDeadline + projected.StatusDuration,
			Produce: schedule.finish,
		})
	} else {
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: impactDeadline, Produce: schedule.impact,
		})
		for _, tickOffset := range projected.HitDelays {
			deadline := impactDeadline + tickOffset
			step := heroProjectileStatusStep{schedule: schedule, deadline: deadline}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: deadline, Produce: step.tick,
			})
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: impactDeadline + projected.StatusDuration, Produce: schedule.finish,
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
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("projectileStatusSchedule: %w", err)
	}
	run.mutex.Lock()
	run.cancel = cancel
	run.mutex.Unlock()
	r.logger.Printf(
		"RakNet projectile trajectory launched kind=hero-status projectile=%d source=%d target=%d ability=%q origin=(%.3f,%.3f,%.3f) launch=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) travel=%.3f delay_ms=%d",
		projectileObjectID, req.command.Common.ObjectID, targetObjectID,
		projected.Name, actorPosition.X, actorPosition.Y, actorPosition.Z,
		launchPosition.X, launchPosition.Y, launchPosition.Z,
		targetPosition.X, targetPosition.Y, targetPosition.Z,
		travelDistance, travelDelay.Milliseconds(),
	)
	packets := append([][]byte{ackPacket, manaPacket, cooldownPacket}, immediatePackets...)
	return packets, nil
}
