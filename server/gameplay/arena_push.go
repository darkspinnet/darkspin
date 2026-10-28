package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// A forced reaction belongs to its recipient, independently of the caster's
// next deployment. The modifier is indefinite on the wire; its coroutine ends
// after landing and the authored outro, rather than a fixed-duration marker.
type arenaHitPushRun struct {
	runtime             campaignAbilityCommandRuntime
	targetKey           string
	gameID              uint32
	generation          uint64
	transportGeneration uint64
	squad               *squad.Session
	noun                uint32
	index               uint32
	objectID            uint32
	modifierID          uint32
	instanceID          uint32
	motionRevision      uint64
	landingAt           time.Time
	landingTime         uint64
	holdUntil           time.Time
	landingCancel       func()
	expiryCancel        func()
	effects             *channelEffects
	isRetired           bool
}

func (e *arenaAbilityTargets) publishCometLocked(plan zoneability.AreaPlan, results []zoneability.AreaResult) error {
	packets, err := e.packets(results)
	if err != nil {
		return fmt.Errorf("cometDamage: %w", err)
	}
	pushPackets, pushErr := e.applyCometPushesLocked(plan, results)
	packets = append(packets, pushPackets...)
	// Damage has committed; retain it even if an optional reaction fails.
	if pushErr != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Comet push omitted source=%d: %v", plan.SourceObjectID, pushErr)
	}
	e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, e.source.binding.GameID, packets)
	return nil
}

func (e *arenaHitPushRun) matches(member gameplayPeerSession) bool {
	return !e.isRetired && member.binding.Mode == game.ModeArena && member.binding.GameID == e.gameID &&
		member.generation == e.generation && member.transportGeneration == e.transportGeneration &&
		member.squad == e.squad && member.deployedObjectID == e.objectID && member.deployedCreatureIndex == e.index &&
		e.index < uint32(len(member.binding.Creatures)) && member.binding.Creatures[e.index].Noun == e.noun
}

// Called with the registry locked, after damage has committed. All returned
// packets join the same realm FIFO as that damage before callbacks can publish.
func (e *arenaAbilityTargets) applyCometPushesLocked(plan zoneability.AreaPlan, results []zoneability.AreaResult) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, result := range results {
		// Zero HP loss can still be accepted, for example a fully absorbed hit.
		if result.Damage.IsDamageImmune || result.Damage.IsDefeated {
			continue
		}
		targetKey, victim, isFound := e.member(result.Damage.ObjectID)
		if !isFound || victim.isEnemyRootActive(e.runtime.now()) {
			continue
		}
		// Template 921 rejects attribute 61 only in IsChainGame. Arena is
		// not ChainGame, so it must not gain an extra immunity exclusion.
		jump, desired := cometKnockbackPlan(plan.SourceObjectID, victim.deployedObjectID,
			plan.Center, game.Vec3(victim.playerPosition))
		jumpPackets, err := npcraknet.ForcedJump(jump, desired, e.timestamp, [3]float32{2, 4, 4})
		if err != nil {
			return packets, fmt.Errorf("cometJump: %w", err)
		}
		instanceID, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			return packets, fmt.Errorf("cometAllocate: %w", err)
		}
		now := e.runtime.now()
		travel := basicKnockbackTravel(jump, desired)
		run := &arenaHitPushRun{runtime: e.runtime, targetKey: targetKey, gameID: victim.binding.GameID,
			generation: victim.generation, transportGeneration: victim.transportGeneration,
			squad: victim.squad, noun: victim.binding.Creatures[victim.deployedCreatureIndex].Noun,
			index: victim.deployedCreatureIndex, objectID: victim.deployedObjectID,
			modifierID: util.HashID("CometKnockback"), instanceID: instanceID,
			landingAt: now.Add(travel), holdUntil: now.Add(travel + 500*time.Millisecond),
			landingTime: e.timestamp + uint64(travel/time.Millisecond)}
		created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: run.objectID, ModifierGUID: run.modifierID, InstanceID: instanceID,
			StackCount: 1, StartMilliseconds: e.timestamp,
		})
		if err != nil {
			return packets, run.discard("cometCreate", err)
		}
		if e.runtime.registry.timer == nil {
			return packets, run.discard("cometTimer", errors.New("recipient timer unavailable"))
		}
		run.landingCancel, err = e.runtime.registry.timer.Schedule(max(time.Millisecond, travel), run.land)
		if err == nil && run.landingCancel == nil {
			err = errors.New("landing cancellation unavailable")
		}
		if err != nil {
			return packets, run.discard("cometLanding", err)
		}
		run.expiryCancel, err = e.runtime.registry.timer.Schedule(travel+500*time.Millisecond, run.expire)
		if err == nil && run.expiryCancel == nil {
			err = errors.New("outro cancellation unavailable")
		}
		if err != nil {
			return packets, run.discard("cometOutro", err)
		}
		err = victim.placeTeleportedPlayer(now, raknet.Vector3(desired))
		if err != nil {
			return packets, run.discard("cometPlacement", err)
		}
		if previous := victim.arenaHitPushes[run.modifierID]; previous != nil {
			removedPackets := previous.release(&victim)
			packets = append(packets, removedPackets...)
		}
		interrupted := victim.resetInterruptibleActionAdmission()
		if interrupted != nil {
			interrupted.Stop()
		}
		victim.campaignPlayerPursuitSession().Cancel()
		victim.heroKnockbackLanding = heroKnockbackLanding{
			objectID: run.objectID, readyAt: run.landingAt, holdUntil: run.holdUntil, isNativeJump: true,
		}
		run.motionRevision = victim.playerMotionRevision()
		if victim.arenaHitPushes == nil {
			victim.arenaHitPushes = make(map[uint32]*arenaHitPushRun)
		}
		victim.arenaHitPushes[run.modifierID] = run
		e.runtime.registry.sessions[targetKey] = victim
		packets = append(packets, created)
		packets = append(packets, jumpPackets...)
	}
	return packets, nil
}

func (e *arenaHitPushRun) cancel() {
	if e.landingCancel != nil {
		e.landingCancel()
		e.landingCancel = nil
	}
	if e.expiryCancel != nil {
		e.expiryCancel()
		e.expiryCancel = nil
	}
}

func (e *arenaHitPushRun) discard(step string, cause error) error {
	e.cancel()
	e.isRetired = true
	err := e.runtime.modifierPool.Release(e.instanceID)
	return fmt.Errorf("%s: %w", step, errors.Join(cause, err))
}

func (e *arenaHitPushRun) ownsLanding(member gameplayPeerSession) bool {
	return member.heroKnockbackLanding.objectID == e.objectID && member.heroKnockbackLanding.readyAt == e.landingAt &&
		member.playerMotionRevision() == e.motionRevision
}

func (e *arenaHitPushRun) land() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.landingCancel = nil
	if e.runtime.registry.isClosed {
		return
	}
	member, isFound := e.runtime.registry.sessions[e.targetKey]
	if !isFound || !e.matches(member) || member.arenaHitPushes[e.modifierID] != e || !e.ownsLanding(member) || member.deployedHitPoint() <= 0 {
		return
	}
	member.heroKnockbackLanding.isLanded = true
	packet, err := npcraknet.ResetAnimation(e.objectID, e.landingTime)
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena push landing failed target=%d: %v", e.objectID, err)
		}
	} else {
		e.queue(&member, [][]byte{packet})
	}
	e.runtime.registry.sessions[e.targetKey] = member
}

func (e *arenaHitPushRun) expire() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.expiryCancel = nil
	if e.runtime.registry.isClosed {
		return
	}
	member, isFound := e.runtime.registry.sessions[e.targetKey]
	if !isFound || !e.matches(member) || member.arenaHitPushes[e.modifierID] != e {
		// Release only this run, never a replacement actor's retained owner.
		e.release(nil)
		return
	}
	e.retire(&member)
	e.runtime.registry.sessions[e.targetKey] = member
}

func (e *arenaHitPushRun) release(member *gameplayPeerSession) [][]byte {
	if e.isRetired {
		return nil
	}
	isSameActor := member != nil && e.matches(*member)
	e.isRetired = true
	e.cancel()
	if member != nil && member.arenaHitPushes[e.modifierID] == e {
		delete(member.arenaHitPushes, e.modifierID)
		if isSameActor && e.ownsLanding(*member) {
			member.heroKnockbackLanding = heroKnockbackLanding{}
		}
	}
	if e.effects != nil {
		e.effects.Retire()
		e.runtime.registry.commitChannelRemovalsLocked()
		return nil
	}
	err := e.runtime.modifierPool.Release(e.instanceID)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena push release failed instance=%d: %v", e.instanceID, err)
	}
	if !isSameActor {
		return nil
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{TargetID: e.objectID, InstanceID: e.instanceID})
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena push delete failed target=%d: %v", e.objectID, err)
		}
		return nil
	}
	return [][]byte{packet}
}

func (e *arenaHitPushRun) queue(member *gameplayPeerSession, packets [][]byte) {
	member.queuePackets(packets)
	for key, candidate := range e.runtime.registry.sessions {
		if key == e.targetKey || candidate.binding.Mode != game.ModeArena || candidate.binding.GameID != e.gameID || candidate.isRejoinPending {
			continue
		}
		candidate.queuePackets(packets)
		e.runtime.registry.sessions[key] = candidate
	}
}

func (e *arenaHitPushRun) retire(member *gameplayPeerSession) {
	packets := e.release(member)
	if len(packets) > 0 {
		e.queue(member, packets)
	}
}
