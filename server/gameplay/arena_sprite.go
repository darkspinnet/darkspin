package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
)

type arenaSpriteRun struct {
	runtime       campaignAbilityCommandRuntime
	packet        raknet.Packet
	world         *arenaAbilityWorld
	sessionKey    string
	gameID        uint32
	generation    uint64
	ownerObjectID uint32
	objectID      uint32
	startedAt     time.Time
	definition    sim.AbilityDefinition
	releasePacket []byte
	deletePacket  []byte
	healUntil     time.Time
	cancel        raknet.CancelSchedule
	isPublished   bool
}

type arenaSpriteFollowStep struct {
	run      *arenaSpriteRun
	revision uint64
	position game.Vec3
}

func (e arenaSpriteFollowStep) execute() {
	e.run.runtime.registry.mutex.Lock()
	_, isCurrent := e.run.isCurrentLocked()
	if isCurrent {
		e.run.world.companion.ReleaseFollow(e.run.objectID, e.revision, e.position)
	}
	e.run.runtime.registry.mutex.Unlock()
}

func (e *arenaSpriteRun) isCurrentLocked() (gameplayPeerSession, bool) {
	match := e.runtime.registry.arenaMatches[e.gameID]
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := !e.runtime.registry.isClosed && match != nil && match.isCombatActive &&
		match.abilityWorld == e.world && e.world.sprites[e.objectID] == e &&
		isFound && source.generation == e.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == e.gameID && source.isArenaCombatActive && source.stage.IsDungeon() &&
		source.deployedObjectID == e.ownerObjectID && source.deployedHitPoint() > 0
	return source, isCurrent
}

func (e *arenaSpriteRun) retireLocked() {
	if e.world.sprites[e.objectID] != e {
		return
	}
	delete(e.world.sprites, e.objectID)
	actor, isFound := e.world.companion.Snapshot(e.objectID)
	e.world.companion.Remove(e.objectID)
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if !e.isPublished {
		return
	}
	packets := make([][]byte, 0, 2)
	if isFound && e.definition.ImpactEffectName != "" {
		effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(e.definition.ImpactEffectName), ObjectID: e.objectID,
			Position: raknet.Vector3{X: actor.Position.X, Y: actor.Position.Y, Z: actor.Position.Z},
		})
		if err == nil {
			packets = append(packets, effectPacket)
		} else if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Sprite retirement effect failed game=%d object=%d: %v", e.gameID, e.objectID, err)
		}
	}
	packets = append(packets, e.deletePacket)
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
}

func (e *gameplaySessionRegistry) retireArenaSpritesLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.sprites {
		run.retireLocked()
	}
}

func (e *gameplaySessionRegistry) retireArenaSpritesForOwnerLocked(gameID uint32, ownerObjectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.sprites {
		if run.ownerObjectID == ownerObjectID {
			run.retireLocked()
		}
	}
}

func (e *arenaSpriteRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	_, isCurrent := e.isCurrentLocked()
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e *arenaSpriteRun) sting() ([][]byte, error) {
	return e.followOrSting(true)
}

func (e *arenaSpriteRun) resumeFollow() ([][]byte, error) {
	return e.followOrSting(false)
}

func (e *arenaSpriteRun) followOrSting(isSting bool) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.isCurrentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	at := e.runtime.now()
	if !isSting && at.Before(e.healUntil) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isStingReleased := !isSting && !e.healUntil.IsZero()
	e.healUntil = time.Time{}
	actor, isFound := e.world.companion.Snapshot(e.objectID)
	if !isFound || actor.HitPoint <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	plans, err := e.world.companion.FollowActor(source.binding.UserID, e.generation,
		e.ownerObjectID, e.objectID, game.Vec3(source.playerPosition),
		spriteFollowDistance, spriteMovementSpeed, at)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteFollow: %w", err)
	}
	isHealing := isSting && len(plans) == 0 && source.deployedHitPoint() > 0 &&
		source.deployedHitPoint() < source.characterHitPointMaximum(source.deployedCreatureIndex)
	if isHealing {
		actor, err = e.world.companion.HoldFollow(e.objectID, at, at.Add(time.Second))
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaSpriteHold: %w", err)
		}
		e.healUntil = at.Add(time.Second)
	}
	e.runtime.registry.mutex.Unlock()
	packets, err := companionraknet.Follow(plans)
	if err != nil {
		return nil, fmt.Errorf("arenaSpriteFollowPackets: %w", err)
	}
	if isStingReleased {
		resetPacket, resetErr := abilityraknet.AnimationReset(e.objectID,
			e.packet.SourceTime+uint64(at.Sub(e.startedAt)/time.Millisecond))
		if resetErr != nil {
			return nil, fmt.Errorf("arenaSpriteReset: %w", resetErr)
		}
		packets = append([][]byte{resetPacket}, packets...)
	}
	for _, plan := range plans {
		step := arenaSpriteFollowStep{run: e, revision: plan.Revision, position: plan.Destination}
		cancel, scheduleErr := e.runtime.registry.timer.Schedule(plan.TravelDuration, step.execute)
		if scheduleErr != nil || cancel == nil {
			e.world.companion.CancelFollow(plan.ObjectID, plan.Revision, at)
			if scheduleErr != nil {
				return nil, fmt.Errorf("arenaSpriteFollowSchedule: %w", scheduleErr)
			}
			return nil, errors.New("arena sprite follow cancellation unavailable")
		}
	}
	if !isHealing {
		return packets, nil
	}
	ownerPosition := game.Vec3(source.playerPosition)
	facing := ownerPosition.Sub(actor.Position)
	if facing.Length() > 0 {
		facing = facing.Scale(1 / facing.Length())
	} else {
		facing = game.Vec3{X: 1}
	}
	posePackets, err := marshalZonePlayerAttackPose(e.objectID, raknet.Vector3(actor.Position),
		raknet.Vector3(facing), raknet.Vector3(ownerPosition))
	if err != nil {
		return nil, fmt.Errorf("arenaSpritePose: %w", err)
	}
	packets = append(packets, posePackets...)
	animationPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.objectID, State: util.HashID("LF_pet_healer_heal"), Scale: 1,
		Timestamp: e.packet.SourceTime + uint64(at.Sub(e.startedAt)/time.Millisecond),
	})
	if err != nil {
		return nil, fmt.Errorf("arenaSpriteAnimation: %w", err)
	}
	return append(packets, animationPacket), nil
}

func (e *arenaSpriteRun) tick() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.isCurrentLocked()
	if !isCurrent || e.healUntil.IsZero() || !e.runtime.now().Before(e.healUntil) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	actor, isFound := e.world.companion.Snapshot(e.objectID)
	if !isFound || actor.HitPoint <= 0 || actor.Position.Sub(game.Vec3(source.playerPosition)).Length() > 20 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	maximumHitPoint := source.characterHitPointMaximum(source.deployedCreatureIndex)
	previousHitPoint := source.deployedHitPoint()
	if previousHitPoint <= 0 || previousHitPoint >= maximumHitPoint {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	healing := maximumHitPoint * spriteHealingFraction
	healing, err := game.ApplyTargetHealingReduction(healing, source.healingTargetProfile(source.deployedCreatureIndex))
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteReduction: %w", err)
	}
	hitPoint := min(maximumHitPoint, previousHitPoint+healing)
	healedAmount := hitPoint - previousHitPoint
	if healedAmount <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	_, err = source.setDeployedHitPoints(hitPoint)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteHealing: %w", err)
	}
	resourcePacket, err := source.marshalCampaignCharacterResource(source.deployedCreatureIndex)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaSpriteResource: %w", err)
	}
	match := e.runtime.registry.arenaMatches[e.gameID]
	match.result.recordHealing(source.binding.UserID, source.binding.UserID, healedAmount)
	e.runtime.registry.sessions[e.sessionKey] = source
	e.runtime.registry.queueArenaAbilityResourcesLocked(source, [][]byte{resourcePacket})
	e.runtime.registry.mutex.Unlock()
	healingDefinition := e.definition
	healingDefinition.HealEffectName = ""
	healingPackets, err := abilityraknet.MarshalTreeOfLifeHealing(healingDefinition,
		raknet.Vector3(actor.Position), []abilityraknet.Healing{{
			SourceObjectID: e.objectID, ObjectID: e.ownerObjectID,
			HitPoint: hitPoint, Amount: healedAmount,
		}}, false)
	if err != nil {
		return nil, fmt.Errorf("arenaSpriteHealPackets: %w", err)
	}
	impactPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID("lf_pocketHealer_healHit.ServerEventDef"), ObjectID: e.ownerObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaSpriteImpact: %w", err)
	}
	packets := append(healingPackets, impactPacket, resourcePacket)
	return packets, nil
}

func (e *arenaSpriteRun) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.isCurrentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	actor, isFound := e.world.companion.Snapshot(e.objectID)
	e.world.companion.Remove(e.objectID)
	delete(e.world.sprites, e.objectID)
	e.cancel = nil
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, 2)
	if isFound && actor.HitPoint > 0 && e.definition.ImpactEffectName != "" {
		effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(e.definition.ImpactEffectName), ObjectID: e.objectID,
			Position: raknet.Vector3{X: actor.Position.X, Y: actor.Position.Y, Z: actor.Position.Z},
		})
		if err != nil {
			return nil, fmt.Errorf("arenaSpriteExpireEffect: %w", err)
		}
		packets = append(packets, effectPacket)
	}
	return append(packets, e.deletePacket), nil
}

func (e *arenaSpriteRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.isCurrentLocked()
	if isCurrent {
		e.retireLocked()
	}
	e.runtime.registry.mutex.Unlock()
	if cause != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Sprite producer failed game=%d object=%d: %v", e.gameID, e.objectID, cause)
	}
}
