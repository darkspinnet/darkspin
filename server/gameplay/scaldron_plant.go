package gameplay

import (
	"strings"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type scaldronPlantDeath struct {
	effectName        string
	isRemnantRetained bool
}

func scaldronPustuleProfile(nounName string) (zonenpc.ActionProfile, bool) {
	presentation := scaldronPlantDeathPresentation(nounName)
	if !presentation.isRemnantRetained {
		return zonenpc.ActionProfile{}, false
	}
	// ScalDiseasePlant and LifePlagueSpread author no initial damage.
	return zonenpc.ActionProfile{
		Radius: 6, ModifierName: "LifePlagueSpread",
		ModifierDuration: 6 * time.Second, ModifierTickDuration: time.Second,
		ModifierMinimumTickDamage: 3, ModifierMaximumTickDamage: 3,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDamageType:            2, ModifierDamageSource: 1,
		ModifierDescriptorMask:       1<<2 | 1<<5,
		IsModifierDamageProfileKnown: true, IsDamageProfileKnown: true,
	}, true
}

// Packaged AI definitions bind the small plant to ScaldronPlantDeath
// (Abilities/0xB593D6DD.lua), which notifies a positioned burst and deletes
// the plant immediately. Both large variants bind ScalDiseasePlant
// (Abilities/0x6FABF146.lua), which emits its own burst and retains state dead.
func scaldronPlantDeathPresentation(nounName string) scaldronPlantDeath {
	switch strings.ToLower(nounName) {
	case "dest_prefab_scaldron_plant_small_3.noun":
		return scaldronPlantDeath{effectName: "scaldron_sink_plant_explosion.ServerEventDef"}
	case "dest_prefab_scaldron_plant_large_1.noun",
		"dest_prefab_scaldron_plant_large_1b.noun":
		return scaldronPlantDeath{
			effectName:        "scaldron_suction_pod_plant_explosion_effect.ServerEventDef",
			isRemnantRetained: true,
		}
	default:
		return scaldronPlantDeath{}
	}
}
