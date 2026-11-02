//go:build scenario

package gameplay

import (
	"errors"
	"fmt"
	"strings"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The caller has validated request ownership. Reuse normal tutorial actions
// for these exact imported actors without activating tutorial progression.
func attachScenarioTutorialAction(requestID string, plan zonenpc.SpawnPlan, programs Programs) (zonenpc.SpawnPlan, error) {
	if requestID == "" {
		return plan, nil
	}
	switch strings.ToLower(plan.NounName) {
	case "tutorialbasicpoison.noun", "tutorialbasicpoisonnoorbs.noun",
		"tutorialbasicdiseased.noun", "tutorialbasicranged.noun", "tutorialsloth.noun",
		"tutorialspecialone.noun", "tutorialspecialone_intro.noun":
	default:
		return plan, nil
	}
	graph := plan.NPCProfile.AIGraph
	if !plan.NPCProfile.IsKnown || graph == nil || !graph.IsResolved {
		return plan, errors.New("tutorial scenario requires an exact resolved imported noun/class/AI")
	}
	plans := []zonenpc.SpawnPlan{plan.Clone()}
	err := attachTutorialActorActionProfiles(plans, programs)
	if err != nil {
		return plan, fmt.Errorf("tutorialScenarioAction: %w", err)
	}
	return plans[0], nil
}
