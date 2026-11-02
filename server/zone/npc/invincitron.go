package npc

import (
	"fmt"
	"strings"
	"time"
)

// InvincitronRage is the timed additive attribute contribution from build-103
// NomadDroneRage (chunk258), independent of class, affix and other buff owners.
type InvincitronRage struct {
	ExpiresAt    time.Time
	MovementBuff float32
	AttackBonus  float32
}

type InvincitronRageStart struct {
	Actor    Snapshot
	Rank     int32
	Duration time.Duration
}

func (e *Session) StartInvincitronRage(ownerObjectID uint32, at time.Time) ([]InvincitronRageStart, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	owner, isOwnerFound := e.npcs[ownerObjectID]
	if !isOwnerFound || owner.IsDefeated {
		return nil, fmt.Errorf("rageOwner: missing %d", ownerObjectID)
	}
	rank := EffectivePassiveRank(owner.Plan)
	duration := [...]time.Duration{6 * time.Second, 8 * time.Second, 10 * time.Second}[rank-1]
	starts := make([]InvincitronRageStart, 0, 1)
	for _, objectID := range e.objectIDs {
		actor := e.npcs[objectID]
		if actor.Plan.OwnerObjectID != ownerObjectID || actor.IsDefeated || !actor.IsPublished || !strings.EqualFold(actor.Plan.NounName, "NomadDrone.Noun") {
			continue
		}
		actor.Plan.InvincitronRage = InvincitronRage{ExpiresAt: at.Add(duration), MovementBuff: [...]float32{1, 1.5, 2}[rank-1], AttackBonus: 0.5}
		e.npcs[objectID] = actor
		starts = append(starts, InvincitronRageStart{Actor: actor, Rank: rank, Duration: duration})
	}
	return starts, nil
}

func (e *Session) ExpireInvincitronRage(objectID uint32, expiresAt time.Time) (Snapshot, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.Plan.InvincitronRage.ExpiresAt != expiresAt {
		return actor, false
	}
	actor.Plan.InvincitronRage = InvincitronRage{}
	e.npcs[objectID] = actor
	return actor, true
}

func (e InvincitronRage) IsActive(at time.Time) bool { return at.Before(e.ExpiresAt) }

// The threshold callback removes IsDebuff modifiers only. Retain positive
// statuses (including haste, shields and enrage) owned by other features.
func purgeInvincitronDebuffs(actor *Snapshot) {
	status := &actor.status
	for key, modifier := range status.basicModifiers {
		if modifier.IsDebuff() {
			delete(status.basicModifiers, key)
		}
	}
	status.banishExpiresAt = time.Time{}
	status.curseExpiresAt = time.Time{}
	status.curseDamage = CurseDamageProfile{}
	status.damageTakenIncrease = 0
	status.damageVulnerabilityOwner = 0
	status.beastVulnerability = BeastVulnerability{}
	status.physicalTakenIncrease = 0
	status.physicalVulnerabilityEnd = time.Time{}
	status.energyTakenIncrease = 0
	status.energyVulnerabilityOwner = 0
	status.energyVulnerabilityEnd = time.Time{}
	status.healingReduction = 0
	status.healingReductionEnd = time.Time{}
	status.fearExpiresAt = time.Time{}
	status.areaShiftExpiresAt = time.Time{}
	status.rootExpiresAt = time.Time{}
	status.bioEntangleExpiresAt = time.Time{}
	status.bioEntangleInstanceID = 0
	status.supportHealerRootExpiresAt = time.Time{}
	status.supportHealerRootInstanceID = 0
	status.silenceExpiresAt = time.Time{}
	status.sleepExpiresAt = time.Time{}
	status.slowExpiresAt = time.Time{}
	status.slowMovementScale = 0
	status.slowAttackScale = 0
	status.stunExpiresAt = time.Time{}
	status.tauntExpiresAt = time.Time{}
	status.tauntTargetObjectID = 0
}
