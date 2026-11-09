package gameplay

import (
	"fmt"
	"maps"
	"time"

	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

type heroControlKind uint8

const (
	heroControlSleep heroControlKind = iota + 1
	heroControlStun
	heroControlRoot
	heroControlSilence
)

// A contribution follows its exact modifier lifetime, even when another peer
// owns that modifier. Released runs cannot hold a hero after handle reuse.
type heroControlContribution struct {
	run            *campaignNPCModifierRun
	kind           heroControlKind
	targetObjectID uint32
	expiresAt      time.Time
}

func (e *gameplayPeerSession) admitHeroControl(kind heroControlKind, run *campaignNPCModifierRun, targetObjectID uint32, expiresAt time.Time) bool {
	if e == nil || run == nil || run.instanceID == 0 || targetObjectID == 0 || e.deployedObjectID != targetObjectID || expiresAt.IsZero() {
		return false
	}
	if e.heroControls == nil {
		e.heroControls = make(map[uint32]heroControlContribution)
	} else {
		e.heroControls = maps.Clone(e.heroControls)
	}
	for instanceID, contribution := range e.heroControls {
		if !contribution.run.isActive() || contribution.targetObjectID != e.deployedObjectID {
			delete(e.heroControls, instanceID)
		}
	}
	e.heroControls[run.instanceID] = heroControlContribution{run: run, kind: kind, targetObjectID: targetObjectID, expiresAt: expiresAt}
	return true
}

func (e gameplayPeerSession) heroControlExpiresAt(kind heroControlKind) time.Time {
	var expiresAt time.Time
	for _, contribution := range e.heroControls {
		if contribution.kind == kind && contribution.targetObjectID == e.deployedObjectID && contribution.run.isActive() && expiresAt.Before(contribution.expiresAt) {
			expiresAt = contribution.expiresAt
		}
	}
	// No-handle admissions retain their separate legacy deadline.
	var legacyExpiresAt time.Time
	switch kind {
	case heroControlSleep:
		legacyExpiresAt = e.enemySleepExpiresAt
	case heroControlStun:
		if e.enemyStunTargetObjectID == e.deployedObjectID {
			legacyExpiresAt = e.enemyStunExpiresAt
		}
	case heroControlRoot:
		if e.enemyRootTargetObjectID == e.deployedObjectID {
			legacyExpiresAt = e.enemyRootExpiresAt
		}
	case heroControlSilence:
		legacyExpiresAt = e.enemySilenceExpiresAt
	}
	if expiresAt.Before(legacyExpiresAt) {
		return legacyExpiresAt
	}
	return expiresAt
}

func (e gameplayPeerSession) isHeroControlActive(kind heroControlKind, at time.Time) bool {
	return !at.IsZero() && at.Before(e.heroControlExpiresAt(kind))
}

func (e gameplayPeerSession) heroControlTargetObjectID(kind heroControlKind, at time.Time) uint32 {
	if e.isHeroControlActive(kind, at) {
		return e.deployedObjectID
	}
	return 0
}

func (e *gameplayPeerSession) clearHeroControl(instanceID, targetObjectID uint32) {
	contribution, isFound := e.heroControls[instanceID]
	if isFound && contribution.targetObjectID == targetObjectID {
		e.heroControls = maps.Clone(e.heroControls)
		delete(e.heroControls, instanceID)
	}
}

func (e *gameplayPeerSession) clearHeroControlKind(kind heroControlKind) {
	e.heroControls = maps.Clone(e.heroControls)
	for instanceID, contribution := range e.heroControls {
		if contribution.kind == kind {
			delete(e.heroControls, instanceID)
		}
	}
}

// SleepModifier wakes on damage; other sleep templates keep their own filters.
// The caller holds the registry lock and owns the source and target copies.
// Commit every deletion to its outgoing queue before recycling any handle.
func (e campaignNPCActionRuntime) wakeHeroSleepLocked(source, target *gameplayPeerSession) error {
	packets := make([][]byte, 0)
	runs := make([]*campaignNPCModifierRun, 0)
	for instanceID, contribution := range target.heroControls {
		run := contribution.run
		if contribution.kind != heroControlSleep || contribution.targetObjectID != target.deployedObjectID ||
			!run.isActive() || run.record.GUID != util.HashID("SleepModifier") {
			continue
		}
		packet, err := effectraknet.ModifierDelete(contribution.targetObjectID, instanceID)
		if err != nil {
			return fmt.Errorf("sleepDelete: %w", err)
		}
		packets = append(packets, packet)
		runs = append(runs, run)
	}
	if len(packets) == 0 {
		return nil
	}
	for sessionKey, candidate := range e.registry.sessions {
		if candidate.zone != target.zone || candidate.isZoneTerminal() {
			continue
		}
		switch {
		case candidate.binding.UserID == target.binding.UserID && candidate.generation == target.generation:
			target.queueCampaignPackets(packets)
			candidate = *target
		case candidate.binding.UserID == source.binding.UserID && candidate.generation == source.generation:
			source.queueCampaignPackets(packets)
			candidate = *source
		default:
			candidate.queueCampaignPackets(packets)
		}
		e.registry.sessions[sessionKey] = candidate
	}
	for _, run := range runs {
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		target.clearHeroControl(run.instanceID, run.record.TargetObjectID)
		target.untrackCampaignNPCModifier(run)
		target.zone.Effect().Remove(run.instanceID)
		isCreated, releaseErr := run.release(e.modifierPool)
		if releaseErr != nil {
			return fmt.Errorf("sleepRelease: %w", releaseErr)
		}
		if !isCreated {
			// The queued deletion also safely retires an already-released alias.
		}
	}
	return nil
}
