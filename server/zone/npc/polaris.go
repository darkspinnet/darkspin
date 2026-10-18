package npc

import (
	"strings"
	"time"
)

// CastMarkOfZelem (Lua chunk 346) is selected only by ranks two and three.
// MarkOfZelem (chunk 971) is unique and expires after thirty seconds.
func ZelemMarkProfile(nounName string) (ActionProfile, bool) {
	var cooldown time.Duration
	var distance float32
	switch strings.ToLower(nounName) {
	case "zelemboss_2.noun":
		cooldown, distance = 5*time.Second, 40
	case "zelemboss_3.noun":
		cooldown, distance = 3*time.Second, 50
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionZelemRanged, AbilityName: "CastMarkOfZelem",
		AnimationName: "zlm_boss_sp_attack2_mark", HitDelay: time.Second,
		ReleaseDelay: 900 * time.Millisecond, Cooldown: cooldown, Range: distance,
		ModifierName: "MarkOfZelem", ModifierID: 0xbf77e6b0,
		ModifierDuration: 30 * time.Second,
		TargetEffectName: "spacetime_boss_target_reticle.ServerEventDef",
	}, true
}
