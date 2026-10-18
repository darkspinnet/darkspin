//go:build scenario

package scenario

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// MilestoneKinds returns the fixed first-milestone execution order. Callers
// cannot add arbitrary steps or resume an old checkpoint.
func MilestoneKinds() []StepKind {
	return []StepKind{Launch, Authenticated, PrepareAccepted, NativeLayout,
		FixtureTakeover, WorldReady, DungeonCommitted, ControllableHero,
		SettledEntry, FixtureComparison, BeforeTraversal, AfterTraversal, BossAdmission, BossPublication}
}

func (e Definition) Validate() error {
	if e.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported scenario schema %d", e.SchemaVersion)
	}
	if !isIdentifier(e.ScenarioID) || !isIdentifier(e.ProgressionID) || !isIdentifier(e.SquadID) {
		return errors.New("scenario, fresh progression and squad IDs must be simple identifiers")
	}
	if e.Level != "zelems_3" || e.Occurrence != 2 || e.Difficulty != 2 || e.ClientCount != 1 {
		return errors.New("first milestone requires zelems_3 occurrence 2 difficulty 2 and one client")
	}
	if e.MapSeed == 0 || e.MapSeed == ^uint32(0) {
		return errors.New("map seed must be explicit and cannot use either automatic-seed sentinel")
	}
	if e.PrepareMask != 0 {
		return errors.New("first milestone supports only confirmed prepare mask 0")
	}
	if strings.TrimSpace(e.PopulationProvenance) == "" {
		return errors.New("population provenance must explicitly state known inputs or unknown ownership")
	}
	if len(e.HeroIDs) != 3 {
		return errors.New("fresh squad requires exactly three explicit hero IDs")
	}
	heroes := make(map[uint64]struct{}, len(e.HeroIDs))
	for _, heroID := range e.HeroIDs {
		if heroID == 0 || heroID > uint64(^uint32(0)) {
			return errors.New("hero IDs must be nonzero uint32 template noun IDs")
		}
		_, isDuplicate := heroes[heroID]
		if isDuplicate {
			return errors.New("hero IDs must be distinct")
		}
		heroes[heroID] = struct{}{}
	}
	kinds := MilestoneKinds()
	if len(e.Milestones) != len(kinds) {
		return errors.New("all first-milestone steps and wall deadlines must be explicit")
	}
	for index, milestone := range e.Milestones {
		if milestone.Kind != kinds[index] {
			return fmt.Errorf("milestone %d must be %s", index, kinds[index])
		}
		if milestone.Timeout < time.Second || milestone.Timeout > 10*time.Minute {
			return fmt.Errorf("milestone %s wall deadline must be between 1 second and 10 minutes", milestone.Kind)
		}
	}
	return nil
}

func isIdentifier(identifier string) bool {
	if len(identifier) == 0 || len(identifier) > 80 {
		return false
	}
	for _, character := range identifier {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
