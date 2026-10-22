package sim

import (
	"errors"
	"math"

	"github.com/darkspinnet/darkspin/server/util"
)

// ObjectiveDropIntent transfers a packaged quest drop to its feature-owned
// outbox. Source operands are captured at death, before the corpse can change.
type ObjectiveDropIntent struct {
	ObjectiveID           uint32
	NounID                uint32
	SourceObjectID        uint32
	SourcePosition        Position
	SourceFootprintRadius float32
}

func (ObjectiveDropIntent) intentDomain() Domain { return DomainAuthority }

func (e *luaCompiler) dropObjectiveObject(arguments []luaValue) ([]luaValue, error) {
	const lootCrystalsObjectiveID = uint32(0x53449566)
	const questNoun = "loot_questCrystal.Noun"
	if e.input.objectiveID != lootCrystalsObjectiveID ||
		e.objectiveEvent.Kind != LuaObjectiveEventDeath || !e.objectiveEvent.IsTargetNPC ||
		e.objectiveEvent.TargetObjectID == 0 || len(arguments) != 2 ||
		arguments[1].kind != luaNumber ||
		arguments[1].number != float64(e.objectiveEvent.TargetObjectID) {
		return nil, errors.New("objective drop context invalid")
	}
	nounID := util.HashID(questNoun)
	isNounValid := arguments[0].kind == luaString && arguments[0].text == questNoun
	if arguments[0].kind == luaNumber && arguments[0].number == float64(nounID) {
		isNounValid = true
	}
	if !isNounValid {
		return nil, errors.New("objective drop noun invalid")
	}
	for _, coordinate := range []float32{
		e.objectiveEvent.TargetPosition.X, e.objectiveEvent.TargetPosition.Y,
		e.objectiveEvent.TargetPosition.Z, e.objectiveEvent.TargetFootprintRadius,
	} {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return nil, errors.New("objective drop source nonfinite")
		}
	}
	if e.objectiveEvent.TargetFootprintRadius < 0 {
		return nil, errors.New("objective drop footprint invalid")
	}
	e.emit(ObjectiveDropIntent{
		ObjectiveID: lootCrystalsObjectiveID, NounID: nounID,
		SourceObjectID:        e.objectiveEvent.TargetObjectID,
		SourcePosition:        e.objectiveEvent.TargetPosition,
		SourceFootprintRadius: e.objectiveEvent.TargetFootprintRadius,
	})
	return nil, nil
}
