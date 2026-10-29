//go:build scenario

package npc

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// This allowlist names actual connected fixture owners, never generic enemy
// action fallbacks. Remaining installed nouns retain an explicit unsupported
// result until their authored callback/controller has a production owner.
func ScenarioFixtureSupport(nounName string) error {
	switch strings.ToLower(nounName) {
	case "cryos_stalagmite_small_destructible.noun",
		"dest_prefab_citadel_factorylight.noun", "dest_mutationagent.noun", "exploding_tree.noun",
		"dest_islands_instrument_scitech_2.noun", "dest_islands_instrument_scitech_3.noun",
		"dest_islands_instrument_scitech_11.noun", "dest_islands_instrument_scitech_7.noun",
		"dest_islands_instrument_scitech_8.noun", "dest_prefab_islands_instrument_scitech_8.noun",
		"dest_citadel_factorypipe.noun", "dest_cryos2_lvl2_stalagmite_small_destructible.noun",
		"dest_cryos_stalagmite_small_destructible.noun",
		"dest_prefab_cryos3_stalagmite_small.noun", "dest_prefab_cryos3_stalagmite_large.noun",
		"dest_prefab_cryos_stalagmite_small.noun", "dest_prefab_cryos_stalagmite_large.noun",
		"dest_prefab_citadel_factorypipe.noun", "dest_prefab_citadel_factorypipe_plasma.noun",
		"dest_prefab_citadel_factorypipe_smoke.noun", "dest_citadel_fuelcanister.noun",
		"dest_citadel_factoryvent_boss.noun", "dest_prefab_citadel_factoryvent.noun",
		"dest_prefab_cryos_acunit_small.noun", "dest_prefab_cryos_acunit_small_animated.noun",
		"dest_prefab_cryos_plants_shascope.noun", "dest_cryos_plants_shascope.noun",
		"dest_prefab_islands_instrument_scitech_2.noun", "dest_prefab_islands_instrument_scitech_3.noun",
		"dest_prefab_islands_instrument_scitech_11.noun", "dest_prefab_islands_instrument_scitech_11_noshadow.noun",
		"dest_prefab_islands_instrument_scitech_7.noun", "dest_prefab_islands_instrument_scitech_7_noshadow.noun",
		"dest_nocturna_herotree_yellow_1.noun", "dest_nocturna_plant_expl.noun",
		"dest_nocturna_plant_supnat.noun", "dest_prefab_nocturna_plant_supnat.noun",
		"dest_prefab_scaldron_plant_small_3.noun", "dest_prefab_scaldron_plant_large_1.noun",
		"dest_prefab_scaldron_plant_large_1b.noun", "dest_prefab_scaldron_tem_column_statue.noun",
		"dest_prefab_tota_heroplant_p3_b.noun", "dest_tota_heroplant_p3_b.noun",
		"dest_tota_headstatue_b.noun", "dest_tota_headstatue_c.noun":
		return nil
	case "dest_citadel_plasma_pool.noun":
		return errors.New("unsupported fixture: zero-health plasma pool requires selected scenery ownership")
	case "dest_prefab_cryos_ice_crack1.noun", "dest_prefab_cryos_ice_crack2.noun":
		return errors.New("unsupported fixture: geyser requires its selected Cryos marker and scenery context")
	case "enemyportal.noun":
		return errors.New("unsupported fixture: portal requires its normal Corruptor controller owner")
	default:
		return fmt.Errorf("unsupported fixture: authored callbacks are not connected for exact noun %q", nounName)
	}
}

func PrepareScenarioControlledFixture(plan SpawnPlan, controlID string) (SpawnPlan, error) {
	if controlID == "" || len(controlID) > 128 || !plan.IsFixture || plan.IsBoss || plan.IsCaptain || plan.IsArena || plan.OwnerObjectID != 0 || !plan.NPCProfile.IsKnown || plan.NPCProfile.HitPoint <= 0 || plan.NPCProfile.AIGraph == nil || !plan.NPCProfile.AIGraph.IsResolved {
		return SpawnPlan{}, errors.New("controlled fixture requires an exact imported fixture noun/class/AI graph and positive authored health")
	}
	err := ScenarioFixtureSupport(plan.NounName)
	if err != nil {
		return SpawnPlan{}, fmt.Errorf("fixtureOwner: %w", err)
	}
	plan = plan.Clone()
	plan.IsEncounterAuxiliary = true
	plan.IsLootSuppressed = true
	plan.IsExperienceSuppressed = true
	plan.scenarioControl = scenarioControlPlan{controlID: controlID, generation: 1, lastCommand: "spawn", lastCommandAt: time.Now()}
	return plan, nil
}

func (e *Session) recordScenarioFixtureRetirement(plan SpawnPlan) {
	index, isFound := e.recordIndexesByID[plan.ObjectID]
	if !isFound || !scenarioFixtureRecordMatches(e.records[index], plan) {
		e.markScenarioFixtureIncomplete("fixture retirement has no matching admission episode")
		return
	}
	e.records[index].State = "retired"
}
