package lua51

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
)

// WriteDisassembly writes a stable, human-readable inventory of every
// prototype, constant, and instruction in a decoded chunk.
func WriteDisassembly(output io.Writer, chunk *Chunk) error {
	if chunk == nil || chunk.Main == nil {
		return errors.New("nil chunk")
	}
	err := writePrototype(output, chunk.Main, "0")
	if err != nil {
		return fmt.Errorf("prototypeWrite: %w", err)
	}
	return nil
}

func writePrototype(output io.Writer, prototype *Prototype, path string) error {
	_, err := fmt.Fprintf(output,
		"prototype %s source=%q lines=%d-%d params=%d upvalues=%d vararg=%d stack=%d\n",
		path, prototype.SourceName, prototype.LineDefined, prototype.LastLineDefined,
		prototype.ParameterCount, prototype.UpvalueCount, prototype.IsVararg, prototype.MaxStackSize)
	if err != nil {
		return fmt.Errorf("headerWrite: %w", err)
	}
	constantTable := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	_, err = fmt.Fprintln(constantTable, "CONSTANT\tKIND\tVALUE")
	if err != nil {
		return fmt.Errorf("constantHeader: %w", err)
	}
	for index, constant := range prototype.Constants {
		_, err = fmt.Fprintf(constantTable, "%d\t%s\t%s\n", index, constantKindName(constant.Kind), constantText(constant))
		if err != nil {
			return fmt.Errorf("constantWrite[%d]: %w", index, err)
		}
	}
	err = constantTable.Flush()
	if err != nil {
		return fmt.Errorf("constantFlush: %w", err)
	}
	instructionTable := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	_, err = fmt.Fprintln(instructionTable, "PC\tOPCODE\tA\tB\tC\tBX\tSBX")
	if err != nil {
		return fmt.Errorf("instructionHeader: %w", err)
	}
	for pc, instruction := range prototype.Instructions {
		_, err = fmt.Fprintf(instructionTable, "%d\t%s\t%d\t%d\t%d\t%d\t%d\n",
			pc, instruction.Opcode(), instruction.A(), instruction.B(), instruction.C(),
			instruction.Bx(), instruction.SBx())
		if err != nil {
			return fmt.Errorf("instructionWrite[%d]: %w", pc, err)
		}
	}
	err = instructionTable.Flush()
	if err != nil {
		return fmt.Errorf("instructionFlush: %w", err)
	}
	for index, child := range prototype.Prototypes {
		_, err = fmt.Fprintln(output)
		if err != nil {
			return fmt.Errorf("separatorWrite[%d]: %w", index, err)
		}
		childPath := path + "." + strconv.Itoa(index)
		err = writePrototype(output, child, childPath)
		if err != nil {
			return fmt.Errorf("childWrite[%d]: %w", index, err)
		}
	}
	return nil
}

func constantKindName(kind ConstantKind) string {
	switch kind {
	case ConstantNil:
		return "nil"
	case ConstantBoolean:
		return "boolean"
	case ConstantNumber:
		return "number"
	case ConstantString:
		return "string"
	default:
		return fmt.Sprintf("type_%d", kind)
	}
}

func constantText(constant Constant) string {
	switch constant.Kind {
	case ConstantNil:
		return "nil"
	case ConstantBoolean:
		return strconv.FormatBool(constant.IsTrue)
	case ConstantNumber:
		return strconv.FormatFloat(constant.Number, 'g', -1, 64)
	case ConstantString:
		return strconv.Quote(constant.String)
	default:
		return ""
	}
}
