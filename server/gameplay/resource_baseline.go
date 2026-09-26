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
	return raknet.HeroResourceStateMessages(
		objectID, hitPoint, powerPoint, maximumHitPoint, maximumPowerPoint,
	)
}
