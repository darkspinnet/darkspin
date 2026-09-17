package npc

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// These are server balance policies, not recovered retail formulas. NPC rating
// protection is continuous so fast attacks and periodic damage receive the same
// expected protection without adding random misses to hero ability sequences.
const (
	maximumPassiveReduction = float32(0.50)
	maximumPhaseReduction   = float32(0.75)
)

// ConfigureDefense selects the same authored difficulty conversion used for
// hero ratings. Session ownership covers dynamically summoned and restored NPCs.
func (e *Session) ConfigureDefense(difficulty uint32, conversions []float32) error {
	if e == nil || difficulty == 0 || difficulty > uint32(len(conversions)) {
		return fmt.Errorf("defenseDifficulty: %d", difficulty)
	}
	conversion := conversions[difficulty-1]
	if conversion <= 0 || math.IsNaN(float64(conversion)) || math.IsInf(float64(conversion), 0) {
		return fmt.Errorf("defenseConversion: %g", conversion)
	}
	e.mu.Lock()
	e.defenseConversion = conversion
	e.mu.Unlock()
	return nil
}

func (e *Session) reduceDamage(npc Snapshot, profile ActionProfile, damage float32,
	sourcePosition *game.Vec3, isPhysical, isEnergy, isArea, isPeriodic bool, now time.Time,
) float32 {
	incoming := damage
	rating := float32(0)
	if isPhysical {
		rating = npc.Plan.NPCProfile.DodgeRating + profile.PassivePhysicalDefense
	}
	if isEnergy {
		rating = npc.Plan.NPCProfile.ResistRating
		// Turtle suspends this passive; combat_turtle also removes its client
		// attribute until the stance ends.
		if !npc.IsTurtleActive {
			rating += profile.PassiveEnergyDefense
		}
	}
	if rating > 0 && !math.IsNaN(float64(rating)) && !math.IsInf(float64(rating), 0) {
		// Same diminishing-returns form as hero avoidance, with a lower ceiling.
		conversion := float64(e.defenseConversion) * 100
		reduction := float64(maximumPassiveReduction) * float64(rating) /
			(float64(rating) + float64(maximumPassiveReduction)*conversion)
		damage *= 1 - float32(reduction)
	}
	if isPhysical {
		damage *= npcDefenseRemaining(profile.PassivePhysicalDamageReduction, maximumPassiveReduction)
	}
	if isPeriodic {
		damage *= npcDefenseRemaining(profile.PassiveDamageOverTimeReduction, maximumPassiveReduction)
	}
	if isArea && nounSpecies(npc.Plan.NounName) == "zelemspecialone" {
		damage *= 1 - maximumPassiveReduction
	}
	if npc.status.carapaceDamageMaximum > 0 {
		damage = min(damage, npc.status.carapaceDamageMaximum)
	}
	// One combined ordinary-defense budget, including carapace's per-hit limit.
	damage = max(damage, incoming*(1-maximumPassiveReduction))
	if now.Before(npc.status.damageReductionEnd) {
		damage *= npcDefenseRemaining(npc.status.damageReduction, maximumPhaseReduction)
	}
	isProtected := now.Before(npc.status.chargeProtectionEnd) ||
		(npc.IsTurtleActive && isPhysical) ||
		(npc.IsShieldActive && isShieldDamageImmune(npc, sourcePosition)) ||
		(isArea && nounSpecies(npc.Plan.NounName) == "zelembasicflyingmelee" &&
			now.Before(npc.status.areaShiftExpiresAt))
	if isProtected {
		damage *= 1 - maximumPhaseReduction
	}
	// Vulnerability and finite absorption are applied by the caller afterward.
	return max(damage, incoming*(1-maximumPhaseReduction))
}

func npcDefenseRemaining(reduction, ceiling float32) float32 {
	if math.IsNaN(float64(reduction)) || math.IsInf(float64(reduction), 0) {
		return 1
	}
	return 1 - min(max(reduction, float32(0)), ceiling)
}
