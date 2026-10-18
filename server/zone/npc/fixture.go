package npc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
)

// InstrumentSlowAura and ZelemSlow, recovered from the authored Lua modifiers.
const GraviticFieldRadius = float32(12.5)
const GraviticMovementSpeedBuff = float32(-0.4)
const GraviticFieldEffectName = "effect_zelem_instrument_slow_aura.ServerEventDef"

func IsWarpTotem(plan SpawnPlan) bool {
	return plan.IsFixture && strings.EqualFold(plan.NounName, "DEST_prefab_scaldron_tem_column_statue.Noun")
}

func IsGraviticStabilizer(plan SpawnPlan) bool {
	return plan.IsFixture && game.IsGraviticStabilizerNoun(plan.NounName)
}

// Both instruments retain their authored dead graphics and solid foundation.
func IsGraviticRemnant(plan SpawnPlan) bool {
	return IsGraviticRegulator(plan) || IsGraviticStabilizer(plan)
}

// CryosGeyserDeath opens the ice plug and leaves the vent alive as a hazard.
func IsCryosGeyser(plan SpawnPlan) bool {
	return plan.IsFixture && strings.EqualFold(plan.NounName, "DEST_prefab_cryos_ice_crack1.Noun")
}

// Gravitic Regulators leave a solid base after their machinery is destroyed.
func IsGraviticRegulator(plan SpawnPlan) bool {
	return plan.IsFixture && game.IsGraviticRegulatorNoun(plan.NounName)
}

// IsVerdanthTotem identifies the ancient stone destructibles promoted from
// Verdanth's authored smart-object layout.
func IsVerdanthTotem(plan SpawnPlan) bool {
	if !plan.IsFixture {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(plan.NounName)) {
	case "dest_tota_headstatue_b.noun", "dest_tota_headstatue_c.noun":
		return true
	default:
		return false
	}
}

func PlanFixtures(
	markers []game.CampaignDirectorMarker, firstObjectID uint32,
	objectIDLimit uint32,
) ([]SpawnPlan, uint32, error) {
	return planMarkers(markers, firstObjectID, objectIDLimit, true)
}

// PlanActors maps authored fixed NPC placements into ordinary combat actors.
func PlanActors(
	markers []game.CampaignDirectorMarker, firstObjectID uint32,
	objectIDLimit uint32,
) ([]SpawnPlan, uint32, error) {
	return planMarkers(markers, firstObjectID, objectIDLimit, false)
}

func planMarkers(
	markers []game.CampaignDirectorMarker, firstObjectID uint32,
	objectIDLimit uint32, isFixture bool,
) ([]SpawnPlan, uint32, error) {
	if len(markers) == 0 {
		return nil, firstObjectID, errors.New("authored markers empty")
	}
	if objectIDLimit == 0 || firstObjectID == 0 ||
		firstObjectID >= objectIDLimit {
		return nil, firstObjectID, errors.New("fixture object range invalid")
	}
	plans := make([]SpawnPlan, 0, len(markers))
	nextObjectID := firstObjectID
	for markerIndex, currentMarker := range markers {
		if nextObjectID >= objectIDLimit {
			return nil, firstObjectID, fmt.Errorf(
				"fixtureObjectID[%d]: exhausted", markerIndex,
			)
		}
		currentPlan := SpawnPlan{
			ObjectID:         nextObjectID,
			NounName:         currentMarker.NounName,
			AuthoredNounName: currentMarker.AuthoredNounName,
			Position:         currentMarker.Position,
			Rotation:         currentMarker.Rotation,
			LocusID:          currentMarker.MarkerID,
			IsFixture:        isFixture,
			MarkerSetName:    currentMarker.MarkerSetName,
			NPCProfile:       currentMarker.NPCProfile,
		}
		if isFixture {
			currentPlan.PlacementScale = currentMarker.Scale
		}
		if IsGraviticStabilizer(currentPlan) && currentMarker.Scale > 0 {
			currentPlan.NPCProfile.FootprintRadius *= currentMarker.Scale
		}
		err := ValidateSpawnPlan(currentPlan, objectIDLimit)
		if err != nil {
			return nil, firstObjectID, fmt.Errorf(
				"fixturePlan[%d]: %w", markerIndex, err,
			)
		}
		plans = append(plans, currentPlan)
		nextObjectID++
	}
	return plans, nextObjectID, nil
}
