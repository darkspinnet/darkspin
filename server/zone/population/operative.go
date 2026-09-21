package population

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// AddOperatives selects one or two ordinary encounter groups for the whole
// map. Selection is independent of movement order and random draws, so a
// reconnect or a second player approaching cannot replenish the budget.
func (e *Session) AddOperatives(
	director game.CampaignDirector, plans []zonenpc.SpawnPlan, isCoop bool,
	spawnedCount int,
) []zonenpc.SpawnPlan {
	if e == nil || !isCoop || len(plans) == 0 {
		return plans
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	locusIDs := make([]uint32, 0, len(e.candidates))
	for _, candidate := range e.candidates {
		if candidate.locusID == 0 || len(candidate.positions) == 0 {
			continue
		}
		locusIDs = append(locusIDs, candidate.locusID)
	}
	// Map identity remains stable even when a checkpoint is resumed in a new
	// game shell with a different population random seed.
	seed := util.HashID(director.Level)
	count := min(1+int(seed%2), len(locusIDs))
	for index := 0; index < count; index++ {
		if spawnedCount >= count {
			break
		}
		// Spread the encounters through the route, away from the entry group.
		locusID := locusIDs[(index+1)*len(locusIDs)/(count+1)]
		for planIndex, plan := range plans {
			if plan.LocusID != locusID || plan.IsCaptain || plan.IsElite ||
				plan.IsBoss || plan.IsFixture || plan.AuthoredNounName != "" {
				continue
			}
			nounName := "NomadCyberOne.Noun"
			if (seed+uint32(index))%2 != 0 {
				nounName = "NomadSpacetimeAgent.Noun"
			}
			// Match the authored tier of this map's ordinary encounter.
			lower := strings.ToLower(plan.NounName)
			for _, suffix := range []string{"_2.noun", "_3.noun"} {
				if strings.HasSuffix(lower, suffix) {
					nounName = strings.TrimSuffix(nounName, ".Noun") + suffix
				}
			}
			profile, isFound := director.NPCProfilesByNoun[strings.ToLower(nounName)]
			if !isFound || !profile.IsKnown || profile.HitPoint <= 0 {
				break
			}
			// Use an existing navigable spawn point without overlapping an escort.
			plans[planIndex].NounName = nounName
			plans[planIndex].NPCProfile = profile
			plans[planIndex].ActionProfile = zonenpc.ActionProfile{}
			plans[planIndex].IsActionKnown = false
			spawnedCount++
			break
		}
	}
	return plans
}
