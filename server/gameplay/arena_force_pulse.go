package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type arenaForcePulseKey struct {
	sourceObjectID uint32
	targetObjectID uint32
}

type arenaForcePulseRun struct {
	registry       *gameplaySessionRegistry
	world          *arenaAbilityWorld
	gameID         uint32
	key            arenaForcePulseKey
	targetKey      string
	generation     uint64
	motionRevision uint64
	landingAt      time.Time
	landingTime    uint64
	expiresAt      time.Time
	modifier       *campaignNPCModifierRun
	pool           *modifierPool
	landingCancel  func()
	expiryCancel   func()
}

// Called after the ordinary projectile collision and player damage commit,
// with the gameplay registry locked. Players remain real squad participants.
func (e *arenaAbilityTargets) applyForcePulseLocked(sourceObjectID uint32, result zoneability.BasicResult) ([][]byte, error) {
	if e.definition.Name != "ForcePulse" || result.Damage.IsDefeated || result.Damage.IsDamageImmune {
		return nil, nil
	}
	targetKey, victim, isFound := e.member(result.Damage.ObjectID)
	if !isFound {
		return nil, nil
	}
	world, err := e.runtime.registry.arenaAbilityWorldLocked(e.source.binding.GameID)
	if err != nil {
		return nil, fmt.Errorf("pulseWorld: %w", err)
	}
	key := arenaForcePulseKey{sourceObjectID: sourceObjectID, targetObjectID: victim.deployedObjectID}
	now := e.runtime.now()
	previous := world.forcePulses[key]
	if previous != nil && now.Before(previous.expiresAt) {
		return nil, nil
	}
	if previous != nil {
		err = previous.retireLocked()
		if err != nil {
			return nil, fmt.Errorf("pulsePrevious: %w", err)
		}
	}
	plan, destination, isJump := basicKnockbackPlan(key.sourceObjectID, key.targetObjectID,
		game.Vec3(e.source.playerPosition), game.Vec3(victim.playerPosition))
	// Build 103 still installs the ten-second caster-unique marker on a
	// rooted recipient; the Tick skips only its JumpInDirection branch.
	isJump = isJump && !victim.isEnemyRootActive(now)
	profile := plan.Profile
	if !isJump {
		profile = zonenpc.BasicKnockbackProfile()
	}
	modifier, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("pulseAllocate: %w", err)
	}
	run := &arenaForcePulseRun{registry: e.runtime.registry, world: world,
		gameID: e.source.binding.GameID, key: key, targetKey: targetKey, generation: victim.generation,
		expiresAt: now.Add(profile.ModifierDuration), modifier: modifier, pool: e.runtime.modifierPool}
	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: key.sourceObjectID, TargetObjectID: key.targetObjectID,
		ModifierID: util.HashID(profile.ModifierName), InstanceID: modifier.instanceID,
		StackCount: 1, Duration: profile.ModifierDuration, Timestamp: e.timestamp,
	})
	if err != nil {
		return nil, run.discard("pulseCreate", err)
	}
	packets := [][]byte{createPacket}
	if isJump {
		jumpPackets, jumpErr := npcraknet.ForcedJump(plan, destination, e.timestamp, [3]float32{})
		if jumpErr != nil {
			return nil, run.discard("pulseJump", jumpErr)
		}
		packets = append(packets, jumpPackets...)
		travel := basicKnockbackTravel(plan, destination)
		run.landingAt = now.Add(travel)
		run.landingTime = e.timestamp + uint64(travel/time.Millisecond)
		run.landingCancel, err = e.runtime.registry.timer.Schedule(max(time.Millisecond, travel), run.land)
		if err == nil && run.landingCancel == nil {
			err = errors.New("Force Pulse landing cancellation unavailable")
		}
		if err != nil {
			return nil, run.discard("pulseLanding", err)
		}
	}
	run.expiryCancel, err = e.runtime.registry.timer.Schedule(profile.ModifierDuration, run.expire)
	if err == nil && run.expiryCancel == nil {
		err = errors.New("Force Pulse expiry cancellation unavailable")
	}
	if err != nil {
		return nil, run.discard("pulseExpiry", err)
	}
	if isJump {
		err = victim.placeTeleportedPlayer(now, raknet.Vector3(destination))
		if err != nil {
			return nil, run.discard("pulsePlacement", err)
		}
		interrupted := victim.resetInterruptibleActionAdmission()
		if interrupted != nil {
			interrupted.Stop()
		}
		victim.campaignPlayerPursuitSession().Cancel()
		victim.heroKnockbackLanding = heroKnockbackLanding{
			objectID: key.targetObjectID, readyAt: run.landingAt, holdUntil: run.landingAt, isNativeJump: true,
		}
		run.motionRevision = victim.playerMotionRevision()
		e.runtime.registry.sessions[targetKey] = victim
	}
	if world.forcePulses == nil {
		world.forcePulses = make(map[arenaForcePulseKey]*arenaForcePulseRun)
	}
	isCreated := modifier.create()
	if !isCreated {
		return nil, run.discard("pulsePublish", errors.New("Force Pulse modifier unavailable before publication"))
	}
	world.forcePulses[key] = run
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Force Pulse modifier source=%d target=%d instance=%d jump=%t duration=%s",
			key.sourceObjectID, key.targetObjectID, modifier.instanceID, isJump, profile.ModifierDuration)
	}
	return packets, nil
}

func (e *arenaForcePulseRun) discard(step string, cause error) error {
	if e.landingCancel != nil {
		e.landingCancel()
	}
	if e.expiryCancel != nil {
		e.expiryCancel()
	}
	wasCreated, err := e.modifier.release(e.pool)
	if err != nil {
		return fmt.Errorf("%s: %w", step, errors.Join(cause, fmt.Errorf("pulseRelease: %w", err)))
	}
	if wasCreated {
		return fmt.Errorf("%s: %w", step, errors.Join(cause, errors.New("uncommitted Force Pulse was already published")))
	}
	return fmt.Errorf("%s: %w", step, cause)
}

func (e *arenaForcePulseRun) isCurrentLocked() bool {
	match := e.registry.arenaMatches[e.gameID]
	return !e.registry.isClosed && match != nil && match.abilityWorld == e.world && e.world.forcePulses[e.key] == e
}

func (e *arenaForcePulseRun) land() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if !e.isCurrentLocked() {
		return
	}
	e.landingCancel = nil
	member, isFound := e.registry.sessions[e.targetKey]
	if !isFound || member.generation != e.generation || member.deployedObjectID != e.key.targetObjectID ||
		member.deployedHitPoint() <= 0 || member.playerMotionRevision() != e.motionRevision ||
		member.heroKnockbackLanding.readyAt != e.landingAt {
		return
	}
	member.heroKnockbackLanding = heroKnockbackLanding{}
	e.registry.sessions[e.targetKey] = member
	packet, err := npcraknet.ResetAnimation(e.key.targetObjectID, e.landingTime)
	if err != nil {
		if e.registry.logger != nil {
			e.registry.logger.Printf("Arena Force Pulse landing reset failed target=%d: %v", e.key.targetObjectID, err)
		}
		return
	}
	e.registry.queueArenaForcePulsePacketsLocked(e.gameID, [][]byte{packet})
}

func (e *arenaForcePulseRun) expire() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if !e.isCurrentLocked() {
		return
	}
	e.expiryCancel = nil
	err := e.retireLocked()
	if err != nil && e.registry.logger != nil {
		e.registry.logger.Printf("Arena Force Pulse expiry failed target=%d: %v", e.key.targetObjectID, err)
	}
}

func (e *arenaForcePulseRun) retireLocked() error {
	packet, err := effectraknet.ModifierDelete(e.key.targetObjectID, e.modifier.instanceID)
	if err != nil {
		return fmt.Errorf("pulseDelete: %w", err)
	}
	if e.landingCancel != nil {
		e.landingCancel()
	}
	if e.expiryCancel != nil {
		e.expiryCancel()
	}
	isCreated, err := e.modifier.release(e.pool)
	if err != nil {
		return fmt.Errorf("pulseRelease: %w", err)
	}
	delete(e.world.forcePulses, e.key)
	if isCreated {
		e.registry.queueArenaForcePulsePacketsLocked(e.gameID, [][]byte{packet})
	}
	return nil
}

func (e *gameplaySessionRegistry) queueArenaForcePulsePacketsLocked(gameID uint32, packets [][]byte) {
	for key, member := range e.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != gameID || member.isRejoinPending {
			continue
		}
		member.queuePackets(packets)
		e.sessions[key] = member
	}
}

func (e *gameplaySessionRegistry) retireArenaForcePulsesLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.forcePulses {
		err := run.retireLocked()
		if err != nil && e.logger != nil {
			e.logger.Printf("Arena Force Pulse cleanup failed game=%d target=%d: %v", gameID, run.key.targetObjectID, err)
		}
	}
}

func (e *gameplaySessionRegistry) retireArenaForcePulseVictimLocked(gameID uint32, objectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for key, run := range match.abilityWorld.forcePulses {
		if key.targetObjectID != objectID {
			continue
		}
		err := run.retireLocked()
		if err != nil && e.logger != nil {
			e.logger.Printf("Arena Force Pulse death cleanup failed game=%d target=%d: %v", gameID, objectID, err)
		}
	}
}
