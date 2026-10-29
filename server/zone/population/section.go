package population

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

// selectSectionThemes follows the recovered SectionConfig branch: each route
// section draws a different bucket, then archetypes from shared minion and
// special bags. The bucket counts are archetype counts, not occupied markers.
func selectSectionThemes(
	buckets []game.CampaignSectionBucket, roster campaignPopulationTheme,
	random *sim.SimulatorRandom, directors ...game.CampaignDirector,
) (map[sim.DirectorRouteSection]campaignPopulationTheme, error) {
	if random == nil {
		return nil, errors.New("sectionRandom: nil")
	}
	if len(buckets) != 4 {
		return nil, fmt.Errorf("sectionBuckets: got %d, want 4", len(buckets))
	}
	remainingBuckets := append([]game.CampaignSectionBucket(nil), buckets...)
	minionBag := sectionRosterBag{nouns: roster.minionNouns}
	specialBag := sectionRosterBag{nouns: roster.lieutenantNouns}
	if len(directors) > 0 && directors[0].EnemyPopulationRules.Resolved().RosterSelection == "local_and_external" {
		director := directors[0]
		minionBag.entries = PoolEntries(director, "minion")
		specialBag.entries = PoolEntries(director, "special")
		minionBag.isSourceWeighted = true
		specialBag.isSourceWeighted = true
		minionBag.externalWeightPercent = director.EnemyPopulationRules.ExternalRosterWeightPercent
		specialBag.externalWeightPercent = minionBag.externalWeightPercent
	}
	themes := make(map[sim.DirectorRouteSection]campaignPopulationTheme, 3)
	for _, section := range []sim.DirectorRouteSection{
		sim.DirectorRouteSectionA, sim.DirectorRouteSectionB, sim.DirectorRouteSectionC,
	} {
		bucketIndex, err := random.Index(uint32(len(remainingBuckets)))
		if err != nil {
			return nil, fmt.Errorf("sectionBucketRoll[%d]: %w", section, err)
		}
		bucket := remainingBuckets[bucketIndex]
		remainingBuckets = append(remainingBuckets[:bucketIndex], remainingBuckets[bucketIndex+1:]...)
		minionNouns, err := minionBag.draw(bucket.MinionCount, random)
		if err != nil {
			return nil, fmt.Errorf("sectionMinions[%d]: %w", section, err)
		}
		specialNouns, err := specialBag.draw(bucket.SpecialCount, random)
		if err != nil {
			return nil, fmt.Errorf("sectionSpecials[%d]: %w", section, err)
		}
		themes[section] = campaignPopulationTheme{
			minionNouns: minionNouns, lieutenantNouns: specialNouns,
		}
	}
	return themes, nil
}

type sectionRosterBag struct {
	entries               []game.CampaignDirectorEntry
	isSourceWeighted      bool
	externalWeightPercent uint32
	nouns                 []string
	remainingIndexes      []int
}

func (e *sectionRosterBag) draw(count int, random *sim.SimulatorRandom) ([]string, error) {
	if e == nil || random == nil {
		return nil, errors.New("section bag unavailable")
	}
	if count < 0 || (count > 0 && len(e.nouns) == 0) {
		return nil, fmt.Errorf("archetypeCount: got %d, available %d", count, len(e.nouns))
	}
	selectedNouns := make([]string, 0, count)
	for len(selectedNouns) < count {
		if len(e.remainingIndexes) == 0 {
			for index := range e.nouns {
				e.remainingIndexes = append(e.remainingIndexes, index)
			}
		}
		bagEntries := make([]game.CampaignDirectorEntry, 0, len(e.remainingIndexes))
		for _, index := range e.remainingIndexes {
			entry := game.CampaignDirectorEntry{NounName: e.nouns[index]}
			for _, candidate := range e.entries {
				if candidate.NounName == entry.NounName {
					entry = candidate
					break
				}
			}
			bagEntries = append(bagEntries, entry)
		}
		bagIndex, err := game.RosterIndex(bagEntries, random, e.isSourceWeighted, e.externalWeightPercent)
		if err != nil {
			return nil, fmt.Errorf("archetypeRoll: %w", err)
		}
		selectedNouns = append(selectedNouns, e.nouns[e.remainingIndexes[bagIndex]])
		e.remainingIndexes = append(e.remainingIndexes[:bagIndex], e.remainingIndexes[bagIndex+1:]...)
	}
	return selectedNouns, nil
}
