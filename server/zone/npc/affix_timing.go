package npc

import (
	"math"
	"time"

	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const (
	captainCooldownReductionPerPlayer = float32(0.133)
	captainAttackSpeedPerPlayer       = float32(0.2)
	frenziedAttackSpeedBonus          = float32(0.5)
	frenziedCooldownReductionBonus    = float32(0.33)
	packflyHelperRadius               = float32(8)
	packflyMaximumHelperCount         = uint32(7)
)

// AffixTimingBonuses evaluates the native permanent modifiers at the current
// party and nearby-helper counts. The Session stores the result on each plan
// so all ActionProfileForPlan consumers use the same current attributes.
func AffixTimingBonuses(
	plan SpawnPlan, playerCount uint32, helperCount uint32,
) (attackSpeed float32, cooldownReduction float32, movementSpeedBuff float32) {
	if plan.IsCaptain && playerCount > 1 {
		extraPlayerCount := float32(playerCount - 1)
		attackSpeed += extraPlayerCount * captainAttackSpeedPerPlayer
		cooldownReduction += extraPlayerCount * captainCooldownReductionPerPlayer
	}
	if plan.HasAffixModifier("Frenzied_NPCAffixModifier") {
		attackSpeed += frenziedAttackSpeedBonus
		cooldownReduction += frenziedCooldownReductionBonus
	}
	if !isPackfly(plan.NounName) {
		return attackSpeed, cooldownReduction, movementSpeedBuff
	}
	helperCount = min(helperCount, packflyMaximumHelperCount)
	rank := packflyRank(plan)
	attackSpeeds := [...]float32{0.075, 0.15, 0.2}
	movementSpeeds := [...]float32{0.15, 0.25, 0.35}
	attackSpeed += float32(helperCount) * attackSpeeds[rank-1]
	movementSpeedBuff += float32(helperCount) * movementSpeeds[rank-1]
	return attackSpeed, cooldownReduction, movementSpeedBuff
}

func isPackfly(nounName string) bool {
	return nounSpecies(nounName) == "zelembasicpackfly"
}

func packflyRank(plan SpawnPlan) int32 {
	if plan.PackflyPassiveRank > 0 {
		return max(int32(1), min(int32(3), plan.PackflyPassiveRank))
	}
	// Native passive startup uses NonPlayerClass rank plus one when Elite is
	// already active. The instance keeps that rank for its lifetime.
	return EffectivePassiveRank(plan)
}

// PackflyBoostEffectName is the native effect tier for the nearby helper count.
func PackflyBoostEffectName(helperCount uint32) string {
	switch {
	case helperCount == 0:
		return ""
	case helperCount <= 3:
		return "packfly_boost_lo.ServerEventDef"
	case helperCount <= 6:
		return "packfly_boost_med.ServerEventDef"
	default:
		return "packfly_boost_hi.ServerEventDef"
	}
}

// CrowdControlDuration applies CaptainModifier's recipient duration decrease.
// Callers use the result for both authoritative expiry and modifier packets.
func CrowdControlDuration(plan SpawnPlan, duration time.Duration) time.Duration {
	if !plan.IsCaptain || duration <= 0 {
		return duration
	}
	return duration / 2
}

// ApplyAffixTiming reproduces build-103 duration scaling for basic attacks
// (descriptor bit 2) and other abilities. Bonuses are additive attributes.
func ApplyAffixTiming(profile ActionProfile, plan SpawnPlan) ActionProfile {
	if profile.AbilityName == "NomadSpecialThree" {
		profile.PassiveEnergyDefense = NomadSpecialThreePassiveDefense(plan)
	}
	profile = RankedActionProfile(profile, plan)
	attackSpeed := EffectiveAttackSpeedBonus(plan)
	cooldownReduction := plan.AffixCooldownReductionBonus
	if attackSpeed == 0 && cooldownReduction == 0 {
		return profile
	}
	if profile.DescriptorMask&2 != 0 {
		attackSpeed = max(attackSpeed, -0.9)
		scale := float64(1 + attackSpeed)
		profile.HitDelay = time.Duration(float64(profile.HitDelay) / scale)
		profile.ReleaseDelay = time.Duration(float64(profile.ReleaseDelay) / scale)
		profile.Cooldown = time.Duration(float64(profile.Cooldown) / scale)
		return profile
	}
	scale := float64(max(float32(0), 1-cooldownReduction))
	profile.Cooldown = time.Duration(float64(profile.Cooldown) * scale)
	return profile
}

func (s *Session) SameSpeciesAllyCount(
	sourceObjectID uint32, maximumRange float32, maximumCount uint32,
) uint32 {
	if s == nil || sourceObjectID == 0 || maximumRange <= 0 || maximumCount == 0 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sameSpeciesAllyCountLocked(sourceObjectID, maximumRange, maximumCount)
}

// The caller holds the Session lock while refreshing every actor's affixes.
func (s *Session) sameSpeciesAllyCountLocked(
	sourceObjectID uint32, maximumRange float32, maximumCount uint32,
) uint32 {
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished {
		return 0
	}
	sourceSpecies := nounSpecies(source.Plan.NounName)
	helperCount := uint32(0)
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		if objectID == sourceObjectID || candidate.IsDefeated ||
			!candidate.IsPublished || candidate.Faction != source.Faction ||
			nounSpecies(candidate.Plan.NounName) != sourceSpecies ||
			zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position) >
				maximumRange {
			continue
		}
		helperCount++
		if helperCount == maximumCount {
			return helperCount
		}
	}
	return helperCount
}

// EffectiveAttackSpeedBonus retains additive modifier ownership. Descriptor-based
// timing in ApplyAffixTiming remains authoritative for basic/nonbasic abilities.
func EffectiveAttackSpeedBonus(plan SpawnPlan) float32 {
	bonus := plan.AffixAttackSpeedBonus
	if plan.InvincitronRage.IsActive(time.Now()) {
		bonus += plan.InvincitronRage.AttackBonus
	}
	return bonus
}
