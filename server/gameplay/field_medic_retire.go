package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func scheduleFieldMedicExpiry(runtime campaignAbilityCommandRuntime, expiresAt time.Time, execute func()) (raknet.CancelSchedule, error) {
	if runtime.registry == nil || runtime.registry.timer == nil {
		return nil, errors.New("field medic expiry timer unavailable")
	}
	cancel, err := runtime.registry.timer.Schedule(max(time.Duration(0), expiresAt.Sub(runtime.now())), execute)
	if err != nil {
		return nil, fmt.Errorf("expiryTimer: %w", err)
	}
	if cancel == nil {
		return nil, errors.New("field medic expiry cancellation unavailable")
	}
	return cancel, nil
}

func (e *fieldMedicHeroBuffRun) retire(peerSession *gameplayPeerSession, pool *modifierPool) ([][]byte, error) {
	if e == nil || peerSession == nil {
		return nil, nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isRetired {
		return nil, nil
	}
	deletePacket, err := effectraknet.ModifierDelete(e.targetObjectID, e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("heroDelete: %w", err)
	}
	err = e.remove(peerSession)
	if err != nil {
		return nil, fmt.Errorf("heroRemove: %w", err)
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.isRetired = true
	var retirementErr error
	if !e.isCompanion && peerSession.squad != nil {
		maximumHitPoint := peerSession.characterHitPointMaximum(e.creatureIndex)
		character, isFound := peerSession.squad.Character(e.creatureIndex)
		if isFound && character.HitPoints > maximumHitPoint {
			isGameOver, clampErr := peerSession.setCampaignCharacterHitPoints(e.creatureIndex, maximumHitPoint)
			if clampErr != nil {
				retirementErr = errors.Join(retirementErr, fmt.Errorf("heroClamp: %w", clampErr))
			}
			// Lowering a positive capacity must not change defeat admission.
			_ = isGameOver
		}
		syncErr := peerSession.syncZoneHero()
		if syncErr != nil {
			retirementErr = errors.Join(retirementErr, fmt.Errorf("heroSync: %w", syncErr))
		}
	}
	if e.zone != nil && e.zone.Effect() != nil {
		e.zone.Effect().Remove(e.instanceID)
	}
	packets := [][]byte{deletePacket}
	resourcePackets, resourceErr := fieldMedicTargetResourcePackets(*peerSession, e)
	if resourceErr != nil {
		retirementErr = errors.Join(retirementErr, fmt.Errorf("heroResource: %w", resourceErr))
	} else {
		packets = append(packets, resourcePackets...)
	}
	if retirementErr != nil {
		return packets, fmt.Errorf("heroRetirement: %w", retirementErr)
	}
	return packets, nil
}

// Call before session removal, recipient teardown, or retained pet capture.
// Both natural expiry and early retirement queue into the original zone while
// the registry lock serializes recipient stats and surviving peers' delivery.
func (e *gameplaySessionRegistry) retireFieldMedicBuffsLocked(peerSession *gameplayPeerSession, pool *modifierPool) {
	heroPackets, heroErr := peerSession.stopFieldMedicHeroBuffs(pool)
	companionPackets, companionErr := peerSession.stopFieldMedicCompanionBuffs(pool)
	packets := append(heroPackets, companionPackets...)
	peerSession.queueCampaignPackets(packets)
	e.queueFieldMedicPacketsLocked(peerSession.zone, packets, peerSession.binding.UserID)
	peerSession.releaseFieldMedicBuffs(pool)
	for _, retirementErr := range []error{heroErr, companionErr} {
		if retirementErr != nil && e.logger != nil {
			e.logger.Printf("RakNet Field Medic retirement failed user=%d: %v", peerSession.binding.UserID, retirementErr)
		}
	}
}

func (e *fieldMedicHeroBuffRun) release(pool *modifierPool) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if !e.isRetired || e.isReleased || pool == nil {
		return nil
	}
	err := pool.Release(e.instanceID)
	if err != nil {
		return fmt.Errorf("heroRelease: %w", err)
	}
	e.isReleased = true
	return nil
}

func (e *fieldMedicHeroBuffRun) retireMissingRecipient() ([][]byte, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isRetired {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(e.targetObjectID, e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("missingDelete: %w", err)
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	// Recipient removal retires its stat storage. Never subtract an old run
	// from a replacement member that happens to reuse the same hero object ID.
	if e.zone != nil && e.zone.Effect() != nil {
		e.zone.Effect().Remove(e.instanceID)
	}
	e.isRetired = true
	return [][]byte{packet}, nil
}

func (e *fieldMedicHeroBuffRun) retryRetirement(runtime campaignAbilityCommandRuntime, execute func()) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isRetired {
		return
	}
	cancel, err := scheduleFieldMedicExpiry(runtime, runtime.now().Add(time.Second), execute)
	if err != nil {
		if runtime.logger != nil {
			runtime.logger.Printf("RakNet Field Medic hero retirement retry failed instance=%d: %v", e.instanceID, err)
		}
		return
	}
	e.cancel = cancel
}

func (e *fieldMedicCompanionBuffRun) retire() ([][]byte, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isRetired {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(e.targetObjectID, e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("companionDelete: %w", err)
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.zone != nil {
		actor, isFound := e.zone.Companion().Snapshot(e.targetObjectID)
		if isFound && actor.UserID == e.userID && actor.PeerGeneration == e.generation &&
			actor.OwnerObjectID == e.ownerObjectID {
			e.zone.Companion().RemoveBuff(e.targetObjectID, e.buff)
		}
		e.zone.Effect().Remove(e.instanceID)
	}
	e.isRetired = true
	return [][]byte{packet}, nil
}

func (e *fieldMedicCompanionBuffRun) release(pool *modifierPool) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if !e.isRetired || e.isReleased || pool == nil {
		return nil
	}
	err := pool.Release(e.instanceID)
	if err != nil {
		return fmt.Errorf("companionRelease: %w", err)
	}
	e.isReleased = true
	return nil
}

func (e *fieldMedicCompanionBuffRun) retryRetirement(runtime campaignAbilityCommandRuntime, execute func()) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isRetired {
		return
	}
	cancel, err := scheduleFieldMedicExpiry(runtime, runtime.now().Add(time.Second), execute)
	if err != nil {
		if runtime.logger != nil {
			runtime.logger.Printf("RakNet Field Medic pet retirement retry failed instance=%d: %v", e.instanceID, err)
		}
		return
	}
	e.cancel = cancel
}

func (e *gameplaySessionRegistry) queueFieldMedicPacketsLocked(originalZone *zone.Zone, packets [][]byte, excludedUserIDs ...uint64) {
	if originalZone == nil || len(packets) == 0 {
		return
	}
	for sessionKey, candidate := range e.sessions {
		if candidate.zone != originalZone || candidate.isZoneTerminal() {
			continue
		}
		if len(excludedUserIDs) == 1 && candidate.binding.UserID == excludedUserIDs[0] {
			continue
		}
		candidate.queueCampaignPackets(packets)
		e.sessions[sessionKey] = candidate
	}
}

func rollbackFieldMedicHeroBuff(peerSession *gameplayPeerSession, run *fieldMedicHeroBuffRun, pool *modifierPool) error {
	run.mutex.Lock()
	defer run.mutex.Unlock()
	removeErr := run.remove(peerSession)
	peerSession.zone.Effect().Remove(run.instanceID)
	delete(peerSession.fieldMedicHeroBuffs, run.instanceID)
	var syncErr error
	if !run.isCompanion {
		syncErr = peerSession.syncZoneHero()
	}
	releaseErr := pool.Release(run.instanceID)
	run.isRetired = true
	run.isReleased = releaseErr == nil
	rollbackErr := errors.Join(removeErr, syncErr, releaseErr)
	if rollbackErr != nil {
		return fmt.Errorf("heroRollback: %w", rollbackErr)
	}
	return nil
}

func (e *gameplayPeerSession) releaseFieldMedicBuffs(pool *modifierPool) {
	for instanceID, run := range e.fieldMedicHeroBuffs {
		releaseErr := run.release(pool)
		if releaseErr != nil {
			log.Printf("RakNet Field Medic hero release failed instance=%d: %v", instanceID, releaseErr)
		}
		if run.isRetired {
			delete(e.fieldMedicHeroBuffs, instanceID)
		}
	}
	for instanceID, run := range e.fieldMedicCompanionBuffs {
		releaseErr := run.release(pool)
		if releaseErr != nil {
			log.Printf("RakNet Field Medic companion release failed instance=%d: %v", instanceID, releaseErr)
		}
		if run.isRetired {
			delete(e.fieldMedicCompanionBuffs, instanceID)
		}
	}
}
