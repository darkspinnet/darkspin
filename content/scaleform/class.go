package scaleform

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var typeScriptClassMetadataPattern = regexp.MustCompile(`^// avm1-class2 guard ("(?:[^"\\]|\\.)*") function ("(?:[^"\\]|\\.)*") (avm1\.function[12]\(.*\);)$`)
var typeScriptClassAssignmentPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.]*) = class ([A-Za-z_][A-Za-z0-9_]*) extends ([A-Za-z_][A-Za-z0-9_.]*) \{$`)
var typeScriptRootClassAssignmentPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.]*) = class ([A-Za-z_][A-Za-z0-9_]*) \{$`)

func liftRenderedClasses(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		declaration, consumedLines, isClass := renderedClass(lines[lineIndex:])
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

func renderedClass(lines []string) ([]string, int, bool) {
	if len(lines) < 25 {
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
	guardLabel, isGuard := renderedBranch(lines[4], "IF")
	assignmentBase, isAssignmentBase := renderedVariable(lines[5])
	if !isGuard || !isAssignmentBase || assignmentBase != members[0] {
		return nil, 0, false
	}
	lineIndex := 6
	parentMembers := members[1 : len(members)-1]
	if len(parentMembers) > 0 {
		assignmentMembers, areAssignmentMembers := renderedMembers(lines[lineIndex])
		if !areAssignmentMembers || !equalStrings(assignmentMembers, parentMembers) {
			return nil, 0, false
		}
		lineIndex++
	}
	className, isClassName := renderedConstant(lines[lineIndex])
	if !isClassName || className != members[len(members)-1] {
		return nil, 0, false
	}
	lineIndex++
	metadata := strings.TrimSpace(lines[lineIndex])
	if !strings.HasPrefix(metadata, "avm1.function1(") && !strings.HasPrefix(metadata, "avm1.function2(") {
		return nil, 0, false
	}
	lineIndex++
	functionName, parameters, isFunction := renderedFunctionHeader(lines[lineIndex])
	if !isFunction {
		return nil, 0, false
	}
	functionStart := lineIndex
	functionEnd := matchingTypeScriptBrace(lines, functionStart)
	if functionEnd < 0 || functionEnd+12 >= len(lines) {
		return nil, 0, false
	}
	superRegister, isSuperRegister := renderedSuperRegister(metadata)
	if !isSuperRegister {
		return nil, 0, false
	}
	superCall := []string{"avm1.pushDouble(0);", fmt.Sprintf("avm1.pushRegister(%d);", superRegister), "avm1.pushUndefined();", "avm1.callMethod();", "avm1.pop();"}
	if functionEnd-functionStart < len(superCall)+1 {
		return nil, 0, false
	}
	for superIndex, expected := range superCall {
		if strings.TrimSpace(lines[functionStart+1+superIndex]) != expected {
			return nil, 0, false
		}
	}
	lineIndex = functionEnd + 1
	if strings.TrimSpace(lines[lineIndex]) != "avm1.storeRegister(1);" || strings.TrimSpace(lines[lineIndex+1]) != "avm1.setMember();" {
		return nil, 0, false
	}
	setupStart := lineIndex + 2
	setupBase, isSetupBase := renderedVariable(lines[setupStart])
	setupMembers, areSetupMembers := renderedMembers(lines[setupStart+1])
	classBase, classBaseLines, isClassBase := renderedCallObject(lines[setupStart+2:])
	if !isSetupBase || setupBase != members[0] || !areSetupMembers || !equalStrings(setupMembers, members[1:]) || !isClassBase {
		return nil, 0, false
	}
	expectedSetup := []string{"avm1.extends();", "avm1.pushRegister(1);", "avm1.getMember(\"prototype\");", "avm1.storeRegister(2);", "avm1.pop();"}
	setupOperationStart := setupStart + 2 + classBaseLines
	for setupIndex, expected := range expectedSetup {
		if strings.TrimSpace(lines[setupOperationStart+setupIndex]) != expected {
			return nil, 0, false
		}
	}
	middleStart := setupOperationStart + len(expectedSetup)
	guardIndex := -1
	for candidateIndex := middleStart; candidateIndex+1 < len(lines); candidateIndex++ {
		if strings.TrimSpace(lines[candidateIndex]) == guardLabel+":" && strings.TrimSpace(lines[candidateIndex+1]) == "avm1.pop();" {
			guardIndex = candidateIndex
			break
		}
	}
	if guardIndex < 0 {
		return nil, 0, false
	}
	path := "_global." + strings.Join(members, ".")
	declaration := []string{
		fmt.Sprintf("%sif (!%s) {", indent, path),
		fmt.Sprintf("%s\t// avm1-class2 guard %s function %s %s", indent, strconv.Quote(guardLabel), strconv.Quote(functionName), metadata),
		fmt.Sprintf("%s\t%s = class %s extends %s {", indent, path, className, classBase),
		fmt.Sprintf("%s\t\tconstructor(%s) {", indent, parameters),
		indent + "\t\t\tsuper();",
	}
	for _, bodyLine := range lines[functionStart+1+len(superCall) : functionEnd] {
		declaration = append(declaration, indent+"\t\t\t"+strings.TrimPrefix(bodyLine, indent+"\t"))
	}
	declaration = append(declaration, indent+"\t\t}", indent+"\t\tstatic {")
	for _, middleLine := range lines[middleStart:guardIndex] {
		declaration = append(declaration, indent+"\t\t\t"+strings.TrimPrefix(middleLine, indent))
	}
	declaration = append(declaration, indent+"\t\t}", indent+"\t};", indent+"}")
	return declaration, guardIndex + 2, true
}

func liftRenderedRootClasses(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		declaration, consumedLines, isClass := renderedRootClass(lines[lineIndex:])
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

func renderedRootClass(lines []string) ([]string, int, bool) {
	if len(lines) < 18 {
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
	guardLabel, isGuard := renderedBranch(lines[4], "IF")
	assignmentBase, isAssignmentBase := renderedVariable(lines[5])
	if !isGuard || !isAssignmentBase || assignmentBase != members[0] {
		return nil, 0, false
	}
	lineIndex := 6
	parentMembers := members[1 : len(members)-1]
	if len(parentMembers) > 0 {
		assignmentMembers, areAssignmentMembers := renderedMembers(lines[lineIndex])
		if !areAssignmentMembers || !equalStrings(assignmentMembers, parentMembers) {
			return nil, 0, false
		}
		lineIndex++
	}
	className, isClassName := renderedConstant(lines[lineIndex])
	if !isClassName || className != members[len(members)-1] || !isTypeScriptIdentifier(className) {
		return nil, 0, false
	}
	lineIndex++
	_, isConstructor := renderedEmptyFunction1(lines[lineIndex:])
	if !isConstructor {
		return nil, 0, false
	}
	lineIndex += 3
	expectedSetup := []string{
		"avm1.storeRegister(1);",
		"avm1.setMember();",
		"avm1.pushRegister(1);",
		"avm1.getMember(\"prototype\");",
		"avm1.storeRegister(2);",
		"avm1.pop();",
	}
	if lineIndex+len(expectedSetup) >= len(lines) {
		return nil, 0, false
	}
	for setupIndex, expected := range expectedSetup {
		if strings.TrimSpace(lines[lineIndex+setupIndex]) != expected {
			return nil, 0, false
		}
	}
	middleStart := lineIndex + len(expectedSetup)
	guardIndex := -1
	for candidateIndex := middleStart; candidateIndex+1 < len(lines); candidateIndex++ {
		if strings.TrimSpace(lines[candidateIndex]) == guardLabel+":" && strings.TrimSpace(lines[candidateIndex+1]) == "avm1.pop();" {
			guardIndex = candidateIndex
			break
		}
	}
	if guardIndex < 0 {
		return nil, 0, false
	}
	path := "_global." + strings.Join(members, ".")
	declaration := []string{
		fmt.Sprintf("%sif (!%s) {", indent, path),
		fmt.Sprintf("%s\t%s = class %s {", indent, path, className),
		indent + "\t\tstatic {",
	}
	for _, middleLine := range lines[middleStart:guardIndex] {
		declaration = append(declaration, indent+"\t\t\t"+strings.TrimPrefix(middleLine, indent))
	}
	declaration = append(declaration, indent+"\t\t}", indent+"\t};", indent+"}")
	return declaration, guardIndex + 2, true
}

func expandTypeScriptClasses(source string) (string, error) {
	lines := strings.Split(source, "\n")
	expandedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		expanded, consumedLines, isClass, err := expandedTypeScriptClass(lines[lineIndex:])
		if err != nil {
			return "", fmt.Errorf("class[%d]: %w", lineIndex+1, err)
		}
		if isClass {
			expandedLines = append(expandedLines, expanded...)
			lineIndex += consumedLines
			continue
		}
		expandedLines = append(expandedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(expandedLines, "\n"), nil
}

func expandTypeScriptRootClasses(source string) (string, error) {
	lines := strings.Split(source, "\n")
	expandedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		expanded, consumedLines, isClass, err := expandedTypeScriptRootClass(lines[lineIndex:], lineIndex)
		if err != nil {
			return "", fmt.Errorf("rootClass[%d]: %w", lineIndex+1, err)
		}
		if isClass {
			expandedLines = append(expandedLines, expanded...)
			lineIndex += consumedLines
			continue
		}
		expandedLines = append(expandedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(expandedLines, "\n"), nil
}

func expandedTypeScriptRootClass(lines []string, sourceLine int) ([]string, int, bool, error) {
	if len(lines) < 6 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "if (!_global.") {
		return nil, 0, false, nil
	}
	assignmentMatches := typeScriptRootClassAssignmentPattern.FindStringSubmatch(strings.TrimSpace(lines[1]))
	if len(assignmentMatches) != 3 || strings.TrimSpace(lines[2]) != "static {" {
		return nil, 0, false, nil
	}
	path := assignmentMatches[1]
	className := assignmentMatches[2]
	guardPath := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(lines[0]), "if (!"), ") {")
	if path != guardPath || !strings.HasPrefix(path, "_global.") {
		return nil, 0, true, fmt.Errorf("path: %q", path)
	}
	members := strings.Split(strings.TrimPrefix(path, "_global."), ".")
	if len(members) < 2 || members[len(members)-1] != className {
		return nil, 0, true, fmt.Errorf("className: %q", className)
	}
	staticEnd := matchingTypeScriptBrace(lines, 2)
	if staticEnd < 0 || staticEnd+2 >= len(lines) || strings.TrimSpace(lines[staticEnd+1]) != "};" || strings.TrimSpace(lines[staticEnd+2]) != "}" {
		return nil, 0, true, fmt.Errorf("classScope")
	}
	guardLabel := fmt.Sprintf("L_ROOT_CLASS_%06d", sourceLine+1)
	functionName := fmt.Sprintf("anonymous_root_class_%06d", sourceLine+1)
	constructorLabel := fmt.Sprintf("L_ROOT_CLASS_CONSTRUCTOR_%06d", sourceLine+1)
	expanded := []string{
		"avm1.pushVariable(\"_global\");",
		fmt.Sprintf("avm1.getMember(%s);", quotedTypeScriptMembers(members)),
		"avm1.not();",
		"avm1.not();",
		fmt.Sprintf("avm1.if(%q);", guardLabel),
		fmt.Sprintf("avm1.pushVariable(%q);", members[0]),
	}
	if len(members) > 2 {
		expanded = append(expanded, fmt.Sprintf("avm1.getMember(%s);", quotedTypeScriptMembers(members[1:len(members)-1])))
	}
	expanded = append(expanded,
		fmt.Sprintf("avm1.pushConstant(%q);", className),
		fmt.Sprintf("avm1.function1([], %q, \"\");", constructorLabel),
		fmt.Sprintf("function %s(): void {", functionName),
		"}",
		"avm1.storeRegister(1);",
		"avm1.setMember();",
		"avm1.pushRegister(1);",
		"avm1.getMember(\"prototype\");",
		"avm1.storeRegister(2);",
		"avm1.pop();",
	)
	for _, middleLine := range lines[3:staticEnd] {
		expanded = append(expanded, strings.TrimSpace(middleLine))
	}
	expanded = append(expanded, guardLabel+":", "avm1.pop();")
	return expanded, staticEnd + 3, true, nil
}

func expandedTypeScriptClass(lines []string) ([]string, int, bool, error) {
	if len(lines) < 9 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "if (!_global.") {
		return nil, 0, false, nil
	}
	metadataMatches := typeScriptClassMetadataPattern.FindStringSubmatch(strings.TrimSpace(lines[1]))
	assignmentMatches := typeScriptClassAssignmentPattern.FindStringSubmatch(strings.TrimSpace(lines[2]))
	if len(metadataMatches) != 4 || len(assignmentMatches) != 4 {
		return nil, 0, false, nil
	}
	guardLabel, err := strconv.Unquote(metadataMatches[1])
	if err != nil {
		return nil, 0, true, fmt.Errorf("guard: %w", err)
	}
	functionName, err := strconv.Unquote(metadataMatches[2])
	if err != nil {
		return nil, 0, true, fmt.Errorf("function: %w", err)
	}
	path := assignmentMatches[1]
	className := assignmentMatches[2]
	classBase := assignmentMatches[3]
	superRegister, isSuperRegister := renderedSuperRegister(metadataMatches[3])
	if !isSuperRegister {
		return nil, 0, true, fmt.Errorf("superRegister")
	}
	guardPath := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(lines[0]), "if (!"), ") {")
	if path != guardPath || !strings.HasPrefix(path, "_global.") {
		return nil, 0, true, fmt.Errorf("path: %q", path)
	}
	constructorParameters, isConstructor := renderedConstructorHeader(lines[3])
	if !isConstructor {
		return nil, 0, true, fmt.Errorf("constructor: %q", strings.TrimSpace(lines[3]))
	}
	constructorEnd := matchingTypeScriptBrace(lines, 3)
	if constructorEnd < 0 || constructorEnd+3 >= len(lines) || strings.TrimSpace(lines[constructorEnd+1]) != "static {" {
		return nil, 0, true, fmt.Errorf("constructorScope")
	}
	staticEnd := matchingTypeScriptBrace(lines, constructorEnd+1)
	if staticEnd < 0 || staticEnd+2 >= len(lines) || strings.TrimSpace(lines[staticEnd+1]) != "};" || strings.TrimSpace(lines[staticEnd+2]) != "}" {
		return nil, 0, true, fmt.Errorf("classScope")
	}
	members := strings.Split(strings.TrimPrefix(path, "_global."), ".")
	if len(members) < 2 || members[len(members)-1] != className {
		return nil, 0, true, fmt.Errorf("className: %q", className)
	}
	expanded := []string{
		"avm1.pushVariable(\"_global\");",
		fmt.Sprintf("avm1.getMember(%s);", quotedTypeScriptMembers(members)),
		"avm1.not();",
		"avm1.not();",
		fmt.Sprintf("avm1.if(%q);", guardLabel),
		fmt.Sprintf("avm1.pushVariable(%q);", members[0]),
	}
	if len(members) > 2 {
		expanded = append(expanded, fmt.Sprintf("avm1.getMember(%s);", quotedTypeScriptMembers(members[1:len(members)-1])))
	}
	expanded = append(expanded,
		fmt.Sprintf("avm1.pushConstant(%q);", className),
		metadataMatches[3],
		fmt.Sprintf("function %s(%s): void {", functionName, constructorParameters),
		"\tavm1.pushDouble(0);",
		fmt.Sprintf("\tavm1.pushRegister(%d);", superRegister),
		"\tavm1.pushUndefined();",
		"\tavm1.callMethod();",
		"\tavm1.pop();",
	)
	if constructorEnd < 5 || strings.TrimSpace(lines[4]) != "super();" {
		return nil, 0, true, fmt.Errorf("superCall")
	}
	for _, bodyLine := range lines[5:constructorEnd] {
		expanded = append(expanded, "\t"+strings.TrimSpace(bodyLine))
	}
	expanded = append(expanded,
		"}",
		"avm1.storeRegister(1);",
		"avm1.setMember();",
		fmt.Sprintf("avm1.pushVariable(%q);", members[0]),
		fmt.Sprintf("avm1.getMember(%s);", quotedTypeScriptMembers(members[1:])),
	)
	classBaseLines, classBaseErr := renderedReferencePush(classBase, "")
	if classBaseErr != nil {
		return nil, 0, true, fmt.Errorf("classBase: %w", classBaseErr)
	}
	expanded = append(expanded, classBaseLines...)
	expanded = append(expanded,
		"avm1.extends();",
		"avm1.pushRegister(1);",
		"avm1.getMember(\"prototype\");",
		"avm1.storeRegister(2);",
		"avm1.pop();",
	)
	for _, middleLine := range lines[constructorEnd+2 : staticEnd] {
		expanded = append(expanded, strings.TrimSpace(middleLine))
	}
	expanded = append(expanded, guardLabel+":", "avm1.pop();")
	return expanded, staticEnd + 3, true, nil
}

func renderedSuperRegister(metadata string) (int, bool) {
	row, err := parseActionStatement(metadata)
	if err != nil {
		return 0, false
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name == "FUNCTION1" {
		return 2, true
	}
	if name != "FUNCTION2" || len(row) != 7 {
		return 0, false
	}
	flags, areFlags := rowFieldNumber(row, 2)
	if !areFlags || flags&0x10 == 0 {
		return 0, false
	}
	register := 1
	if flags&0x01 != 0 {
		register++
	}
	if flags&0x04 != 0 {
		register++
	}
	return register, true
}

func renderedFunctionHeader(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "function ") || !strings.HasSuffix(line, "): void {") {
		return "", "", false
	}
	openIndex := strings.IndexByte(line, '(')
	if openIndex < 10 {
		return "", "", false
	}
	return line[len("function "):openIndex], line[openIndex+1 : len(line)-len("): void {")], true
}

func renderedConstructorHeader(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "constructor(") || !strings.HasSuffix(line, ") {") {
		return "", false
	}
	return line[len("constructor(") : len(line)-len(") {")], true
}

func matchingTypeScriptBrace(lines []string, start int) int {
	depth := 0
	for lineIndex := start; lineIndex < len(lines); lineIndex++ {
		depth += unquotedBraceCount(lines[lineIndex], '{')
		depth -= unquotedBraceCount(lines[lineIndex], '}')
		if depth == 0 {
			return lineIndex
		}
	}
	return -1
}

func unquotedBraceCount(line string, brace byte) int {
	count := 0
	isQuoted := false
	isEscaped := false
	for characterIndex := 0; characterIndex < len(line); characterIndex++ {
		character := line[characterIndex]
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
		if !isQuoted && character == brace {
			count++
		}
	}
	return count
}

func quotedTypeScriptMembers(members []string) string {
	quotedMembers := make([]string, len(members))
	for memberIndex, member := range members {
		quotedMembers[memberIndex] = strconv.Quote(member)
	}
	return strings.Join(quotedMembers, ", ")
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
