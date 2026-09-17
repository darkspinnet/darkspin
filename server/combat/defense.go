package combat

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/sim"
)

// These are server balance limits, not recovered retail formulas. Ordinary
// player defenses cannot provide guaranteed avoidance or complete mitigation.
const (
	MaximumAvoidanceChance = float32(0.75)
	MaximumDamageReduction = float32(0.90)
)

// DefenseProfile separates avoidance ratings from fractional and flat defenses.
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
	// Damage is the incoming hit before passive, aura, and equipment reductions.
	// It keeps the combined mitigation limit independent of the number of stages.
	Damage        float32
	DamageSource  uint8
	DamageType    uint8
	IsSourceKnown bool
	IsTypeKnown   bool
	IsArea        bool
	IsPeriodic    bool
}

// RollAvoidance uses a diminishing-returns server curve, C*r/(r+C*k), where C
// is the avoidance ceiling and k is the authored difficulty conversion * 100.
// It preserves the old low-rating slope without allowing guaranteed avoidance.
// Periodic damage already attached to a target is not rerolled.
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
	if rating <= 0 || math.IsNaN(float64(rating)) || math.IsInf(float64(rating), 0) {
		return false, nil
	}
	if difficulty == 0 || difficulty > uint32(len(conversions)) {
		return false, fmt.Errorf("defenseDifficulty: %d", difficulty)
	}
	conversion := conversions[difficulty-1]
	if math.IsNaN(float64(conversion)) || math.IsInf(float64(conversion), 0) || conversion <= 0 {
		return false, fmt.Errorf("defenseConversion: %g", conversion)
	}
	if random == nil {
		return false, errors.New("nil defense random")
	}
	ceiling := float64(MaximumAvoidanceChance)
	chance := ceiling * float64(rating) /
		(float64(rating) + ceiling*float64(conversion)*100)
	return random.Float64() < min(chance, ceiling), nil
}

// ReduceIncomingDamage applies independent fractional defenses multiplicatively,
// then flat armor. The combined cap also includes reductions applied before this
// call, using req.Damage as the original hit. A landed positive hit retains at
// least 10% damage and one point (without increasing hits smaller than one).
// Explicit temporary immunity, finite shields, and damage sharing are separate.
func ReduceIncomingDamage(damage float32, profile DefenseProfile, req DefenseRequest) float32 {
	incomingDamage := max(damage, req.Damage)
	if incomingDamage <= 0 {
		return 0
	}
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
	minimumDamage := min(incomingDamage,
		max(float32(1), incomingDamage*(1-MaximumDamageReduction)))
	return max(minimumDamage, damage-max(float32(0), armor))
}

// ApplyDamageReduction caps a fractional reduction before the combined limit
// in ReduceIncomingDamage accounts for the rest of the player's defenses.
func ApplyDamageReduction(damage, reduction float32) float32 {
	return damage * defenseRemaining(reduction)
}

func defenseRemaining(reduction float32) float32 {
	if math.IsNaN(float64(reduction)) || math.IsInf(float64(reduction), 0) {
		return 1
	}
	return 1 - min(max(reduction, float32(0)), MaximumDamageReduction)
}
