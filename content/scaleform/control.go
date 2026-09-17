package scaleform

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var typeScriptConditionalValuePattern = regexp.MustCompile(`^const ([A-Za-z_][A-Za-z0-9_]*) = ([A-Za-z_][A-Za-z0-9_.]*) \? ([A-Za-z_][A-Za-z0-9_.]*\([^;]*\)) : ([A-Za-z_][A-Za-z0-9_.]*);$`)
var typeScriptPredicateGuardPattern = regexp.MustCompile(`^if \(([A-Za-z_][A-Za-z0-9_]*\([A-Za-z_][A-Za-z0-9_.]*\))\) \{$`)

func liftRenderedReturnGuards(source string) (string, error) {
	lines := strings.Split(source, "\n")
	aliasesByLine := functionRegisterAliasesByLine(lines)
	referencesByLine := branchReferencesByLine(lines)
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		guard, consumedLines, isGuard := renderedReturnGuard(lines[lineIndex:], aliasesByLine[lineIndex], referencesByLine[lineIndex])
		if isGuard {
			liftedLines = append(liftedLines, guard...)
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func branchReferencesByLine(lines []string) []map[string]int {
	referencesByLine := make([]map[string]int, len(lines))
	for blockStart := 0; blockStart < len(lines); {
		blockEnd := blockStart + 1
		for blockEnd < len(lines) && !strings.HasPrefix(lines[blockEnd], "// block: ") {
			blockEnd++
		}
		references := make(map[string]int)
		for _, line := range lines[blockStart:blockEnd] {
			row, err := parseActionStatement(strings.TrimSpace(line))
			if err != nil {
				continue
			}
			name := rowFieldStringOrEmpty(row, 0)
			if name != "IF" && name != "JUMP" {
				continue
			}
			label, isLabel := rowFieldString(row, 1)
			if isLabel {
				references[label]++
			}
		}
		for lineIndex := blockStart; lineIndex < blockEnd; lineIndex++ {
			referencesByLine[lineIndex] = references
		}
		blockStart = blockEnd
	}
	return referencesByLine
}

func renderedReturnGuard(lines []string, aliases map[string]string, branchReferences map[string]int) ([]string, int, bool) {
	if len(lines) < 7 {
		return nil, 0, false
	}
	condition, conditionLines, isCondition := renderedCallObject(lines)
	if !isCondition || conditionLines+4 >= len(lines) || !isTypeScriptReferencePath(condition) {
		return nil, 0, false
	}
	if strings.TrimSpace(lines[conditionLines]) != "avm1.not();" {
		return nil, 0, false
	}
	label, isBranch := renderedBranch(lines[conditionLines+1], "IF")
	if !isBranch || branchReferences[label] != 1 || strings.TrimSpace(lines[conditionLines+2]) != "avm1.pushUndefined();" || strings.TrimSpace(lines[conditionLines+3]) != "avm1.return();" || strings.TrimSpace(lines[conditionLines+4]) != label+":" {
		return nil, 0, false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	for lineIndex := 0; lineIndex < conditionLines+5; lineIndex++ {
		if !strings.HasPrefix(lines[lineIndex], indent) || strings.HasPrefix(strings.TrimPrefix(lines[lineIndex], indent), "\t") {
			return nil, 0, false
		}
	}
	condition = replaceTypeScriptIdentifiers(condition, aliases)
	return []string{
		fmt.Sprintf("%sif (%s) {", indent, condition),
		indent + "\treturn;",
		indent + "}",
	}, conditionLines + 5, true
}

func expandTypeScriptReturnGuards(source string) (string, error) {
	lines := strings.Split(source, "\n")
	expandedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		if lineIndex+2 >= len(lines) {
			expandedLines = append(expandedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		trimmed := strings.TrimSpace(lines[lineIndex])
		if !strings.HasPrefix(trimmed, "if (") || !strings.HasSuffix(trimmed, ") {") || strings.TrimSpace(lines[lineIndex+1]) != "return;" || strings.TrimSpace(lines[lineIndex+2]) != "}" {
			expandedLines = append(expandedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		condition := strings.TrimSuffix(strings.TrimPrefix(trimmed, "if ("), ") {")
		if !isTypeScriptReferencePath(condition) {
			expandedLines = append(expandedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		indent := lines[lineIndex][:len(lines[lineIndex])-len(strings.TrimLeft(lines[lineIndex], "\t"))]
		pathLines, err := renderedReferencePush(condition, indent)
		if err != nil {
			return "", fmt.Errorf("line[%d]: %w", lineIndex+1, err)
		}
		label := fmt.Sprintf("AUTO_RETURN_GUARD_%06d", lineIndex+1)
		expandedLines = append(expandedLines, pathLines...)
		expandedLines = append(expandedLines,
			indent+"avm1.not();",
			fmt.Sprintf("%savm1.if(%q);", indent, label),
			indent+"avm1.pushUndefined();",
			indent+"avm1.return();",
			indent+label+":",
		)
		lineIndex += 3
	}
	return strings.Join(expandedLines, "\n"), nil
}

func renderedReferencePush(path, indent string) ([]string, error) {
	parts := strings.Split(path, ".")
	lines := make([]string, 0, 2)
	if strings.HasPrefix(parts[0], "register") {
		register, err := strconv.ParseUint(strings.TrimPrefix(parts[0], "register"), 10, 8)
		if err != nil {
			return nil, fmt.Errorf("register: %w", err)
		}
		lines = append(lines, fmt.Sprintf("%savm1.pushRegister(%d);", indent, register))
	} else {
		lines = append(lines, fmt.Sprintf("%savm1.pushVariable(%q);", indent, parts[0]))
	}
	if len(parts) > 1 {
		members := make([]string, 0, len(parts)-1)
		for _, member := range parts[1:] {
			members = append(members, strconv.Quote(member))
		}
		lines = append(lines, fmt.Sprintf("%savm1.getMember(%s);", indent, strings.Join(members, ", ")))
	}
	return lines, nil
}

func liftRenderedConditionalValues(source string) (string, error) {
	lines := strings.Split(source, "\n")
	aliasesByLine := functionRegisterAliasesByLine(lines)
	referencesByLine := branchReferencesByLine(lines)
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		declaration, followingLine, consumedLines, isConditional := renderedConditionalValue(lines[lineIndex:], aliasesByLine[lineIndex], referencesByLine[lineIndex])
		if isConditional {
			liftedLines = append(liftedLines, declaration, followingLine)
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func functionRegisterAliasesByLine(lines []string) []map[string]string {
	aliasesByLine := make([]map[string]string, len(lines))
	for lineIndex := 0; lineIndex+1 < len(lines); lineIndex++ {
		matches := typeScriptMemberFunctionMetadataPattern.FindStringSubmatch(strings.TrimSpace(lines[lineIndex]))
		metadata := ""
		if len(matches) == 4 {
			metadata = matches[3]
		} else {
			row, err := parseActionStatement(strings.TrimSpace(lines[lineIndex]))
			if err != nil {
				continue
			}
			name := rowFieldStringOrEmpty(row, 0)
			if name != "FUNCTION2" {
				continue
			}
			metadata = strings.TrimSpace(lines[lineIndex])
		}
		functionEnd := matchingTypeScriptBrace(lines, lineIndex+1)
		if functionEnd < 0 {
			continue
		}
		aliases := memberFunctionRegisterNames(metadata)
		row, err := parseActionStatement(metadata)
		if err == nil {
			name := rowFieldStringOrEmpty(row, 0)
			flags, areFlags := rowFieldNumber(row, 2)
			if name == "FUNCTION2" && areFlags && flags&0x01 != 0 {
				aliases["register1"] = "this"
			}
		}
		for bodyIndex := lineIndex + 2; bodyIndex < functionEnd; bodyIndex++ {
			aliasesByLine[bodyIndex] = aliases
		}
	}
	return aliasesByLine
}

func renderedConditionalValue(lines []string, aliases map[string]string, branchReferences map[string]int) (string, string, int, bool) {
	if len(lines) < 15 {
		return "", "", 0, false
	}
	condition, conditionLines, isCondition := renderedCallObject(lines)
	if !isCondition || conditionLines+11 >= len(lines) {
		return "", "", 0, false
	}
	trueLabel, isTrueBranch := renderedBranch(lines[conditionLines], "IF")
	falseExpression, falseLines, isFalseExpression := renderedCallObject(lines[conditionLines+1:])
	jumpIndex := conditionLines + 1 + falseLines
	mergeLabel, isMergeBranch := renderedBranch(lines[jumpIndex], "JUMP")
	if !isTrueBranch || !isFalseExpression || !isMergeBranch || branchReferences[trueLabel] != 1 || branchReferences[mergeLabel] != 1 || strings.TrimSpace(lines[jumpIndex+1]) != trueLabel+":" {
		return "", "", 0, false
	}
	trueStart := jumpIndex + 2
	count, isCount := renderedArgumentCount(lines[trueStart])
	trueObject, objectLines, isTrueObject := renderedCallObject(lines[trueStart+1:])
	methodIndex := trueStart + 1 + objectLines
	method, isMethod := renderedMethodName(lines[methodIndex])
	callIndex := methodIndex + 1
	mergeIndex := callIndex + 1
	if !isCount || count != 0 || !isTrueObject || !isMethod || strings.TrimSpace(lines[callIndex]) != "avm1.callMethod();" || strings.TrimSpace(lines[mergeIndex]) != mergeLabel+":" {
		return "", "", 0, false
	}
	storeRow, storeErr := parseActionStatement(strings.TrimSpace(lines[mergeIndex+1]))
	storeName := rowFieldStringOrEmpty(storeRow, 0)
	register, isRegister := rowFieldNumber(storeRow, 1)
	if storeErr != nil || storeName != "STORE_REGISTER" || !isRegister || strings.TrimSpace(lines[mergeIndex+2]) != "avm1.pop();" {
		return "", "", 0, false
	}
	followingIndex := mergeIndex + 3
	if followingIndex >= len(lines) || !strings.Contains(lines[followingIndex], fmt.Sprintf("register%d", register)) {
		return "", "", 0, false
	}
	condition = replaceTypeScriptIdentifiers(condition, aliases)
	falseExpression = replaceTypeScriptIdentifiers(falseExpression, aliases)
	trueObject = replaceTypeScriptIdentifiers(trueObject, aliases)
	localName := fmt.Sprintf("resolvedRegister%d", register)
	if isTypeScriptIdentifier(falseExpression) && falseExpression != "this" {
		localName = "resolved" + strings.ToUpper(falseExpression[:1]) + falseExpression[1:]
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	declaration := fmt.Sprintf("%sconst %s = %s ? %s.%s() : %s;", indent, localName, condition, trueObject, method, falseExpression)
	followingLine := replaceTypeScriptIdentifiers(lines[followingIndex], map[string]string{fmt.Sprintf("register%d", register): localName})
	return declaration, followingLine, followingIndex + 1, true
}

func expandTypeScriptConditionalValues(source string) (string, error) {
	lines := strings.Split(source, "\n")
	localRegistersByLine := availableFunctionRegisterByLine(lines)
	expandedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		matches := typeScriptConditionalValuePattern.FindStringSubmatch(strings.TrimSpace(lines[lineIndex]))
		if len(matches) != 5 || lineIndex+1 >= len(lines) || localRegistersByLine[lineIndex] == 0 || !strings.Contains(lines[lineIndex+1], matches[1]) {
			expandedLines = append(expandedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		indent := lines[lineIndex][:len(lines[lineIndex])-len(strings.TrimLeft(lines[lineIndex], "\t"))]
		conditionRows, err := parseTypeScriptExpression(matches[2], nil)
		if err != nil {
			return "", fmt.Errorf("line[%d]: condition: %w", lineIndex+1, err)
		}
		trueRows, err := parseTypeScriptExpression(matches[3], nil)
		if err != nil {
			return "", fmt.Errorf("line[%d]: true: %w", lineIndex+1, err)
		}
		falseRows, err := parseTypeScriptExpression(matches[4], nil)
		if err != nil {
			return "", fmt.Errorf("line[%d]: false: %w", lineIndex+1, err)
		}
		trueLabel := fmt.Sprintf("AUTO_CONDITIONAL_TRUE_%06d", lineIndex+1)
		mergeLabel := fmt.Sprintf("AUTO_CONDITIONAL_MERGE_%06d", lineIndex+1)
		conditionLines, err := renderRows(conditionRows, indent)
		if err != nil {
			return "", fmt.Errorf("line[%d]: conditionRender: %w", lineIndex+1, err)
		}
		falseLines, err := renderRows(falseRows, indent)
		if err != nil {
			return "", fmt.Errorf("line[%d]: falseRender: %w", lineIndex+1, err)
		}
		trueLines, err := renderRows(trueRows, indent)
		if err != nil {
			return "", fmt.Errorf("line[%d]: trueRender: %w", lineIndex+1, err)
		}
		expandedLines = append(expandedLines, conditionLines...)
		expandedLines = append(expandedLines, fmt.Sprintf("%savm1.if(%q);", indent, trueLabel))
		expandedLines = append(expandedLines, falseLines...)
		expandedLines = append(expandedLines, fmt.Sprintf("%savm1.jump(%q);", indent, mergeLabel), indent+trueLabel+":")
		expandedLines = append(expandedLines, trueLines...)
		register := localRegistersByLine[lineIndex]
		expandedLines = append(expandedLines, indent+mergeLabel+":", fmt.Sprintf("%savm1.storeRegister(%d);", indent, register), indent+"avm1.pop();")
		expandedLines = append(expandedLines, replaceTypeScriptIdentifiers(lines[lineIndex+1], map[string]string{matches[1]: fmt.Sprintf("register%d", register)}))
		lineIndex += 2
	}
	return strings.Join(expandedLines, "\n"), nil
}

func availableFunctionRegisterByLine(lines []string) []int {
	registersByLine := make([]int, len(lines))
	for lineIndex := 0; lineIndex+1 < len(lines); lineIndex++ {
		row, err := parseActionStatement(strings.TrimSpace(lines[lineIndex]))
		if err != nil {
			continue
		}
		name := rowFieldStringOrEmpty(row, 0)
		if name != "FUNCTION2" || len(row) != 7 || !strings.HasPrefix(strings.TrimSpace(lines[lineIndex+1]), "function ") {
			continue
		}
		functionEnd := matchingTypeScriptBrace(lines, lineIndex+1)
		register := firstAvailableFunctionRegister(row)
		if functionEnd < 0 || register == 0 {
			continue
		}
		for bodyIndex := lineIndex + 2; bodyIndex < functionEnd; bodyIndex++ {
			registersByLine[bodyIndex] = register
		}
	}
	return registersByLine
}

func firstAvailableFunctionRegister(row []any) int {
	registerCount, isRegisterCount := rowFieldNumber(row, 1)
	flags, areFlags := rowFieldNumber(row, 2)
	parameterRegisters, areRegisters := row[3].([]any)
	if !isRegisterCount || !areFlags || !areRegisters {
		return 0
	}
	used := make(map[int]bool)
	nextImplicit := 1
	for _, flag := range []int{0x01, 0x04, 0x10, 0x40, 0x80, 0x100} {
		if flags&flag != 0 {
			used[nextImplicit] = true
			nextImplicit++
		}
	}
	for registerIndex := range parameterRegisters {
		register, isRegister := rowFieldNumber(parameterRegisters, registerIndex)
		if isRegister {
			used[register] = true
		}
	}
	for register := 1; register <= registerCount; register++ {
		if !used[register] {
			return register
		}
	}
	return 0
}

func renderRows(rows [][]any, indent string) ([]string, error) {
	lines := make([]string, 0, len(rows))
	for rowIndex, row := range rows {
		statements, err := renderActionStatements(row)
		if err != nil {
			return nil, fmt.Errorf("row[%d]: %w", rowIndex, err)
		}
		for _, statement := range statements {
			lines = append(lines, indent+statement)
		}
	}
	return lines, nil
}

func liftRenderedFunctionAliases(source string) (string, error) {
	lines := strings.Split(source, "\n")
	aliasesByLine := functionRegisterAliasesByLine(lines)
	for lineIndex, aliases := range aliasesByLine {
		lines[lineIndex] = replaceTypeScriptIdentifiers(lines[lineIndex], aliases)
	}
	return strings.Join(lines, "\n"), nil
}

func expandTypeScriptFunctionAliases(source string) (string, error) {
	lines := strings.Split(source, "\n")
	aliasesByLine := functionRegisterAliasesByLine(lines)
	for lineIndex, aliases := range aliasesByLine {
		registersByName := make(map[string]string, len(aliases))
		for registerName, visibleName := range aliases {
			registersByName[visibleName] = registerName
		}
		lines[lineIndex] = replaceTypeScriptIdentifiers(lines[lineIndex], registersByName)
	}
	return strings.Join(lines, "\n"), nil
}

func liftRenderedNullGuards(source string) (string, error) {
	lines := strings.Split(source, "\n")
	aliasesByLine := functionRegisterAliasesByLine(lines)
	referencesByLine := branchReferencesByLine(lines)
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		guard, consumedLines, isGuard := renderedNullGuard(lines[lineIndex:], aliasesByLine[lineIndex], referencesByLine[lineIndex])
		if isGuard {
			liftedLines = append(liftedLines, guard...)
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func renderedNullGuard(lines []string, aliases map[string]string, branchReferences map[string]int) ([]string, int, bool) {
	if len(lines) < 8 {
		return nil, 0, false
	}
	condition, conditionLines, isCondition := renderedCallObject(lines)
	if !isCondition || conditionLines+4 >= len(lines) || strings.TrimSpace(lines[conditionLines]) != "avm1.pushNull();" || strings.TrimSpace(lines[conditionLines+1]) != "avm1.equals2();" || strings.TrimSpace(lines[conditionLines+2]) != "avm1.not();" || strings.TrimSpace(lines[conditionLines+3]) != "avm1.not();" {
		return nil, 0, false
	}
	endLabel, isBranch := renderedBranch(lines[conditionLines+4], "IF")
	if !isBranch || branchReferences[endLabel] != 1 {
		return nil, 0, false
	}
	endIndex := -1
	for candidateIndex := conditionLines + 5; candidateIndex < len(lines); candidateIndex++ {
		trimmed := strings.TrimSpace(lines[candidateIndex])
		if trimmed == endLabel+":" {
			endIndex = candidateIndex
			break
		}
		if strings.HasPrefix(trimmed, "// block: ") || strings.HasSuffix(trimmed, ":") {
			return nil, 0, false
		}
	}
	if endIndex < 0 || endIndex == conditionLines+5 {
		return nil, 0, false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	for lineIndex := 0; lineIndex <= endIndex; lineIndex++ {
		if !strings.HasPrefix(lines[lineIndex], indent) || strings.HasPrefix(strings.TrimPrefix(lines[lineIndex], indent), "\t") {
			return nil, 0, false
		}
	}
	condition = replaceTypeScriptIdentifiers(condition, aliases)
	guard := []string{fmt.Sprintf("%sif (%s !== null) {", indent, condition)}
	for _, bodyLine := range lines[conditionLines+5 : endIndex] {
		bodyLine = replaceTypeScriptIdentifiers(bodyLine, aliases)
		guard = append(guard, indent+"\t"+strings.TrimPrefix(bodyLine, indent))
	}
	guard = append(guard, indent+"}")
	return guard, endIndex + 1, true
}

func expandTypeScriptNullGuards(source string) (string, error) {
	lines := strings.Split(source, "\n")
	expandedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		trimmed := strings.TrimSpace(lines[lineIndex])
		if !strings.HasPrefix(trimmed, "if (") || !strings.HasSuffix(trimmed, " !== null) {") {
			expandedLines = append(expandedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		guardEnd := matchingTypeScriptBrace(lines, lineIndex)
		if guardEnd <= lineIndex {
			return "", fmt.Errorf("line[%d]: nullGuardScope", lineIndex+1)
		}
		condition := strings.TrimSuffix(strings.TrimPrefix(trimmed, "if ("), " !== null) {")
		conditionRows, err := parseTypeScriptExpression(condition, nil)
		if err != nil {
			return "", fmt.Errorf("line[%d]: nullGuardCondition: %w", lineIndex+1, err)
		}
		indent := lines[lineIndex][:len(lines[lineIndex])-len(strings.TrimLeft(lines[lineIndex], "\t"))]
		conditionLines, err := renderRows(conditionRows, indent)
		if err != nil {
			return "", fmt.Errorf("line[%d]: nullGuardRender: %w", lineIndex+1, err)
		}
		label := fmt.Sprintf("AUTO_NULL_GUARD_%06d", lineIndex+1)
		expandedLines = append(expandedLines, conditionLines...)
		expandedLines = append(expandedLines,
			indent+"avm1.pushNull();",
			indent+"avm1.equals2();",
			indent+"avm1.not();",
			indent+"avm1.not();",
			fmt.Sprintf("%savm1.if(%q);", indent, label),
		)
		bodyIndent := indent + "\t"
		for _, bodyLine := range lines[lineIndex+1 : guardEnd] {
			expandedLines = append(expandedLines, indent+strings.TrimPrefix(bodyLine, bodyIndent))
		}
		expandedLines = append(expandedLines, indent+label+":")
		lineIndex = guardEnd + 1
	}
	return strings.Join(expandedLines, "\n"), nil
}

func liftRenderedPredicateGuards(source string) (string, error) {
	lines := strings.Split(source, "\n")
	aliasesByLine := functionRegisterAliasesByLine(lines)
	referencesByLine := branchReferencesByLine(lines)
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		guard, consumedLines, isGuard := renderedPredicateGuard(lines[lineIndex:], aliasesByLine[lineIndex], referencesByLine[lineIndex])
		if isGuard {
			liftedLines = append(liftedLines, guard...)
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func renderedPredicateGuard(lines []string, aliases map[string]string, branchReferences map[string]int) ([]string, int, bool) {
	if len(lines) < 8 {
		return nil, 0, false
	}
	argument, argumentLines, isArgument := renderedCallObject(lines)
	if !isArgument || argumentLines+4 >= len(lines) {
		return nil, 0, false
	}
	count, isCount := renderedArgumentCount(lines[argumentLines])
	functionName, isFunctionName := renderedMethodName(lines[argumentLines+1])
	if !isCount || count != 1 || !isFunctionName || !isTypeScriptIdentifier(functionName) || strings.TrimSpace(lines[argumentLines+2]) != "avm1.callFunction();" || strings.TrimSpace(lines[argumentLines+3]) != "avm1.not();" {
		return nil, 0, false
	}
	endLabel, isBranch := renderedBranch(lines[argumentLines+4], "IF")
	if !isBranch || branchReferences[endLabel] != 1 {
		return nil, 0, false
	}
	endIndex := -1
	for candidateIndex := argumentLines + 5; candidateIndex < len(lines); candidateIndex++ {
		trimmed := strings.TrimSpace(lines[candidateIndex])
		if trimmed == endLabel+":" {
			endIndex = candidateIndex
			break
		}
		if strings.HasPrefix(trimmed, "// block: ") || strings.HasSuffix(trimmed, ":") {
			return nil, 0, false
		}
	}
	if endIndex < 0 || endIndex == argumentLines+5 {
		return nil, 0, false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	argument = replaceTypeScriptIdentifiers(argument, aliases)
	guard := []string{fmt.Sprintf("%sif (%s(%s)) {", indent, functionName, argument)}
	for _, bodyLine := range lines[argumentLines+5 : endIndex] {
		bodyLine = replaceTypeScriptIdentifiers(bodyLine, aliases)
		guard = append(guard, indent+"\t"+strings.TrimPrefix(bodyLine, indent))
	}
	guard = append(guard, indent+"}")
	return guard, endIndex + 1, true
}

func expandTypeScriptPredicateGuards(source string) (string, error) {
	lines := strings.Split(source, "\n")
	expandedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		matches := typeScriptPredicateGuardPattern.FindStringSubmatch(strings.TrimSpace(lines[lineIndex]))
		if len(matches) != 2 {
			expandedLines = append(expandedLines, lines[lineIndex])
			lineIndex++
			continue
		}
		guardEnd := matchingTypeScriptBrace(lines, lineIndex)
		if guardEnd <= lineIndex {
			return "", fmt.Errorf("line[%d]: predicateGuardScope", lineIndex+1)
		}
		conditionRows, err := parseTypeScriptExpression(matches[1], nil)
		if err != nil {
			return "", fmt.Errorf("line[%d]: predicateGuardCondition: %w", lineIndex+1, err)
		}
		indent := lines[lineIndex][:len(lines[lineIndex])-len(strings.TrimLeft(lines[lineIndex], "\t"))]
		conditionLines, err := renderRows(conditionRows, indent)
		if err != nil {
			return "", fmt.Errorf("line[%d]: predicateGuardRender: %w", lineIndex+1, err)
		}
		label := fmt.Sprintf("AUTO_PREDICATE_GUARD_%06d", lineIndex+1)
		expandedLines = append(expandedLines, conditionLines...)
		expandedLines = append(expandedLines, indent+"avm1.not();", fmt.Sprintf("%savm1.if(%q);", indent, label))
		bodyIndent := indent + "\t"
		for _, bodyLine := range lines[lineIndex+1 : guardEnd] {
			expandedLines = append(expandedLines, indent+strings.TrimPrefix(bodyLine, bodyIndent))
		}
		expandedLines = append(expandedLines, indent+label+":")
		lineIndex = guardEnd + 1
	}
	return strings.Join(expandedLines, "\n"), nil
}
