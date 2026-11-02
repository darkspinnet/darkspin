package game

import "strings"

// IsDestructibleDoor binds the installed door's death callback independently
// of its missing phase. It does not recover an ordinary combat action.
func IsDestructibleDoor(nounName string, profile CampaignNPCProfile) bool {
	graph := profile.AIGraph
	return strings.EqualFold(nounName, "design_blockin_door_destructible.Noun") &&
		profile.IsKnown && profile.IsClassKnown && profile.NPCClassInstanceID == 2347653672 &&
		graph != nil && graph.IsResolved && graph.DefinitionID == 2347653672 &&
		graph.DeathAbility != nil && *graph.DeathAbility == "Destructible"
}
