package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/darkspinnet/darkspin/content/lua51"
)

type luaTokenBinding struct {
	abilityTableName string
	tokenName        string
	sourceTableName  string
	propertyName     string
	elementIndex     int
	multiplier       float64
}

type luaTokenExpression struct {
	kind            string
	name            string
	sourceTableName string
	propertyName    string
	elementIndex    int
	multiplier      float64
}

func writeLuaTokenBindings(ctx context.Context, transaction *sql.Tx, chunks []luaChunkImport) error {
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO lua_token_binding
		(id, lua_chunk_id, ability_table_name, token_name, source_table_name,
		 property_name, element_index, multiplier, evidence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'TranslateToken bytecode')`)
	if err != nil {
		return fmt.Errorf("tokenPrepare: %w", err)
	}
	bindingID := int64(1)
	for _, chunk := range chunks {
		inspection, inspectErr := lua51.Inspect(chunk.bytecode)
		if inspectErr != nil {
			_ = statement.Close()
			return fmt.Errorf("tokenInspect[%d]: %w", chunk.id, inspectErr)
		}
		for _, binding := range luaTranslateTokenBindings(inspection) {
			_, err = statement.ExecContext(ctx, bindingID, chunk.id, binding.abilityTableName,
				binding.tokenName, binding.sourceTableName, binding.propertyName,
				binding.elementIndex, binding.multiplier)
			if err != nil {
				_ = statement.Close()
				return fmt.Errorf("tokenInsert[%d:%s]: %w", chunk.id, binding.tokenName, err)
			}
			bindingID++
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("tokenClose: %w", err)
	}
	return nil
}

func luaTranslateTokenBindings(chunk *lua51.Chunk) []luaTokenBinding {
	if chunk == nil || chunk.Main == nil {
		return nil
	}
	type registerState struct {
		kind         string
		globalName   string
		closureIndex int
	}
	register := make([]registerState, int(chunk.Main.MaxStackSize)+1)
	bindings := make([]luaTokenBinding, 0)
	for _, instruction := range chunk.Main.Instructions {
		a := int(instruction.A())
		switch instruction.Opcode() {
		case lua51.OpcodeMove:
			register[a] = register[int(instruction.B())]
		case lua51.OpcodeGetGlobal:
			register[a] = registerState{kind: "global", globalName: luaConstantString(chunk.Main, instruction.Bx()), closureIndex: -1}
		case lua51.OpcodeClosure:
			register[a] = registerState{kind: "closure", closureIndex: int(instruction.Bx())}
		case lua51.OpcodeSetTable:
			if int(instruction.C()) >= len(register) || register[a].globalName == "" ||
				luaRKString(chunk.Main, instruction.B()) != "TranslateToken" ||
				register[instruction.C()].kind != "closure" {
				continue
			}
			closureIndex := register[instruction.C()].closureIndex
			if closureIndex < 0 || closureIndex >= len(chunk.Main.Prototypes) {
				continue
			}
			bindings = append(bindings,
				luaPrototypeTokenBindings(register[a].globalName, chunk.Main.Prototypes[closureIndex])...)
		}
	}
	return bindings
}

func luaPrototypeTokenBindings(abilityTableName string, prototype *lua51.Prototype) []luaTokenBinding {
	bindings := make([]luaTokenBinding, 0)
	for pc := 0; pc < len(prototype.Instructions); pc++ {
		tokenName, isToken := luaTokenComparison(prototype, prototype.Instructions[pc])
		if !isToken {
			continue
		}
		end := len(prototype.Instructions)
		for next := pc + 1; next < len(prototype.Instructions); next++ {
			_, isNextToken := luaTokenComparison(prototype, prototype.Instructions[next])
			if isNextToken {
				end = next
				break
			}
		}
		expression := luaEvaluateTokenBranch(prototype, pc+1, end)
		if expression.kind != "property" {
			continue
		}
		bindings = append(bindings, luaTokenBinding{
			abilityTableName: abilityTableName, tokenName: tokenName,
			sourceTableName: expression.sourceTableName, propertyName: expression.propertyName,
			elementIndex: expression.elementIndex, multiplier: expression.multiplier,
		})
	}
	return bindings
}

func luaTokenComparison(prototype *lua51.Prototype, instruction lua51.Instruction) (string, bool) {
	if instruction.Opcode() != lua51.OpcodeEq {
		return "", false
	}
	if instruction.B() == 0 && instruction.C() >= 256 {
		return luaConstantString(prototype, uint32(instruction.C()-256)), true
	}
	if instruction.C() == 0 && instruction.B() >= 256 {
		return luaConstantString(prototype, uint32(instruction.B()-256)), true
	}
	return "", false
}

func luaEvaluateTokenBranch(prototype *lua51.Prototype, start, end int) luaTokenExpression {
	register := make([]luaTokenExpression, int(prototype.MaxStackSize)+1)
	for pc := start; pc < end; pc++ {
		instruction := prototype.Instructions[pc]
		a := int(instruction.A())
		switch instruction.Opcode() {
		case lua51.OpcodeMove:
			register[a] = register[int(instruction.B())]
		case lua51.OpcodeLoadK:
			register[a] = luaConstantExpression(prototype, instruction.Bx())
		case lua51.OpcodeGetGlobal:
			name := luaConstantString(prototype, instruction.Bx())
			kind := "global"
			if name == "GetRankedValueHelper" {
				kind = "rankHelper"
			}
			register[a] = luaTokenExpression{kind: kind, name: name, multiplier: 1}
		case lua51.OpcodeGetTable:
			container := register[int(instruction.B())]
			key := luaRKExpression(prototype, register, instruction.C())
			switch {
			case container.kind == "global" && key.kind == "string":
				register[a] = luaTokenExpression{kind: "property", sourceTableName: container.name,
					propertyName: key.name, multiplier: 1}
			case container.kind == "property" && key.kind == "number":
				if key.multiplier == 1 || key.multiplier == 2 {
					container.elementIndex = int(key.multiplier)
					register[a] = container
				} else {
					register[a] = luaTokenExpression{}
				}
			case container.kind == "property" && instruction.C() == 1:
				register[a] = container
			default:
				register[a] = luaTokenExpression{}
			}
		case lua51.OpcodeCall:
			if register[a].kind == "rankHelper" && a+1 < len(register) && register[a+1].kind == "property" {
				register[a] = register[a+1]
			} else {
				register[a] = luaTokenExpression{}
			}
		case lua51.OpcodeMul:
			left := luaRKExpression(prototype, register, instruction.B())
			right := luaRKExpression(prototype, register, instruction.C())
			register[a] = luaMultiplyTokenExpression(left, right)
		}
	}
	if len(register) <= 2 {
		return luaTokenExpression{}
	}
	return register[2]
}

func luaMultiplyTokenExpression(left, right luaTokenExpression) luaTokenExpression {
	if left.kind == "property" && right.kind == "number" {
		left.multiplier *= right.multiplier
		return left
	}
	if right.kind == "property" && left.kind == "number" {
		right.multiplier *= left.multiplier
		return right
	}
	return luaTokenExpression{}
}

func luaRKExpression(prototype *lua51.Prototype, register []luaTokenExpression, operand uint16) luaTokenExpression {
	if operand >= 256 {
		return luaConstantExpression(prototype, uint32(operand-256))
	}
	if int(operand) >= len(register) {
		return luaTokenExpression{}
	}
	return register[operand]
}

func luaConstantExpression(prototype *lua51.Prototype, index uint32) luaTokenExpression {
	if prototype == nil || index >= uint32(len(prototype.Constants)) {
		return luaTokenExpression{}
	}
	constant := prototype.Constants[index]
	if constant.Kind == lua51.ConstantString {
		return luaTokenExpression{kind: "string", name: constant.String, multiplier: 1}
	}
	if constant.Kind == lua51.ConstantNumber {
		return luaTokenExpression{kind: "number", multiplier: constant.Number}
	}
	return luaTokenExpression{}
}

func luaConstantString(prototype *lua51.Prototype, index uint32) string {
	if prototype == nil || index >= uint32(len(prototype.Constants)) ||
		prototype.Constants[index].Kind != lua51.ConstantString {
		return ""
	}
	return prototype.Constants[index].String
}

func luaRKString(prototype *lua51.Prototype, operand uint16) string {
	if operand >= 256 {
		return luaConstantString(prototype, uint32(operand-256))
	}
	return ""
}
