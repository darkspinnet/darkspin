package lua51

import (
	"math"
	"strings"
)

// StaticValueKind identifies a constant value recovered without executing Lua.
type StaticValueKind uint8

const (
	StaticUnknown StaticValueKind = iota
	StaticNumber
	StaticString
	StaticBoolean
	StaticTable
)

// StaticValue is one conservatively evaluated Lua constant or table.
type StaticValue struct {
	Kind    StaticValueKind
	Number  float64
	String  string
	IsTrue  bool
	Fields  map[string]StaticValue
	Entries []StaticValue
}

type staticValue struct {
	kind    StaticValueKind
	number  float64
	text    string
	isTrue  bool
	table   *staticTable
	builtin staticBuiltin
}

type staticBuiltin uint8

const (
	staticBuiltinNone staticBuiltin = iota
	staticBuiltinBitOr
)

type staticTable struct {
	fields  map[string]staticValue
	entries []staticValue
}

// StaticGlobals evaluates only deterministic constant/table instructions in
// the main prototype. Calls and other runtime-dependent operations become
// unknown instead of being guessed.
func StaticGlobals(chunk *Chunk) map[string]StaticValue {
	if chunk == nil || chunk.Main == nil {
		return nil
	}
	prototype := chunk.Main
	register := make([]staticValue, int(prototype.MaxStackSize)+1)
	global := staticRuntimeGlobals()
	for pc := 0; pc < len(prototype.Instructions); pc++ {
		instruction := prototype.Instructions[pc]
		a := int(instruction.A())
		switch instruction.Opcode() {
		case OpcodeMove:
			register[a] = register[int(instruction.B())]
		case OpcodeLoadK:
			register[a] = staticConstant(prototype, instruction.Bx())
		case OpcodeLoadBool:
			register[a] = staticValue{kind: StaticBoolean, isTrue: instruction.B() != 0}
			if instruction.C() != 0 {
				pc++
			}
		case OpcodeLoadNil:
			for index := a; index <= int(instruction.B()) && index < len(register); index++ {
				register[index] = staticValue{}
			}
		case OpcodeGetGlobal:
			name := staticConstant(prototype, instruction.Bx()).text
			register[a] = global[name]
		case OpcodeSetGlobal:
			name := staticConstant(prototype, instruction.Bx()).text
			if name != "" {
				field := register[a]
				if field.kind == StaticUnknown &&
					(strings.HasPrefix(name, "nAbility_") || strings.HasPrefix(name, "nModifier_")) {
					field = staticValue{kind: StaticTable, table: &staticTable{fields: make(map[string]staticValue)}}
					register[a] = field
				}
				global[name] = field
			}
		case OpcodeGetTable:
			register[a] = staticTableGet(register[int(instruction.B())], staticRK(prototype, register, instruction.C()))
		case OpcodeSetTable:
			staticTableSet(register[a], staticRK(prototype, register, instruction.B()), staticRK(prototype, register, instruction.C()))
		case OpcodeNewTable:
			register[a] = staticValue{kind: StaticTable, table: &staticTable{fields: make(map[string]staticValue)}}
		case OpcodeAdd, OpcodeSub, OpcodeMul, OpcodeDiv, OpcodeMod, OpcodePow:
			left := staticRK(prototype, register, instruction.B())
			right := staticRK(prototype, register, instruction.C())
			register[a] = staticArithmetic(instruction.Opcode(), left, right)
		case OpcodeUnm:
			operand := register[int(instruction.B())]
			if operand.kind == StaticNumber {
				register[a] = staticValue{kind: StaticNumber, number: -operand.number}
			} else {
				register[a] = staticValue{}
			}
		case OpcodeSetList:
			count := int(instruction.B())
			block := int(instruction.C())
			if count == 0 || block == 0 || register[a].table == nil {
				continue
			}
			start := (block - 1) * 50
			table := register[a].table
			for len(table.entries) < start+count {
				table.entries = append(table.entries, staticValue{})
			}
			for index := 0; index < count; index++ {
				table.entries[start+index] = register[a+index+1]
			}
		case OpcodeCall, OpcodeTailCall:
			resultCount := int(instruction.C()) - 1
			if resultCount <= 0 {
				resultCount = 1
			}
			if register[a].builtin == staticBuiltinBitOr {
				register[a] = staticBitOr(register, a, int(instruction.B()))
				for index := 1; index < resultCount && a+index < len(register); index++ {
					register[a+index] = staticValue{}
				}
				continue
			}
			for index := 0; index < resultCount && a+index < len(register); index++ {
				register[a+index] = staticValue{}
			}
		case OpcodeClosure, OpcodeVararg:
			register[a] = staticValue{}
		}
	}
	result := make(map[string]StaticValue, len(global))
	for name, field := range global {
		result[name] = exportStatic(field)
	}
	return result
}

func staticRuntimeGlobals() map[string]staticValue {
	descriptorTable := &staticTable{fields: make(map[string]staticValue, len(staticDescriptorBits))}
	for name, bit := range staticDescriptorBits {
		descriptorTable.fields[name] = staticValue{kind: StaticNumber, number: float64(uint32(1) << bit)}
	}
	damageSourceTable := staticNumberTable(map[string]uint32{
		"Physical": 0,
		"Energy":   1,
	})
	damageTypeTable := staticNumberTable(map[string]uint32{
		"Technology":   0,
		"Spacetime":    1,
		"Life":         2,
		"Elements":     3,
		"Supernatural": 4,
		"Generic":      5,
	})
	bitTable := &staticTable{fields: map[string]staticValue{
		"Or": {builtin: staticBuiltinBitOr},
	}}
	return map[string]staticValue{
		"nDescriptors":   {kind: StaticTable, table: descriptorTable},
		"nDamageSources": {kind: StaticTable, table: damageSourceTable},
		"nDamageTypes":   {kind: StaticTable, table: damageTypeTable},
		"nBit":           {kind: StaticTable, table: bitTable},
	}
}

func staticNumberTable(numberByName map[string]uint32) *staticTable {
	table := &staticTable{fields: make(map[string]staticValue, len(numberByName))}
	for name, number := range numberByName {
		table.fields[name] = staticValue{kind: StaticNumber, number: float64(number)}
	}
	return table
}

func staticBitOr(register []staticValue, functionIndex, argumentCount int) staticValue {
	if argumentCount <= 1 || functionIndex+argumentCount > len(register) {
		return staticValue{}
	}
	mask := uint32(0)
	for index := functionIndex + 1; index < functionIndex+argumentCount; index++ {
		argument := register[index]
		if argument.kind != StaticNumber || argument.number < 0 || argument.number > math.MaxUint32 ||
			argument.number != math.Trunc(argument.number) {
			return staticValue{}
		}
		mask |= uint32(argument.number)
	}
	return staticValue{kind: StaticNumber, number: float64(mask)}
}

var staticDescriptorBits = map[string]uint32{
	"IsMelee": 0, "IsBasic": 1, "IsDoT": 2, "IsAoE": 3, "IsBuff": 4,
	"IsDebuff": 5, "IsPhysicalDamage": 6, "IsEnergyDamage": 7, "IsCosmetic": 8,
	"IsHaste": 9, "IsChannel": 10, "HitReactNone": 11, "IsHoT": 12,
	"IsProjectile": 13, "IgnorePlayerCount": 14, "IgnoreDifficulty": 15,
	"IsSelfResurrect": 16, "IsInteract": 17, "IsThorns": 18,
}

func staticConstant(prototype *Prototype, index uint32) staticValue {
	if prototype == nil || index >= uint32(len(prototype.Constants)) {
		return staticValue{}
	}
	constant := prototype.Constants[index]
	switch constant.Kind {
	case ConstantNumber:
		return staticValue{kind: StaticNumber, number: constant.Number}
	case ConstantString:
		return staticValue{kind: StaticString, text: constant.String}
	case ConstantBoolean:
		return staticValue{kind: StaticBoolean, isTrue: constant.IsTrue}
	default:
		return staticValue{}
	}
}

func staticRK(prototype *Prototype, register []staticValue, operand uint16) staticValue {
	if operand >= 256 {
		return staticConstant(prototype, uint32(operand-256))
	}
	if int(operand) >= len(register) {
		return staticValue{}
	}
	return register[operand]
}

func staticTableGet(container, key staticValue) staticValue {
	if container.table == nil {
		return staticValue{}
	}
	if key.kind == StaticString {
		return container.table.fields[key.text]
	}
	if key.kind == StaticNumber && key.number >= 1 && key.number == math.Trunc(key.number) {
		index := int(key.number) - 1
		if index < len(container.table.entries) {
			return container.table.entries[index]
		}
	}
	return staticValue{}
}

func staticTableSet(container, key, field staticValue) {
	if container.table == nil {
		return
	}
	if key.kind == StaticString {
		container.table.fields[key.text] = field
		return
	}
	if key.kind != StaticNumber || key.number < 1 || key.number != math.Trunc(key.number) {
		return
	}
	index := int(key.number) - 1
	for len(container.table.entries) <= index {
		container.table.entries = append(container.table.entries, staticValue{})
	}
	container.table.entries[index] = field
}

func staticArithmetic(opcode Opcode, left, right staticValue) staticValue {
	if left.kind != StaticNumber || right.kind != StaticNumber {
		return staticValue{}
	}
	number := 0.0
	switch opcode {
	case OpcodeAdd:
		number = left.number + right.number
	case OpcodeSub:
		number = left.number - right.number
	case OpcodeMul:
		number = left.number * right.number
	case OpcodeDiv:
		if right.number == 0 {
			return staticValue{}
		}
		number = left.number / right.number
	case OpcodeMod:
		if right.number == 0 {
			return staticValue{}
		}
		number = math.Mod(left.number, right.number)
	case OpcodePow:
		number = math.Pow(left.number, right.number)
	}
	return staticValue{kind: StaticNumber, number: number}
}

func exportStatic(field staticValue) StaticValue {
	result := StaticValue{Kind: field.kind, Number: field.number, String: field.text, IsTrue: field.isTrue}
	if field.table == nil {
		return result
	}
	result.Fields = make(map[string]StaticValue, len(field.table.fields))
	for name, child := range field.table.fields {
		result.Fields[name] = exportStatic(child)
	}
	result.Entries = make([]StaticValue, len(field.table.entries))
	for index, child := range field.table.entries {
		result.Entries[index] = exportStatic(child)
	}
	return result
}
