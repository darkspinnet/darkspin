package objective

import (
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/sim"
)

const healthObeliskID = uint32(0x79278139)
const stayAliveID = uint32(0x6d26f97d)
const sameTypeID = uint32(0xc0fbb81e)

// ValidateRuntimeInput separates reviewed production support from successful
// Lua registration. A pool selector must still establish authored membership
// and supply the real zone census before constructing the session.
func ValidateRuntimeInput(req sim.LuaObjectiveInput) error {
	err := validateRuntimeIdentity(sim.LuaObjectiveSnapshot{
		ObjectiveID: req.ObjectiveID, ChunkID: req.Root.ChunkID,
		SHA256: req.Root.SHA256,
	})
	if err != nil {
		return fmt.Errorf("runtimeFamily: %w", err)
	}
	if req.ObjectiveID == healthObeliskID && req.HealthInteractableCount <= 0 {
		return fmt.Errorf("objective health obelisk census: %d", req.HealthInteractableCount)
	}
	return nil
}

func isPlayerObjective(objectiveID uint32) bool {
	switch objectiveID {
	case hugeDamageID, damageOftenID, stayAliveID, sameTypeID:
		return true
	default:
		return false
	}
}

// resetSameTypePlayer keeps the authored per-slot child table present. Lua849
// indexes it without a nil guard and records both the current run and its best.
func resetSameTypePlayer(snapshot sim.LuaObjectiveSnapshot, playerIndex uint8) (sim.LuaObjectiveSnapshot, error) {
	err := validateSameTypeState(snapshot)
	if err != nil {
		return sim.LuaObjectiveSnapshot{}, fmt.Errorf("resetPlayer: %w", err)
	}
	snapshot = snapshot.Clone()
	for _, field := range snapshot.PrivateTables[0].Fields {
		if field.Key.Kind != "number" || field.Key.Number != float64(playerIndex) {
			continue
		}
		if field.Data.Kind != "table" || field.Data.TableIndex <= 0 ||
			field.Data.TableIndex >= len(snapshot.PrivateTables) {
			return sim.LuaObjectiveSnapshot{}, fmt.Errorf("objective player table invalid: %d", playerIndex)
		}
		snapshot.PrivateTables[field.Data.TableIndex].Fields = []sim.LuaObjectiveField{
			{Key: sim.LuaObjectiveScalar{Kind: "string", Text: "numberDefeated"}, Data: sim.LuaObjectiveScalar{Kind: "number"}},
			{Key: sim.LuaObjectiveScalar{Kind: "string", Text: "mostDefeated"}, Data: sim.LuaObjectiveScalar{Kind: "number"}},
			{Key: sim.LuaObjectiveScalar{Kind: "string", Text: "typeDefeating"}, Data: sim.LuaObjectiveScalar{Kind: "number"}},
		}
		return snapshot, nil
	}
	return sim.LuaObjectiveSnapshot{}, fmt.Errorf("objective player table missing: %d", playerIndex)
}

func validateSameTypeState(snapshot sim.LuaObjectiveSnapshot) error {
	if len(snapshot.PrivateTables) != 5 || len(snapshot.PrivateTables[0].Fields) != 4 {
		return fmt.Errorf("objective player table count invalid")
	}
	arePlayerSlots := [4]bool{}
	areTableSlots := [5]bool{}
	for _, field := range snapshot.PrivateTables[0].Fields {
		if field.Key.Kind != "number" || field.Key.Number < 0 || field.Key.Number >= 4 ||
			field.Key.Number != math.Trunc(field.Key.Number) || field.Data.Kind != "table" ||
			field.Data.TableIndex <= 0 || field.Data.TableIndex >= len(snapshot.PrivateTables) {
			return fmt.Errorf("objective player reference invalid")
		}
		playerIndex := int(field.Key.Number)
		if arePlayerSlots[playerIndex] || areTableSlots[field.Data.TableIndex] {
			return fmt.Errorf("objective player reference repeated")
		}
		arePlayerSlots[playerIndex] = true
		areTableSlots[field.Data.TableIndex] = true
		fields := snapshot.PrivateTables[field.Data.TableIndex].Fields
		if len(fields) != 3 {
			return fmt.Errorf("objective player field count invalid")
		}
		isRunFound := false
		isBestFound := false
		isAssetFound := false
		for _, child := range fields {
			if child.Key.Kind != "string" || child.Data.Kind != "number" ||
				math.IsNaN(child.Data.Number) || math.IsInf(child.Data.Number, 0) ||
				child.Data.Number < 0 || child.Data.Number != math.Trunc(child.Data.Number) {
				return fmt.Errorf("objective player field invalid")
			}
			switch child.Key.Text {
			case "numberDefeated":
				if isRunFound {
					return fmt.Errorf("objective player run repeated")
				}
				isRunFound = true
			case "mostDefeated":
				if isBestFound {
					return fmt.Errorf("objective player best repeated")
				}
				isBestFound = true
			case "typeDefeating":
				if isAssetFound {
					return fmt.Errorf("objective player asset repeated")
				}
				isAssetFound = true
				if child.Data.Number > math.MaxUint32 {
					return fmt.Errorf("objective player asset invalid")
				}
			default:
				return fmt.Errorf("objective player field unknown: %s", child.Key.Text)
			}
		}
	}
	return nil
}
