package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

type cloudActionReply struct {
	syncStamp    uint8
	abilityID    uint32
	abilityIndex uint32
}

type cloudActionCommit struct {
	registry   *gameplaySessionRegistry
	sessionKey string
	generation uint64
	runs       []*heroAuraAreaRun
	now        time.Time
	timestamp  uint64
}

func (e cloudActionCommit) commit() {
	e.registry.mutex.Lock()
	err := e.registry.deliverCloudActionLocked(e.sessionKey, e.generation, e.runs, e.now, e.timestamp)
	e.registry.mutex.Unlock()
	if err != nil {
		log.Printf("Sleeping Cloud action delivery failed: %v", err)
	}
}

// Capture existing listeners before admitting a new cast. Its own callback
// must never receive the StartedNewAction event that starts it.
func (e *gameplaySessionRegistry) sleepingCloudListeners(sessionKey string, generation uint64) []*heroAuraAreaRun {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	member, isFound := e.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		return nil
	}
	runs := make([]*heroAuraAreaRun, 0)
	for _, run := range member.heroAuraAreas {
		if run != nil && run.isSleepingCloud {
			runs = append(runs, run)
		}
	}
	return runs
}

// Caller holds the registry lock. A copied listener cannot end a replacement
// field or complete another cast's action reservation.
func (e *gameplaySessionRegistry) deliverCloudActionLocked(sessionKey string, generation uint64,
	runs []*heroAuraAreaRun, now time.Time, timestamp uint64,
) error {
	for _, run := range runs {
		member, isFound := e.sessions[sessionKey]
		if !isFound || member.generation != generation || member.zone != run.originalZone ||
			member.squad != run.cast.squad || member.deployedCreatureIndex != run.cast.creatureIndex ||
			run.cast.generation != generation || run.cast.userID != member.binding.UserID ||
			run.cast.sourceObjectID != member.deployedObjectID || !now.Before(run.expiresAt) || run.cloudAction == nil {
			continue
		}
		isRunning, callbackErr := run.cloudAction.StartedNewAction()
		if isRunning {
			continue
		}
		releasePacket, err := abilityraknet.ReleaseResponse(run.cloudReply.syncStamp, run.cloudReply.abilityID,
			run.cloudReply.abilityIndex, timestamp, 0, 0)
		if err != nil {
			return fmt.Errorf("cloudRelease: %w", err)
		}
		for abilityID, current := range member.heroAuraAreas {
			if current == run {
				delete(member.heroAuraAreas, abilityID)
			}
		}
		e.sessions[sessionKey] = member
		isCompleted := run.cast.release.Complete(run.cast.releaseReservation, now)
		if !isCompleted {
			// An accepted replacement owns its own release reservation.
		}
		e.queueChannelReleaseLocked(run.originalZone, sessionKey, generation, [][]byte{releasePacket})
		err = run.retireAuraLocked()
		if err != nil {
			auraRetirement{run: run}.schedule(heroAuraAreaScanInterval)
			return fmt.Errorf("cloudRetire: %w", errors.Join(callbackErr, err))
		}
		if callbackErr != nil {
			return fmt.Errorf("cloudAction: %w", callbackErr)
		}
	}
	return nil
}

func (e *gameplaySessionRegistry) startedMovementAction(sessionKey string, generation uint64,
	sourceObjectID uint32, goal game.Vec3, isStop bool, now time.Time, timestamp uint64,
) error {
	if isStop {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	member, isFound := e.sessions[sessionKey]
	if !isFound || member.generation != generation || member.deployedObjectID != sourceObjectID ||
		member.zone == nil || member.isZoneTerminal() || member.deployedHitPoint() <= 0 ||
		member.isHeroSelectionPending || member.campaignTunnelTransfer != nil || member.basicAttack != nil ||
		member.isKnockbackActive(now) || member.isEnemySleepActive(now) || member.isEnemyStunActive(now) ||
		member.isEnemyRootActive(now) || member.isEnemyFearActive(now) ||
		goal == game.Vec3(member.playerPosition) || goal == game.Vec3(member.playerMovementGoal) {
		return nil
	}
	runs := make([]*heroAuraAreaRun, 0)
	for _, run := range member.heroAuraAreas {
		if run != nil && run.isSleepingCloud && run.cast.release.IsCurrent(run.cast.releaseReservation) {
			runs = append(runs, run)
		}
	}
	err := e.deliverCloudActionLocked(sessionKey, generation, runs, now, timestamp)
	if err != nil {
		return fmt.Errorf("movementAction: %w", err)
	}
	return nil
}
