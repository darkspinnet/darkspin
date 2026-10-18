package npc

// PromoteMutationAllies applies the agent's aura to nearby ordinary enemies.
// Existing elites and unrelated fixtures retain their own profiles.
func (e *Session) PromoteMutationAllies() []Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	promoted := make([]Snapshot, 0)
	for _, sourceID := range e.objectIDs {
		source := e.npcs[sourceID]
		if source.IsDefeated || source.Plan.NounName != MutationAgentNounName {
			continue
		}
		for _, objectID := range e.objectIDs {
			npc := e.npcs[objectID]
			if npc.IsDefeated || npc.Plan.IsFixture || npc.Plan.IsElite || npc.Plan.IsCaptain ||
				npc.Plan.IsBoss || npc.Plan.OwnerObjectID != 0 || npc.Faction != source.Faction ||
				npc.Plan.NounName == MutationAgentNounName ||
				npc.Plan.Position.Sub(source.Plan.Position).Length() > 15 {
				continue
			}
			npc.Plan.IsElite = true
			isFullHealth := npc.HitPoint == npc.Plan.NPCProfile.HitPoint
			npc.Plan.NPCProfile = ApplyEliteProfile(npc.Plan.NPCProfile)
			if npc.status.oozeBaseMaximumHitPoint > 0 {
				// Growth and death recovery must retain the promoted baseline.
				npc.status.oozeBaseMaximumHitPoint *=
					1 + EliteBonusForType(npc.Plan.NPCProfile.NPCType).Health
			}
			// EliteModifier heals after changing MaxHealth only when the actor
			// was full beforehand. Injured actors keep their current HP.
			if isFullHealth {
				npc.HitPoint = npc.Plan.NPCProfile.HitPoint
			}
			e.npcs[objectID] = npc
			promoted = append(promoted, npc)
		}
	}
	return promoted
}
