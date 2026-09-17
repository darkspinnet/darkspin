package gameplay

import (
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func scaleNocturnaSpecialHomerCooldown(
	profile zonenpc.ActionProfile, playerCount uint32,
) zonenpc.ActionProfile {
	scale, isFound := zonenpc.NocturnaSpecialHomerCooldownScale(playerCount)
	if !isFound || profile.Cooldown <= 0 {
		return profile
	}
	profile.Cooldown = time.Duration(float64(profile.Cooldown) * scale)
	return profile
}
