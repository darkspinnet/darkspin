package gameplay

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

type hostileProjectileOwner struct {
	sessionKey string
	generation uint64
	objectID   uint32
	run        *abilityraknet.ProjectileRun
}

// Capture the caster's working mutations before reading the shared registry.
// Conflicting identities for one wire ID are skipped rather than acting on a
// stale alias. Identical aliases represent one projectile and one result.
func (e *gameplaySessionRegistry) hostileProjectileOwnersLocked(
	peerSession *gameplayPeerSession, sessionKey string,
) []hostileProjectileOwner {
	e.sessions[sessionKey] = *peerSession
	sessionKeys := make([]string, 0, len(e.sessions))
	for ownerSessionKey := range e.sessions {
		sessionKeys = append(sessionKeys, ownerSessionKey)
	}
	slices.Sort(sessionKeys)
	ownersByObjectIDs := make(map[uint32]hostileProjectileOwner)
	conflicts := make(map[uint32]bool)
	for _, ownerSessionKey := range sessionKeys {
		member := e.sessions[ownerSessionKey]
		if member.zone != peerSession.zone {
			continue
		}
		for objectID, run := range member.campaignNPCProjectiles {
			if run == nil {
				continue
			}
			owner, isFound := ownersByObjectIDs[objectID]
			if isFound && owner.run != run {
				conflicts[objectID] = true
				continue
			}
			retirement := e.projectileRetirements[run]
			if retirement == nil || !retirement.isAdmitted || !retirement.isPublished || retirement.isRetired || retirement.isRetirementPending {
				continue
			}
			if !isFound || (retirement != nil && retirement.sessionKey == ownerSessionKey && retirement.generation == member.generation) {
				ownersByObjectIDs[objectID] = hostileProjectileOwner{
					sessionKey: ownerSessionKey, generation: member.generation, objectID: objectID, run: run,
				}
			}
		}
	}
	objectIDs := make([]uint32, 0, len(ownersByObjectIDs))
	for objectID := range ownersByObjectIDs {
		if !conflicts[objectID] {
			objectIDs = append(objectIDs, objectID)
		}
	}
	slices.Sort(objectIDs)
	owners := make([]hostileProjectileOwner, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		owners = append(owners, ownersByObjectIDs[objectID])
	}
	return owners
}

func (e *gameplaySessionRegistry) isHostileProjectileCurrentLocked(originZone *zone.Zone, objectID uint32, run *abilityraknet.ProjectileRun) bool {
	isFound := false
	for _, member := range e.sessions {
		if member.zone != originZone {
			continue
		}
		current := member.campaignNPCProjectiles[objectID]
		if current != nil && current != run {
			return false
		}
		if current == run {
			isFound = true
		}
	}
	return isFound
}

func scheduleSharedFreezeLocked(runtime campaignAbilityCommandRuntime, deadline time.Time, execute func()) (raknet.CancelSchedule, error) {
	if runtime.registry.timer == nil {
		return nil, errors.New("shared freeze timer unavailable")
	}
	delay := max(time.Duration(0), deadline.Sub(runtime.now()))
	cancel, err := runtime.registry.timer.Schedule(delay, execute)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("freezeTimer: %w", err)
	}
	if cancel == nil {
		return nil, errors.New("shared freeze cancellation unavailable")
	}
	return cancel, nil
}
