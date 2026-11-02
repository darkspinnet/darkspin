package cmd

import (
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/lua51"
)

type luaAuditOperand struct {
	name      string
	isLiteral bool
}

// Recover literal registration operands without executing the chunk. This is
// candidate evidence: branch feasibility and native admission are not proved.
func luaAuditRegistrations(prototype *lua51.Prototype) []string {
	if prototype == nil {
		return nil
	}
	registers := make([]luaAuditOperand, int(prototype.MaxStackSize)+1)
	names := make(map[string]bool)
	for _, instruction := range prototype.Instructions {
		index := int(instruction.A())
		if index >= len(registers) {
			continue
		}
		switch instruction.Opcode() {
		case lua51.OpcodeLoadK, lua51.OpcodeGetGlobal:
			registers[index] = luaAuditConstant(prototype, int(instruction.Bx()))
			registers[index].isLiteral = instruction.Opcode() == lua51.OpcodeLoadK
		case lua51.OpcodeMove:
			if int(instruction.B()) < len(registers) {
				registers[index] = registers[instruction.B()]
			}
		case lua51.OpcodeGetTable:
			left := luaAuditRK(prototype, registers, instruction.B())
			right := luaAuditRK(prototype, registers, instruction.C())
			registers[index] = luaAuditOperand{}
			if left.name != "" && right.isLiteral {
				registers[index].name = left.name + "." + right.name
			}
		case lua51.OpcodeCall, lua51.OpcodeTailCall:
			callee := registers[index].name
			argument := luaAuditOperand{}
			if instruction.B() > 1 && index+1 < len(registers) {
				argument = registers[index+1]
			}
			if argument.isLiteral && argument.name != "" &&
				(callee == "nAbility.RegisterAbility" || callee == "nModifier.RegisterModifier" ||
					strings.HasSuffix(callee, ".RegisterBehavior")) {
				names[argument.name] = true
			}
			resultCount := int(instruction.C()) - 1
			if resultCount < 0 {
				resultCount = len(registers) - index
			}
			for result := 0; result < resultCount && index+result < len(registers); result++ {
				registers[index+result] = luaAuditOperand{}
			}
			if callee == "nUtil.SPID" && instruction.C() == 2 {
				registers[index] = argument
			}
		case lua51.OpcodeSetTable, lua51.OpcodeSetGlobal, lua51.OpcodeSetUpval,
			lua51.OpcodeSetList, lua51.OpcodeReturn, lua51.OpcodeClose:
			// These operations do not overwrite the source register.
		case lua51.OpcodeJmp, lua51.OpcodeEq, lua51.OpcodeLt, lua51.OpcodeLe,
			lua51.OpcodeTest, lua51.OpcodeTestSet, lua51.OpcodeForLoop,
			lua51.OpcodeForPrep, lua51.OpcodeTForLoop:
			clear(registers)
		default:
			registers[index] = luaAuditOperand{}
		}
	}
	for _, child := range prototype.Prototypes {
		for _, name := range luaAuditRegistrations(child) {
			names[name] = true
		}
	}
	registrations := make([]string, 0, len(names))
	for name := range names {
		registrations = append(registrations, name)
	}
	sort.Strings(registrations)
	return registrations
}

func luaAuditConstant(prototype *lua51.Prototype, index int) luaAuditOperand {
	if index < 0 || index >= len(prototype.Constants) || prototype.Constants[index].Kind != lua51.ConstantString {
		return luaAuditOperand{}
	}
	return luaAuditOperand{name: prototype.Constants[index].String, isLiteral: true}
}

func luaAuditRK(prototype *lua51.Prototype, registers []luaAuditOperand, operand uint16) luaAuditOperand {
	if operand >= 256 {
		return luaAuditConstant(prototype, int(operand-256))
	}
	if int(operand) >= len(registers) {
		return luaAuditOperand{}
	}
	return registers[operand]
}
