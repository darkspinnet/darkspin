package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

func campaignHeroResourceStateMessages(
	objectID uint32, creature game.GameplayCreature,
) []raknet.ApplicationMessage {
	hitPoint := creature.HitPoint
	maximumHitPoint := creature.MaximumHitPoint
	if maximumHitPoint <= 0 {
		maximumHitPoint = hitPoint
		if maximumHitPoint <= 0 {
			maximumHitPoint = campaignHeroResourceFallback
			hitPoint = maximumHitPoint
		}
	}
	powerPoint := creature.PowerPoint
	maximumPowerPoint := creature.MaximumPowerPoint
	if maximumPowerPoint <= 0 {
		maximumPowerPoint = powerPoint
		if maximumPowerPoint <= 0 {
			maximumPowerPoint = campaignHeroResourceFallback
			powerPoint = maximumPowerPoint
		}
	}
	attributes := make(map[uint8]float32)
	for index, amount := range creature.PartAttribute {
		if amount != 0 {
			attributes[uint8(index)] = amount
		}
	}
	// Native damage-token translation reads the live agent attribute snapshot
	// (build 103 sub_43A9D0 -> sub_9E5B10), including weapon reflection 111/112.
	// Publish the same derived core stats and weapon range used by combat,
	// rather than HeroResourceStateMessages' compatibility constants.
	attributes[0], attributes[1], attributes[2] = creature.Strength, creature.Dexterity, creature.Mind
	attributes[4], attributes[5] = maximumHitPoint, maximumPowerPoint
	attributes[7], attributes[9], attributes[10] = creature.PhysicalDefense, creature.EnergyDefense, creature.CriticalRating
	attributes[111], attributes[112] = creature.MinimumWeaponDamage, creature.MaximumWeaponDamage
	if attributes[11] == 0 {
		attributes[11] = 8.25
	}
	if attributes[12] == 0 {
		attributes[12] = 7.5
	}
	attributes[109] = creature.PartAttribute[109]
	return []raknet.ApplicationMessage{
		raknet.CombatantDataUpdateMessage{ObjectID: objectID, HitPoints: hitPoint, ManaPoints: powerPoint},
		raknet.AttributeDataUpdateMessage{ObjectID: objectID, Value: attributes},
	}
}
