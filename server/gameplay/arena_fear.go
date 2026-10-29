package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const arenaHeroFear sim.AbilityStatusKind = "arena_hero_fear"

func (e gameplayPeerSession) isArenaHitFearActive(at time.Time) bool {
	for _, status := range e.arenaHitStatuses {
		if status.matches(e) && status.kind == arenaHeroFear && at.Before(status.expiresAt) {
			return true
		}
	}
	return false
}

func (e gameplayPeerSession) isArenaHitCurseActive(at time.Time) bool {
	for _, status := range e.arenaHitStatuses {
		if status.matches(e) && (status.kind == sim.AbilityStatusKindCurse || status.kind == arenaVoodooCurse) &&
			at.Before(status.expiresAt) {
			return true
		}
	}
	return false
}

type arenaFearRun struct {
	status       *arenaHitStatus
	origin       game.Vec3
	moveCancel   func()
	expiryCancel func()
}

func (e *arenaFearRun) cancel() {
	if e.moveCancel != nil {
		e.moveCancel()
		e.moveCancel = nil
	}
	if e.expiryCancel != nil {
		e.expiryCancel()
		e.expiryCancel = nil
	}
}

// Called with the registry lock. A fear has one recipient-owned modifier and
// a server-driven locomotion goal that renews every 600ms until expiry.
func (e campaignAbilityCommandRuntime) applyArenaFearLocked(source gameplayPeerSession,
	targetObjectID uint32, modifierID uint32, duration time.Duration, timestamp uint64,
) error {
	_, err := e.applyArenaFearStatusLocked(source, targetObjectID, modifierID, duration, timestamp)
	if err != nil {
		return fmt.Errorf("arenaFearStatus: %w", err)
	}
	return nil
}

func (e campaignAbilityCommandRuntime) applyArenaFearStatusLocked(source gameplayPeerSession,
	targetObjectID uint32, modifierID uint32, duration time.Duration, timestamp uint64,
) (*arenaHitStatus, error) {
	match := e.registry.arenaMatches[source.binding.GameID]
	if source.binding.Mode != game.ModeArena || match == nil || !match.isCombatActive ||
		match.navigation == nil || match.random == nil || e.registry.timer == nil ||
		targetObjectID == 0 || modifierID == 0 || duration <= 0 {
		return nil, nil
	}
	targets := arenaAbilityTargets{runtime: e, source: source}
	key, member, isFound := targets.member(targetObjectID)
	if !isFound || member.isHeroDebuffImmune(targetObjectID) || member.isOverdriveActiveAt(e.now()) {
		return nil, nil
	}
	status := &arenaHitStatus{runtime: e, targetKey: key, gameID: source.binding.GameID,
		objectID: targetObjectID, index: member.deployedCreatureIndex,
		noun:       member.binding.Creatures[member.deployedCreatureIndex].Noun,
		generation: member.generation, transportGeneration: member.transportGeneration,
		squad: member.squad, sourceObjectID: source.deployedObjectID,
		modifierID: modifierID, kind: arenaHeroFear, duration: duration,
		expiresAt: e.now().Add(duration), revision: 1}
	instanceID, err := e.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("arenaFearAllocate: %w", err)
	}
	status.instanceID = instanceID
	creation, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: targetObjectID, ModifierGUID: modifierID, InstanceID: instanceID,
		DurationMilliseconds: uint32(duration.Milliseconds()), StackCount: 1,
		StartMilliseconds: timestamp, NPCAffixAssetID: 0})
	if err != nil {
		return nil, status.discard("arenaFearCreate", err)
	}
	run := &arenaFearRun{status: status, origin: game.Vec3(member.playerPosition)}
	status.cancel = run.cancel
	if duration > heroFearMovementInterval {
		run.moveCancel, err = e.registry.timer.Schedule(heroFearMovementInterval, run.move)
		if err == nil && run.moveCancel == nil {
			err = errors.New("fear movement cancellation unavailable")
		}
		if err != nil {
			return nil, status.discard("arenaFearMoveSchedule", err)
		}
	}
	run.expiryCancel, err = e.registry.timer.Schedule(duration, run.expire)
	if err == nil && run.expiryCancel == nil {
		err = errors.New("fear expiry cancellation unavailable")
	}
	if err != nil {
		return nil, status.discard("arenaFearExpirySchedule", err)
	}
	if member.arenaHitStatuses == nil {
		member.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous := member.arenaHitStatuses[status.key()]; previous != nil {
		previous.retire(&member)
	}
	member.arenaHitStatuses[status.key()] = status
	if interrupted := member.resetInterruptibleActionAdmission(); interrupted != nil {
		interrupted.Stop()
	}
	member.campaignPlayerPursuitSession().Cancel()
	status.queue(&member, [][]byte{creation})
	e.registry.sessions[key] = member
	return status, nil
}

func (e *arenaFearRun) move() {
	status := e.status
	status.runtime.registry.mutex.Lock()
	defer status.runtime.registry.mutex.Unlock()
	e.moveCancel = nil
	member, isFound := status.runtime.registry.sessions[status.targetKey]
	match := status.runtime.registry.arenaMatches[status.gameID]
	now := status.runtime.now()
	if !isFound || !status.matches(member) || member.arenaHitStatuses[status.key()] != status ||
		member.deployedHitPoint() <= 0 || match == nil || !match.isCombatActive ||
		match.navigation == nil || match.random == nil || !now.Before(status.expiresAt) {
		return
	}
	if member.isEnemyStunActive(now) || member.isEnemySleepActive(now) ||
		member.isEnemyRootActive(now) || member.isOperativeCaged(now) {
		packets, err := member.stopHeroFearMovement(now)
		if err == nil {
			status.queue(&member, packets)
		}
	} else {
		destination, isFound, err := zonenavigation.RandomTeleportDestination(
			match.navigation, match.random, zonenavigation.RandomTeleportRequest{
				SourcePosition: e.origin, FootprintRadius: member.deployedCampaignNavigationRadius(),
				MinimumDistance: 1, NormalDistance: 4, MaximumDistance: 6})
		if err == nil && isFound {
			err = member.startArenaFearMovement(now, raknet.Vector3(destination),
				status.runtime.registry.passiveMovementIncrease(member), match)
			if err == nil {
				packets, marshalErr := marshalHeroFearMovement(member, true)
				if marshalErr == nil {
					status.queue(&member, packets)
				}
			}
		}
	}
	status.runtime.registry.sessions[status.targetKey] = member
	if now.Add(heroFearMovementInterval).Before(status.expiresAt) {
		cancel, err := status.runtime.registry.timer.Schedule(heroFearMovementInterval, e.move)
		if err == nil {
			e.moveCancel = cancel
		}
	}
}

func (e *arenaFearRun) expire() {
	status := e.status
	status.runtime.registry.mutex.Lock()
	defer status.runtime.registry.mutex.Unlock()
	e.expiryCancel = nil
	member, isFound := status.runtime.registry.sessions[status.targetKey]
	if !isFound || !status.matches(member) || member.arenaHitStatuses[status.key()] != status {
		status.retire(&member)
		return
	}
	status.retire(&member)
	if !member.isEnemyFearActive(status.runtime.now()) {
		packets, err := member.stopHeroFearMovement(status.runtime.now())
		if err == nil {
			status.queue(&member, packets)
		}
	}
	status.runtime.registry.sessions[status.targetKey] = member
}

func (e *gameplayPeerSession) startArenaFearMovement(now time.Time,
	destination raknet.Vector3, movementIncrease float32, match *arenaMatchState,
) error {
	if e == nil || match == nil || match.navigation == nil || !isFiniteZonePosition(destination) {
		return errors.New("arena fear movement invalid")
	}
	if e.playerMotion == nil {
		motion, err := zoneaction.NewMotion(toSimPosition(e.playerPosition), now)
		if err != nil {
			return fmt.Errorf("arenaFearSeed: %w", err)
		}
		e.playerMotion = motion
	}
	e.playerMotion.SetActorNavigation(match.navigation, e.deployedCampaignNavigationRadius(),
		e.deployedObjectID, uint8(e.binding.Mode))
	speed := zonePlayerMoveSpeed * (1 + max(float32(-0.9), movementIncrease+e.enemyMovementSpeedBuff()))
	_, position, err := e.playerMotion.Advance(now, toSimPosition(e.playerPosition),
		toSimPosition(destination), false, false, zonePlayerPoseCorrectionRange, speed)
	if err != nil {
		return fmt.Errorf("arenaFearAdvance: %w", err)
	}
	e.playerPosition = toRakNetPosition(position)
	e.playerMovementGoal = toRakNetPosition(e.playerMotion.Snapshot().Movement().Goal)
	return nil
}
