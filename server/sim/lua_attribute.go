package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/darkspinnet/darkspin/server/attribute"
)

// LuaAttributeState is authority's native baseline and other owners' ordered
// contributions. It excludes this compiler's own grants, which are overlaid
// here so a script can read its changes before its program is dispatched.
type LuaAttributeState struct {
	Base          float32
	Contributions []attribute.Contribution
}

// LuaAttributeQuery supplies live state at a retained callback boundary.
// A query is scoped to its modifier owner, just like the other Lua world ports.
type LuaAttributeQuery interface {
	AttributeState(Role, AttributeKind) (LuaAttributeState, error)
}

func cloneLuaAttributeStates(states map[AttributeKind]LuaAttributeState) map[AttributeKind]LuaAttributeState {
	if states == nil {
		return nil
	}
	clones := make(map[AttributeKind]LuaAttributeState, len(states))
	for kind, state := range states {
		state.Contributions = append([]attribute.Contribution(nil), state.Contributions...)
		clones[kind] = state
	}
	return clones
}

func luaAttributeKind(argument luaValue) (AttributeKind, error) {
	if argument.kind == luaString {
		switch AttributeKind(argument.text) {
		case AttributeImmobilized, AttributeIntangible, AttributeInvisibleToSecurityTeleporter:
			return AttributeKind(argument.text), nil
		}
	}
	if argument.kind == luaNumber {
		switch argument.number {
		case 4:
			return AttributeMaxHealth, nil
		case 23:
			return AttributeAttackSpeedScale, nil
		case 48:
			return AttributeMovementSpeedBuff, nil
		case 62:
			return AttributeAoERadius, nil
		case 73:
			return AttributeImmuneToStunned, nil
		case 108:
			return AttributeDirectAttackDamage, nil
		case 113:
			return AttributeBodyScale, nil
		}
	}
	return "", errors.New("unsupported attribute kind")
}

func isLegacyLuaAttribute(kind AttributeKind) bool {
	return kind == AttributeImmobilized || kind == AttributeIntangible || kind == AttributeInvisibleToSecurityTeleporter
}

func (c *luaCompiler) addAttributeModifier(arguments []luaValue) ([]luaValue, error) {
	err := c.requireRole(arguments)
	if err != nil {
		return nil, fmt.Errorf("attributeRole: %w", err)
	}
	if (len(arguments) != 3 && len(arguments) != 4) || arguments[2].kind != luaNumber {
		return nil, errors.New("invalid attribute arguments")
	}
	kind, err := luaAttributeKind(arguments[1])
	if err != nil {
		return nil, fmt.Errorf("attributeKind: %w", err)
	}
	operation := attribute.Add
	if len(arguments) == 4 {
		if arguments[3].kind != luaNumber || arguments[3].number < 1 || arguments[3].number > 5 ||
			math.Trunc(arguments[3].number) != arguments[3].number {
			return nil, errors.New("unsupported attribute operation")
		}
		operation = attribute.Operation(arguments[3].number)
	}
	amount := arguments[2].number
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount > math.MaxFloat32 || amount < -math.MaxFloat32 {
		return nil, errors.New("invalid attribute amount")
	}
	if c.attributeHandle == math.MaxUint32 {
		return nil, errors.New("attribute handle exhausted")
	}
	c.attributeHandle++
	modifier := AttributeModifierIntent{Role: c.input.Role, AttributeKind: kind,
		Amount: float32(amount), Handle: c.attributeHandle, Operation: operation}
	c.attributeModifiers[c.attributeHandle] = modifier
	c.emitAttributeModifier(modifier, false)
	return []luaValue{numberLuaValue(float64(c.attributeHandle))}, nil
}

func (c *luaCompiler) removeAttributeModifier(arguments []luaValue) ([]luaValue, error) {
	err := c.requireRole(arguments)
	if err != nil {
		return nil, fmt.Errorf("attributeRemoveRole: %w", err)
	}
	if len(arguments) != 2 || arguments[1].kind != luaNumber || arguments[1].number < 1 ||
		arguments[1].number > math.MaxUint32 || math.Trunc(arguments[1].number) != arguments[1].number {
		return nil, errors.New("invalid attribute remove handle")
	}
	handle := uint32(arguments[1].number)
	modifier, isFound := c.attributeModifiers[handle]
	if !isFound {
		return nil, fmt.Errorf("attributeHandle: %d", handle)
	}
	delete(c.attributeModifiers, handle)
	c.emitAttributeModifier(modifier, true)
	return nil, nil
}

func (c *luaCompiler) emitAttributeModifier(modifier AttributeModifierIntent, isRemoval bool) {
	if isLegacyLuaAttribute(modifier.AttributeKind) && modifier.Operation == attribute.Add {
		// Existing spawn/death/teleport adapters own these verified deltas.
		modifier.Handle = 0
		modifier.Operation = 0
		if isRemoval {
			modifier.Amount = -modifier.Amount
		}
	} else {
		modifier.IsRemoval = isRemoval
	}
	c.emit(modifier)
}

func (c *luaCompiler) attributeValue(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || arguments[0].kind != luaString {
		return nil, errors.New("invalid attribute read arguments")
	}
	kind, err := luaAttributeKind(arguments[1])
	if err != nil {
		return nil, fmt.Errorf("attributeReadKind: %w", err)
	}
	role := Role(arguments[0].text)
	state, isFound := c.input.AttributeStates[kind]
	if role != c.input.Role {
		isFound = false
	}
	query, hasAttributeQuery := c.input.Query.(LuaAttributeQuery)
	if hasAttributeQuery {
		state, err = query.AttributeState(role, kind)
		if err != nil {
			return nil, fmt.Errorf("attributeQuery: %w", err)
		}
		isFound = true
	}
	if !isFound && kind == AttributeInvisibleToSecurityTeleporter {
		object, objectErr := c.luaObjectArgument(arguments[:1])
		if objectErr != nil {
			return nil, fmt.Errorf("attributeObject: %w", objectErr)
		}
		// This older world port exposes an effective boolean, not a native
		// baseline. Overlaying retained grants would count them twice.
		if object.IsInvisibleToSecurityTeleporter {
			return []luaValue{numberLuaValue(1)}, nil
		}
		return []luaValue{numberLuaValue(0)}, nil
	}
	if !isFound {
		return nil, fmt.Errorf("attributeBaseline: %s", kind)
	}
	contributions := append([]attribute.Contribution(nil), state.Contributions...)
	if role == c.input.Role {
		handles := make([]uint32, 0, len(c.attributeModifiers))
		for handle, modifier := range c.attributeModifiers {
			if modifier.AttributeKind == kind {
				handles = append(handles, handle)
			}
		}
		slices.Sort(handles)
		for _, handle := range handles {
			modifier := c.attributeModifiers[handle]
			contributions = append(contributions, attribute.Contribution{Handle: handle,
				Operation: modifier.Operation, Amount: modifier.Amount})
		}
	}
	aggregate, err := attribute.Resolve(state.Base, contributions)
	if err != nil {
		return nil, fmt.Errorf("attributeResolve: %w", err)
	}
	return []luaValue{numberLuaValue(float64(aggregate))}, nil
}
