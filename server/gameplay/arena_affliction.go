package gameplay

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	"github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type arenaAfflictionRun struct {
	status       *arenaHitStatus
	source       gameplayPeerSession
	creature     game.GameplayCreature
	damage       game.DamageRange
	definition   sim.AbilityDefinition
	timestamp    uint64
	completed    uint32
	cancels      []func()
	parentCancel func()
}

func (e gameplayPeerSession) isArenaStealthed() bool {
	return e.isTrapperStealthed || e.heroModifierRun != nil && e.heroModifierRun.isShadowStealth
}

type arenaAfflictionTickStep struct {
	run      *arenaAfflictionRun
	index    uint32
	deadline time.Duration
}

func (e arenaAfflictionTickStep) tick() {
	e.run.tick(e.index, e.deadline)
}

func (e *arenaAfflictionRun) cancel() {
	for _, cancel := range e.cancels {
		if cancel != nil {
			cancel()
		}
	}
	e.cancels = nil
	if e.parentCancel != nil {
		e.parentCancel()
		e.parentCancel = nil
	}
}

func (e *arenaAfflictionRun) attachTicksLocked() error {
	e.parentCancel = e.status.cancel
	e.status.cancel = e.cancel
	for index, delay := range e.definition.HitDelays {
		step := arenaAfflictionTickStep{run: e, index: uint32(index), deadline: delay}
		cancel, err := e.status.runtime.registry.timer.Schedule(delay, step.tick)
		if err == nil && cancel == nil {
			err = errors.New("status tick cancellation unavailable")
		}
		if err != nil {
			e.cancel()
			return fmt.Errorf("statusTickSchedule[%d]: %w", index, err)
		}
		e.cancels = append(e.cancels, cancel)
	}
	return nil
}

func (e *arenaAfflictionRun) scheduleLocked() error {
	timer := e.status.runtime.registry.timer
	if timer == nil {
		return errors.New("affliction recipient timer unavailable")
	}
	for index, delay := range e.definition.HitDelays {
		step := arenaAfflictionTickStep{run: e, index: uint32(index), deadline: delay}
		cancel, err := timer.Schedule(delay, step.tick)
		if err == nil && cancel == nil {
			err = errors.New("affliction tick cancellation unavailable")
		}
		if err != nil {
			e.cancel()
			return fmt.Errorf("afflictionTickSchedule[%d]: %w", index, err)
		}
		e.cancels = append(e.cancels, cancel)
	}
	expiry := arenaHitStatusExpiry{status: e.status, revision: e.status.revision}
	cancel, err := timer.Schedule(e.definition.StatusDuration, expiry.expire)
	if err == nil && cancel == nil {
		err = errors.New("affliction expiry cancellation unavailable")
	}
	if err != nil {
		e.cancel()
		return fmt.Errorf("afflictionExpirySchedule: %w", err)
	}
	e.cancels = append(e.cancels, cancel)
	e.status.cancel = e.cancel
	return nil
}

func (e *arenaAfflictionRun) tick(index uint32, deadline time.Duration) {
	status := e.status
	status.runtime.registry.mutex.Lock()
	member, isFound := status.runtime.registry.sessions[status.targetKey]
	match := status.runtime.registry.arenaMatches[status.gameID]
	if !isFound || !status.matches(member) || member.arenaHitStatuses[status.key()] != status ||
		match == nil || !match.isCombatActive || match.random == nil || member.deployedHitPoint() <= 0 ||
		index < e.completed {
		status.runtime.registry.mutex.Unlock()
		return
	}
	selected, err := sim.SelectRankDamage(match.random, sim.DamageRange{
		Minimum: e.damage.Minimum, Maximum: e.damage.Maximum})
	if err != nil {
		status.retire(&member)
		status.runtime.registry.sessions[status.targetKey] = member
		status.runtime.registry.mutex.Unlock()
		return
	}
	targets := arenaAbilityTargets{runtime: status.runtime, source: e.source,
		definition: e.definition, timestamp: e.timestamp + uint64(deadline/time.Millisecond),
		hits: make(map[uint32]arenaAbilityHit)}
	result, err := targets.Hit(zonenpc.HitRequest{SourceObjectID: status.sourceObjectID,
		TargetObjectID: status.objectID, Damage: selected, IsPeriodic: true,
		Metadata: zoneability.NPCDamageMetadata(e.definition)})
	if err != nil {
		if latest, isLatestFound := status.runtime.registry.sessions[status.targetKey]; isLatestFound {
			member = latest
		}
		status.retire(&member)
		status.runtime.registry.sessions[status.targetKey] = member
		status.runtime.registry.mutex.Unlock()
		return
	}
	e.completed = index + 1
	packets, err := targets.packets([]zoneability.AreaResult{{Damage: result, Definition: e.definition}})
	if err == nil {
		status.runtime.registry.queueArenaForcePulsePacketsLocked(status.gameID, packets)
	}
	if result.IsDefeated {
		latest, isLatestFound := status.runtime.registry.sessions[status.targetKey]
		if isLatestFound {
			status.retire(&latest)
			status.runtime.registry.sessions[status.targetKey] = latest
		}
	}
	status.runtime.registry.mutex.Unlock()
	targets.finish()
}

func (e *arenaProjectileStatusRun) scanAffliction(deadline time.Duration) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.isCurrentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	snapshot := e.projectile.Snapshot(e.runtime.now())
	if !snapshot.IsActive {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	previous, current := e.wrapper.advanceScanPosition(game.Vec3(snapshot.Position))
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, member, e.definition,
		e.packet.SourceTime+uint64(deadline/time.Millisecond)).(*arenaAbilityTargets)
	candidates := targets.LiveSnapshots()
	slices.SortFunc(candidates, func(left, right zonenpc.Snapshot) int {
		return cmp.Compare(left.Plan.ObjectID, right.Plan.ObjectID)
	})
	contacts := make([]uint32, 0)
	retarget := uint32(0)
	retargetPosition := game.Vec3{}
	retargetDistance := float32(afflictionBoltRetargetRange + 1)
	for _, candidate := range candidates {
		objectID := candidate.Plan.ObjectID
		_, victim, isFound := targets.member(objectID)
		if !isFound || victim.isHeroDebuffImmune(objectID) || victim.isOverdriveActiveAt(e.runtime.now()) ||
			victim.isArenaStealthed() {
			continue
		}
		statusKey := arenaHitStatusKey{modifierID: e.definition.RootModifierID}
		if status := victim.arenaHitStatuses[statusKey]; status != nil && status.matches(victim) &&
			e.runtime.now().Before(status.expiresAt) {
			continue
		}
		contactPosition := candidate.Plan.Position
		contactPosition.Z += candidate.Plan.NPCProfile.FootprintRadius
		if geometry.SegmentIntersectsSphere(previous, current, contactPosition, e.definition.Radius) {
			if err := e.applyAfflictionLocked(targets, member, objectID, deadline); err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("arenaAfflictionContact: %w", err)
			}
			contacts = append(contacts, objectID)
			continue
		}
		distance := contactPosition.Sub(game.Vec3(snapshot.Position)).Length()
		if distance <= afflictionBoltRetargetRange && distance < retargetDistance {
			retarget, retargetPosition, retargetDistance = objectID, contactPosition, distance
		}
	}
	var packets [][]byte
	if retarget != 0 {
		retargetPackets, err := e.projectile.Retarget(e.runtime.now(), retarget,
			sim.Position(retargetPosition), afflictionBoltTurnRate)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaAfflictionRetarget: %w", err)
		}
		packets = append(packets, retargetPackets...)
	} else if len(contacts) > 0 {
		continuePackets, err := e.projectile.Retarget(e.runtime.now(), 0, snapshot.Position,
			afflictionBoltTurnRate)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaAfflictionContinue: %w", err)
		}
		packets = append(packets, continuePackets...)
	}
	e.queueLocked(packets)
	e.runtime.registry.mutex.Unlock()
	return nil, nil
}

func (e *arenaProjectileStatusRun) applyAfflictionLocked(targets *arenaAbilityTargets,
	source gameplayPeerSession, objectID uint32, deadline time.Duration,
) error {
	key, target, isFound := targets.member(objectID)
	if !isFound {
		return nil
	}
	status := &arenaHitStatus{runtime: e.runtime, targetKey: key, gameID: e.gameID,
		objectID: objectID, index: target.deployedCreatureIndex,
		noun:       target.binding.Creatures[target.deployedCreatureIndex].Noun,
		generation: target.generation, transportGeneration: target.transportGeneration,
		squad: target.squad, sourceObjectID: e.sourceObjectID,
		modifierID: e.definition.RootModifierID, kind: sim.AbilityStatusKindCurse,
		stackCount: 1, duration: e.definition.StatusDuration,
		expiresAt: e.runtime.now().Add(e.definition.StatusDuration), revision: 1}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("afflictionAllocate: %w", err)
	}
	status.instanceID = instanceID
	creation, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: objectID, ModifierGUID: status.modifierID, InstanceID: instanceID,
		DurationMilliseconds: uint32(e.definition.StatusDuration.Milliseconds()),
		StackCount:           1, StartMilliseconds: e.packet.SourceTime + uint64(deadline/time.Millisecond),
		SourceID: e.sourceObjectID})
	if err != nil {
		return status.discard("arenaAfflictionCreate", err)
	}
	run := &arenaAfflictionRun{status: status, source: source, creature: e.creature, damage: e.damage,
		definition: e.definition, timestamp: e.packet.SourceTime + uint64(deadline/time.Millisecond)}
	status.affliction = run
	err = run.scheduleLocked()
	if err != nil {
		return status.discard("arenaAfflictionSchedule", err)
	}
	if target.arenaHitStatuses == nil {
		target.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous := target.arenaHitStatuses[status.key()]; previous != nil {
		previous.retire(&target)
	}
	target.arenaHitStatuses[status.key()] = status
	status.queue(&target, [][]byte{creation})
	e.runtime.registry.sessions[key] = target
	return nil
}

func (e *arenaProjectileStatusRun) finishFlight() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.isCurrentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.projectile.RemainingFlightDelay(e.runtime.now()) > afflictionBoltScanInterval {
		deadline := e.projectile.Now()
		e.runtime.registry.mutex.Unlock()
		_, err := e.scanAffliction(deadline)
		if err != nil {
			return nil, fmt.Errorf("arenaAfflictionSlowScan: %w", err)
		}
		err = scheduleNPCProducer(e.runtime.registry, e.packet,
			afflictionBoltScanInterval, e.finishFlight)
		if err != nil {
			return nil, fmt.Errorf("arenaAfflictionSlowResume: %w", err)
		}
		return nil, nil
	}
	packets, _, err := e.projectile.DeleteProjectile(context.Background())
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaAfflictionDelete: %w", err)
	}
	e.queueLocked(packets)
	member := e.runtime.registry.sessions[e.sessionKey]
	delete(member.heroProjectileRuns, e.projectileObjectID)
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	e.wrapper.Stop()
	return nil, nil
}
