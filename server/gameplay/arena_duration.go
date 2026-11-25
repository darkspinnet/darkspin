package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

// These exact build-103 modifiers own native duration fields and IsDebuff
// descriptors. Their Tick callbacks wait indefinitely; ability descriptors and
// raw Lua waits must not be substituted for this modifier metadata.
func isArenaNativeModifierDuration(definition sim.AbilityDefinition, duration time.Duration) bool {
	switch definition.Name {
	case "TCShieldedSentinelSupport":
		//740 inherits descriptor32 and indefinite Tick from SpeedTemplate453.
		return definition.StatusKind == sim.AbilityStatusKindSlow &&
			definition.RootModifierID == util.HashID("TCShieldedSentinel_Support_Snare") && duration == 5*time.Second
	case "MissileTempestSupport":
		//28 has alternating6/4s duration ranks and the same SpeedTemplate.
		return definition.StatusKind == sim.AbilityStatusKindSlow &&
			definition.RootModifierID == 0x95917802 && (duration == 6*time.Second || duration == 4*time.Second)
	case "PlasmaRandom":
		//549 authors duration{3,2}, descriptors=IsDebuff and WaitForever.
		return definition.StatusKind == sim.AbilityStatusKindStun &&
			definition.RootModifierID == util.HashID("PlasmaRandomModifier") &&
			(duration == 3*time.Second || duration == 2*time.Second)
	case "ShadowRavagerActive":
		//858's six-second native duration also uses IsDebuff despite lacking
		// a crowd-control debuff tag. Native93 still scales this duration.
		return definition.StatusKind == sim.AbilityStatusKindPhysicalVulnerability &&
			definition.RootModifierID == util.HashID("ShadowRavagerPhysicalVulnerabilityModifier") && duration == 6*time.Second
	default:
		return false
	}
}
