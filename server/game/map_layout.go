package game

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/darkspinnet/darkspin/server/util"
)

// SelectMapLayout reproduces build 103's sub_9D6340. The returned ordinals
// follow native load order: unconditional sets, then one set per group in
// unsigned group-hash order. Empty alternatives still consume a random draw.
func (e CampaignDirector) SelectMapLayout(seed, conditionMask uint32) ([]int, error) {
	if seed == 0 || seed == ^uint32(0) {
		return nil, errors.New("map layout requires an explicit seed")
	}
	sets := slices.Clone(e.MarkerSets)
	slices.SortFunc(sets, func(left, right CampaignDirectorMarkerSet) int {
		return cmp.Compare(left.Ordinal, right.Ordinal)
	})
	groups := make(map[uint32][]CampaignDirectorMarkerSet)
	ordinals := make([]int, 0, len(sets))
	for index, set := range sets {
		if index > 0 && sets[index-1].Ordinal == set.Ordinal {
			return nil, fmt.Errorf("map set ordinal %d duplicated", set.Ordinal)
		}
		if set.Name == "" || set.GroupName == "" {
			return nil, fmt.Errorf("map set %d metadata unavailable", set.Ordinal)
		}
		isEligible := true
		for _, condition := range set.Conditions {
			if condition >= 32 {
				return nil, fmt.Errorf("map set %d condition %d unsupported", set.Ordinal, condition)
			}
			if conditionMask&(uint32(1)<<condition) == 0 {
				isEligible = false
			}
		}
		if !isEligible {
			continue
		}
		groupID := util.HashID(set.GroupName)
		if groupID == util.HashID("none") {
			ordinals = append(ordinals, set.Ordinal)
			continue
		}
		groups[groupID] = append(groups[groupID], set)
	}
	groupIDs := make([]uint32, 0, len(groups))
	for groupID := range groups {
		groupIDs = append(groupIDs, groupID)
	}
	slices.Sort(groupIDs)
	state := seed
	for _, groupID := range groupIDs {
		alternatives := groups[groupID]
		// The native sort uses stable insertion for up to 28 entries. Every
		// inventoried campaign group has at most four. Larger groups require
		// the native introsort partition/tie behavior, not Go's stable sort.
		if len(alternatives) > 28 {
			return nil, fmt.Errorf("map group %#x has unsupported size %d", groupID, len(alternatives))
		}
		total := float32(0)
		for _, set := range alternatives {
			total += float32(set.Weight)
		}
		if total == 0 {
			return nil, fmt.Errorf("map group %#x has zero weight", groupID)
		}
		reciprocal := float32(1 / float64(total))
		slices.SortStableFunc(alternatives, func(left, right CampaignDirectorMarkerSet) int {
			return cmp.Compare(float32(left.Weight)*reciprocal, float32(right.Weight)*reciprocal)
		})
		// sub_AE52A0 uses a wrapping multiply, interprets the result as signed,
		// and multiplies by the exact float constant 2^-32 (VA 0x103EB94).
		state *= 663608941
		draw := float64(int32(state))*(1.0/4294967296.0) + 0.5
		cumulative := float32(0)
		for _, set := range alternatives {
			weight := float32(set.Weight) * reciprocal
			// x87 compares the sum before storing its rounded float for the
			// next iteration. Do not compare only after rounding to float32.
			upper := float64(cumulative) + float64(weight)
			cumulative = float32(upper)
			if upper >= draw {
				ordinals = append(ordinals, set.Ordinal)
				break
			}
		}
	}
	return ordinals, nil
}

// InitialMapLayout applies the native choice to every placement consumer on
// any map, including script-only sets, horde triggers and empty alternatives.
func (e CampaignDirector) InitialMapLayout(seed uint32, ordinals []int) (CampaignDirector, error) {
	if seed == 0 || seed == ^uint32(0) {
		return CampaignDirector{}, errors.New("initial layout seed unavailable")
	}
	selectedOrdinals := make(map[int]struct{}, len(ordinals))
	setsByOrdinal := make(map[int]CampaignDirectorMarkerSet, len(e.MarkerSets))
	for _, set := range e.MarkerSets {
		_, isDuplicate := setsByOrdinal[set.Ordinal]
		if isDuplicate {
			return CampaignDirector{}, fmt.Errorf("initial layout source set %d duplicated", set.Ordinal)
		}
		setsByOrdinal[set.Ordinal] = set
	}
	scriptsBySetOrdinal := make(map[int][]CampaignScriptBinding)
	for _, script := range e.Scripts {
		scriptsBySetOrdinal[script.MarkerSetOrdinal] = append(
			scriptsBySetOrdinal[script.MarkerSetOrdinal], script,
		)
	}
	selectedDefinitionsByMarkerID := make(map[uint32]CampaignMarkerDefinition)
	sets := make([]CampaignDirectorMarkerSet, 0, len(ordinals))
	scripts := make([]CampaignScriptBinding, 0, len(e.Scripts))
	for _, ordinal := range ordinals {
		set, isFound := setsByOrdinal[ordinal]
		if !isFound {
			return CampaignDirector{}, fmt.Errorf("initial layout set %d unavailable", ordinal)
		}
		_, isDuplicate := selectedOrdinals[ordinal]
		if isDuplicate {
			return CampaignDirector{}, fmt.Errorf("initial layout set %d duplicated", ordinal)
		}
		// Native 9DAC70 inserts every definition before any role/object filter.
		// Set acceptance order, rather than ordinal sorting, decides duplicates.
		for _, definition := range set.Definitions {
			selectedDefinitionsByMarkerID[definition.MarkerID] = definition
		}
		sets = append(sets, set)
		scripts = append(scripts, scriptsBySetOrdinal[ordinal]...)
		selectedOrdinals[ordinal] = struct{}{}
	}
	e.selectedDefinitionsByMarkerID = selectedDefinitionsByMarkerID
	e.MarkerSets = sets
	e.Scripts = scripts
	e.IsInitialLayoutSelected = true
	e.MapVariantSeed = seed
	return e, nil
}

// MapDestructibles transfers only selected client fixtures to authoritative
// NPC ownership. Unselected alternatives never produce deletion or spawn IDs.
func (e CampaignDirector) MapDestructibles() (
	[]CampaignDirectorMarker, []uint32, error,
) {
	if !e.IsInitialLayoutSelected {
		return nil, nil, errors.New("map destructible layout unavailable")
	}
	markers := make([]CampaignDirectorMarker, 0)
	deletedObjectIDs := make([]uint32, 0)
	markerIDs := make(map[uint32]struct{})
	for _, set := range e.MarkerSets {
		for _, marker := range set.Markers {
			if !isMapDestructible(marker) {
				continue
			}
			if marker.MarkerID == 0 || !isFiniteCampaignPosition(marker.Position) ||
				!isFiniteCampaignPosition(marker.Rotation) || marker.Scale <= 0 ||
				!marker.NPCProfile.IsKnown || !marker.NPCProfile.IsTargetable || marker.NPCProfile.HitPoint <= 0 {
				return nil, nil, fmt.Errorf("selected fixture %d invalid", marker.MarkerID)
			}
			_, isDuplicate := markerIDs[marker.MarkerID]
			if isDuplicate {
				return nil, nil, fmt.Errorf("selected fixture %d duplicated", marker.MarkerID)
			}
			markerIDs[marker.MarkerID] = struct{}{}
			markers = append(markers, marker)
			// Retire only this selected client's static copy before publishing
			// the server-owned damageable object at its authored transform.
			deletedObjectIDs = append(deletedObjectIDs, marker.MarkerID)
		}
	}
	return markers, deletedObjectIDs, nil
}

func isMapDestructible(marker CampaignDirectorMarker) bool {
	return IsGraviticRegulatorNoun(marker.NounName) || IsGraviticStabilizerNoun(marker.NounName) ||
		isVerdanthCombatFixture(marker) || isNocturnaCombatFixture(marker) ||
		isCryosOneFixtureNoun(marker.NounName) || isCryosTwoFixtureNoun(marker.NounName) ||
		isCryosFourFixtureNoun(marker.NounName) || isCryosCaveFixtureNoun(marker.NounName) ||
		isInfinityFixtureNoun(marker.NounName) || isInfinityThreeFixtureNoun(marker.NounName) ||
		isInfinityOneFixtureNoun(marker.NounName) || isInfinityFourFixtureNoun(marker.NounName) ||
		isScaldronFixtureNoun(marker.NounName)
}
