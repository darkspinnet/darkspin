package game

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/util"
)

type CatalystColor uint8

const (
	CatalystBlue CatalystColor = iota
	CatalystPrismatic
	CatalystGold
	CatalystRed
	CatalystGreen
	CatalystColorNone CatalystColor = 0xff
)

type CatalystRarity uint8

const (
	CatalystCommon CatalystRarity = iota
	CatalystRare
	CatalystEpic
	CatalystRarityNone CatalystRarity = 0xff
)

type CatalystType uint8

const (
	CatalystAoEDamage CatalystType = iota
	CatalystAttackSpeed
	CatalystBuffDuration
	CatalystCCReduction
	CatalystCooldown
	CatalystCrit
	CatalystDamage
	CatalystDamageAura
	CatalystDebuffDuration
	CatalystDebuffIncrease
	CatalystDefenseRating
	CatalystDeflectionRating
	CatalystDexterity
	CatalystDodgeRating
	CatalystHealth
	CatalystImmunePoison
	CatalystImmuneSleep
	CatalystImmuneSlow
	CatalystImmuneStun
	CatalystKnockback
	CatalystLifeLeech
	CatalystMana
	CatalystManaCostReduction
	CatalystManaLeech
	CatalystMind
	CatalystMoveSpeed
	CatalystOrbEffectiveness
	CatalystOverdriveBuildup
	CatalystPetDamage
	CatalystPetHealth
	CatalystProjectileSpeed
	CatalystRangeIncrease
	CatalystStrength
	CatalystSurefooted
	CatalystThorns
	CatalystTypeNone CatalystType = 0xff
)

type Catalyst struct {
	NounID uint32
	Type   CatalystType
	Color  CatalystColor
	Rarity CatalystRarity
}

func NewCatalyst(catalystType CatalystType, rarity CatalystRarity, isPrismatic bool) Catalyst {
	name := "crystal_"
	color := CatalystColorNone
	if isPrismatic {
		name += "wild_"
		color = CatalystPrismatic
	}
	name += catalystType.String()
	if rarity == CatalystRare {
		name += "_rare"
	}
	if rarity == CatalystEpic {
		name += "_epic"
	}
	if !isPrismatic {
		color = catalystColor(catalystType)
	}
	return Catalyst{NounID: util.HashID(name + ".noun"), Type: catalystType, Color: color, Rarity: rarity}
}

// CatalystFromNounName resolves the authored crystal noun vocabulary to the
// catalyst identity consumed by the build-103 crystal HUD.
func CatalystFromNounName(nounName string) (Catalyst, bool) {
	name := strings.ToLower(strings.TrimSpace(nounName))
	name = strings.TrimSuffix(name, ".noun")
	name, isCrystal := strings.CutPrefix(name, "crystal_")
	if !isCrystal {
		return Catalyst{}, false
	}
	isPrismatic := false
	if strings.HasPrefix(name, "wild_") {
		name = strings.TrimPrefix(name, "wild_")
		isPrismatic = true
	}
	rarity := CatalystCommon
	if strings.HasSuffix(name, "_rare") {
		name = strings.TrimSuffix(name, "_rare")
		rarity = CatalystRare
	} else if strings.HasSuffix(name, "_epic") {
		name = strings.TrimSuffix(name, "_epic")
		rarity = CatalystEpic
	}
	for catalystType := CatalystAoEDamage; catalystType < CatalystTypeNone; catalystType++ {
		if catalystType.String() == name {
			return NewCatalyst(catalystType, rarity, isPrismatic), true
		}
	}
	return Catalyst{}, false
}

func (c Catalyst) MatchingColor(other Catalyst) bool {
	if c.Color == CatalystColorNone || other.Color == CatalystColorNone {
		return false
	}
	return c.Color == CatalystPrismatic || other.Color == CatalystPrismatic || c.Color == other.Color
}

func (t CatalystType) String() string {
	names := []string{"aoedamage", "attackspeed", "buffduration", "ccreduction", "cooldown", "crit", "damage", "damageaura", "debuffduration", "debuffincrease", "defenserating", "deflectionrating", "dexterity", "dodgerating", "health", "immunepoison", "immunesleep", "immuneslow", "immunestun", "knockback", "lifeleech", "mana", "manacostreduction", "manaleech", "mind", "movespeed", "orbeffectiveness", "overdrivebuildup", "petdamage", "pethealth", "projectilespeed", "rangeincrease", "strength", "surefooted", "thorns"}
	if int(t) >= len(names) {
		return "unknown"
	}
	return names[t]
}

func catalystColor(value CatalystType) CatalystColor {
	name := value.String()
	if containsName(name, "aoedamage", "attackspeed", "crit", "damage", "damageaura", "debuffincrease", "knockback", "mana", "projectilespeed") {
		return CatalystRed
	}
	if containsName(name, "buffduration", "debuffduration", "dexterity", "mind", "petdamage", "pethealth", "strength") {
		return CatalystGold
	}
	if containsName(name, "ccreduction", "defenserating", "deflectionrating", "dodgerating", "health", "immunepoison", "immunesleep", "immuneslow", "immunestun", "manacostreduction", "surefooted", "thorns") {
		return CatalystBlue
	}
	return CatalystGreen
}

func containsName(value string, values ...string) bool {
	return strings.Contains("|"+strings.Join(values, "|")+"|", "|"+value+"|")
}
