package sim

import (
	"errors"
	"fmt"
	"math"
)

// This is a server execution budget, not an emulation of the client's spatial
// traversal limit. Exceeding it rejects the callback without dropping actors.
const maxLuaRadiusObjects = 256

// LuaWorldQuery reads current authoritative objects at each native Lua call.
// Implementations preserve their stable world order and return at most 256
// radius candidates. They must not retain or mutate the supplied request.
type LuaWorldQuery interface {
	Object(role Role) (LuaObjectInput, error)
	ObjectsInRadius(req LuaRadiusQuery) ([]LuaObjectInput, error)
}

type LuaRadiusQuery struct {
	Center                  Position
	Radius                  float32
	IsOrganicDamageableOnly bool
}

func (e *luaCompiler) queryObject(role Role) (LuaObjectInput, error) {
	err := e.chargeWorldQuery()
	if err != nil {
		return LuaObjectInput{}, fmt.Errorf("objectBudget: %w", err)
	}
	object, err := e.input.Query.Object(role)
	if err != nil {
		return LuaObjectInput{}, fmt.Errorf("worldObject: %w", err)
	}
	if object.Role != role || !isLuaQueryPosition(object.Position) {
		return LuaObjectInput{}, errors.New("invalid world object")
	}
	return object, nil
}

func (e *luaCompiler) chargeWorldQuery() error {
	if e.instructionBudget <= 0 {
		return errors.New("instruction budget exhausted")
	}
	e.instructionBudget--
	return nil
}

func isLuaQueryPosition(position Position) bool {
	return !math.IsNaN(float64(position.X)) && !math.IsInf(float64(position.X), 0) &&
		!math.IsNaN(float64(position.Y)) && !math.IsInf(float64(position.Y), 0) &&
		!math.IsNaN(float64(position.Z)) && !math.IsInf(float64(position.Z), 0)
}

func (e *luaCompiler) objectPosition(arguments []luaValue) ([]luaValue, error) {
	position := e.input.Position
	if e.input.Query == nil {
		err := e.requireRole(arguments)
		if err != nil {
			return nil, fmt.Errorf("positionRole: %w", err)
		}
	} else {
		object, err := e.luaObjectArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("positionObject: %w", err)
		}
		position = object.Position
	}
	return []luaValue{numberLuaValue(float64(position.X)), numberLuaValue(float64(position.Y)), numberLuaValue(float64(position.Z))}, nil
}

func (e *luaCompiler) objectOwner(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 1 || arguments[0].kind != luaString {
		return nil, errors.New("owner arguments")
	}
	if e.input.Query == nil {
		return []luaValue{{kind: luaNil}}, nil
	}
	object, err := e.luaObjectArgument(arguments)
	if err != nil {
		return nil, fmt.Errorf("ownerObject: %w", err)
	}
	if object.OwnerRole == "" {
		return []luaValue{{kind: luaNil}}, nil
	}
	return []luaValue{stringLuaValue(string(object.OwnerRole))}, nil
}

func (e *luaCompiler) objectsInRadius(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 5 || arguments[0].kind != luaNumber ||
		arguments[1].kind != luaNumber || arguments[2].kind != luaNumber ||
		arguments[3].kind != luaNumber || arguments[4].kind != luaTableValue {
		return nil, fmt.Errorf("objects radius arguments: %s", luaKinds(arguments))
	}
	nearbyObjects := e.input.nearbyObjects
	if e.input.Query != nil {
		req := LuaRadiusQuery{
			Center: Position{X: float32(arguments[0].number), Y: float32(arguments[1].number), Z: float32(arguments[2].number)},
			Radius: float32(arguments[3].number),
		}
		if !isLuaQueryPosition(req.Center) || math.IsNaN(float64(req.Radius)) || math.IsInf(float64(req.Radius), 0) || req.Radius < 0 {
			return nil, errors.New("invalid radius query")
		}
		sporeLabs := e.global.get(stringLuaKey("nSporeLabs"))
		if sporeLabs.kind != luaTableValue || sporeLabs.table == nil {
			return nil, errors.New("radius object filter unavailable")
		}
		organicTypes := sporeLabs.table.get(stringLuaKey("organicDamageableObjectTypes"))
		if organicTypes.kind != luaTableValue || organicTypes.table == nil || arguments[4].table != organicTypes.table {
			return nil, errors.New("unsupported radius object filter")
		}
		req.IsOrganicDamageableOnly = true
		err := e.chargeWorldQuery()
		if err != nil {
			return nil, fmt.Errorf("radiusBudget: %w", err)
		}
		nearbyObjects, err = e.input.Query.ObjectsInRadius(req)
		if err != nil {
			return nil, fmt.Errorf("worldRadius: %w", err)
		}
		if len(nearbyObjects) > maxLuaRadiusObjects {
			return nil, errors.New("radius object limit exceeded")
		}
	} else if len(e.input.nearbyObjectScans) != 0 {
		scanIndex := min(e.input.nearbyObjectScanIndex, len(e.input.nearbyObjectScans)-1)
		nearbyObjects = e.input.nearbyObjectScans[scanIndex]
		e.input.nearbyObjectScanIndex++
	}
	objects, err := e.newRuntimeTable(nil)
	if err != nil {
		return nil, fmt.Errorf("objectsTable: %w", err)
	}
	for index, object := range nearbyObjects {
		if object.Role == "" {
			return nil, fmt.Errorf("nearbyRole[%d]", index)
		}
		err = e.setRuntimeField(objects, numberLuaKey(float64(index+1)), stringLuaValue(string(object.Role)))
		if err != nil {
			return nil, fmt.Errorf("nearbyField[%d]: %w", index, err)
		}
	}
	return []luaValue{tableLuaValue(objects)}, nil
}
