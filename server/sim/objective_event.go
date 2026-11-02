package sim

import (
	"errors"
	"math"
)

// The installed GlobalDefinitions objective event bits are also the native
// objective descriptor subscription mask (A24660). A missing mask subscribes
// to no events; a callback's branches do not imply a subscription.
func luaObjectiveEventMask(objective *luaTable) (uint32, error) {
	mask := objective.get(stringLuaKey("handledEvents"))
	if mask.kind == luaNil {
		return 0, nil
	}
	if mask.kind != luaNumber || mask.number < 0 || mask.number > math.MaxUint32 ||
		mask.number != math.Trunc(mask.number) {
		return 0, errors.New("objective event mask invalid")
	}
	return uint32(mask.number), nil
}

func luaObjectiveEventFlag(kind LuaObjectiveEventKind) uint32 {
	switch kind {
	case LuaObjectiveEventTouchedObelisk:
		return 1
	case LuaObjectiveEventDamage:
		return 2
	case LuaObjectiveEventDeath:
		return 4
	case LuaObjectiveEventFullClear:
		return 8
	case LuaObjectiveEventModifierCreated:
		return 16
	case LuaObjectiveEventHeal:
		return 64
	case LuaObjectiveEventTouchedHealthObelisk:
		return 128
	default:
		return 0
	}
}
