package npc

import "strings"

// WithPendingIntroduction gives direct combat spawns the same initial hidden
// state as their entrance. Unlike dormant actors these skip pre-aggro idle;
// their existing first-action path requests the primary entrance immediately.
func WithPendingIntroduction(plan SpawnPlan) SpawnPlan {
	if plan.IsIntroductionComplete || plan.Introduction == SpawnIntroductionFloorWarp {
		return plan
	}
	profile, isKnown := ActionProfileForPlan(plan)
	// Owned summons may explicitly suppress their entrance (for example,
	// Nashira's duplicates). Preserve that request rather than hiding a clone.
	isEntranceRequested := plan.OwnerObjectID == 0 ||
		(isKnown && profile.FirstAggroAnimationName != "")
	graph := plan.NPCProfile.AIGraph
	if !plan.IsStartupInitialized && isEntranceRequested && !plan.IsFixture &&
		graph != nil && graph.IsResolved && strings.EqualFold(graph.PreAggroIdle, "nBehavior_Invisible") {
		plan.IsIntroductionHidden = true
	}
	if isKnown && profile.FirstAggroRevealDelay > 0 {
		plan.IsIntroductionHidden = true
	}
	return plan
}

// RevealIntroduction commits an entrance reveal only for its live action.
func (e *Session) RevealIntroduction(objectID uint32, owner ActionOwner, generation uint64) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsActionStarted ||
		npc.ActionOwner != owner || npc.ActionGeneration != generation {
		return false
	}
	npc.Plan.IsIntroductionHidden = false
	npc.Plan.IsIntroductionComplete = true
	npc.IsInvisibleToSecurityTeleporter = false
	e.npcs[objectID] = npc
	return true
}

// Keep a boss's entrance independent of the distance-selected opening attack.
func WithBossIntroduction(plan SpawnPlan, profile ActionProfile) ActionProfile {
	if plan.OwnerObjectID != 0 {
		return profile
	}
	name := strings.TrimSuffix(strings.ToLower(plan.NounName), ".noun")
	name = strings.TrimSuffix(strings.TrimSuffix(name, "_2"), "_3")
	switch name {
	case "shadowboss", "zelemboss", "verdanthboss", "cryosboss", "citadelboss", "scaldronboss":
	default:
		return profile
	}
	intro, isFound := ActionProfileForNoun(plan.NounName)
	if !isFound {
		intro, isFound = OrcusFallbackProfile(plan.NounName)
	}
	if !isFound {
		return profile
	}
	profile.FirstAggroAnimationName = intro.FirstAggroAnimationName
	profile.FirstAggroAbilityName = intro.FirstAggroAbilityName
	profile.FirstAggroEffectName = intro.FirstAggroEffectName
	profile.FirstAggroEffectDelay = intro.FirstAggroEffectDelay
	profile.FirstAggroDelay = intro.FirstAggroDelay
	profile.FirstAggroRevealDelay = intro.FirstAggroRevealDelay
	profile.FirstAggroCinematicDuration = intro.FirstAggroCinematicDuration
	profile.FirstAggroCinematicRadius = intro.FirstAggroCinematicRadius
	profile.IsFirstAggroDurationKnown = intro.IsFirstAggroDurationKnown
	return profile
}

// IntroductionProfile retains ordinary aggro reactions but omits arrival
// animations for actors that are already standing in the map.
func IntroductionProfile(plan SpawnPlan, profile ActionProfile) ActionProfile {
	profile = WithBossIntroduction(plan, profile)
	if plan.IsStartupInitialized && plan.IsSecondaryStart &&
		plan.Introduction != SpawnIntroductionFloorWarp {
		graph := plan.NPCProfile.AIGraph
		if graph != nil && strings.EqualFold(graph.PreAggroIdle, "nBehavior_Invisible") &&
			(graph.FirstAggroAbility2 == nil ||
				strings.EqualFold(*graph.FirstAggroAbility2, "FirstAggro_FaceTarget")) {
			profile.FirstAggroAnimationName = ""
			profile.FirstAggroAbilityName = ""
			profile.PreAggroAnimationName = ""
			profile.IsFirstAggroFacingSuppressed = graph.FirstAggroAbility2 == nil
			if graph.FirstAggroAbility2 != nil {
				profile.FirstAggroAbilityName = *graph.FirstAggroAbility2
			}
			profile.FirstAggroEffectName = ""
			profile.FirstAggroEffectDelay = 0
			profile.FirstAggroDelay = 0
			profile.FirstAggroRevealDelay = 0
			profile.FirstAggroCinematicDuration = 0
			profile.FirstAggroCinematicRadius = 0
			profile.IsFirstAggroDurationKnown = true
			return profile
		}
	}
	if plan.IsIntroductionHidden {
		profile = authoredInvisibleEntrance(plan, profile)
		// Deactivate of nBehavior_Invisible reveals even when an entrance has
		// no recovered animation timing. Never leave a fallback attacker hidden.
		profile.IsFirstAggroDurationKnown = true
		return profile
	}
	if plan.Introduction != SpawnIntroductionDormant || plan.OwnerObjectID != 0 || plan.IsBoss {
		return profile
	}
	switch profile.FirstAggroAnimationName {
	case "gen_aggro_sp_beam_in", "zelem_flying_melee_beamin":
		profile.FirstAggroAnimationName = ""
		profile.FirstAggroDelay = 0
	}
	return profile
}
