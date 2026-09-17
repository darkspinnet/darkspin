package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const tutorialActorFirstAggroDelay = 1291667 * time.Microsecond

const tutorialQuadraIntroRevealDelay = 2 * time.Second
const tutorialQuadraIntroDuration = 4291667 * time.Microsecond
const tutorialQuadraRecoveryDuration = 4400 * time.Millisecond
const tutorialQuadraRecoveryEveryActionCount = uint32(3)

type tutorialActorAction struct {
	definition             sim.AbilityDefinition
	family                 zonenpc.ActionFamily
	movementSpeed          float32
	nonCombatMovementSpeed float32
}

func attachTutorialActorActionProfiles(
	plans []zonenpc.SpawnPlan, programs Programs,
) error {
	for index := range plans {
		actionNounName := plans[index].AuthoredNounName
		if actionNounName == "" {
			actionNounName = plans[index].NounName
		}
		action, isFound := tutorialActorActionForNoun(actionNounName, programs)
		if !isFound {
			return fmt.Errorf("tutorialAction[%s]: missing", plans[index].NounName)
		}
		profile, err := zonenpc.ActionProfileFromAbility(
			action.family, action.definition, action.movementSpeed,
			tutorialActorFirstAggroDelay,
		)
		if err != nil {
			return fmt.Errorf("tutorialAction[%s]: %w", plans[index].NounName, err)
		}
		profile.FirstAggroAnimationName = "character_teleport_in"
		if strings.EqualFold(actionNounName, "TutorialSpecialOne.Noun") ||
			strings.EqualFold(actionNounName, "TutorialSpecialOne_Intro.Noun") {
			profile.RecoveryAnimationName = "wander_flavor"
			profile.RecoveryDuration = tutorialQuadraRecoveryDuration
			profile.RecoveryEveryActionCount = tutorialQuadraRecoveryEveryActionCount
		}
		if strings.EqualFold(actionNounName, "TutorialSpecialOne_Intro.Noun") {
			profile.FirstAggroDelay = tutorialQuadraIntroDuration
			profile.FirstAggroRevealDelay = tutorialQuadraIntroRevealDelay
			profile.FirstAggroCinematicDuration = tutorialQuadraIntroDuration
			profile.FirstAggroCinematicRadius = 100
		}
		profile.NonCombatMovementSpeed = action.nonCombatMovementSpeed
		plans[index].ActionProfile = profile
		plans[index].IsActionKnown = true
	}
	return nil
}

func tutorialActorActionForNoun(
	nounName string, programs Programs,
) (tutorialActorAction, bool) {
	switch strings.ToLower(nounName) {
	case "tutorialbasicpoison.noun", "tutorialbasicpoisonnoorbs.noun":
		return tutorialActorAction{
			definition: programs.PoisonMelee, family: zonenpc.ActionMelee,
			movementSpeed: 4.5, nonCombatMovementSpeed: 4.5,
		}, true
	case "tutorialbasicdiseased.noun":
		return tutorialActorAction{
			definition: programs.PoisonCloud, family: zonenpc.ActionProjectile,
			movementSpeed: 5, nonCombatMovementSpeed: 3,
		}, true
	case "tutorialbasicranged.noun":
		definition := programs.PlasmaLightning
		// TutorialPlasmaLightning inherits this release from the shared
		// projectile template, while the constrained Lua projection retains
		// only its authored 0.3-second launch deadline.
		definition.ReleaseDelay = 450 * time.Millisecond
		return tutorialActorAction{
			definition: definition, family: zonenpc.ActionProjectile,
			movementSpeed: 4.5, nonCombatMovementSpeed: 3,
		}, true
	case "tutorialsloth.noun":
		return tutorialActorAction{
			definition: programs.TailZap, family: zonenpc.ActionMelee,
			movementSpeed: 3.5, nonCombatMovementSpeed: 2,
		}, true
	case "tutorialspecialone.noun", "tutorialspecialone_intro.noun":
		return tutorialActorAction{
			definition: programs.BurstShot, family: zonenpc.ActionProjectile,
			movementSpeed: 5.5, nonCombatMovementSpeed: 4,
		}, true
	default:
		return tutorialActorAction{}, false
	}
}
