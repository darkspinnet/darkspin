package lua51

import "fmt"

const maximumOpcode = 37

// Instruction is one native Lua 5.1 32-bit VM instruction.
type Instruction uint32

// Opcode identifies a Lua 5.1 operation.
type Opcode uint8

const (
	OpcodeMove Opcode = iota
	OpcodeLoadK
	OpcodeLoadBool
	OpcodeLoadNil
	OpcodeGetUpval
	OpcodeGetGlobal
	OpcodeGetTable
	OpcodeSetGlobal
	OpcodeSetUpval
	OpcodeSetTable
	OpcodeNewTable
	OpcodeSelf
	OpcodeAdd
	OpcodeSub
	OpcodeMul
	OpcodeDiv
	OpcodeMod
	OpcodePow
	OpcodeUnm
	OpcodeNot
	OpcodeLen
	OpcodeConcat
	OpcodeJmp
	OpcodeEq
	OpcodeLt
	OpcodeLe
	OpcodeTest
	OpcodeTestSet
	OpcodeCall
	OpcodeTailCall
	OpcodeReturn
	OpcodeForLoop
	OpcodeForPrep
	OpcodeTForLoop
	OpcodeSetList
	OpcodeClose
	OpcodeClosure
	OpcodeVararg
)

var opcodeName = [...]string{
	"MOVE", "LOADK", "LOADBOOL", "LOADNIL", "GETUPVAL", "GETGLOBAL", "GETTABLE",
	"SETGLOBAL", "SETUPVAL", "SETTABLE", "NEWTABLE", "SELF", "ADD", "SUB", "MUL",
	"DIV", "MOD", "POW", "UNM", "NOT", "LEN", "CONCAT", "JMP", "EQ", "LT", "LE",
	"TEST", "TESTSET", "CALL", "TAILCALL", "RETURN", "FORLOOP", "FORPREP", "TFORLOOP",
	"SETLIST", "CLOSE", "CLOSURE", "VARARG",
}

func (instruction Instruction) Opcode() Opcode { return Opcode(uint32(instruction) & 0x3f) }
func (instruction Instruction) A() uint8       { return uint8((uint32(instruction) >> 6) & 0xff) }
func (instruction Instruction) C() uint16      { return uint16((uint32(instruction) >> 14) & 0x1ff) }
func (instruction Instruction) B() uint16      { return uint16((uint32(instruction) >> 23) & 0x1ff) }
func (instruction Instruction) Bx() uint32     { return (uint32(instruction) >> 14) & 0x3ffff }
func (instruction Instruction) SBx() int32     { return int32(instruction.Bx()) - 131071 }

func (opcode Opcode) String() string {
	if opcode > maximumOpcode {
		return fmt.Sprintf("OP_%d", opcode)
	}
	return opcodeName[opcode]
}
