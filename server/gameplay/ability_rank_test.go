package gameplay

import (
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

func TestRankedProjectileControlPreservesAuthoredCostAndDuration(t *testing.T) {
	cooldowns := []time.Duration{time.Second, 6 * time.Second, time.Second, 6 * time.Second,
		6 * time.Second, 12 * time.Second, time.Second, 6 * time.Second}
	for index, cooldown := range cooldowns {
		rank := int32(index + 1)
		definition := sim.AbilityDefinition{Name: "MissileTempestSupport", Kind: sim.AbilityKindCursorArea,
			Cooldown: time.Second, ManaCost: 8, ManaCoefficient: .04,
			StatusKind: sim.AbilityStatusKindSlow, StatusDuration: 6 * time.Second, StatusMovementScale: .5}
		projected := projectHeroAbilityRank(definition, rank)
		duration := 6 * time.Second
		if rank%2 == 0 {
			duration = 4 * time.Second
		}
		if projected.Cooldown != cooldown || projected.StatusDuration != duration ||
			projected.ManaCost != 8 || projected.ManaCoefficient != .04 || projected.StatusMovementScale != .5 {
			t.Fatalf("rank=%d changed authored pulse cost, cooldown or slow: %+v", rank, projected)
		}
	}
}

// Build-103 Dazing Shot increases splash base damage at ranks 7/8, while
// retaining its authored splash coefficient and the same cost at every rank.
func TestRankedAreaPreservesCostAndSplashScaling(t *testing.T) {
	for _, rank := range []int32{1, 2, 3, 4, 5, 6, 7, 8} {
		definition := sim.AbilityDefinition{
			Name: "TCShieldedSentinelSupport", Kind: sim.AbilityKindCursorArea,
			MinimumDamage: 20, MaximumDamage: 30, DamageCoefficient: .05,
			SecondaryMinimumDamage: 10, SecondaryMaximumDamage: 16,
			SecondaryDamageCoefficient: .025, ManaCost: 12, ManaCoefficient: .06,
			HitEffectName: "cyber_shield_concussiveblast.ServerEventDef",
			StatusKind:    sim.AbilityStatusKindSlow,
		}
		projected := projectHeroAbilityRank(definition, rank)
		if projected.ManaCost != 12 || projected.ManaCoefficient != .06 || projected.SecondaryDamageCoefficient != .025 {
			t.Fatalf("rank=%d changed authored cost or splash coefficient: %+v", rank, projected)
		}
		if rank >= 7 && (projected.SecondaryMinimumDamage != projected.MinimumDamage || projected.SecondaryMaximumDamage != projected.MaximumDamage) {
			t.Fatalf("rank=%d did not promote splash base damage", rank)
		}
		if rank < 7 && (projected.SecondaryMinimumDamage != 10 || projected.SecondaryMaximumDamage != 16) {
			t.Fatalf("rank=%d changed ordinary splash base damage", rank)
		}
		effects := []string{
			"cyber_shield_concussiveblast.ServerEventDef",
			"cyber_shield_concussiveblast.ServerEventDef",
			"cyber_shield_concussiveblast.ServerEventDef",
			"cyber_shield_concussiveblast_suppress.ServerEventDef",
			"cyber_shield_concussiveblast_suppress.ServerEventDef",
			"cyber_shield_concussiveblast_equalDamage.ServerEventDef",
			"cyber_shield_concussiveblast_equalDamage.ServerEventDef",
			"cyber_shield_concussiveblast_equalDamage.ServerEventDef",
		}
		if projected.HitEffectName != effects[rank-1] {
			t.Fatalf("rank=%d selected effect %q", rank, projected.HitEffectName)
		}
		isShared := isSharedArenaArea(projected, 6)
		if !isShared {
			t.Fatalf("rank=%d shared admission rejected a supported area status", rank)
		}
	}
}
