package npc

// ActionNounName resolves shared build-103 AI to an existing action profile
// without changing the actor's noun, class, attributes or graph. These three
// definitions are also referenced by authored showcase/challenge creatures.
func ActionNounName(plan SpawnPlan) string {
	graph := plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved {
		return plan.NounName
	}
	name := ""
	switch graph.DefinitionID {
	case 3805523970: // nct_minn_su_drainer.AIDefinition
		name = "nct_minn_su_drainer"
	case 1661800818: // nct_lieu_su_stealther.AIDefinition
		name = "nct_lieu_su_stealther"
	case 89335581: // CryosBasicPoison.AIDefinition
		name = "CryosBasicPoison"
	default:
		return plan.NounName
	}
	profile, isKnown := ActionProfileForNoun(plan.NounName)
	if isKnown && profile.AbilityName != "" {
		return plan.NounName
	}
	switch plan.NPCProfile.NPCRank {
	case 1:
		return name + ".Noun"
	case 2:
		return name + "_2.Noun"
	case 3:
		return name + "_3.Noun"
	default:
		return plan.NounName
	}
}

// RecoveredActionProfileForPlan excludes the generic compatibility fallback.
// Discovery and controlled admission must use the same shared-AI resolution as
// normal gameplay before describing an authored action as recovered.
func RecoveredActionProfileForPlan(plan SpawnPlan) (ActionProfile, bool) {
	profile, isSpoffitVictim := spoffitVictimProfile(plan)
	if isSpoffitVictim {
		return profile, true
	}
	return ActionProfileForNoun(ActionNounName(plan))
}
