package scaleform

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type ActionBlock struct {
	Path    string
	Payload []byte
}

type actionRecord struct {
	offset  int
	opcode  byte
	payload []byte
}

type typeScriptClass struct {
	path       string
	name       string
	properties []typeScriptClassProperty
}

type typeScriptClassProperty struct {
	name   string
	source string
}

var simpleActionNames = map[byte]string{
	0x04: "NEXT_FRAME", 0x05: "PREVIOUS_FRAME", 0x06: "PLAY", 0x07: "STOP",
	0x08: "TOGGLE_QUALITY", 0x09: "STOP_SOUNDS", 0x0A: "ADD", 0x0B: "SUBTRACT",
	0x0C: "MULTIPLY", 0x0D: "DIVIDE", 0x0E: "EQUALS", 0x0F: "LESS",
	0x10: "AND", 0x11: "OR", 0x12: "NOT", 0x13: "STRING_EQUALS",
	0x14: "STRING_LENGTH", 0x15: "STRING_EXTRACT", 0x17: "POP", 0x18: "TO_INTEGER",
	0x1C: "GET_VARIABLE", 0x1D: "SET_VARIABLE", 0x20: "SET_TARGET2", 0x21: "STRING_ADD",
	0x22: "GET_PROPERTY", 0x23: "SET_PROPERTY", 0x24: "CLONE_SPRITE", 0x25: "REMOVE_SPRITE",
	0x26: "TRACE", 0x27: "START_DRAG", 0x28: "END_DRAG", 0x29: "STRING_LESS",
	0x2A: "THROW", 0x2B: "CAST_OP", 0x2C: "IMPLEMENTS_OP", 0x30: "RANDOM_NUMBER",
	0x31: "MB_STRING_LENGTH", 0x32: "CHAR_TO_ASCII", 0x33: "ASCII_TO_CHAR",
	0x34: "GET_TIME", 0x35: "MB_STRING_EXTRACT", 0x36: "MB_CHAR_TO_ASCII",
	0x37: "MB_ASCII_TO_CHAR", 0x3A: "DELETE", 0x3B: "DELETE2", 0x3C: "DEFINE_LOCAL",
	0x3D: "CALL_FUNCTION", 0x3E: "RETURN", 0x3F: "MODULO", 0x40: "NEW_OBJECT",
	0x41: "DEFINE_LOCAL2", 0x42: "INIT_ARRAY", 0x43: "INIT_OBJECT", 0x44: "TYPEOF",
	0x45: "TARGET_PATH", 0x46: "ENUMERATE", 0x47: "ADD2", 0x48: "LESS2",
	0x49: "EQUALS2", 0x4A: "TO_NUMBER", 0x4B: "TO_STRING", 0x4C: "PUSH_DUPLICATE",
	0x4D: "STACK_SWAP", 0x4E: "GET_MEMBER", 0x4F: "SET_MEMBER", 0x50: "INCREMENT",
	0x51: "DECREMENT", 0x52: "CALL_METHOD", 0x53: "NEW_METHOD", 0x54: "INSTANCE_OF",
	0x55: "ENUMERATE2", 0x60: "BIT_AND", 0x61: "BIT_OR", 0x62: "BIT_XOR",
	0x63: "BIT_LSHIFT", 0x64: "BIT_RSHIFT", 0x65: "BIT_URSHIFT", 0x66: "STRICT_EQUALS",
	0x67: "GREATER", 0x68: "STRING_GREATER", 0x69: "EXTENDS", 0x9E: "CALL",
}

var simpleActionCodes = reverseActionNames(simpleActionNames)

func ExtractActionBlocks(tags []Tag) ([]ActionBlock, error) {
	blocks := make([]ActionBlock, 0, 8)
	err := extractActionBlocks(tags, "root", &blocks)
	if err != nil {
		return nil, err
	}
	return blocks, nil
}

func ApplyActionBlocks(tags []Tag, blocks map[string][]byte) error {
	seenPaths := make(map[string]bool, len(blocks))
	err := applyActionBlocks(tags, "root", blocks, seenPaths)
	if err != nil {
		return err
	}
	for path := range blocks {
		if !seenPaths[path] {
			return fmt.Errorf("actionPathMissing: %q", path)
		}
	}
	return nil
}

func WriteTypeScript(path string, payload []byte) ([]byte, error) {
	records, err := decodeActionRecords(payload)
	if err != nil {
		return nil, err
	}
	labels := actionLabels(records)
	branchTargets := actionBranchTargets(records)
	isConstantPoolInferred := canInferConstantPool(records, labels)
	var source strings.Builder
	_, _ = fmt.Fprintf(&source, "// darkspin reversible AVM1 TypeScript v1\n// block: %s\n", path)
	constantPool := []string(nil)
	functionEnds := make([]int, 0, 4)
	functionEndOffsets := make(map[int]bool)
	indent := ""
	for recordIndex := 0; recordIndex < len(records); recordIndex++ {
		record := records[recordIndex]
		for len(functionEnds) > 0 && functionEnds[len(functionEnds)-1] == record.offset {
			functionEnds = functionEnds[:len(functionEnds)-1]
			indent = strings.TrimSuffix(indent, "\t")
			_, _ = fmt.Fprintf(&source, "%s}\n", indent)
		}
		if label := labels[record.offset]; label != "" && (!functionEndOffsets[record.offset] || branchTargets[record.offset]) {
			if recordIndex == len(records)-1 && record.opcode == 0 {
				_, _ = fmt.Fprintf(&source, "%s%s: {}\n", indent, label)
			} else {
				_, _ = fmt.Fprintf(&source, "%s%s:\n", indent, label)
			}
		}
		row, renderErr := renderAction(record, labels)
		if renderErr != nil {
			return nil, fmt.Errorf("action[%d]: %w", record.offset, renderErr)
		}
		name := rowFieldStringOrEmpty(row, 0)
		if name == "CONSTANT_POOL" {
			constantPool = actionConstantPool(row)
			if isConstantPoolInferred {
				continue
			}
		}
		if name == "END" && recordIndex == len(records)-1 {
			continue
		}
		if name == "DEFINE_FUNCTION" || name == "DEFINE_FUNCTION2" {
			metadata, declaration, functionErr := renderActionFunction(row, recordIndex)
			if functionErr != nil {
				return nil, fmt.Errorf("actionFunction[%d]: %w", record.offset, functionErr)
			}
			_, _ = fmt.Fprintf(&source, "%s%s\n%s%s\n", indent, metadata, indent, declaration)
			codeSize, isCodeSize := actionBlockSize(record)
			if !isCodeSize {
				return nil, fmt.Errorf("actionSize[%d]: invalid", record.offset)
			}
			functionEnd := record.offset + 3 + len(record.payload) + int(codeSize)
			functionEnds = append(functionEnds, functionEnd)
			functionEndOffsets[functionEnd] = true
			indent += "\t"
			continue
		}
		if name == "PUSH" {
			assignment, consumedRecords, isAssignment := liftMemberAssignment(records, recordIndex, row, constantPool, labels)
			if isAssignment {
				_, _ = fmt.Fprintf(&source, "%s%s\n", indent, assignment)
				recordIndex += consumedRecords
				continue
			}
		}
		if name == "PUSH" && recordIndex+1 < len(records) && records[recordIndex+1].opcode == 0x4E {
			memberNames, remainingRow, consumedRecords, isMember := liftPushedMember(records, recordIndex, row, constantPool, labels)
			if isMember {
				if len(remainingRow) > 1 {
					remainingRow = namePushedConstants(remainingRow, constantPool)
					statements, statementErr := renderActionStatements(remainingRow)
					if statementErr != nil {
						return nil, fmt.Errorf("actionStatement[%d]: %w", record.offset, statementErr)
					}
					for _, statement := range statements {
						_, _ = fmt.Fprintf(&source, "%s%s\n", indent, statement)
					}
				}
				fields := stringsToFields(memberNames)
				statement, err := renderActionCall("getMember", fields)
				if err != nil {
					return nil, fmt.Errorf("getMember[%d]: %w", record.offset, err)
				}
				_, _ = fmt.Fprintf(&source, "%s%s\n", indent, statement)
				recordIndex += consumedRecords
				continue
			}
		}
		if name == "PUSH" && recordIndex+1 < len(records) && records[recordIndex+1].opcode == 0x1C {
			variableName, remainingRow, isVariable := liftPushedVariable(row, constantPool)
			if isVariable {
				if len(remainingRow) > 1 {
					remainingRow = namePushedConstants(remainingRow, constantPool)
					statements, statementErr := renderActionStatements(remainingRow)
					if statementErr != nil {
						return nil, fmt.Errorf("actionStatement[%d]: %w", record.offset, statementErr)
					}
					for _, statement := range statements {
						_, _ = fmt.Fprintf(&source, "%s%s\n", indent, statement)
					}
				}
				statement, err := renderActionCall("pushVariable", []any{variableName})
				if err != nil {
					return nil, fmt.Errorf("pushVariable[%d]: %w", record.offset, err)
				}
				_, _ = fmt.Fprintf(&source, "%s%s\n", indent, statement)
				recordIndex++
				continue
			}
		}
		row = namePushedConstants(row, constantPool)
		statements, statementErr := renderActionStatements(row)
		if statementErr != nil {
			return nil, fmt.Errorf("actionStatement[%d]: %w", record.offset, statementErr)
		}
		for _, statement := range statements {
			_, _ = fmt.Fprintf(&source, "%s%s\n", indent, statement)
		}
	}
	for len(functionEnds) > 0 && functionEnds[len(functionEnds)-1] == len(payload) {
		functionEnds = functionEnds[:len(functionEnds)-1]
		indent = strings.TrimSuffix(indent, "\t")
		_, _ = fmt.Fprintf(&source, "%s}\n", indent)
	}
	if len(functionEnds) != 0 {
		return nil, fmt.Errorf("functionEnd: %v", functionEnds)
	}
	if label := labels[len(payload)]; label != "" && (!functionEndOffsets[len(payload)] || branchTargets[len(payload)]) {
		_, _ = fmt.Fprintf(&source, "%s%s: {}\n", indent, label)
	}
	liftedSource, err := liftRenderedExpressions(source.String())
	if err != nil {
		return nil, fmt.Errorf("expressionLift: %w", err)
	}
	liftedSource, err = liftRenderedAssignments(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("assignmentLift: %w", err)
	}
	liftedSource, err = liftRenderedMethodCalls(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("methodLift: %w", err)
	}
	liftedSource, err = liftRenderedReturnGuards(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("returnGuardLift: %w", err)
	}
	liftedSource, err = liftRenderedMemberFunctions(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("memberFunctionLift: %w", err)
	}
	liftedSource, err = liftRenderedConditionalValues(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("conditionalValueLift: %w", err)
	}
	liftedSource, err = liftRenderedNullGuards(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("nullGuardLift: %w", err)
	}
	liftedSource, err = liftRenderedPredicateGuards(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("predicateGuardLift: %w", err)
	}
	liftedSource, err = liftRenderedObjectInitializers(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("objectLift: %w", err)
	}
	liftedSource, err = liftRenderedClasses(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("classLift: %w", err)
	}
	liftedSource, err = liftRenderedEnumClasses(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("classLift: %w", err)
	}
	liftedSource, err = liftRenderedRootClasses(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("rootClassLift: %w", err)
	}
	liftedSource, err = liftRenderedFunction1Metadata(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("function1Lift: %w", err)
	}
	liftedSource, err = liftRenderedCallbackReferences(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("callbackLift: %w", err)
	}
	liftedSource, err = liftRenderedFunctionAliases(liftedSource)
	if err != nil {
		return nil, fmt.Errorf("functionAliasLift: %w", err)
	}
	return []byte(liftedSource), nil
}

func WriteTypeScriptBlocks(blocks []ActionBlock) ([]byte, error) {
	var source strings.Builder
	source.WriteString("// darkspin reversible AVM1 TypeScript v1\n")
	for blockIndex, block := range blocks {
		blockSource, err := WriteTypeScript(block.Path, block.Payload)
		if err != nil {
			return nil, fmt.Errorf("block[%d]: %w", blockIndex, err)
		}
		content := strings.TrimPrefix(string(blockSource), "// darkspin reversible AVM1 TypeScript v1\n")
		separatorIndex := strings.IndexByte(content, '\n')
		if separatorIndex < 0 {
			return nil, fmt.Errorf("block[%d]: header", blockIndex)
		}
		if blockIndex > 0 {
			source.WriteByte('\n')
		}
		source.WriteString(content[:separatorIndex+1])
		source.WriteString("{\n")
		body := strings.TrimSuffix(content[separatorIndex+1:], "\n")
		if body != "" {
			for _, line := range strings.Split(body, "\n") {
				source.WriteByte('\t')
				source.WriteString(line)
				source.WriteByte('\n')
			}
		}
		source.WriteString("}\n")
	}
	return []byte(source.String()), nil
}

func ReadTypeScriptBlocks(source []byte) (map[string][]byte, error) {
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	blocks := make(map[string][]byte)
	blockPath := ""
	blockStart := 0
	for lineIndex, line := range lines {
		if !strings.HasPrefix(line, "// block: ") {
			continue
		}
		if blockPath != "" {
			err := compileTypeScriptBlock(lines[blockStart:lineIndex], blockPath, blocks)
			if err != nil {
				return nil, fmt.Errorf("block[%s]: %w", blockPath, err)
			}
		}
		blockPath = strings.TrimSpace(strings.TrimPrefix(line, "// block: "))
		if blockPath == "" {
			return nil, fmt.Errorf("line[%d]: blockPath", lineIndex+1)
		}
		blockStart = lineIndex
	}
	if blockPath != "" {
		err := compileTypeScriptBlock(lines[blockStart:], blockPath, blocks)
		if err != nil {
			return nil, fmt.Errorf("block[%s]: %w", blockPath, err)
		}
	}
	return blocks, nil
}

func compileTypeScriptBlock(lines []string, blockPath string, blocks map[string][]byte) error {
	if _, isDuplicate := blocks[blockPath]; isDuplicate {
		return fmt.Errorf("duplicate: %q", blockPath)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 3 || strings.TrimSpace(lines[1]) != "{" || strings.TrimSpace(lines[len(lines)-1]) != "}" {
		return errors.New("scope")
	}
	payload, err := ReadTypeScript([]byte(strings.Join(lines[2:len(lines)-1], "\n")))
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	blocks[blockPath] = payload
	return nil
}

func ReadTypeScript(source []byte) ([]byte, error) {
	expandedSource, err := expandTypeScriptMemberFunctions(string(source))
	if err != nil {
		return nil, fmt.Errorf("memberFunctionExpand: %w", err)
	}
	expandedSource, err = expandTypeScriptClasses(expandedSource)
	if err != nil {
		return nil, fmt.Errorf("classExpand: %w", err)
	}
	expandedSource, err = expandTypeScriptRootClasses(expandedSource)
	if err != nil {
		return nil, fmt.Errorf("rootClassExpand: %w", err)
	}
	expandedSource, err = expandTypeScriptFunctionAliases(expandedSource)
	if err != nil {
		return nil, fmt.Errorf("functionAliasExpand: %w", err)
	}
	expandedSource, err = expandTypeScriptReturnGuards(expandedSource)
	if err != nil {
		return nil, fmt.Errorf("returnGuardExpand: %w", err)
	}
	expandedSource, err = expandTypeScriptConditionalValues(expandedSource)
	if err != nil {
		return nil, fmt.Errorf("conditionalValueExpand: %w", err)
	}
	expandedSource, err = expandTypeScriptNullGuards(expandedSource)
	if err != nil {
		return nil, fmt.Errorf("nullGuardExpand: %w", err)
	}
	expandedSource, err = expandTypeScriptPredicateGuards(expandedSource)
	if err != nil {
		return nil, fmt.Errorf("predicateGuardExpand: %w", err)
	}
	rows := make([][]any, 0, 32)
	labelsByRow := make(map[int]string)
	var pendingFunction []any
	var pendingClass *typeScriptClass
	pendingClassPath := ""
	isClassGuardCloseExpected := false
	functionLabels := make([]string, 0, 4)
	lines := strings.Split(strings.ReplaceAll(expandedSource, "\r\n", "\n"), "\n")
	constantPool, isConstantPoolInferred := inferTypeScriptConstants(lines)
	if isConstantPoolInferred && len(constantPool) > 0 {
		constantRow := []any{"CONSTANT_POOL"}
		for _, constant := range constantPool {
			constantRow = append(constantRow, constant)
		}
		rows = append(rows, constantRow)
	}
	for namespaceIndex, namespacePath := range inferTypeScriptNamespaces(lines) {
		label := fmt.Sprintf("AUTO_NAMESPACE_%06d", namespaceIndex)
		namespaceRows, namespaceErr := parseObjectInitializer(namespacePath+" ||= {};", constantPool, label)
		if namespaceErr != nil {
			return nil, fmt.Errorf("namespace[%d]: %w", namespaceIndex, namespaceErr)
		}
		rows = append(rows, namespaceRows...)
		labelsByRow[len(rows)] = label
		rows = append(rows, []any{"POP"})
	}
	for lineIndex, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "export {};" || strings.HasPrefix(line, "//") {
			continue
		}
		if isClassGuardCloseExpected {
			if line != "}" {
				return nil, fmt.Errorf("line[%d]: classGuardClose", lineIndex+1)
			}
			isClassGuardCloseExpected = false
			continue
		}
		if pendingClass != nil {
			if line == "};" {
				classRows, classLabels, classErr := parseEnumClass(pendingClass, constantPool, lineIndex+1)
				if classErr != nil {
					return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, classErr)
				}
				rowOffset := len(rows)
				rows = append(rows, classRows...)
				for relativeRow, label := range classLabels {
					labelsByRow[rowOffset+relativeRow] = label
				}
				pendingClass = nil
				isClassGuardCloseExpected = true
				continue
			}
			property, propertyErr := parseTypeScriptClassProperty(line)
			if propertyErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, propertyErr)
			}
			pendingClass.properties = append(pendingClass.properties, property)
			continue
		}
		if pendingClassPath != "" {
			class, classErr := parseTypeScriptClassHeader(line, pendingClassPath)
			if classErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, classErr)
			}
			pendingClass = class
			pendingClassPath = ""
			continue
		}
		if strings.HasPrefix(line, "if (!") && strings.HasSuffix(line, ") {") {
			classPath, classErr := parseTypeScriptClassGuard(line)
			if classErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, classErr)
			}
			pendingClassPath = classPath
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.Contains(line, "(") {
			label := strings.TrimSuffix(line, ":")
			if label == "" {
				return nil, fmt.Errorf("line[%d]: labelName", lineIndex+1)
			}
			labelsByRow[len(rows)] = label
			continue
		}
		if strings.HasSuffix(line, ": {}") {
			label := strings.TrimSuffix(line, ": {}")
			if label == "" {
				return nil, fmt.Errorf("line[%d]: labelName", lineIndex+1)
			}
			labelsByRow[len(rows)] = label
			continue
		}
		if line == "}" {
			if len(functionLabels) == 0 {
				return nil, fmt.Errorf("line[%d]: functionClose", lineIndex+1)
			}
			label := functionLabels[len(functionLabels)-1]
			functionLabels = functionLabels[:len(functionLabels)-1]
			labelsByRow[len(rows)] = label
			continue
		}
		if strings.HasPrefix(line, "function ") {
			if len(pendingFunction) == 0 {
				row, label, isInlineFunction, isInferredFunction, functionErr := parseInferredFunction1(line, lineIndex+1)
				if functionErr != nil {
					return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, functionErr)
				}
				if !isInferredFunction {
					return nil, fmt.Errorf("line[%d]: functionMetadata", lineIndex+1)
				}
				rows = append(rows, row)
				if isInlineFunction {
					labelsByRow[len(rows)] = label
				} else {
					functionLabels = append(functionLabels, label)
				}
				continue
			}
			row, functionErr := parseActionFunction(line, pendingFunction)
			if functionErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, functionErr)
			}
			rows = append(rows, row)
			label := rowFieldStringOrEmpty(row, len(row)-1)
			functionLabels = append(functionLabels, label)
			pendingFunction = nil
			continue
		}
		if !strings.HasPrefix(line, "avm1.") && strings.Contains(line, " = ") && strings.HasSuffix(line, ";") {
			assignmentRows, assignmentErr := parseMemberAssignment(line, constantPool)
			if assignmentErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, assignmentErr)
			}
			rows = append(rows, assignmentRows...)
			continue
		}
		if !strings.HasPrefix(line, "avm1.") && strings.HasSuffix(line, " ||= {};") {
			label := fmt.Sprintf("AUTO_OBJECT_%06d", lineIndex+1)
			initializerRows, initializerErr := parseObjectInitializer(line, constantPool, label)
			if initializerErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, initializerErr)
			}
			rows = append(rows, initializerRows...)
			labelsByRow[len(rows)] = label
			rows = append(rows, []any{"POP"})
			continue
		}
		if !strings.HasPrefix(line, "avm1.") && strings.Contains(line, ".") && strings.HasSuffix(line, ");") {
			callRows, callErr := parseMemberCall(line, constantPool)
			if callErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, callErr)
			}
			rows = append(rows, callRows...)
			continue
		}
		row, parseErr := parseActionStatement(line)
		if parseErr != nil {
			return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, parseErr)
		}
		if len(row) == 0 {
			return nil, fmt.Errorf("line[%d]: instructionEmpty", lineIndex+1)
		}
		_, isString := row[0].(string)
		if !isString {
			return nil, fmt.Errorf("line[%d]: instructionName", lineIndex+1)
		}
		name := rowFieldStringOrEmpty(row, 0)
		if name == "FUNCTION1" || name == "FUNCTION2" {
			if len(pendingFunction) != 0 {
				return nil, fmt.Errorf("line[%d]: functionMetadataDuplicate", lineIndex+1)
			}
			pendingFunction = row
			continue
		}
		if name == "PUSH_CONSTANT" || name == "PUSH_VARIABLE" {
			resolvedRows, resolveErr := resolveNamedPush(row, constantPool)
			if resolveErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, resolveErr)
			}
			rows = append(rows, resolvedRows...)
			continue
		}
		if name == "GET_MEMBER" && len(row) > 1 {
			resolvedRows, resolveErr := resolveMemberPath(row, constantPool)
			if resolveErr != nil {
				return nil, fmt.Errorf("line[%d]: %w", lineIndex+1, resolveErr)
			}
			rows = append(rows, resolvedRows...)
			continue
		}
		rows = append(rows, row)
		if name == "CONSTANT_POOL" {
			constantPool = actionConstantPool(row)
		}
	}
	if pendingClass != nil || pendingClassPath != "" || isClassGuardCloseExpected || len(pendingFunction) != 0 || len(functionLabels) != 0 {
		return nil, errors.New("functionUnclosed")
	}
	lastName := ""
	if len(rows) > 0 {
		lastName, _ = rowFieldString(rows[len(rows)-1], 0)
	}
	if lastName != "END" {
		rows = append(rows, []any{"END"})
	}
	offsets := []int{0}
	for rowIndex, row := range rows {
		recordPayload, err := assembleAction(row, 0)
		if err != nil {
			return nil, fmt.Errorf("instruction[%d]: %w", rowIndex, err)
		}
		if len(recordPayload) > math.MaxInt-offsets[rowIndex] {
			return nil, fmt.Errorf("instructionSize[%d]: exceeds int", rowIndex)
		}
		offsets = append(offsets, offsets[rowIndex]+len(recordPayload))
	}
	labelOffsets := make(map[string]int, len(labelsByRow))
	for rowIndex, label := range labelsByRow {
		if _, isDuplicate := labelOffsets[label]; isDuplicate {
			return nil, fmt.Errorf("labelDuplicate: %q", label)
		}
		labelOffsets[label] = offsets[rowIndex]
	}
	var payload bytes.Buffer
	for rowIndex, row := range rows {
		recordPayload, err := assembleAction(row, offsets[rowIndex])
		if err != nil {
			return nil, fmt.Errorf("instruction[%d]: %w", rowIndex, err)
		}
		name, isName := row[0].(string)
		if !isName {
			return nil, fmt.Errorf("instruction[%d]: name", rowIndex)
		}
		if name == "JUMP" || name == "IF" {
			label, isLabel := rowFieldString(row, 1)
			if !isLabel {
				return nil, fmt.Errorf("instruction[%d]: branchLabel", rowIndex)
			}
			targetOffset, isFound := labelOffsets[label]
			if !isFound {
				return nil, fmt.Errorf("instruction[%d]: labelMissing %q", rowIndex, label)
			}
			relativeOffset := targetOffset - (offsets[rowIndex] + len(recordPayload))
			if relativeOffset < math.MinInt16 || relativeOffset > math.MaxInt16 {
				return nil, fmt.Errorf("instruction[%d]: branchRange", rowIndex)
			}
			binary.LittleEndian.PutUint16(recordPayload[len(recordPayload)-2:], uint16(int16(relativeOffset)))
		}
		if name == "DEFINE_FUNCTION" || name == "DEFINE_FUNCTION2" || name == "WITH" {
			labelIndex := len(row) - 1
			label, isLabel := rowFieldString(row, labelIndex)
			if !isLabel {
				return nil, fmt.Errorf("instruction[%d]: blockLabel", rowIndex)
			}
			targetOffset, isFound := labelOffsets[label]
			if !isFound {
				return nil, fmt.Errorf("instruction[%d]: labelMissing %q", rowIndex, label)
			}
			codeSize := targetOffset - (offsets[rowIndex] + len(recordPayload))
			if codeSize < 0 || codeSize > math.MaxUint16 {
				return nil, fmt.Errorf("instruction[%d]: blockRange", rowIndex)
			}
			binary.LittleEndian.PutUint16(recordPayload[len(recordPayload)-2:], uint16(codeSize))
		}
		_, _ = payload.Write(recordPayload)
	}
	return payload.Bytes(), nil
}

func inferTypeScriptNamespaces(lines []string) []string {
	paths := make(map[string]bool, 8)
	for lineIndex, sourceLine := range lines {
		line := strings.TrimSpace(sourceLine)
		if strings.HasSuffix(line, " ||= {};") {
			continue
		}
		for _, path := range globalPathsInLine(line) {
			parts := strings.Split(path, ".")
			for memberCount := 1; memberCount < len(parts)-1; memberCount++ {
				paths[strings.Join(parts[:memberCount+1], ".")] = true
			}
		}
		if line != "avm1.pushVariable(\"_global\");" || lineIndex+1 >= len(lines) {
			continue
		}
		row, err := parseActionStatement(strings.TrimSpace(lines[lineIndex+1]))
		if err != nil {
			continue
		}
		name := rowFieldStringOrEmpty(row, 0)
		if name != "GET_MEMBER" || len(row) < 3 {
			continue
		}
		members := make([]string, 0, len(row)-1)
		for fieldIndex := 1; fieldIndex < len(row); fieldIndex++ {
			member, isMember := rowFieldString(row, fieldIndex)
			if !isMember || !isActionIdentifier(member) {
				members = nil
				break
			}
			members = append(members, member)
		}
		for memberCount := 1; memberCount < len(members); memberCount++ {
			paths["_global."+strings.Join(members[:memberCount], ".")] = true
		}
	}
	namespaces := make([]string, 0, len(paths))
	for path := range paths {
		namespaces = append(namespaces, path)
	}
	sort.Slice(namespaces, func(firstIndex, secondIndex int) bool {
		firstDepth := strings.Count(namespaces[firstIndex], ".")
		secondDepth := strings.Count(namespaces[secondIndex], ".")
		if firstDepth != secondDepth {
			return firstDepth < secondDepth
		}
		return namespaces[firstIndex] < namespaces[secondIndex]
	})
	return namespaces
}

func globalPathsInLine(line string) []string {
	paths := make([]string, 0, 2)
	for offset := 0; offset < len(line); {
		index := strings.Index(line[offset:], "_global.")
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len("_global")
		for end < len(line) {
			character := line[end]
			if character != '.' && character != '_' && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
				break
			}
			end++
		}
		path := strings.TrimSuffix(line[start:end], ".")
		if isTypeScriptPath(path) {
			paths = append(paths, path)
		}
		offset = end
	}
	return paths
}

func inferTypeScriptConstants(lines []string) ([]string, bool) {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "avm1.constantPool(") {
			return nil, false
		}
	}
	constants := make([]string, 0, 128)
	seenConstants := make(map[string]bool, 128)
	for _, sourceLine := range lines {
		line := strings.TrimSpace(sourceLine)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		row, err := parseActionStatement(line)
		if err == nil {
			name := rowFieldStringOrEmpty(row, 0)
			switch name {
			case "PUSH_CONSTANT", "PUSH_VARIABLE":
				constant, isConstant := rowFieldString(row, 1)
				if isConstant {
					constants = appendUniqueConstant(constants, seenConstants, constant)
				}
			case "GET_MEMBER":
				for fieldIndex := 1; fieldIndex < len(row); fieldIndex++ {
					constant, isConstant := rowFieldString(row, fieldIndex)
					if isConstant {
						constants = appendUniqueConstant(constants, seenConstants, constant)
					}
				}
			}
		}
		for _, identifier := range typeScriptLineIdentifiers(line) {
			constants = appendUniqueConstant(constants, seenConstants, identifier)
		}
	}
	return constants, true
}

func appendUniqueConstant(constants []string, seenConstants map[string]bool, constant string) []string {
	if constant == "" || seenConstants[constant] {
		return constants
	}
	seenConstants[constant] = true
	return append(constants, constant)
}

func typeScriptLineIdentifiers(line string) []string {
	identifiers := make([]string, 0, 8)
	start := -1
	isQuoted := false
	isEscaped := false
	for index, character := range line {
		if isEscaped {
			isEscaped = false
			continue
		}
		if isQuoted && character == '\\' {
			isEscaped = true
			continue
		}
		if character == '"' {
			isQuoted = !isQuoted
			if start >= 0 {
				identifiers = append(identifiers, line[start:index])
				start = -1
			}
			continue
		}
		if isQuoted {
			continue
		}
		isIdentifierCharacter := character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || start >= 0 && character >= '0' && character <= '9'
		if isIdentifierCharacter {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 {
			identifiers = append(identifiers, line[start:index])
			start = -1
		}
	}
	if start >= 0 {
		identifiers = append(identifiers, line[start:])
	}
	return identifiers
}

func actionConstantPool(row []any) []string {
	constants := make([]string, 0, len(row)-1)
	for _, field := range row[1:] {
		constant, isConstant := field.(string)
		if !isConstant {
			return nil
		}
		constants = append(constants, constant)
	}
	return constants
}

func canInferConstantPool(records []actionRecord, labels map[int]string) bool {
	constants := []string(nil)
	for _, record := range records {
		row, err := renderAction(record, labels)
		if err != nil {
			return false
		}
		name := rowFieldStringOrEmpty(row, 0)
		if name == "CONSTANT_POOL" {
			constants = actionConstantPool(row)
			continue
		}
		if name != "PUSH" {
			continue
		}
		for _, field := range row[1:] {
			item, isItem := field.([]any)
			if !isItem || len(item) != 2 {
				continue
			}
			kind := rowFieldStringOrEmpty(item, 0)
			if kind != "CONSTANT8" && kind != "CONSTANT16" {
				continue
			}
			index, isIndex := rowFieldNumber(item, 1)
			if !isIndex || index < 0 || index >= len(constants) {
				return false
			}
		}
	}
	return true
}

func liftPushedVariable(row []any, constants []string) (string, []any, bool) {
	if len(row) < 2 {
		return "", row, false
	}
	item, isItem := row[len(row)-1].([]any)
	if !isItem || len(item) != 2 {
		return "", row, false
	}
	kind, isKind := rowFieldString(item, 0)
	if !isKind {
		return "", row, false
	}
	name := ""
	switch kind {
	case "STRING":
		name, _ = rowFieldString(item, 1)
	case "CONSTANT8", "CONSTANT16":
		index, isIndex := rowFieldNumber(item, 1)
		if !isIndex || index < 0 || index >= len(constants) {
			return "", row, false
		}
		name = constants[index]
	default:
		return "", row, false
	}
	if name == "" {
		return "", row, false
	}
	remaining := append([]any(nil), row[:len(row)-1]...)
	return name, remaining, true
}

func liftPushedMember(records []actionRecord, recordIndex int, row []any, constants []string, labels map[int]string) ([]string, []any, int, bool) {
	firstName, remainingRow, isMember := liftPushedName(row, constants)
	if !isMember || recordIndex+1 >= len(records) || records[recordIndex+1].opcode != 0x4E || labels[records[recordIndex+1].offset] != "" {
		return nil, row, 0, false
	}
	names := []string{firstName}
	consumedRecords := 1
	for nextIndex := recordIndex + 2; nextIndex+1 < len(records); nextIndex += 2 {
		if labels[records[nextIndex].offset] != "" || labels[records[nextIndex+1].offset] != "" || records[nextIndex+1].opcode != 0x4E {
			break
		}
		nextRow, renderErr := renderAction(records[nextIndex], labels)
		if renderErr != nil {
			break
		}
		nextName, nextRemaining, isNextMember := liftPushedName(nextRow, constants)
		if !isNextMember || len(nextRemaining) != 1 {
			break
		}
		names = append(names, nextName)
		consumedRecords += 2
	}
	return names, remainingRow, consumedRecords, true
}

func liftMemberAssignment(records []actionRecord, recordIndex int, row []any, constants []string, labels map[int]string) (string, int, bool) {
	members := make([]string, 0, 4)
	baseRow := row
	nextIndex := recordIndex + 1
	firstMember, remainingRow, isFirstMember := liftPushedName(row, constants)
	if isFirstMember && isActionIdentifier(firstMember) && len(remainingRow) == 2 && nextIndex < len(records) && records[nextIndex].opcode == 0x4E && labels[records[nextIndex].offset] == "" {
		baseRow = remainingRow
		members = append(members, firstMember)
		nextIndex++
	}
	baseName, isBase := pushedBaseName(baseRow)
	if !isBase {
		return "", 0, false
	}
	for nextIndex+1 < len(records) && records[nextIndex+1].opcode == 0x4E {
		if labels[records[nextIndex].offset] != "" || labels[records[nextIndex+1].offset] != "" {
			return "", 0, false
		}
		memberRow, renderErr := renderAction(records[nextIndex], labels)
		if renderErr != nil {
			return "", 0, false
		}
		memberName, remainingRow, isMember := liftPushedName(memberRow, constants)
		if !isMember || len(remainingRow) != 1 || !isActionIdentifier(memberName) {
			return "", 0, false
		}
		members = append(members, memberName)
		nextIndex += 2
	}
	if len(members) == 0 || nextIndex >= len(records) {
		return "", 0, false
	}
	propertyRow, propertyErr := renderAction(records[nextIndex], labels)
	if propertyErr != nil || labels[records[nextIndex].offset] != "" {
		return "", 0, false
	}
	propertyName, valueSource, isCombined := pushedPropertyValue(propertyRow, constants)
	setIndex := 0
	if isCombined {
		setIndex = nextIndex + 1
	} else {
		if nextIndex+2 >= len(records) || records[nextIndex+2].opcode != 0x4F || labels[records[nextIndex+1].offset] != "" || labels[records[nextIndex+2].offset] != "" {
			return "", 0, false
		}
		propertyRemaining := []any(nil)
		var isProperty bool
		propertyName, propertyRemaining, isProperty = liftPushedName(propertyRow, constants)
		if !isProperty || len(propertyRemaining) != 1 || !isActionIdentifier(propertyName) {
			return "", 0, false
		}
		valueRow, valueErr := renderAction(records[nextIndex+1], labels)
		if valueErr != nil {
			return "", 0, false
		}
		var isValue bool
		valueSource, isValue = simplePushedSource(valueRow)
		if !isValue {
			return "", 0, false
		}
		setIndex = nextIndex + 2
	}
	if setIndex >= len(records) || records[setIndex].opcode != 0x4F || labels[records[setIndex].offset] != "" || !isActionIdentifier(propertyName) {
		return "", 0, false
	}
	members = append(members, propertyName)
	left := baseName + "." + strings.Join(members, ".")
	return fmt.Sprintf("%s = %s;", left, valueSource), setIndex - recordIndex, true
}

func pushedPropertyValue(row []any, constants []string) (string, string, bool) {
	if len(row) != 3 {
		return "", "", false
	}
	propertyRow := []any{"PUSH", row[1]}
	propertyName, remainingRow, isProperty := liftPushedName(propertyRow, constants)
	if !isProperty || len(remainingRow) != 1 {
		return "", "", false
	}
	valueSource, isValue := simplePushedSource([]any{"PUSH", row[2]})
	if !isValue {
		return "", "", false
	}
	return propertyName, valueSource, true
}

func pushedBaseName(row []any) (string, bool) {
	if len(row) != 2 {
		return "", false
	}
	item, isItem := row[1].([]any)
	if !isItem || len(item) != 2 {
		return "", false
	}
	kind := rowFieldStringOrEmpty(item, 0)
	if kind != "REGISTER" {
		return "", false
	}
	register, isRegister := rowFieldNumber(item, 1)
	if !isRegister || register < 0 || register > math.MaxUint8 {
		return "", false
	}
	return fmt.Sprintf("register%d", register), true
}

func simplePushedSource(row []any) (string, bool) {
	if len(row) != 2 {
		return "", false
	}
	item, isItem := row[1].([]any)
	if !isItem || len(item) == 0 {
		return "", false
	}
	kind := rowFieldStringOrEmpty(item, 0)
	switch kind {
	case "BOOL":
		if len(item) != 2 {
			return "", false
		}
		flag, isFlag := item[1].(bool)
		if !isFlag {
			return "", false
		}
		return strconv.FormatBool(flag), true
	case "NULL":
		return "null", true
	case "STRING":
		text, isText := rowFieldString(item, 1)
		if !isText {
			return "", false
		}
		return strconv.Quote(text), true
	case "INT":
		number := item[1]
		payload, err := json.Marshal(number)
		if err != nil {
			return "", false
		}
		return string(payload), true
	case "DOUBLE":
		number, isNumber := item[1].(float64)
		if !isNumber {
			return "", false
		}
		source := strconv.FormatFloat(number, 'g', -1, 64)
		if !strings.ContainsAny(source, ".eE") {
			source += ".0"
		}
		return source, true
	case "REGISTER":
		register, isRegister := rowFieldNumber(item, 1)
		if !isRegister {
			return "", false
		}
		return fmt.Sprintf("register%d", register), true
	default:
		return "", false
	}
}

func parseMemberAssignment(line string, constants []string) ([][]any, error) {
	line = strings.TrimSuffix(line, ";")
	parts := strings.SplitN(line, " = ", 2)
	if len(parts) != 2 {
		return nil, errors.New("assignmentFields")
	}
	pathParts := strings.Split(strings.TrimSpace(parts[0]), ".")
	if len(pathParts) == 1 && isTypeScriptIdentifier(pathParts[0]) {
		nameItem := namedPushItem(pathParts[0], constants)
		expressionRows, expressionErr := parseTypeScriptExpression(strings.TrimSpace(parts[1]), constants)
		if expressionErr != nil {
			return nil, fmt.Errorf("assignmentValue: %w", expressionErr)
		}
		rows := [][]any{{"PUSH", nameItem}}
		rows = append(rows, expressionRows...)
		rows = append(rows, []any{"SET_VARIABLE"})
		return rows, nil
	}
	if len(pathParts) < 2 || !isTypeScriptReferencePath(strings.Join(pathParts, ".")) {
		return nil, errors.New("assignmentPath")
	}
	rows, err := parseReferencePath(pathParts[:len(pathParts)-1], constants)
	if err != nil {
		return nil, fmt.Errorf("assignmentTarget: %w", err)
	}
	propertyItem := namedPushItem(pathParts[len(pathParts)-1], constants)
	rows = append(rows, []any{"PUSH", propertyItem})
	rightSource := strings.TrimSpace(parts[1])
	comparisonParts := strings.SplitN(rightSource, " == ", 2)
	if len(comparisonParts) == 2 {
		firstRows, expressionErr := parseMemberExpression(comparisonParts[0], constants)
		if expressionErr != nil {
			return nil, fmt.Errorf("assignmentFirst: %w", expressionErr)
		}
		secondRows, expressionErr := parseMemberExpression(comparisonParts[1], constants)
		if expressionErr != nil {
			return nil, fmt.Errorf("assignmentSecond: %w", expressionErr)
		}
		rows = append(rows, firstRows...)
		rows = append(rows, secondRows...)
		rows = append(rows, []any{"EQUALS2"})
	} else {
		expressionRows, expressionErr := parseTypeScriptExpression(rightSource, constants)
		if expressionErr != nil {
			return nil, fmt.Errorf("assignmentValue: %w", expressionErr)
		}
		rows = append(rows, expressionRows...)
	}
	rows = append(rows, []any{"SET_MEMBER"})
	return rows, nil
}

func parseMemberExpression(source string, constants []string) ([][]any, error) {
	parts := strings.Split(strings.TrimSpace(source), ".")
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "register") {
		return nil, fmt.Errorf("path: %q", source)
	}
	register, err := strconv.ParseUint(strings.TrimPrefix(parts[0], "register"), 10, 8)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	rows := [][]any{{"PUSH", []any{"REGISTER", float64(register)}}}
	for memberIndex, memberName := range parts[1:] {
		if !isActionIdentifier(memberName) {
			return nil, fmt.Errorf("member[%d]: %q", memberIndex, memberName)
		}
		item := namedPushItem(memberName, constants)
		rows = append(rows, []any{"PUSH", item}, []any{"GET_MEMBER"})
	}
	return rows, nil
}

func liftRenderedAssignments(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		assignment, isAssignment := renderedEqualityAssignment(lines[lineIndex:])
		if isAssignment {
			liftedLines = append(liftedLines, assignment)
			lineIndex += 9
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func liftRenderedMethodCalls(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		call, consumedLines, isCall := renderedMethodCall(lines[lineIndex:])
		if isCall {
			liftedLines = append(liftedLines, call)
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func liftRenderedObjectInitializers(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		initializer, consumedLines, isInitializer := renderedObjectInitializer(lines[lineIndex:])
		if isInitializer {
			if strings.HasPrefix(initializer, "\t") {
				liftedLines = append(liftedLines, initializer)
			}
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func liftRenderedEnumClasses(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		declaration, consumedLines, isClass := renderedEnumClass(lines[lineIndex:])
		if isClass {
			liftedLines = append(liftedLines, declaration...)
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func liftRenderedFunction1Metadata(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		if lineIndex+1 >= len(lines) {
			liftedLines = append(liftedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		metadata, metadataErr := parseActionStatement(strings.TrimSpace(lines[lineIndex]))
		metadataName := rowFieldStringOrEmpty(metadata, 0)
		if metadataErr != nil || metadataName != "FUNCTION1" || len(metadata) != 4 || !isInferableFunction1Metadata(metadata) {
			liftedLines = append(liftedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		_, functionErr := parseActionFunction(strings.TrimSpace(lines[lineIndex+1]), metadata)
		if functionErr != nil {
			liftedLines = append(liftedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		functionEnd := matchingTypeScriptBrace(lines, lineIndex+1)
		endLabel := rowFieldStringOrEmpty(metadata, 2)
		if functionEnd >= 0 && functionEnd+1 < len(lines) && strings.TrimSpace(lines[functionEnd+1]) == endLabel+":" {
			liftedLines = append(liftedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		if lineIndex+2 < len(lines) && strings.TrimSpace(lines[lineIndex+2]) == "}" {
			declaration := strings.TrimSuffix(lines[lineIndex+1], "{") + "{}"
			liftedLines = append(liftedLines, declaration)
			lineIndex += 3
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex+1])
		lineIndex += 2
	}
	return strings.Join(liftedLines, "\n"), nil
}

func isInferableFunction1Metadata(metadata []any) bool {
	parameters, areParameters := metadata[1].([]any)
	originalName, isOriginalName := rowFieldString(metadata, 3)
	if !areParameters || !isOriginalName || originalName != "" && !isTypeScriptIdentifier(originalName) {
		return false
	}
	for _, parameter := range parameters {
		parameterName, isParameterName := parameter.(string)
		if !isParameterName || !isTypeScriptIdentifier(parameterName) {
			return false
		}
	}
	return true
}

func liftRenderedCallbackReferences(source string) (string, error) {
	lines := strings.Split(source, "\n")
	functionNames := make(map[string]bool)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "function ") {
			continue
		}
		nameEnd := strings.IndexByte(strings.TrimPrefix(line, "function "), '(')
		if nameEnd > 0 {
			functionNames[strings.TrimPrefix(line, "function ")[:nameEnd]] = true
		}
	}
	for lineIndex, line := range lines {
		trimmed := strings.TrimSpace(line)
		methodIndex := strings.Index(trimmed, ".addEventListener(")
		if methodIndex < 0 || !strings.HasSuffix(trimmed, ");") {
			continue
		}
		argumentStart := methodIndex + len(".addEventListener(")
		arguments, err := splitMemberCallArguments(trimmed[argumentStart : len(trimmed)-2])
		if err != nil || len(arguments) != 3 || arguments[1] != "this" {
			continue
		}
		callbackName, err := strconv.Unquote(arguments[2])
		if err != nil || !functionNames[callbackName] {
			continue
		}
		arguments[2] = callbackName
		indent := line[:len(line)-len(strings.TrimLeft(line, "\t"))]
		lines[lineIndex] = indent + trimmed[:argumentStart] + strings.Join(arguments, ", ") + ");"
	}
	return strings.Join(lines, "\n"), nil
}

func renderedEnumClass(lines []string) ([]string, int, bool) {
	if len(lines) < 29 {
		return nil, 0, false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	baseName, isBase := renderedVariable(lines[0])
	members, areMembers := renderedMembers(lines[1])
	if !isBase || baseName != "_global" || len(members) < 2 || !areMembers {
		return nil, 0, false
	}
	if strings.TrimSpace(lines[2]) != "avm1.not();" || strings.TrimSpace(lines[3]) != "avm1.not();" {
		return nil, 0, false
	}
	branchLabel, isBranch := renderedBranch(lines[4], "IF")
	assignmentBase, isAssignmentBase := renderedVariable(lines[5])
	if !isBranch || !isAssignmentBase || assignmentBase != members[0] {
		return nil, 0, false
	}
	lineIndex := 6
	parentMembers := members[1 : len(members)-1]
	if len(parentMembers) > 0 {
		assignmentMembers, areAssignmentMembers := renderedMembers(lines[lineIndex])
		if !areAssignmentMembers || !slices.Equal(assignmentMembers, parentMembers) {
			return nil, 0, false
		}
		lineIndex++
	}
	className, isClassName := renderedConstant(lines[lineIndex])
	if !isClassName || className != members[len(members)-1] || !isTypeScriptIdentifier(className) {
		return nil, 0, false
	}
	lineIndex++
	constructorEnd, isConstructor := renderedEmptyFunction1(lines[lineIndex:])
	if !isConstructor {
		return nil, 0, false
	}
	lineIndex += 3
	if strings.TrimSpace(lines[lineIndex]) != "avm1.storeRegister(1);" || strings.TrimSpace(lines[lineIndex+1]) != "avm1.setMember();" || strings.TrimSpace(lines[lineIndex+2]) != "avm1.pushRegister(1);" || strings.TrimSpace(lines[lineIndex+3]) != "avm1.getMember(\"prototype\");" || strings.TrimSpace(lines[lineIndex+4]) != "avm1.storeRegister(2);" || strings.TrimSpace(lines[lineIndex+5]) != "avm1.pop();" {
		return nil, 0, false
	}
	_ = constructorEnd
	lineIndex += 6
	properties := make([]typeScriptClassProperty, 0, 16)
	for lineIndex+3 < len(lines) {
		property, isProperty := renderedStaticClassProperty(lines[lineIndex:])
		if !isProperty {
			break
		}
		properties = append(properties, property)
		lineIndex += 4
	}
	if len(properties) == 0 || lineIndex+8 >= len(lines) {
		return nil, 0, false
	}
	if strings.TrimSpace(lines[lineIndex]) != "avm1.pushInt(1);" || strings.TrimSpace(lines[lineIndex+1]) != "avm1.pushNull();" {
		return nil, 0, false
	}
	flagsBase, isFlagsBase := renderedVariable(lines[lineIndex+2])
	flagsMembers, areFlagsMembers := renderedMembers(lines[lineIndex+3])
	expectedFlagsMembers := append(append([]string(nil), members[1:]...), "prototype")
	if !isFlagsBase || flagsBase != members[0] || !areFlagsMembers || !slices.Equal(flagsMembers, expectedFlagsMembers) {
		return nil, 0, false
	}
	if strings.TrimSpace(lines[lineIndex+4]) != "avm1.pushInt(3);" {
		return nil, 0, false
	}
	flagsMethod, isFlagsMethod := renderedConstant(lines[lineIndex+5])
	if !isFlagsMethod || flagsMethod != "ASSetPropFlags" || strings.TrimSpace(lines[lineIndex+6]) != "avm1.callFunction();" || strings.TrimSpace(lines[lineIndex+7]) != branchLabel+":" || strings.TrimSpace(lines[lineIndex+8]) != "avm1.pop();" {
		return nil, 0, false
	}
	consumedLines := lineIndex + 9
	for consumedIndex := 0; consumedIndex < consumedLines; consumedIndex++ {
		if !strings.HasPrefix(lines[consumedIndex], indent) || strings.HasPrefix(strings.TrimPrefix(lines[consumedIndex], indent), "\t") {
			return nil, 0, false
		}
	}
	declaration := make([]string, 0, len(properties)+4)
	path := baseName + "." + strings.Join(members, ".")
	declaration = append(declaration, fmt.Sprintf("%sif (!%s) {", indent, path))
	declaration = append(declaration, fmt.Sprintf("%s\t%s = class %s {", indent, path, className))
	for _, property := range properties {
		declaration = append(declaration, fmt.Sprintf("%s\t\tstatic readonly %s = %s;", indent, property.name, property.source))
	}
	declaration = append(declaration, indent+"\t};")
	declaration = append(declaration, indent+"}")
	return declaration, consumedLines, true
}

func renderedEmptyFunction1(lines []string) (string, bool) {
	if len(lines) < 3 {
		return "", false
	}
	row, err := parseActionStatement(strings.TrimSpace(lines[0]))
	if err != nil {
		return "", false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name != "FUNCTION1" || len(row) != 4 {
		return "", false
	}
	parameters, areParameters := row[1].([]any)
	endLabel, isEndLabel := rowFieldString(row, 2)
	originalName, isOriginalName := rowFieldString(row, 3)
	if !areParameters || len(parameters) != 0 || !isEndLabel || !isOriginalName || originalName != "" {
		return "", false
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[1]), "function anonymous_") || !strings.HasSuffix(strings.TrimSpace(lines[1]), "(): void {") || strings.TrimSpace(lines[2]) != "}" {
		return "", false
	}
	return endLabel, true
}

func renderedStaticClassProperty(lines []string) (typeScriptClassProperty, bool) {
	if len(lines) < 4 || strings.TrimSpace(lines[0]) != "avm1.pushRegister(1);" || strings.TrimSpace(lines[3]) != "avm1.setMember();" {
		return typeScriptClassProperty{}, false
	}
	name, isName := renderedConstant(lines[1])
	if !isName || !isTypeScriptIdentifier(name) {
		return typeScriptClassProperty{}, false
	}
	row, err := parseActionStatement(strings.TrimSpace(lines[2]))
	if err != nil {
		return typeScriptClassProperty{}, false
	}
	source, isSource := simplePushedSource(row)
	if !isSource {
		constant, isConstant := renderedConstant(lines[2])
		if !isConstant {
			return typeScriptClassProperty{}, false
		}
		source = strconv.Quote(constant)
	}
	return typeScriptClassProperty{name: name, source: source}, true
}

func renderedObjectInitializer(lines []string) (string, int, bool) {
	if len(lines) < 13 {
		return "", 0, false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	baseName, isBase := renderedVariable(lines[0])
	members, areMembers := renderedMembers(lines[1])
	if !isBase || len(members) == 0 || !areMembers || baseName != "_global" && baseName != "_root" {
		return "", 0, false
	}
	if strings.TrimSpace(lines[2]) != "avm1.not();" || strings.TrimSpace(lines[3]) != "avm1.not();" {
		return "", 0, false
	}
	branchLabel, isBranch := renderedBranch(lines[4], "IF")
	assignmentBase, isAssignmentBase := renderedVariable(lines[5])
	if !isBranch || !isAssignmentBase || assignmentBase != baseName {
		return "", 0, false
	}
	propertyIndex := 6
	if len(members) > 1 {
		assignmentMembers, areAssignmentMembers := renderedMembers(lines[propertyIndex])
		if !areAssignmentMembers || !slices.Equal(assignmentMembers, members[:len(members)-1]) {
			return "", 0, false
		}
		propertyIndex++
	}
	propertyName, isProperty := renderedConstant(lines[propertyIndex])
	if !isProperty || propertyName != members[len(members)-1] {
		return "", 0, false
	}
	if len(lines) < propertyIndex+7 || !isRenderedPushZero(lines[propertyIndex+1]) {
		return "", 0, false
	}
	objectName, isObject := renderedConstant(lines[propertyIndex+2])
	if !isObject || objectName != "Object" || strings.TrimSpace(lines[propertyIndex+3]) != "avm1.newObject();" || strings.TrimSpace(lines[propertyIndex+4]) != "avm1.setMember();" {
		return "", 0, false
	}
	if strings.TrimSpace(lines[propertyIndex+5]) != branchLabel+":" {
		return "", 0, false
	}
	if strings.TrimSpace(lines[propertyIndex+6]) != "avm1.pop();" {
		return "", 0, false
	}
	consumedLines := propertyIndex + 7
	for consumedIndex := 0; consumedIndex < consumedLines; consumedIndex++ {
		if !strings.HasPrefix(lines[consumedIndex], indent) || strings.HasPrefix(strings.TrimPrefix(lines[consumedIndex], indent), "\t") {
			return "", 0, false
		}
	}
	return fmt.Sprintf("%s%s.%s ||= {};", indent, baseName, strings.Join(members, ".")), consumedLines, true
}

func renderedVariable(line string) (string, bool) {
	row, err := parseActionStatement(strings.TrimSpace(line))
	if err != nil {
		return "", false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name != "PUSH_VARIABLE" {
		return "", false
	}
	return rowFieldString(row, 1)
}

func renderedBranch(line, expectedName string) (string, bool) {
	row, err := parseActionStatement(strings.TrimSpace(line))
	if err != nil {
		return "", false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name != expectedName {
		return "", false
	}
	return rowFieldString(row, 1)
}

func isRenderedPushZero(line string) bool {
	row, err := parseActionStatement(strings.TrimSpace(line))
	if err != nil || len(row) != 2 {
		return false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name != "PUSH" {
		return false
	}
	item, isItem := row[1].([]any)
	if !isItem || len(item) != 2 {
		return false
	}
	kind := rowFieldStringOrEmpty(item, 0)
	number, isNumber := rowFieldNumber(item, 1)
	return kind == "DOUBLE" && isNumber && number == 0
}

func parseObjectInitializer(line string, constants []string, label string) ([][]any, error) {
	path := strings.TrimSpace(strings.TrimSuffix(line, " ||= {};"))
	parts := strings.Split(path, ".")
	if len(parts) < 2 || parts[0] != "_global" && parts[0] != "_root" {
		return nil, fmt.Errorf("objectInitializer: %q", path)
	}
	for memberIndex, member := range parts[1:] {
		if !isActionIdentifier(member) {
			return nil, fmt.Errorf("objectInitializerMember[%d]: %q", memberIndex, member)
		}
	}
	baseItem := namedPushItem(parts[0], constants)
	objectItem := namedPushItem("Object", constants)
	rows := [][]any{{"PUSH", baseItem}, {"GET_VARIABLE"}}
	for _, member := range parts[1:] {
		memberItem := namedPushItem(member, constants)
		rows = append(rows, []any{"PUSH", memberItem}, []any{"GET_MEMBER"})
	}
	rows = append(rows, []any{"NOT"}, []any{"NOT"}, []any{"IF", label}, []any{"PUSH", baseItem}, []any{"GET_VARIABLE"})
	for _, member := range parts[1 : len(parts)-1] {
		memberItem := namedPushItem(member, constants)
		rows = append(rows, []any{"PUSH", memberItem}, []any{"GET_MEMBER"})
	}
	propertyItem := namedPushItem(parts[len(parts)-1], constants)
	rows = append(rows, []any{"PUSH", propertyItem}, []any{"PUSH", []any{"DOUBLE", float64(0)}}, []any{"PUSH", objectItem}, []any{"NEW_OBJECT"}, []any{"SET_MEMBER"})
	return rows, nil
}

func parseTypeScriptClassGuard(line string) (string, error) {
	path := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "if (!"), ") {")
	if !isTypeScriptPath(path) || !strings.HasPrefix(path, "_global.") {
		return "", fmt.Errorf("classGuard: %q", line)
	}
	return path, nil
}

func parseTypeScriptClassHeader(line, expectedPath string) (*typeScriptClass, error) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "{"))
	parts := strings.SplitN(line, " = class ", 2)
	if len(parts) != 2 || !isTypeScriptPath(parts[0]) || !isTypeScriptIdentifier(parts[1]) {
		return nil, fmt.Errorf("classHeader: %q", line)
	}
	if parts[0] != expectedPath {
		return nil, fmt.Errorf("classTarget: got %q, want %q", parts[0], expectedPath)
	}
	pathParts := strings.Split(parts[0], ".")
	if len(pathParts) < 3 || pathParts[0] != "_global" || pathParts[len(pathParts)-1] != parts[1] {
		return nil, fmt.Errorf("classPath: %q", parts[0])
	}
	return &typeScriptClass{path: parts[0], name: parts[1]}, nil
}

func parseTypeScriptClassProperty(line string) (typeScriptClassProperty, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "static readonly ") || !strings.HasSuffix(line, ";") {
		return typeScriptClassProperty{}, fmt.Errorf("classProperty: %q", line)
	}
	declaration := strings.TrimSuffix(strings.TrimPrefix(line, "static readonly "), ";")
	parts := strings.SplitN(declaration, " = ", 2)
	if len(parts) != 2 || !isTypeScriptIdentifier(parts[0]) {
		return typeScriptClassProperty{}, fmt.Errorf("classProperty: %q", line)
	}
	_, err := parseAssignmentValue(parts[1])
	if err != nil {
		return typeScriptClassProperty{}, fmt.Errorf("classPropertyValue: %w", err)
	}
	return typeScriptClassProperty{name: parts[0], source: parts[1]}, nil
}

func parseEnumClass(class *typeScriptClass, constants []string, lineNumber int) ([][]any, map[int]string, error) {
	pathParts := strings.Split(class.path, ".")
	if len(pathParts) < 3 || len(class.properties) == 0 {
		return nil, nil, errors.New("classEmpty")
	}
	branchLabel := fmt.Sprintf("AUTO_CLASS_%06d", lineNumber)
	constructorLabel := fmt.Sprintf("AUTO_CONSTRUCTOR_%06d", lineNumber)
	baseItem := namedPushItem(pathParts[0], constants)
	rows := [][]any{{"PUSH", baseItem}, {"GET_VARIABLE"}}
	for _, member := range pathParts[1:] {
		memberItem := namedPushItem(member, constants)
		rows = append(rows, []any{"PUSH", memberItem}, []any{"GET_MEMBER"})
	}
	rows = append(rows, []any{"NOT"}, []any{"NOT"}, []any{"IF", branchLabel})
	assignmentBase := namedPushItem(pathParts[1], constants)
	rows = append(rows, []any{"PUSH", assignmentBase}, []any{"GET_VARIABLE"})
	for _, member := range pathParts[2 : len(pathParts)-1] {
		memberItem := namedPushItem(member, constants)
		rows = append(rows, []any{"PUSH", memberItem}, []any{"GET_MEMBER"})
	}
	classItem := namedPushItem(class.name, constants)
	rows = append(rows, []any{"PUSH", classItem}, []any{"DEFINE_FUNCTION", "", []any{}, constructorLabel})
	labels := map[int]string{len(rows): constructorLabel}
	rows = append(rows, []any{"STORE_REGISTER", float64(1)}, []any{"SET_MEMBER"}, []any{"PUSH", []any{"REGISTER", float64(1)}})
	prototypeItem := namedPushItem("prototype", constants)
	rows = append(rows, []any{"PUSH", prototypeItem}, []any{"GET_MEMBER"}, []any{"STORE_REGISTER", float64(2)}, []any{"POP"})
	for _, property := range class.properties {
		propertyItem := namedPushItem(property.name, constants)
		contentItem, err := parseAssignmentValue(property.source)
		if err != nil {
			return nil, nil, fmt.Errorf("classProperty[%s]: %w", property.name, err)
		}
		if contentText, isText := rowFieldString(contentItem, 1); isText {
			contentItem = namedPushItem(contentText, constants)
		}
		rows = append(rows, []any{"PUSH", []any{"REGISTER", float64(1)}}, []any{"PUSH", propertyItem}, []any{"PUSH", contentItem}, []any{"SET_MEMBER"})
	}
	rows = append(rows, []any{"PUSH", []any{"INT", float64(1)}}, []any{"PUSH", []any{"NULL"}}, []any{"PUSH", assignmentBase}, []any{"GET_VARIABLE"})
	for _, member := range pathParts[2:] {
		memberItem := namedPushItem(member, constants)
		rows = append(rows, []any{"PUSH", memberItem}, []any{"GET_MEMBER"})
	}
	rows = append(rows, []any{"PUSH", prototypeItem}, []any{"GET_MEMBER"}, []any{"PUSH", []any{"INT", float64(3)}})
	flagsItem := namedPushItem("ASSetPropFlags", constants)
	rows = append(rows, []any{"PUSH", flagsItem}, []any{"CALL_FUNCTION"})
	labels[len(rows)] = branchLabel
	rows = append(rows, []any{"POP"})
	return rows, labels, nil
}

func renderedMethodCall(lines []string) (string, int, bool) {
	if len(lines) < 5 {
		return "", 0, false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	arguments := make([]string, 0, 8)
	for countIndex := 0; countIndex < len(lines)-4 && len(arguments) <= math.MaxUint8; {
		argumentCount := len(arguments)
		count, isCount := renderedArgumentCount(lines[countIndex])
		if isCount && count == argumentCount {
			object, objectLines, isObject := renderedCallObject(lines[countIndex+1:])
			if !isObject {
				return "", 0, false
			}
			methodIndex := countIndex + 1 + objectLines
			if methodIndex+2 >= len(lines) {
				return "", 0, false
			}
			method, isMethod := renderedMethodName(lines[methodIndex])
			if !isMethod || !isTypeScriptIdentifier(method) {
				return "", 0, false
			}
			if strings.TrimSpace(lines[methodIndex+1]) != "avm1.callMethod();" || strings.TrimSpace(lines[methodIndex+2]) != "avm1.pop();" {
				return "", 0, false
			}
			consumedLines := methodIndex + 3
			for consumedIndex := 0; consumedIndex < consumedLines; consumedIndex++ {
				if !strings.HasPrefix(lines[consumedIndex], indent) || strings.HasPrefix(strings.TrimPrefix(lines[consumedIndex], indent), "\t") {
					return "", 0, false
				}
			}
			slices.Reverse(arguments)
			return fmt.Sprintf("%s%s.%s(%s);", indent, object, method, strings.Join(arguments, ", ")), consumedLines, true
		}
		argument, argumentLines, isArgument := renderedCallArgument(lines[countIndex:])
		if !isArgument {
			return "", 0, false
		}
		arguments = append(arguments, argument)
		countIndex += argumentLines
	}
	return "", 0, false
}

func renderedArgumentCount(line string) (int, bool) {
	row, err := parseActionStatement(strings.TrimSpace(line))
	if err != nil {
		return 0, false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name != "PUSH" || len(row) != 2 {
		return 0, false
	}
	item, isItem := row[1].([]any)
	if !isItem || len(item) != 2 {
		return 0, false
	}
	kind := rowFieldStringOrEmpty(item, 0)
	count, isCount := rowFieldNumber(item, 1)
	return count, (kind == "INT" || kind == "DOUBLE" && count == 0) && isCount && count >= 0
}

func renderedCallArgument(lines []string) (string, int, bool) {
	if len(lines) == 0 {
		return "", 0, false
	}
	object, consumedLines, isObject := renderedCallObject(lines)
	if isObject {
		return object, consumedLines, true
	}
	row, err := parseActionStatement(strings.TrimSpace(lines[0]))
	if err != nil {
		return "", 0, false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name == "PUSH_CONSTANT" {
		constant, isConstant := rowFieldString(row, 1)
		if isConstant {
			return strconv.Quote(constant), 1, true
		}
		return "", 0, false
	}
	source, isSource := simplePushedSource(row)
	return source, 1, isSource
}

func renderedCallObject(lines []string) (string, int, bool) {
	if len(lines) == 0 {
		return "", 0, false
	}
	row, err := parseActionStatement(strings.TrimSpace(lines[0]))
	if err != nil {
		return "", 0, false
	}
	name := rowFieldStringOrEmpty(row, 0)
	object := ""
	switch name {
	case "PUSH_VARIABLE":
		object, _ = rowFieldString(row, 1)
		if !isTypeScriptReferencePath(object) {
			return "", 0, false
		}
	case "PUSH":
		object, _ = pushedBaseName(row)
		if object == "" {
			return "", 0, false
		}
	default:
		return "", 0, false
	}
	consumedLines := 1
	if len(lines) > 1 {
		members, areMembers := renderedMembers(lines[1])
		if areMembers {
			object += "." + strings.Join(members, ".")
			consumedLines++
		}
	}
	return object, consumedLines, true
}

func renderedMethodName(line string) (string, bool) {
	row, err := parseActionStatement(strings.TrimSpace(line))
	if err != nil {
		return "", false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name == "PUSH_CONSTANT" {
		return rowFieldString(row, 1)
	}
	if name != "PUSH" || len(row) != 2 {
		return "", false
	}
	item, isItem := row[1].([]any)
	if !isItem || len(item) != 2 {
		return "", false
	}
	kind := rowFieldStringOrEmpty(item, 0)
	if kind != "STRING" {
		return "", false
	}
	return rowFieldString(item, 1)
}

func parseMemberCall(line string, constants []string) ([][]any, error) {
	call := strings.TrimSuffix(strings.TrimSpace(line), ");")
	openIndex := strings.IndexByte(call, '(')
	if openIndex <= 0 {
		return nil, errors.New("memberCall")
	}
	pathParts := strings.Split(call[:openIndex], ".")
	if len(pathParts) < 2 {
		return nil, errors.New("memberCallPath")
	}
	for partIndex, part := range pathParts {
		if !isActionIdentifier(part) {
			return nil, fmt.Errorf("memberCallPart[%d]: %q", partIndex, part)
		}
	}
	argumentText := strings.TrimSpace(call[openIndex+1:])
	argumentSources, err := splitMemberCallArguments(argumentText)
	if err != nil {
		return nil, fmt.Errorf("memberCallArguments: %w", err)
	}
	rows := make([][]any, 0)
	methodName := pathParts[len(pathParts)-1]
	for argumentIndex := len(argumentSources) - 1; argumentIndex >= 0; argumentIndex-- {
		argumentSource := argumentSources[argumentIndex]
		var argumentRows [][]any
		if methodName == "addEventListener" && argumentIndex == 2 && isTypeScriptIdentifier(argumentSource) {
			callbackItem := namedPushItem(argumentSource, constants)
			argumentRows = [][]any{{"PUSH", callbackItem}}
		} else {
			argumentRows, err = parseCallArgument(argumentSource, constants)
		}
		if err != nil {
			return nil, fmt.Errorf("memberCallArgument[%d]: %w", argumentIndex, err)
		}
		rows = append(rows, argumentRows...)
	}
	if len(argumentSources) == 0 {
		rows = append(rows, []any{"PUSH", []any{"DOUBLE", float64(0)}})
	} else {
		rows = append(rows, []any{"PUSH", []any{"INT", float64(len(argumentSources))}})
	}
	baseName := pathParts[0]
	if strings.HasPrefix(baseName, "register") {
		register, err := strconv.ParseUint(strings.TrimPrefix(baseName, "register"), 10, 8)
		if err != nil {
			return nil, fmt.Errorf("memberCallRegister: %w", err)
		}
		rows = append(rows, []any{"PUSH", []any{"REGISTER", float64(register)}})
	} else if isTypeScriptIdentifier(baseName) || baseName == "this" {
		item := namedPushItem(baseName, constants)
		rows = append(rows, []any{"PUSH", item}, []any{"GET_VARIABLE"})
	} else {
		return nil, fmt.Errorf("memberCallBase: %q", baseName)
	}
	for _, member := range pathParts[1 : len(pathParts)-1] {
		item := namedPushItem(member, constants)
		rows = append(rows, []any{"PUSH", item}, []any{"GET_MEMBER"})
	}
	methodItem := namedPushItem(methodName, constants)
	rows = append(rows, []any{"PUSH", methodItem}, []any{"CALL_METHOD"}, []any{"POP"})
	return rows, nil
}

func parseCallArgument(source string, constants []string) ([][]any, error) {
	if strings.HasPrefix(source, "\"") {
		text, err := strconv.Unquote(source)
		if err != nil {
			return nil, fmt.Errorf("string: %w", err)
		}
		item := namedPushItem(text, constants)
		return [][]any{{"PUSH", item}}, nil
	}
	item, err := parseAssignmentValue(source)
	if err == nil {
		return [][]any{{"PUSH", item}}, nil
	}
	if !isTypeScriptReferencePath(source) {
		return nil, fmt.Errorf("argument: %q", source)
	}
	parts := strings.Split(source, ".")
	variableItem := namedPushItem(parts[0], constants)
	rows := [][]any{{"PUSH", variableItem}, {"GET_VARIABLE"}}
	for _, member := range parts[1:] {
		memberItem := namedPushItem(member, constants)
		rows = append(rows, []any{"PUSH", memberItem}, []any{"GET_MEMBER"})
	}
	return rows, nil
}

func splitMemberCallArguments(source string) ([]string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, nil
	}
	arguments := make([]string, 0, 4)
	start := 0
	isQuoted := false
	isEscaped := false
	depth := 0
	for index, character := range source {
		if isEscaped {
			isEscaped = false
			continue
		}
		if isQuoted && character == '\\' {
			isEscaped = true
			continue
		}
		if character == '"' {
			isQuoted = !isQuoted
			continue
		}
		if isQuoted {
			continue
		}
		if character == '(' {
			depth++
			continue
		}
		if character == ')' {
			depth--
			if depth < 0 {
				return nil, errors.New("argumentScope")
			}
			continue
		}
		if character != ',' || depth != 0 {
			continue
		}
		argument := strings.TrimSpace(source[start:index])
		if argument == "" {
			return nil, errors.New("argumentEmpty")
		}
		arguments = append(arguments, argument)
		start = index + 1
	}
	if isQuoted || isEscaped || depth != 0 {
		return nil, errors.New("argumentString")
	}
	argument := strings.TrimSpace(source[start:])
	if argument == "" {
		return nil, errors.New("argumentEmpty")
	}
	arguments = append(arguments, argument)
	return arguments, nil
}

func renderedEqualityAssignment(lines []string) (string, bool) {
	if len(lines) < 9 {
		return "", false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	for lineIndex := 1; lineIndex < 9; lineIndex++ {
		if !strings.HasPrefix(lines[lineIndex], indent) {
			return "", false
		}
	}
	targetBase, isTargetBase := renderedRegister(lines[0])
	targetMembers, areTargetMembers := renderedMembers(lines[1])
	propertyName, isProperty := renderedConstant(lines[2])
	firstBase, isFirstBase := renderedRegister(lines[3])
	firstMembers, areFirstMembers := renderedMembers(lines[4])
	secondBase, isSecondBase := renderedRegister(lines[5])
	secondMembers, areSecondMembers := renderedMembers(lines[6])
	if !isTargetBase || !areTargetMembers || !isProperty || !isFirstBase || !areFirstMembers || !isSecondBase || !areSecondMembers {
		return "", false
	}
	if strings.TrimSpace(lines[7]) != "avm1.equals2();" || strings.TrimSpace(lines[8]) != "avm1.setMember();" || !isActionIdentifier(propertyName) {
		return "", false
	}
	left := targetBase + "." + strings.Join(append(targetMembers, propertyName), ".")
	first := firstBase + "." + strings.Join(firstMembers, ".")
	second := secondBase + "." + strings.Join(secondMembers, ".")
	return fmt.Sprintf("%s%s = %s == %s;", indent, left, first, second), true
}

func renderedRegister(line string) (string, bool) {
	row, err := parseActionStatement(strings.TrimSpace(line))
	if err != nil {
		return "", false
	}
	return pushedBaseName(row)
}

func renderedMembers(line string) ([]string, bool) {
	row, err := parseActionStatement(strings.TrimSpace(line))
	if err != nil {
		return nil, false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name != "GET_MEMBER" || len(row) < 2 {
		return nil, false
	}
	members := make([]string, 0, len(row)-1)
	for memberIndex := 1; memberIndex < len(row); memberIndex++ {
		memberName, isMemberName := rowFieldString(row, memberIndex)
		if !isMemberName || !isActionIdentifier(memberName) {
			return nil, false
		}
		members = append(members, memberName)
	}
	return members, true
}

func renderedConstant(line string) (string, bool) {
	row, err := parseActionStatement(strings.TrimSpace(line))
	if err != nil {
		return "", false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name != "PUSH_CONSTANT" {
		return "", false
	}
	return rowFieldString(row, 1)
}

func parseAssignmentValue(source string) ([]any, error) {
	if source == "null" {
		return []any{"NULL"}, nil
	}
	if strings.HasPrefix(source, "register") {
		register, err := strconv.ParseUint(strings.TrimPrefix(source, "register"), 10, 8)
		if err != nil {
			return nil, fmt.Errorf("register: %w", err)
		}
		return []any{"REGISTER", float64(register)}, nil
	}
	if source == "true" || source == "false" {
		flag, err := strconv.ParseBool(source)
		if err != nil {
			return nil, fmt.Errorf("boolean: %w", err)
		}
		return []any{"BOOL", flag}, nil
	}
	if strings.HasPrefix(source, "\"") {
		text, err := strconv.Unquote(source)
		if err != nil {
			return nil, fmt.Errorf("string: %w", err)
		}
		return []any{"STRING", text}, nil
	}
	number, err := strconv.ParseInt(source, 10, 32)
	if err == nil {
		return []any{"INT", float64(number)}, nil
	}
	double, doubleErr := strconv.ParseFloat(source, 64)
	if doubleErr != nil {
		return nil, fmt.Errorf("number: %w", doubleErr)
	}
	return []any{"DOUBLE", double}, nil
}

func liftPushedName(row []any, constants []string) (string, []any, bool) {
	if len(row) < 2 {
		return "", row, false
	}
	item, isItem := row[len(row)-1].([]any)
	if !isItem || len(item) != 2 {
		return "", row, false
	}
	kind, isKind := rowFieldString(item, 0)
	if !isKind {
		return "", row, false
	}
	name := ""
	switch kind {
	case "STRING":
		name, _ = rowFieldString(item, 1)
	case "CONSTANT8", "CONSTANT16":
		index, isIndex := rowFieldNumber(item, 1)
		if !isIndex || index < 0 || index >= len(constants) {
			return "", row, false
		}
		name = constants[index]
	default:
		return "", row, false
	}
	if name == "" {
		return "", row, false
	}
	remaining := append([]any(nil), row[:len(row)-1]...)
	return name, remaining, true
}

func namePushedConstants(row []any, constants []string) []any {
	name := rowFieldStringOrEmpty(row, 0)
	if name != "PUSH" || len(constants) == 0 {
		return row
	}
	namedRow := append([]any(nil), row...)
	for fieldIndex, field := range namedRow[1:] {
		item, isItem := field.([]any)
		if !isItem || len(item) != 2 {
			continue
		}
		kind := rowFieldStringOrEmpty(item, 0)
		if kind != "CONSTANT8" && kind != "CONSTANT16" {
			continue
		}
		index, isIndex := rowFieldNumber(item, 1)
		if !isIndex || index < 0 || index >= len(constants) {
			continue
		}
		namedRow[fieldIndex+1] = []any{"CONSTANT", constants[index]}
	}
	return namedRow
}

func renderActionFunction(row []any, functionIndex int) (string, string, error) {
	name := rowFieldStringOrEmpty(row, 0)
	originalName, isOriginalName := rowFieldString(row, 1)
	if !isOriginalName {
		return "", "", errors.New("functionName")
	}
	functionName := originalName
	if !isTypeScriptIdentifier(functionName) {
		functionName = fmt.Sprintf("anonymous_%03d", functionIndex)
	}
	parameterNames := make([]string, 0, 4)
	parameterRegisters := make([]any, 0, 4)
	metadataFields := make([]any, 0, 6)
	metadataMethod := "function1"
	switch name {
	case "DEFINE_FUNCTION2":
		if len(row) != 6 {
			return "", "", errors.New("function2Fields")
		}
		parameters, areParameters := row[4].([]any)
		if !areParameters {
			return "", "", errors.New("function2Parameters")
		}
		for parameterIndex, field := range parameters {
			parameter, isParameter := field.([]any)
			if !isParameter || len(parameter) != 2 {
				return "", "", fmt.Errorf("function2Parameter[%d]", parameterIndex)
			}
			parameterRegisters = append(parameterRegisters, parameter[0])
			parameterName, isParameterName := rowFieldString(parameter, 1)
			if !isParameterName {
				return "", "", fmt.Errorf("function2Parameter[%d]", parameterIndex)
			}
			parameterNames = append(parameterNames, parameterName)
		}
		metadataMethod = "function2"
		metadataFields = []any{row[2], row[3], parameterRegisters, stringsToFields(parameterNames), row[5], originalName}
	case "DEFINE_FUNCTION":
		if len(row) != 4 {
			return "", "", errors.New("functionFields")
		}
		parameters, areParameters := row[2].([]any)
		if !areParameters {
			return "", "", errors.New("functionParameters")
		}
		for parameterIndex, field := range parameters {
			parameterName, isParameterName := field.(string)
			if !isParameterName {
				return "", "", fmt.Errorf("functionParameter[%d]", parameterIndex)
			}
			parameterNames = append(parameterNames, parameterName)
		}
		metadataFields = []any{stringsToFields(parameterNames), row[3], originalName}
	default:
		return "", "", fmt.Errorf("functionType: %q", name)
	}
	metadata, err := renderActionCall(metadataMethod, metadataFields)
	if err != nil {
		return "", "", err
	}
	visibleParameters := make([]string, len(parameterNames))
	for parameterIndex, parameterName := range parameterNames {
		if isTypeScriptIdentifier(parameterName) {
			visibleParameters[parameterIndex] = parameterName + ": AVM1Value"
		} else {
			visibleParameters[parameterIndex] = fmt.Sprintf("argument%d: AVM1Value", parameterIndex)
		}
	}
	declaration := fmt.Sprintf("function %s(%s): void {", functionName, strings.Join(visibleParameters, ", "))
	return metadata, declaration, nil
}

func parseActionFunction(line string, metadata []any) ([]any, error) {
	declaration := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "function "), "{"))
	openIndex := strings.IndexByte(declaration, '(')
	closeIndex := strings.LastIndex(declaration, ")")
	returnType := strings.TrimSpace(declaration[closeIndex+1:])
	if openIndex <= 0 || closeIndex < openIndex || returnType != "" && returnType != ": void" {
		return nil, errors.New("functionDeclaration")
	}
	visibleName := strings.TrimSpace(declaration[:openIndex])
	if !isActionIdentifier(visibleName) {
		return nil, fmt.Errorf("functionIdentifier: %q", visibleName)
	}
	visibleParameters := splitActionParameters(declaration[openIndex+1 : closeIndex])
	metadataName := rowFieldStringOrEmpty(metadata, 0)
	switch metadataName {
	case "FUNCTION2":
		if len(metadata) != 7 {
			return nil, errors.New("function2Metadata")
		}
		registers, areRegisters := metadata[3].([]any)
		parameterFields, areParameters := metadata[4].([]any)
		if !areRegisters || !areParameters || len(registers) != len(parameterFields) || len(visibleParameters) != len(parameterFields) {
			return nil, errors.New("function2Arity")
		}
		parameters := make([]any, len(parameterFields))
		for parameterIndex, field := range parameterFields {
			parameterName, isParameterName := field.(string)
			if !isParameterName {
				return nil, fmt.Errorf("function2Parameter[%d]", parameterIndex)
			}
			parameters[parameterIndex] = []any{registers[parameterIndex], parameterName}
		}
		return []any{"DEFINE_FUNCTION2", metadata[6], metadata[1], metadata[2], parameters, metadata[5]}, nil
	case "FUNCTION1":
		if len(metadata) != 4 {
			return nil, errors.New("function1Metadata")
		}
		parameterFields, areParameters := metadata[1].([]any)
		if !areParameters || len(visibleParameters) != len(parameterFields) {
			return nil, errors.New("function1Arity")
		}
		return []any{"DEFINE_FUNCTION", metadata[3], parameterFields, metadata[2]}, nil
	default:
		return nil, fmt.Errorf("functionMetadata: %q", metadataName)
	}
}

func parseInferredFunction1(line string, lineNumber int) ([]any, string, bool, bool, error) {
	const declarationPrefix = "function "
	if !strings.HasPrefix(line, declarationPrefix) {
		return nil, "", false, false, nil
	}
	isInline := strings.HasSuffix(line, "{}")
	declaration := line
	if isInline {
		declaration = strings.TrimSuffix(line, "{}") + "{"
	}
	if !strings.HasSuffix(declaration, "{") {
		return nil, "", false, false, nil
	}
	functionSource := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(declaration, declarationPrefix), "{"))
	openIndex := strings.IndexByte(functionSource, '(')
	closeIndex := strings.LastIndex(functionSource, ")")
	if openIndex <= 0 || closeIndex < openIndex || strings.TrimSpace(functionSource[closeIndex+1:]) != ": void" {
		return nil, "", false, true, errors.New("functionDeclaration")
	}
	visibleName := strings.TrimSpace(functionSource[:openIndex])
	if !isActionIdentifier(visibleName) {
		return nil, "", false, true, fmt.Errorf("functionIdentifier: %q", visibleName)
	}
	visibleParameters := splitActionParameters(functionSource[openIndex+1 : closeIndex])
	for parameterIndex, parameterName := range visibleParameters {
		if !isTypeScriptIdentifier(parameterName) {
			return nil, "", false, true, fmt.Errorf("functionParameter[%d]: %q", parameterIndex, parameterName)
		}
	}
	originalName := visibleName
	if isGeneratedAnonymousFunction(visibleName) {
		originalName = ""
	}
	label := fmt.Sprintf("AUTO_EMPTY_FUNCTION_%06d", lineNumber)
	return []any{"DEFINE_FUNCTION", originalName, stringsToFields(visibleParameters), label}, label, isInline, true, nil
}

func isGeneratedAnonymousFunction(name string) bool {
	suffix := strings.TrimPrefix(name, "anonymous_")
	if suffix == name || suffix == "" {
		return false
	}
	for _, character := range suffix {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func resolveNamedPush(row []any, constants []string) ([][]any, error) {
	name := rowFieldStringOrEmpty(row, 0)
	text, isText := rowFieldString(row, 1)
	if len(row) != 2 || !isText {
		return nil, errors.New("namedPushFields")
	}
	item := namedPushItem(text, constants)
	rows := [][]any{{"PUSH", item}}
	if name == "PUSH_VARIABLE" {
		rows = append(rows, []any{"GET_VARIABLE"})
	}
	return rows, nil
}

func resolveMemberPath(row []any, constants []string) ([][]any, error) {
	if len(row) < 2 {
		return nil, errors.New("memberPathMissing")
	}
	rows := make([][]any, 0, (len(row)-1)*2)
	for memberIndex := 1; memberIndex < len(row); memberIndex++ {
		memberName, isMemberName := rowFieldString(row, memberIndex)
		if !isMemberName || memberName == "" {
			return nil, fmt.Errorf("member[%d]", memberIndex-1)
		}
		item := namedPushItem(memberName, constants)
		rows = append(rows, []any{"PUSH", item}, []any{"GET_MEMBER"})
	}
	return rows, nil
}

func namedPushItem(text string, constants []string) []any {
	for constantIndex, constant := range constants {
		if constant != text || constantIndex > math.MaxUint16 {
			continue
		}
		kind := "CONSTANT16"
		if constantIndex <= math.MaxUint8 {
			kind = "CONSTANT8"
		}
		return []any{kind, float64(constantIndex)}
	}
	return []any{"STRING", text}
}

func splitActionParameters(payload string) []string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil
	}
	parts := strings.Split(payload, ",")
	for partIndex := range parts {
		part := strings.TrimSpace(parts[partIndex])
		separator := strings.IndexByte(part, ':')
		if separator >= 0 {
			part = strings.TrimSpace(part[:separator])
		}
		parts[partIndex] = part
	}
	return parts
}

func isActionIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		isLetter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character == '_'
		if !isLetter && (index == 0 || character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func isTypeScriptIdentifier(name string) bool {
	if !isActionIdentifier(name) {
		return false
	}
	reservedNames := map[string]bool{
		"await": true, "break": true, "case": true, "catch": true, "class": true,
		"const": true, "continue": true, "debugger": true, "default": true,
		"delete": true, "do": true, "else": true, "enum": true, "export": true,
		"extends": true, "false": true, "finally": true, "for": true, "function": true,
		"if": true, "import": true, "in": true, "instanceof": true, "interface": true,
		"let": true, "new": true, "null": true, "package": true, "private": true,
		"protected": true, "public": true, "return": true, "static": true,
		"super": true, "switch": true, "this": true, "throw": true, "true": true,
		"try": true, "typeof": true, "var": true, "void": true, "while": true,
		"with": true, "yield": true,
	}
	return !reservedNames[name]
}

func isTypeScriptPath(path string) bool {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !isTypeScriptIdentifier(part) {
			return false
		}
	}
	return true
}

func isTypeScriptReferencePath(path string) bool {
	parts := strings.Split(path, ".")
	if len(parts) == 0 || parts[0] != "this" && !isTypeScriptIdentifier(parts[0]) {
		return false
	}
	for _, part := range parts[1:] {
		if !isTypeScriptIdentifier(part) {
			return false
		}
	}
	return true
}

func stringsToFields(stringsPayload []string) []any {
	fields := make([]any, len(stringsPayload))
	for stringIndex, text := range stringsPayload {
		fields[stringIndex] = text
	}
	return fields
}

func rowFieldNumber(row []any, index int) (int, bool) {
	if index >= len(row) {
		return 0, false
	}
	switch number := row[index].(type) {
	case uint8:
		return int(number), true
	case uint16:
		return int(number), true
	case int:
		return number, true
	case float64:
		if math.Trunc(number) != number {
			return 0, false
		}
		return int(number), true
	default:
		return 0, false
	}
}

func renderActionStatements(row []any) ([]string, error) {
	name, isName := rowFieldString(row, 0)
	if !isName {
		return nil, errors.New("nameInvalid")
	}
	if name == "PUSH" {
		statements := make([]string, 0, len(row)-1)
		for itemIndex, field := range row[1:] {
			item, isItem := field.([]any)
			if !isItem || len(item) == 0 {
				return nil, fmt.Errorf("push[%d]", itemIndex)
			}
			kind, isKind := rowFieldString(item, 0)
			if !isKind {
				return nil, fmt.Errorf("pushKind[%d]", itemIndex)
			}
			statement, err := renderActionCall("push"+upperFirst(lowerActionName(kind)), item[1:])
			if err != nil {
				return nil, fmt.Errorf("push[%d]: %w", itemIndex, err)
			}
			statements = append(statements, statement)
		}
		return statements, nil
	}
	method := lowerActionName(name)
	returnCall, err := renderActionCall(method, row[1:])
	if err != nil {
		return nil, err
	}
	return []string{returnCall}, nil
}

func renderActionCall(method string, fields []any) (string, error) {
	arguments := make([]string, len(fields))
	for fieldIndex, field := range fields {
		payload, err := json.Marshal(field)
		if err != nil {
			return "", fmt.Errorf("argument[%d]: %w", fieldIndex, err)
		}
		arguments[fieldIndex] = string(payload)
	}
	return fmt.Sprintf("avm1.%s(%s);", method, strings.Join(arguments, ", ")), nil
}

func parseActionStatement(line string) ([]any, error) {
	if !strings.HasPrefix(line, "avm1.") || !strings.HasSuffix(line, ");") {
		return nil, fmt.Errorf("statement: %q", line)
	}
	call := strings.TrimSuffix(strings.TrimPrefix(line, "avm1."), ");")
	separator := strings.IndexByte(call, '(')
	if separator <= 0 {
		return nil, fmt.Errorf("call: %q", line)
	}
	method := call[:separator]
	argumentText := call[separator+1:]
	var fields []any
	if strings.TrimSpace(argumentText) != "" {
		err := json.Unmarshal([]byte("["+argumentText+"]"), &fields)
		if err != nil {
			return nil, fmt.Errorf("arguments: %w", err)
		}
	}
	kind := ""
	if strings.HasPrefix(method, "push") {
		kind = upperActionName(strings.TrimPrefix(method, "push"))
	}
	if isPushKind(kind) {
		item := append([]any{kind}, fields...)
		return []any{"PUSH", item}, nil
	}
	name := upperActionName(method)
	row := append([]any{name}, fields...)
	return row, nil
}

func isPushKind(kind string) bool {
	switch kind {
	case "STRING", "FLOAT", "FLOAT_BITS", "NULL", "UNDEFINED", "REGISTER", "BOOL", "DOUBLE", "DOUBLE_BITS", "INT", "CONSTANT8", "CONSTANT16":
		return true
	default:
		return false
	}
}

func lowerActionName(name string) string {
	parts := strings.Split(strings.ToLower(name), "_")
	for partIndex := 1; partIndex < len(parts); partIndex++ {
		parts[partIndex] = titleActionName(parts[partIndex])
	}
	return strings.Join(parts, "")
}

func upperActionName(name string) string {
	var result strings.Builder
	for characterIndex, character := range name {
		if character >= 'A' && character <= 'Z' && characterIndex != 0 {
			result.WriteByte('_')
		}
		result.WriteRune(character)
	}
	return strings.ToUpper(result.String())
}

func titleActionName(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
}

func upperFirst(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func extractActionBlocks(tags []Tag, prefix string, blocks *[]ActionBlock) error {
	for tagIndex := range tags {
		tag := &tags[tagIndex]
		path := fmt.Sprintf("%s/tag_%04d", prefix, tagIndex)
		switch tag.Code {
		case 12:
			*blocks = append(*blocks, ActionBlock{Path: path, Payload: append([]byte(nil), tag.Payload...)})
		case 59:
			if len(tag.Payload) < 2 {
				return fmt.Errorf("initAction[%s]: short", path)
			}
			*blocks = append(*blocks, ActionBlock{Path: path, Payload: append([]byte(nil), tag.Payload[2:]...)})
		case 39:
			if len(tag.Payload) < 4 {
				return fmt.Errorf("sprite[%s]: short", path)
			}
			nestedTags, err := decodeTags(tag.Payload[4:])
			if err != nil {
				return fmt.Errorf("sprite[%s]: %w", path, err)
			}
			err = extractActionBlocks(nestedTags, path, blocks)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func applyActionBlocks(tags []Tag, prefix string, blocks map[string][]byte, seenPaths map[string]bool) error {
	for tagIndex := range tags {
		tag := &tags[tagIndex]
		path := fmt.Sprintf("%s/tag_%04d", prefix, tagIndex)
		switch tag.Code {
		case 12:
			if actionPayload, isFound := blocks[path]; isFound {
				tag.Payload = append([]byte(nil), actionPayload...)
				tag.IsLong = len(tag.Payload) >= 63
				seenPaths[path] = true
			}
		case 59:
			if actionPayload, isFound := blocks[path]; isFound {
				if len(tag.Payload) < 2 {
					return fmt.Errorf("initAction[%s]: short", path)
				}
				tag.Payload = append(append([]byte(nil), tag.Payload[:2]...), actionPayload...)
				tag.IsLong = len(tag.Payload) >= 63
				seenPaths[path] = true
			}
		case 39:
			if len(tag.Payload) < 4 {
				return fmt.Errorf("sprite[%s]: short", path)
			}
			nestedTags, err := decodeTags(tag.Payload[4:])
			if err != nil {
				return fmt.Errorf("sprite[%s]: %w", path, err)
			}
			err = applyActionBlocks(nestedTags, path, blocks, seenPaths)
			if err != nil {
				return err
			}
			var nestedPayload bytes.Buffer
			err = encodeTags(&nestedPayload, nestedTags)
			if err != nil {
				return fmt.Errorf("spriteEncode[%s]: %w", path, err)
			}
			tag.Payload = append(append([]byte(nil), tag.Payload[:4]...), nestedPayload.Bytes()...)
		}
	}
	return nil
}

func decodeActionRecords(payload []byte) ([]actionRecord, error) {
	records := make([]actionRecord, 0, 32)
	for offset := 0; offset < len(payload); {
		start := offset
		opcode := payload[offset]
		offset++
		length := 0
		if opcode >= 0x80 {
			if len(payload)-offset < 2 {
				return nil, fmt.Errorf("length[%d]: short", start)
			}
			length = int(binary.LittleEndian.Uint16(payload[offset : offset+2]))
			offset += 2
		}
		if length > len(payload)-offset {
			return nil, fmt.Errorf("payload[%d]: got %d, remain %d", start, length, len(payload)-offset)
		}
		records = append(records, actionRecord{offset: start, opcode: opcode, payload: append([]byte(nil), payload[offset:offset+length]...)})
		offset += length
		if opcode == 0 {
			if offset != len(payload) {
				return nil, fmt.Errorf("trailing[%d]: %d", offset, len(payload)-offset)
			}
			break
		}
	}
	return records, nil
}

func actionLabels(records []actionRecord) map[int]string {
	labels := make(map[int]string)
	for _, record := range records {
		if (record.opcode == 0x99 || record.opcode == 0x9D) && len(record.payload) == 2 {
			recordSize := 3 + len(record.payload)
			target := record.offset + recordSize + int(int16(binary.LittleEndian.Uint16(record.payload)))
			labels[target] = fmt.Sprintf("L%04X", target)
			continue
		}
		codeSize, isBlock := actionBlockSize(record)
		if !isBlock {
			continue
		}
		target := record.offset + 3 + len(record.payload) + int(codeSize)
		labels[target] = fmt.Sprintf("L%04X", target)
	}
	return labels
}

func actionBranchTargets(records []actionRecord) map[int]bool {
	targets := make(map[int]bool)
	for _, record := range records {
		if (record.opcode != 0x99 && record.opcode != 0x9D) || len(record.payload) != 2 {
			continue
		}
		target := record.offset + 5 + int(int16(binary.LittleEndian.Uint16(record.payload)))
		targets[target] = true
	}
	return targets
}

func renderAction(record actionRecord, labels map[int]string) ([]any, error) {
	if record.opcode == 0 {
		return []any{"END"}, nil
	}
	if name := simpleActionNames[record.opcode]; name != "" && len(record.payload) == 0 {
		return []any{name}, nil
	}
	switch record.opcode {
	case 0x81:
		if len(record.payload) == 2 {
			return []any{"GOTO_FRAME", binary.LittleEndian.Uint16(record.payload)}, nil
		}
	case 0x83:
		stringsPayload, err := decodeNullStrings(record.payload, 2)
		if err == nil {
			return []any{"GET_URL", stringsPayload[0], stringsPayload[1]}, nil
		}
	case 0x87:
		if len(record.payload) == 1 {
			return []any{"STORE_REGISTER", record.payload[0]}, nil
		}
	case 0x88:
		return renderConstantPool(record.payload)
	case 0x8B:
		stringsPayload, err := decodeNullStrings(record.payload, 1)
		if err == nil {
			return []any{"SET_TARGET", stringsPayload[0]}, nil
		}
	case 0x8C:
		stringsPayload, err := decodeNullStrings(record.payload, 1)
		if err == nil {
			return []any{"GOTO_LABEL", stringsPayload[0]}, nil
		}
	case 0x96:
		return renderPush(record.payload)
	case 0x8E:
		return renderFunction2(record, labels)
	case 0x94:
		if len(record.payload) == 2 {
			target := record.offset + 5 + int(binary.LittleEndian.Uint16(record.payload))
			return []any{"WITH", labels[target]}, nil
		}
	case 0x9B:
		return renderFunction(record, labels)
	case 0x99, 0x9D:
		if len(record.payload) == 2 {
			target := record.offset + 5 + int(int16(binary.LittleEndian.Uint16(record.payload)))
			name := "JUMP"
			if record.opcode == 0x9D {
				name = "IF"
			}
			label := labels[target]
			if label == "" {
				label = fmt.Sprintf("L%04X", target)
			}
			return []any{name, label}, nil
		}
	}
	return []any{"BYTECODE", fmt.Sprintf("0x%02X", record.opcode), strings.ToUpper(hex.EncodeToString(record.payload))}, nil
}

func renderFunction2(record actionRecord, labels map[int]string) ([]any, error) {
	name, offset, err := decodeNullStringAt(record.payload, 0)
	if err != nil {
		return nil, fmt.Errorf("functionName: %w", err)
	}
	if len(record.payload)-offset < 5 {
		return nil, errors.New("functionHeader")
	}
	parameterCount := int(binary.LittleEndian.Uint16(record.payload[offset:]))
	offset += 2
	registerCount := record.payload[offset]
	offset++
	flags := binary.LittleEndian.Uint16(record.payload[offset:])
	offset += 2
	parameters := make([]any, 0, parameterCount)
	for parameterIndex := 0; parameterIndex < parameterCount; parameterIndex++ {
		if offset >= len(record.payload) {
			return nil, fmt.Errorf("functionParameter[%d]: register", parameterIndex)
		}
		register := record.payload[offset]
		offset++
		parameter, nextOffset, readErr := decodeNullStringAt(record.payload, offset)
		if readErr != nil {
			return nil, fmt.Errorf("functionParameter[%d]: %w", parameterIndex, readErr)
		}
		offset = nextOffset
		parameters = append(parameters, []any{register, parameter})
	}
	if len(record.payload)-offset != 2 {
		return nil, fmt.Errorf("functionTrailing: %d", len(record.payload)-offset)
	}
	codeSize := binary.LittleEndian.Uint16(record.payload[offset:])
	target := record.offset + 3 + len(record.payload) + int(codeSize)
	return []any{"DEFINE_FUNCTION2", name, registerCount, flags, parameters, labels[target]}, nil
}

func renderFunction(record actionRecord, labels map[int]string) ([]any, error) {
	name, offset, err := decodeNullStringAt(record.payload, 0)
	if err != nil {
		return nil, fmt.Errorf("functionName: %w", err)
	}
	if len(record.payload)-offset < 2 {
		return nil, errors.New("functionHeader")
	}
	parameterCount := int(binary.LittleEndian.Uint16(record.payload[offset:]))
	offset += 2
	parameters := make([]any, 0, parameterCount)
	for parameterIndex := 0; parameterIndex < parameterCount; parameterIndex++ {
		parameter, nextOffset, readErr := decodeNullStringAt(record.payload, offset)
		if readErr != nil {
			return nil, fmt.Errorf("functionParameter[%d]: %w", parameterIndex, readErr)
		}
		offset = nextOffset
		parameters = append(parameters, parameter)
	}
	if len(record.payload)-offset != 2 {
		return nil, fmt.Errorf("functionTrailing: %d", len(record.payload)-offset)
	}
	codeSize := binary.LittleEndian.Uint16(record.payload[offset:])
	target := record.offset + 3 + len(record.payload) + int(codeSize)
	return []any{"DEFINE_FUNCTION", name, parameters, labels[target]}, nil
}

func actionBlockSize(record actionRecord) (uint16, bool) {
	switch record.opcode {
	case 0x8E, 0x9B:
		if len(record.payload) < 2 {
			return 0, false
		}
		return binary.LittleEndian.Uint16(record.payload[len(record.payload)-2:]), true
	case 0x94:
		if len(record.payload) != 2 {
			return 0, false
		}
		return binary.LittleEndian.Uint16(record.payload), true
	default:
		return 0, false
	}
}

func renderPush(payload []byte) ([]any, error) {
	row := []any{"PUSH"}
	for offset := 0; offset < len(payload); {
		kind := payload[offset]
		offset++
		switch kind {
		case 0:
			end := bytes.IndexByte(payload[offset:], 0)
			if end < 0 {
				return nil, errors.New("pushString")
			}
			row = append(row, []any{"STRING", string(payload[offset : offset+end])})
			offset += end + 1
		case 1:
			if len(payload)-offset < 4 {
				return nil, errors.New("pushFloat")
			}
			floatBits := binary.LittleEndian.Uint32(payload[offset:])
			floatNumber := math.Float32frombits(floatBits)
			if math.IsNaN(float64(floatNumber)) || math.IsInf(float64(floatNumber), 0) {
				row = append(row, []any{"FLOAT_BITS", fmt.Sprintf("0x%08X", floatBits)})
			} else {
				row = append(row, []any{"FLOAT", floatNumber})
			}
			offset += 4
		case 2:
			row = append(row, []any{"NULL"})
		case 3:
			row = append(row, []any{"UNDEFINED"})
		case 4:
			if len(payload)-offset < 1 {
				return nil, errors.New("pushRegister")
			}
			row = append(row, []any{"REGISTER", payload[offset]})
			offset++
		case 5:
			if len(payload)-offset < 1 {
				return nil, errors.New("pushBool")
			}
			row = append(row, []any{"BOOL", payload[offset] != 0})
			offset++
		case 6:
			if len(payload)-offset < 8 {
				return nil, errors.New("pushDouble")
			}
			bits := uint64(binary.LittleEndian.Uint32(payload[offset:]))<<32 | uint64(binary.LittleEndian.Uint32(payload[offset+4:]))
			doubleNumber := math.Float64frombits(bits)
			if math.IsNaN(doubleNumber) || math.IsInf(doubleNumber, 0) {
				row = append(row, []any{"DOUBLE_BITS", fmt.Sprintf("0x%016X", bits)})
			} else {
				row = append(row, []any{"DOUBLE", doubleNumber})
			}
			offset += 8
		case 7:
			if len(payload)-offset < 4 {
				return nil, errors.New("pushInt")
			}
			row = append(row, []any{"INT", int32(binary.LittleEndian.Uint32(payload[offset:]))})
			offset += 4
		case 8:
			if len(payload)-offset < 1 {
				return nil, errors.New("pushConstant8")
			}
			row = append(row, []any{"CONSTANT8", payload[offset]})
			offset++
		case 9:
			if len(payload)-offset < 2 {
				return nil, errors.New("pushConstant16")
			}
			row = append(row, []any{"CONSTANT16", binary.LittleEndian.Uint16(payload[offset:])})
			offset += 2
		default:
			return nil, fmt.Errorf("pushType: %d", kind)
		}
	}
	return row, nil
}

func renderConstantPool(payload []byte) ([]any, error) {
	if len(payload) < 2 {
		return nil, errors.New("constantPoolShort")
	}
	count := int(binary.LittleEndian.Uint16(payload))
	stringsPayload, err := decodeNullStrings(payload[2:], count)
	if err != nil {
		return nil, err
	}
	row := []any{"CONSTANT_POOL"}
	for _, text := range stringsPayload {
		row = append(row, text)
	}
	return row, nil
}

func assembleAction(row []any, offset int) ([]byte, error) {
	_ = offset
	name, isString := row[0].(string)
	if !isString {
		return nil, errors.New("nameInvalid")
	}
	if name == "END" {
		return []byte{0}, nil
	}
	if opcode, isFound := simpleActionCodes[name]; isFound {
		return []byte{opcode}, nil
	}
	if name == "BYTECODE" {
		if len(row) != 3 {
			return nil, errors.New("bytecodeFields")
		}
		opcodeText, isOpcode := row[1].(string)
		hexPayload, isHex := row[2].(string)
		if !isOpcode || !isHex {
			return nil, errors.New("bytecodeTypes")
		}
		opcodeNumber, err := strconv.ParseUint(strings.TrimPrefix(opcodeText, "0x"), 16, 8)
		if err != nil {
			return nil, fmt.Errorf("opcode: %w", err)
		}
		operand, err := hex.DecodeString(hexPayload)
		if err != nil {
			return nil, fmt.Errorf("operand: %w", err)
		}
		return encodeActionRecord(byte(opcodeNumber), operand)
	}
	if name == "JUMP" || name == "IF" {
		opcode := byte(0x99)
		if name == "IF" {
			opcode = 0x9D
		}
		return encodeActionRecord(opcode, []byte{0, 0})
	}
	if name == "PUSH" {
		operand, err := assemblePush(row[1:])
		if err != nil {
			return nil, err
		}
		return encodeActionRecord(0x96, operand)
	}
	if name == "CONSTANT_POOL" {
		var operand bytes.Buffer
		_ = binary.Write(&operand, binary.LittleEndian, uint16(len(row)-1))
		for fieldIndex := 1; fieldIndex < len(row); fieldIndex++ {
			text, isText := row[fieldIndex].(string)
			if !isText || strings.IndexByte(text, 0) >= 0 {
				return nil, fmt.Errorf("constant[%d]", fieldIndex-1)
			}
			_, _ = operand.WriteString(text)
			_ = operand.WriteByte(0)
		}
		return encodeActionRecord(0x88, operand.Bytes())
	}
	if name == "DEFINE_FUNCTION2" {
		return assembleFunction2(row)
	}
	if name == "DEFINE_FUNCTION" {
		return assembleFunction(row)
	}
	if name == "WITH" {
		if len(row) != 2 {
			return nil, errors.New("withFields")
		}
		return encodeActionRecord(0x94, []byte{0, 0})
	}
	switch name {
	case "GOTO_FRAME":
		number, ok := rowFieldUint(row, 1, 16)
		if !ok {
			return nil, errors.New("frame")
		}
		operand := make([]byte, 2)
		binary.LittleEndian.PutUint16(operand, uint16(number))
		return encodeActionRecord(0x81, operand)
	case "STORE_REGISTER":
		number, ok := rowFieldUint(row, 1, 8)
		if !ok {
			return nil, errors.New("register")
		}
		return encodeActionRecord(0x87, []byte{byte(number)})
	case "GET_URL":
		return assembleStringAction(0x83, row, 2)
	case "SET_TARGET":
		return assembleStringAction(0x8B, row, 1)
	case "GOTO_LABEL":
		return assembleStringAction(0x8C, row, 1)
	default:
		return nil, fmt.Errorf("unsupported: %q", name)
	}
}

func assembleFunction2(row []any) ([]byte, error) {
	if len(row) != 6 {
		return nil, errors.New("function2Fields")
	}
	name, isName := rowFieldString(row, 1)
	registerCount, isRegisterCount := rowFieldUint(row, 2, 8)
	flags, isFlags := rowFieldUint(row, 3, 16)
	parameters, areParameters := row[4].([]any)
	_, isLabel := rowFieldString(row, 5)
	if !isName || !isRegisterCount || !isFlags || !areParameters || !isLabel || strings.IndexByte(name, 0) >= 0 || len(parameters) > math.MaxUint16 {
		return nil, errors.New("function2Header")
	}
	var operand bytes.Buffer
	_, _ = operand.WriteString(name)
	_ = operand.WriteByte(0)
	_ = binary.Write(&operand, binary.LittleEndian, uint16(len(parameters)))
	_ = operand.WriteByte(byte(registerCount))
	_ = binary.Write(&operand, binary.LittleEndian, uint16(flags))
	for parameterIndex, field := range parameters {
		parameter, isParameter := field.([]any)
		if !isParameter || len(parameter) != 2 {
			return nil, fmt.Errorf("function2Parameter[%d]", parameterIndex)
		}
		register, isRegister := rowFieldUint(parameter, 0, 8)
		parameterName, isParameterName := rowFieldString(parameter, 1)
		if !isRegister || !isParameterName || strings.IndexByte(parameterName, 0) >= 0 {
			return nil, fmt.Errorf("function2Parameter[%d]", parameterIndex)
		}
		_ = operand.WriteByte(byte(register))
		_, _ = operand.WriteString(parameterName)
		_ = operand.WriteByte(0)
	}
	_ = binary.Write(&operand, binary.LittleEndian, uint16(0))
	return encodeActionRecord(0x8E, operand.Bytes())
}

func assembleFunction(row []any) ([]byte, error) {
	if len(row) != 4 {
		return nil, errors.New("functionFields")
	}
	name, isName := rowFieldString(row, 1)
	parameters, areParameters := row[2].([]any)
	_, isLabel := rowFieldString(row, 3)
	if !isName || !areParameters || !isLabel || strings.IndexByte(name, 0) >= 0 || len(parameters) > math.MaxUint16 {
		return nil, errors.New("functionHeader")
	}
	var operand bytes.Buffer
	_, _ = operand.WriteString(name)
	_ = operand.WriteByte(0)
	_ = binary.Write(&operand, binary.LittleEndian, uint16(len(parameters)))
	for parameterIndex, field := range parameters {
		parameterName, isParameterName := field.(string)
		if !isParameterName || strings.IndexByte(parameterName, 0) >= 0 {
			return nil, fmt.Errorf("functionParameter[%d]", parameterIndex)
		}
		_, _ = operand.WriteString(parameterName)
		_ = operand.WriteByte(0)
	}
	_ = binary.Write(&operand, binary.LittleEndian, uint16(0))
	return encodeActionRecord(0x9B, operand.Bytes())
}

func assemblePush(fields []any) ([]byte, error) {
	var operand bytes.Buffer
	for fieldIndex, field := range fields {
		item, isItem := field.([]any)
		if !isItem || len(item) == 0 {
			return nil, fmt.Errorf("push[%d]", fieldIndex)
		}
		kind, isKind := item[0].(string)
		if !isKind {
			return nil, fmt.Errorf("pushKind[%d]", fieldIndex)
		}
		switch kind {
		case "STRING":
			text, ok := rowFieldString(item, 1)
			if !ok || strings.IndexByte(text, 0) >= 0 {
				return nil, fmt.Errorf("pushString[%d]", fieldIndex)
			}
			_ = operand.WriteByte(0)
			_, _ = operand.WriteString(text)
			_ = operand.WriteByte(0)
		case "NULL":
			_ = operand.WriteByte(2)
		case "UNDEFINED":
			_ = operand.WriteByte(3)
		case "BOOL":
			if len(item) != 2 {
				return nil, fmt.Errorf("pushBool[%d]", fieldIndex)
			}
			flag, ok := item[1].(bool)
			if !ok {
				return nil, fmt.Errorf("pushBool[%d]", fieldIndex)
			}
			_ = operand.WriteByte(5)
			if flag {
				_ = operand.WriteByte(1)
			} else {
				_ = operand.WriteByte(0)
			}
		case "FLOAT", "DOUBLE":
			if len(item) != 2 {
				return nil, fmt.Errorf("pushNumber[%d]", fieldIndex)
			}
			number, ok := item[1].(float64)
			if !ok {
				return nil, fmt.Errorf("pushNumber[%d]", fieldIndex)
			}
			if kind == "FLOAT" {
				_ = operand.WriteByte(1)
				_ = binary.Write(&operand, binary.LittleEndian, math.Float32bits(float32(number)))
			} else {
				_ = operand.WriteByte(6)
				bits := math.Float64bits(number)
				_ = binary.Write(&operand, binary.LittleEndian, uint32(bits>>32))
				_ = binary.Write(&operand, binary.LittleEndian, uint32(bits))
			}
		case "FLOAT_BITS", "DOUBLE_BITS":
			bitText, isBitText := rowFieldString(item, 1)
			bitSize := 32
			marker := byte(1)
			if kind == "DOUBLE_BITS" {
				bitSize = 64
				marker = 6
			}
			if !isBitText {
				return nil, fmt.Errorf("pushBits[%d]", fieldIndex)
			}
			bits, parseErr := strconv.ParseUint(strings.TrimPrefix(bitText, "0x"), 16, bitSize)
			if parseErr != nil {
				return nil, fmt.Errorf("pushBits[%d]: %w", fieldIndex, parseErr)
			}
			_ = operand.WriteByte(marker)
			if bitSize == 32 {
				_ = binary.Write(&operand, binary.LittleEndian, uint32(bits))
			} else {
				_ = binary.Write(&operand, binary.LittleEndian, uint32(bits>>32))
				_ = binary.Write(&operand, binary.LittleEndian, uint32(bits))
			}
		case "INT":
			number, ok := rowFieldInt(item, 1, 32)
			if !ok {
				return nil, fmt.Errorf("pushInt[%d]", fieldIndex)
			}
			_ = operand.WriteByte(7)
			_ = binary.Write(&operand, binary.LittleEndian, int32(number))
		case "REGISTER", "CONSTANT8", "CONSTANT16":
			bits := 8
			marker := byte(4)
			if kind == "CONSTANT8" {
				marker = 8
			}
			if kind == "CONSTANT16" {
				marker = 9
				bits = 16
			}
			number, ok := rowFieldUint(item, 1, bits)
			if !ok {
				return nil, fmt.Errorf("pushIndex[%d]", fieldIndex)
			}
			_ = operand.WriteByte(marker)
			if bits == 8 {
				if number > math.MaxUint8 {
					return nil, fmt.Errorf("pushByte[%d]: %d", fieldIndex, number)
				}
				_ = operand.WriteByte(byte(number))
			} else {
				if number > math.MaxUint16 {
					return nil, fmt.Errorf("pushWord[%d]: %d", fieldIndex, number)
				}
				_ = binary.Write(&operand, binary.LittleEndian, uint16(number))
			}
		default:
			return nil, fmt.Errorf("pushKind[%d]: %q", fieldIndex, kind)
		}
	}
	return operand.Bytes(), nil
}

func encodeActionRecord(opcode byte, operand []byte) ([]byte, error) {
	if opcode < 0x80 {
		if len(operand) != 0 {
			return nil, errors.New("shortOperand")
		}
		return []byte{opcode}, nil
	}
	if len(operand) > math.MaxUint16 {
		return nil, errors.New("operandLarge")
	}
	payload := make([]byte, 3, 3+len(operand))
	payload[0] = opcode
	binary.LittleEndian.PutUint16(payload[1:3], uint16(len(operand)))
	payload = append(payload, operand...)
	return payload, nil
}

func assembleStringAction(opcode byte, row []any, count int) ([]byte, error) {
	if len(row) != count+1 {
		return nil, errors.New("stringFields")
	}
	var operand bytes.Buffer
	for fieldIndex := 1; fieldIndex < len(row); fieldIndex++ {
		text, isText := row[fieldIndex].(string)
		if !isText || strings.IndexByte(text, 0) >= 0 {
			return nil, fmt.Errorf("string[%d]", fieldIndex-1)
		}
		_, _ = operand.WriteString(text)
		_ = operand.WriteByte(0)
	}
	return encodeActionRecord(opcode, operand.Bytes())
}

func decodeNullStrings(payload []byte, count int) ([]string, error) {
	texts := make([]string, 0, count)
	offset := 0
	for textIndex := 0; textIndex < count; textIndex++ {
		end := bytes.IndexByte(payload[offset:], 0)
		if end < 0 {
			return nil, fmt.Errorf("string[%d]", textIndex)
		}
		texts = append(texts, string(payload[offset:offset+end]))
		offset += end + 1
	}
	if offset != len(payload) {
		return nil, fmt.Errorf("trailing: %d", len(payload)-offset)
	}
	return texts, nil
}

func decodeNullStringAt(payload []byte, offset int) (string, int, error) {
	if offset < 0 || offset > len(payload) {
		return "", offset, errors.New("offset")
	}
	end := bytes.IndexByte(payload[offset:], 0)
	if end < 0 {
		return "", offset, errors.New("terminator")
	}
	return string(payload[offset : offset+end]), offset + end + 1, nil
}

func rowFieldString(row []any, index int) (string, bool) {
	if index >= len(row) {
		return "", false
	}
	text, ok := row[index].(string)
	return text, ok
}

func rowFieldStringOrEmpty(row []any, index int) string {
	text, isText := rowFieldString(row, index)
	if !isText {
		return ""
	}
	return text
}

func rowFieldUint(row []any, index, bits int) (uint64, bool) {
	if index >= len(row) {
		return 0, false
	}
	number, ok := row[index].(float64)
	if !ok || number < 0 || number > float64(uint64(1)<<bits-1) || math.Trunc(number) != number {
		return 0, false
	}
	return uint64(number), true
}
func rowFieldInt(row []any, index, bits int) (int64, bool) {
	if index >= len(row) {
		return 0, false
	}
	number, ok := row[index].(float64)
	minimum := -float64(uint64(1) << (bits - 1))
	maximum := float64(uint64(1)<<(bits-1) - 1)
	if !ok || number < minimum || number > maximum || math.Trunc(number) != number {
		return 0, false
	}
	return int64(number), true
}

func reverseActionNames(names map[byte]string) map[string]byte {
	codes := make(map[string]byte, len(names))
	keys := make([]int, 0, len(names))
	for code := range names {
		keys = append(keys, int(code))
	}
	sort.Ints(keys)
	for _, code := range keys {
		codes[names[byte(code)]] = byte(code)
	}
	return codes
}
