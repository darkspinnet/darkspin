package npc

import (
	"strings"
	"time"
)

type DeathPresentation struct {
	AnimationName        string
	PresentationDuration time.Duration
}

// DeathPresentationForNoun supplies authored presentation for campaign NPC rigs
// whose physics records are not yet part of the curated noun catalog.
func DeathPresentationForNoun(nounName string) (DeathPresentation, bool) {
	switch strings.ToLower(nounName) {
	case "shadowboss.noun", "shadowboss_2.noun", "shadowboss_3.noun":
		// ShadowBoss/ShadowBoss1.CharacterAnimation name the state
		// shadowboss_dead; the nct_boss_* name identifies its raw clip.
		return destructorDeathPresentation("shadowboss_dead", 227), true
	case "zelemboss.noun", "zelemboss_2.noun", "zelemboss_3.noun":
		return destructorDeathPresentation("zlm_boss_sp_death", 250), true
	case "verdanthboss.noun", "verdanthboss_2.noun", "verdanthboss_3.noun":
		return destructorDeathPresentation("ver_boss_lf_spawneater_death", 525), true
	case "cryosboss.noun", "cryosboss_2.noun", "cryosboss_3.noun":
		return destructorDeathPresentation("cry_el_boss_death", 296), true
	case "citadelboss.noun", "citadelboss_2.noun", "citadelboss_3.noun":
		return destructorDeathPresentation("ctd_boss_tc_death", 480), true
	case "scaldronboss.noun", "scaldronboss_stage2.noun",
		"scaldronboss_2.noun", "scaldronboss_3.noun":
		return destructorDeathPresentation("sca_boss_death", 662), true
	case "nocturnabasichealthdrain.noun",
		"nocturnabasichealthdrain_2.noun",
		"nocturnabasichealthdrain_3.noun":
		return DeathPresentation{AnimationName: "gen_death_melee_quad"}, true
	default:
		return DeathPresentation{}, false
	}
}

func destructorDeathPresentation(
	animationName string, frameCount uint32,
) DeathPresentation {
	return DeathPresentation{
		AnimationName:        animationName,
		PresentationDuration: time.Duration(frameCount/30+1) * time.Second,
	}
}
