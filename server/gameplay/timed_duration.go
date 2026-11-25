package gameplay

import (
	"errors"
	"fmt"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Chilled810 and SlowNova92 inherit SpeedTemplate453's native duration and
// IsDebuff descriptor; CryosBossShock722 owns the same general descriptor.
// The separate IsSlow debuff descriptor is not a general modifier descriptor
// and must not be substituted into duration projection.
func projectCampaignTimedModifierDuration(
	profile zonenpc.ActionProfile, member gameplayPeerSession, targetObjectID uint32,
) (time.Duration, error) {
	switch profile.ModifierName {
	case "Chilled", "SlowNovaModifier", "CryosBossShock":
	default:
		return profile.ModifierDuration, nil
	}
	if targetObjectID != member.deployedObjectID {
		return profile.ModifierDuration, nil
	}
	if member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
		return 0, errors.New("timed modifier recipient unavailable")
	}
	duration, err := projectRecipientModifierDuration(profile.ModifierDuration, 1<<5,
		member.binding.Creatures[member.deployedCreatureIndex])
	if err != nil {
		return 0, fmt.Errorf("timedDuration: %w", err)
	}
	return duration, nil
}
