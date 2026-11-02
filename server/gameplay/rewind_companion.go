package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

func (e *gameplaySessionRegistry) setCompanionAttackRelease(
	sessionKey string, generation uint64, objectID uint32, targetObjectID uint32, end time.Time,
) bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	member, isFound := e.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil || member.zone.Companion() == nil {
		return false
	}
	return member.zone.Companion().SetAttackReleaseEnd(objectID, targetObjectID, end)
}

type rewindCompanionWake struct {
	runtime     campaignDamageRuntime
	packet      raknet.Packet
	sessionKey  string
	generation  uint64
	objectID    uint32
	ownerID     uint32
	cooldownEnd time.Time
	sourceTime  uint64
	publication campaignPeerPublication
}

func (e campaignProjectileSchedule) scaleCompanionCooldownsLocked(
	sessionKey string, member gameplayPeerSession, center game.Vec3, radius float32, at time.Time,
) error {
	if member.zone == nil || member.zone.Companion() == nil {
		return nil
	}
	for _, actor := range member.zone.Companion().Snapshots() {
		if !isPartyCompanionOwner(actor, member) || !actor.IsCombatant || !actor.IsTargetable ||
			actor.HitPoint <= 0 || actor.IsIntangible ||
			zonegeometry.Distance(center, actor.PositionAt(at)) > radius {
			continue
		}
		previousEnd := actor.CooldownEnd
		changed, isChanged, err := member.zone.Companion().ScaleRemaining(actor.ObjectID, at, float32(0.33000001311302185))
		if err != nil {
			return fmt.Errorf("actorScale: %w", err)
		}
		if !isChanged {
			continue
		}
		// Existing release/impact producers own the accepted cast. Wake after
		// their exact deadline, even when the scaled cooldown falls before it.
		wakeAt := changed.CooldownEnd
		releaseEnd := changed.AttackReleaseEnd.Add(time.Millisecond)
		if !changed.AttackReleaseEnd.IsZero() && releaseEnd.After(wakeAt) {
			wakeAt = releaseEnd
		}
		wake := &rewindCompanionWake{
			runtime: e.runtime.damage, packet: member.schedulePacket,
			sessionKey: sessionKey, generation: member.generation,
			objectID: actor.ObjectID, ownerID: actor.OwnerObjectID,
			cooldownEnd: changed.CooldownEnd,
			sourceTime:  advanceHeroArrivalTimestamp(e.packet.SourceTime, e.startedAt, wakeAt),
			publication: campaignPeerPublication{registry: e.runtime.registry,
				identity: gameplayProducerIdentityFromSession(sessionKey, member, true)},
		}
		producers := e.runtime.registry.producerGuard.scheduledProducersForIdentity(wake.publication.identity,
			[]raknet.ScheduledPacketProducer{{Delay: max(time.Duration(0), wakeAt.Sub(at)), Produce: wake.produce, AfterCommit: wake.commit}})
		cancel, scheduleErr := member.schedulePacket.ScheduleProducers(producers)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("companion wake cancellation unavailable")
		}
		if scheduleErr != nil {
			if cancel != nil {
				cancel()
			}
			isRestored := member.zone.Companion().RestoreCooldown(actor.ObjectID, changed.CooldownEnd, previousEnd)
			if !isRestored {
				return fmt.Errorf("wakeRestore: %w", scheduleErr)
			}
			return fmt.Errorf("wakeSchedule: %w", scheduleErr)
		}
	}
	return nil
}

func (e *rewindCompanionWake) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.deployedObjectID != e.ownerID ||
		member.zone == nil || member.zone.Companion() == nil || member.isRejoinPending || member.isZoneTerminal() {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	actor, isActorFound := member.zone.Companion().Snapshot(e.objectID)
	isCurrent := isActorFound && isPartyCompanionOwner(actor, member) && actor.HitPoint > 0 &&
		actor.CooldownEnd.Equal(e.cooldownEnd) && actor.TargetObjectID == 0 && !actor.IsAbilityControlled
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.runtime.startCompanionAttacks(e.packet, e.sessionKey, e.generation, e.sourceTime)
	if err != nil {
		return nil, fmt.Errorf("wakeAttack: %w", err)
	}
	e.publication.packets = clonePendingPackets(packets)
	return packets, nil
}

func (e *rewindCompanionWake) commit() {
	e.publication.commit()
}
