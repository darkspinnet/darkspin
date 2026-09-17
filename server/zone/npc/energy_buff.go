package npc

import (
	"errors"
	"math"
	"strings"
	"time"

	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const (
	EnergyBuffModifierName = "EnergyBuffModifier"
	EnergyBuffTargetEffect = "cyber_zlm_lieu_boost_target.ServerEventDef"
)

type EnergyBuffProfile struct {
	Cast              ActionProfile
	SelfAnimationName string
	SelectionRange    float32
	DamageIncrease    float32
}

func ZelemSpecialThreeEnergyBuffProfile(
	nounName string,
) (EnergyBuffProfile, bool) {
	cooldown := time.Duration(0)
	damageIncrease := float32(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "zelemspecialthree.noun", "zelemspecialthree_captain.noun":
		cooldown = 6 * time.Second
		damageIncrease = 1
		movementSpeed, nonCombatMovementSpeed = 5.5, 4
	case "zelemspecialthree_2.noun", "zelemspecialthree_captain_2.noun":
		cooldown = 5 * time.Second
		damageIncrease = 1.5
		movementSpeed, nonCombatMovementSpeed = 8, 4.5
	case "zelemspecialthree_3.noun", "zelemspecialthree_captain_3.noun":
		cooldown = 4 * time.Second
		damageIncrease = 2
		movementSpeed, nonCombatMovementSpeed = 10.5, 5
	default:
		return EnergyBuffProfile{}, false
	}
	return EnergyBuffProfile{
		Cast: ActionProfile{
			Family:        ActionProjectile,
			AbilityName:   "ZelemSpecialThreeEnergyBuff",
			AnimationName: "zlm_lieu_tc_3_attack1",
			HitDelay:      366667 * time.Microsecond,
			ReleaseDelay:  2600 * time.Millisecond,
			Cooldown:      cooldown, Range: 12,
			MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
			ModifierName:     EnergyBuffModifierName,
			ModifierDuration: 30 * time.Second,
			TargetEffectName: EnergyBuffTargetEffect,
		},
		SelfAnimationName: "zlm_lieu_tc_3_attack1_self",
		SelectionRange:    20,
		DamageIncrease:    damageIncrease,
	}, true
}

func (s *Session) FirstEnergyDamageAlly(
	sourceObjectID uint32, maximumRange float32, at time.Time,
) (Snapshot, bool) {
	if s == nil || sourceObjectID == 0 || maximumRange <= 0 || at.IsZero() ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished {
		return Snapshot{}, false
	}
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		profile, isProfileFound := ActionProfileForPlan(candidate.Plan)
		if candidate.Faction != source.Faction || candidate.IsDefeated ||
			!candidate.IsPublished || candidate.Plan.IsFixture || candidate.HitPoint <= 0 ||
			at.Before(candidate.status.energyBuffExpiresAt) ||
			zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position) > maximumRange ||
			!isProfileFound || profile.DamageSource != 1 {
			continue
		}
		return candidate, true
	}
	return Snapshot{}, false
}

func (s *Session) ApplyEnergyBuff(
	objectID uint32, expiresAt time.Time, damageIncrease float32,
) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() || damageIncrease <= 0 {
		return errors.New("invalid npc energy buff")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return errors.New("npc energy buff target unavailable")
	}
	npc.status.energyBuffExpiresAt = expiresAt
	npc.status.energyDamageIncrease = damageIncrease
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) ClearEnergyBuff(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.energyBuffExpiresAt != expiresAt {
		return
	}
	npc.status.energyBuffExpiresAt = time.Time{}
	npc.status.energyDamageIncrease = 0
	s.npcs[objectID] = npc
}

func (s *Session) EnergyBuffRemaining(
	objectID uint32, at time.Time,
) time.Duration {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !at.Before(npc.status.energyBuffExpiresAt) {
		return 0
	}
	return npc.status.energyBuffExpiresAt.Sub(at)
}
