package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// The shared 250 ms aura scan owns field entry, one-second damage ticks and
// final removal. Arena membership binds silence to the recipient hero.
func (e heroAuraAreaSchedule) reconcileArenaControlAuraLocked(
	peerSession *gameplayPeerSession, deadline time.Duration, expiresAt time.Time,
) error {
	e.runtime.registry.sessions[e.sessionKey] = *peerSession
	targets := arenaAbilityTargets{runtime: e.runtime, source: *peerSession}
	liveTargets := make(map[uint32]string)
	for _, target := range targets.LiveSnapshots() {
		if zonegeometry.Distance(game.Vec3(e.center), target.Plan.Position) > e.definition.Radius {
			continue
		}
		key, member, isFound := targets.member(target.Plan.ObjectID)
		if !isFound || member.isHeroDebuffImmune(target.Plan.ObjectID) || member.isOverdriveActiveAt(e.runtime.now()) {
			continue
		}
		liveTargets[target.Plan.ObjectID] = key
	}
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isCleaned || e.run.isStopping {
		return nil
	}
	for objectID, status := range e.run.arenaStatuses {
		if _, isInside := liveTargets[objectID]; isInside {
			continue
		}
		member := e.runtime.registry.sessions[status.targetKey]
		status.retire(&member)
		e.runtime.registry.sessions[status.targetKey] = member
		delete(e.run.arenaStatuses, objectID)
	}
	for objectID, key := range liveTargets {
		if e.run.arenaStatuses[objectID] != nil {
			continue
		}
		if e.run.isSleepingCloud {
			if _, isAttempted := e.run.sleepAttempts[objectID]; isAttempted {
				continue
			}
		}
		member := e.runtime.registry.sessions[key]
		duration := max(heroAuraAreaScanInterval, expiresAt.Sub(e.runtime.now()))
		name := "PsistormDebuff"
		kind := sim.AbilityStatusKindSilence
		if e.run.isSleepingCloud {
			name = "SleepingCloud_SleepModifier"
			kind = sim.AbilityStatusKindSleep
		}
		statusTargets := arenaAbilityTargets{runtime: e.runtime, source: *peerSession}
		status, createPacket, err := statusTargets.prepareBasicStatusLocked(
			key, member, objectID, name, kind,
			1, 1, duration, e.packet.SourceTime+uint64(deadline/time.Millisecond),
		)
		if err != nil {
			return fmt.Errorf("arenaPsiStatus: %w", err)
		}
		status.isCasterUnique = true
		if member.arenaHitStatuses == nil {
			member.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
		}
		if previous := member.arenaHitStatuses[status.key()]; previous != nil {
			previous.retire(&member)
		}
		member.arenaHitStatuses[status.key()] = status
		stopPackets := make([][]byte, 0)
		if e.run.isSleepingCloud {
			stopPackets, err = stopEnemyControlledHeroMovement(&member, e.runtime.now())
			if err != nil {
				status.retire(&member)
				e.runtime.registry.sessions[key] = member
				return fmt.Errorf("arenaSleepStop: %w", err)
			}
		}
		attributes, err := status.refresh(&member)
		if err != nil {
			delete(member.arenaHitStatuses, status.key())
			rollbackErr := status.discard("arenaPsiRollback", err)
			e.runtime.registry.sessions[key] = member
			return fmt.Errorf("arenaPsiAttributes: %w", rollbackErr)
		}
		packets := append([][]byte{createPacket}, stopPackets...)
		packets = append(packets, attributes...)
		if e.definition.HitEffectName != "" {
			effectPacket, effectErr := raknet.MarshalApplication(raknet.ServerEventMessage{
				Asset: util.HashID(e.definition.HitEffectName), ObjectID: objectID,
			})
			if effectErr != nil {
				status.retire(&member)
				e.runtime.registry.sessions[key] = member
				return fmt.Errorf("arenaAuraHitEffect: %w", effectErr)
			}
			packets = append(packets, effectPacket)
		}
		status.queue(&member, packets)
		e.runtime.registry.sessions[key] = member
		if e.run.arenaStatuses == nil {
			e.run.arenaStatuses = make(map[uint32]*arenaHitStatus)
		}
		e.run.arenaStatuses[objectID] = status
		if e.run.isSleepingCloud {
			if e.run.sleepAttempts == nil {
				e.run.sleepAttempts = make(map[uint32]struct{})
			}
			e.run.sleepAttempts[objectID] = struct{}{}
		}
	}
	*peerSession = e.runtime.registry.sessions[e.sessionKey]
	return nil
}

func (e heroAuraAreaSchedule) tickArenaPsistormLocked(
	peerSession gameplayPeerSession, deadline time.Duration, index uint32, isFinal bool,
	statusPackets [][]byte,
) ([][]byte, error) {
	var targets *arenaAbilityTargets
	results := make([]zoneability.AreaResult, 0)
	if index%uint32(time.Second/heroAuraAreaScanInterval) == 0 {
		timestamp := e.packet.SourceTime + uint64(deadline/time.Millisecond)
		targets = e.runtime.registry.abilityTargetsLocked(
			e.runtime, peerSession, e.definition, timestamp,
		).(*arenaAbilityTargets)
		plan, err := zoneability.PlanFixedArea(
			targets, e.sourceObjectID, game.Vec3(e.center), e.creature, e.definition,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaPsiPlan: %w", err)
		}
		random, err := e.runtime.registry.abilityRandomLocked(peerSession)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaPsiRandom: %w", err)
		}
		results, err = zoneability.CommitArea(
			random, targets, plan, e.creature, peerSession.binding.Difficulty,
			e.runtime.program.Critical,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaPsiCommit: %w", err)
		}
	}
	if isFinal {
		current := e.runtime.registry.sessions[e.sessionKey]
		delete(current.heroAuraAreas, e.abilityID)
		e.run.mutex.Lock()
		e.run.cancel = nil
		e.run.mutex.Unlock()
		e.runtime.registry.sessions[e.sessionKey] = current
		err := e.run.retireAuraLocked()
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaPsiRetire: %w", err)
		}
	}
	e.runtime.registry.mutex.Unlock()
	if targets == nil {
		return statusPackets, nil
	}
	targets.finish()
	damagePackets, err := targets.packets(results)
	if err != nil {
		return nil, fmt.Errorf("arenaPsiPackets: %w", err)
	}
	return append(statusPackets, damagePackets...), nil
}

func (e heroAuraAreaSchedule) tickArenaSleepingCloudLocked(
	isFinal bool, statusPackets [][]byte,
) ([][]byte, error) {
	if !isFinal {
		e.runtime.registry.mutex.Unlock()
		return statusPackets, nil
	}
	current := e.runtime.registry.sessions[e.sessionKey]
	delete(current.heroAuraAreas, e.abilityID)
	e.run.mutex.Lock()
	e.run.cancel = nil
	e.run.mutex.Unlock()
	e.runtime.registry.sessions[e.sessionKey] = current
	err := e.run.retireAuraLocked()
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("arenaSleepRetire: %w", err)
	}
	releasePackets, err := e.release()
	if err != nil {
		return nil, fmt.Errorf("arenaSleepRelease: %w", err)
	}
	return append(statusPackets, releasePackets...), nil
}

// Caller owns the registry lock. Status retirement publishes each A4 and
// attribute restoration; the field object is retired once for the match.
func (e *heroAuraAreaRun) retireArenaPsistormLocked() error {
	if e.arenaGameID == 0 {
		return errors.New("Arena Psistorm game unavailable")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return nil
	}
	e.isStopping = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	var deletePacket []byte
	if e.isObjectPublished {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.objectID}})
		if err != nil {
			return fmt.Errorf("arenaPsiDelete: %w", err)
		}
		deletePacket = packet
	}
	for objectID, status := range e.arenaStatuses {
		member := e.runtime.registry.sessions[status.targetKey]
		status.retire(&member)
		e.runtime.registry.sessions[status.targetKey] = member
		delete(e.arenaStatuses, objectID)
	}
	if len(deletePacket) != 0 {
		e.queuePresentationLocked([][]byte{deletePacket}, true)
	}
	e.isCleaned = true
	delete(e.runtime.registry.auraRuns, e)
	return nil
}

// Build-103 Sleeping Cloud retains its one-shot target membership after
// damage wakes the hero, so a later field scan cannot reapply sleep.
func (e *gameplaySessionRegistry) breakArenaSleepingCloudLocked(gameID uint32, objectID uint32) {
	for run := range e.auraRuns {
		if run.arenaGameID != gameID || !run.isSleepingCloud {
			continue
		}
		run.mutex.Lock()
		status := run.arenaStatuses[objectID]
		if status != nil {
			member := e.sessions[status.targetKey]
			status.retire(&member)
			e.sessions[status.targetKey] = member
			delete(run.arenaStatuses, objectID)
		}
		run.mutex.Unlock()
	}
}
