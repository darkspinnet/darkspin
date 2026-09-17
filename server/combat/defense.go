package combat

import (
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/sim"
)

// DefenseProfile separates avoidance ratings from fractional and flat defenses.
// The server policy for missing retail formulas is recorded in notes/combat/damage-mitigation.md.
type DefenseProfile struct {
	DodgeRating        float32
	ResistRating       float32
	DamageReduction    float32
	PhysicalReduction  float32
	EnergyReduction    float32
	ScienceResistances [5]float32
	AreaResistance     float32
	PhysicalArmor      float32
	EnergyArmor        float32
}

type DefenseRequest struct {
	Damage        float32
	DamageSource  uint8
	DamageType    uint8
	IsSourceKnown bool
	IsTypeKnown   bool
	IsArea        bool
	IsPeriodic    bool
}

// RollAvoidance uses the authored difficulty rating conversion as an explicit
// server fallback. Periodic damage already attached to a target is not rerolled.
func RollAvoidance(random *sim.SimulatorRandom, profile DefenseProfile,
	req DefenseRequest, difficulty uint32, conversions []float32,
) (bool, error) {
	if req.Damage <= 0 || req.IsPeriodic || !req.IsSourceKnown {
		return false, nil
	}
	rating := float32(0)
	switch req.DamageSource {
	case 0:
		rating = profile.DodgeRating
	case 1:
		rating = profile.ResistRating
	default:
		return false, nil
	}
	if rating <= 0 {
		return false, nil
	}
	// Avoid guaranteed immunity from rating alone; explicit immunity remains separate.
	if difficulty == 0 || difficulty > uint32(len(conversions)) {
		return false, fmt.Errorf("defenseDifficulty: %d", difficulty)
	}
	conversion := conversions[difficulty-1]
	if math.IsNaN(float64(conversion)) || math.IsInf(float64(conversion), 0) || conversion <= 0 {
		return false, fmt.Errorf("defenseConversion: %g", conversion)
	}
	rating = min(rating, conversion*100*0.75)
	isAvoided, err := sim.RollCritical(random, rating, 0, difficulty, conversions)
	if err != nil {
		return false, fmt.Errorf("defenseRoll: %w", err)
	}
	return isAvoided, nil
}

// ReduceIncomingDamage applies independent fractional defenses multiplicatively,
// then flat armor. It never imposes minimum damage on a fully protected target.
func ReduceIncomingDamage(damage float32, profile DefenseProfile, req DefenseRequest) float32 {
	damage *= defenseRemaining(profile.DamageReduction)
	armor := float32(0)
	if req.IsSourceKnown {
		switch req.DamageSource {
		case 0:
			damage *= defenseRemaining(profile.PhysicalReduction)
			armor = profile.PhysicalArmor
		case 1:
			damage *= defenseRemaining(profile.EnergyReduction)
			armor = profile.EnergyArmor
		}
	}
	if req.IsTypeKnown && req.DamageType < uint8(len(profile.ScienceResistances)) {
		damage *= defenseRemaining(profile.ScienceResistances[req.DamageType])
	}
	if req.IsArea {
		damage *= defenseRemaining(profile.AreaResistance)
	}
	if math.IsNaN(float64(armor)) || math.IsInf(float64(armor), 0) {
		armor = 0
	}
	return max(float32(0), damage-max(float32(0), armor))
}

func defenseRemaining(reduction float32) float32 {
	if math.IsNaN(float64(reduction)) || math.IsInf(float64(reduction), 0) {
		return 1
	}
	return 1 - min(max(reduction, float32(0)), float32(1))
}
