package gameplay

import (
	"strings"
	"time"
)

// ExplodingThornDeath (Abilities/0x646E0E8D.lua) notifies this event at the
// plant's position, waits chainingDelay, then marks the plant for deletion.
const toxicactusDeathEffect = "effect_environment_verdanth_poisonPineapple_aoe.ServerEventDef"
const toxicactusDeleteDelay = 200 * time.Millisecond

func isToxicactus(nounName string) bool {
	return strings.EqualFold(nounName, "DEST_prefab_tota_heroplant_p3_b.Noun") ||
		strings.EqualFold(nounName, "DEST_tota_heroplant_p3_b.Noun")
}
