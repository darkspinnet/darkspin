package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func (e *hostileProjectileFreezeStep) releaseLocked(isRetiring bool) error {
	if e.isReleased {
		return nil
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: e.projectileObjectID, InstanceID: e.instanceID,
	})
	if err != nil {
		return fmt.Errorf("freezeDeleteEncode: %w", err)
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.stopContext != nil {
		isStopped := e.stopContext()
		if !isStopped {
			// A dispatched context callback observes the released lease.
		}
		e.stopContext = nil
	}
	if !e.isDeletionQueued {
		e.runtime.registry.queueProjectileRetirementLocked(e.originalZone, [][]byte{packet})
		e.isDeletionQueued = true
	}
	err = e.runtime.modifierPool.Release(e.instanceID)
	if err != nil {
		return fmt.Errorf("freezeInstanceRelease: %w", err)
	}
	e.isReleased = true
	freezes := e.runtime.registry.projectileFreezes[e.run]
	delete(freezes, e.instanceID)
	if len(freezes) == 0 {
		delete(e.runtime.registry.projectileFreezes, e.run)
	}
	return nil
}

func (e *gameplaySessionRegistry) retireProjectileFreezesLocked(req *campaignProjectileRetirement) error {
	cleanupErrs := make([]error, 0)
	for _, freeze := range e.projectileFreezes[req.run] {
		err := freeze.releaseLocked(true)
		if err != nil {
			cleanupErrs = append(cleanupErrs, err)
			continue
		}
	}
	e.retireProjectileChronoLocked(req.originalZone, req.objectID, req.run)
	cleanupErr := errors.Join(cleanupErrs...)
	if cleanupErr != nil {
		return fmt.Errorf("retireFreezes: %w", cleanupErr)
	}
	return nil
}

// Only actual departing flight ownership retires these leases. A disconnected
// caster's lease on another member's surviving flight keeps its zone deadline.
// The registry lock is held; preserve the caller's working session and queued
// owner delivery when later lifecycle code stores that session again.
func (e *gameplaySessionRegistry) retirePeerProjectileFreezesLocked(peerSession *gameplayPeerSession) {
	ownerSessionKey := ""
	for sessionKey, member := range e.sessions {
		if member.binding.UserID == peerSession.binding.UserID && member.generation == peerSession.generation && member.zone == peerSession.zone {
			ownerSessionKey = sessionKey
			e.sessions[sessionKey] = *peerSession
			break
		}
	}
	for run, retirement := range e.projectileRetirements {
		isOwned := retirement.originalZone == peerSession.zone &&
			(peerSession.campaignNPCProjectiles[retirement.objectID] == run || peerSession.sageAttacks[retirement.objectID] == run)
		if !isOwned {
			continue
		}
		err := e.retireCampaignProjectileLocked(retirement)
		if err != nil && e.logger != nil {
			e.logger.Printf("RakNet projectile owner retirement failed projectile=%d: %v", retirement.objectID, err)
		}
	}
	for run, freezes := range e.projectileFreezes {
		for _, freeze := range freezes {
			isOwned := freeze.originalZone == peerSession.zone &&
				peerSession.campaignNPCProjectiles[freeze.projectileObjectID] == run
			if !isOwned {
				continue
			}
			err := freeze.releaseLocked(true)
			if err != nil && e.logger != nil {
				e.logger.Printf("RakNet projectile freeze teardown failed projectile=%d instance=%d: %v", freeze.projectileObjectID, freeze.instanceID, err)
			}
			if len(e.projectileFreezes[run]) == 0 {
				e.retireProjectileChronoLocked(freeze.originalZone, freeze.projectileObjectID, run)
			}
		}
	}
	if ownerSessionKey != "" {
		*peerSession = e.sessions[ownerSessionKey]
	}
}
