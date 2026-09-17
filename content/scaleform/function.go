package scaleform

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var typeScriptMemberFunctionMetadataPattern = regexp.MustCompile(`^// avm1-member-function target ("(?:[^"\\]|\\.)*") function ("(?:[^"\\]|\\.)*") (avm1\.function[12]\(.*\);)$`)
var typeScriptMemberFunctionPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.]*) = function \((.*)\): AVM1Value \{$`)

func liftRenderedMemberFunctions(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	aliasesByLine := memberFunctionAliases(lines)
	for lineIndex := 0; lineIndex < len(lines); {
		declaration, consumedLines, isFunction := renderedMemberFunction(lines[lineIndex:], aliasesByLine[lineIndex])
		if isFunction {
			liftedLines = append(liftedLines, declaration...)
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func renderedMemberFunction(lines []string, aliases map[string]string) ([]string, int, bool) {
	if len(lines) < 6 {
		return nil, 0, false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	target, targetLines, isTarget := renderedCallObject(lines)
	if !isTarget || !strings.HasPrefix(target, "register") {
		return nil, 0, false
	}
	propertyIndex := targetLines
	propertyName, isProperty := renderedMethodName(lines[propertyIndex])
	if !isProperty || !isTypeScriptIdentifier(propertyName) {
		return nil, 0, false
	}
	metadata := strings.TrimSpace(lines[propertyIndex+1])
	if !strings.HasPrefix(metadata, "avm1.function1(") && !strings.HasPrefix(metadata, "avm1.function2(") {
		return nil, 0, false
	}
	functionName, parameters, isFunction := renderedFunctionHeader(lines[propertyIndex+2])
	if !isFunction {
		return nil, 0, false
	}
	functionEnd := matchingTypeScriptBrace(lines, propertyIndex+2)
	if functionEnd < 0 || functionEnd+1 >= len(lines) || strings.TrimSpace(lines[functionEnd+1]) != "avm1.setMember();" {
		return nil, 0, false
	}
	displayTarget := target
	for registerName, alias := range aliases {
		if target == registerName || strings.HasPrefix(target, registerName+".") {
			displayTarget = alias + strings.TrimPrefix(target, registerName)
			break
		}
	}
	declaration := []string{
		fmt.Sprintf("%s// avm1-member-function target %s function %s %s", indent, strconv.Quote(target), strconv.Quote(functionName), metadata),
		fmt.Sprintf("%s%s.%s = function (%s): AVM1Value {", indent, displayTarget, propertyName, parameters),
	}
	registerNames := memberFunctionRegisterNames(metadata)
	for _, bodyLine := range lines[propertyIndex+3 : functionEnd] {
		declaration = append(declaration, replaceTypeScriptIdentifiers(bodyLine, registerNames))
	}
	declaration = append(declaration, indent+"};")
	return declaration, functionEnd + 2, true
}

func memberFunctionAliases(lines []string) []map[string]string {
	aliasesByLine := make([]map[string]string, len(lines))
	aliases := make(map[string]string)
	guardLabel := ""
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		if lineIndex+4 < len(lines) {
			baseName, isBase := renderedVariable(lines[lineIndex])
			members, areMembers := renderedMembers(lines[lineIndex+1])
			branchLabel, isBranch := renderedBranch(lines[lineIndex+4], "IF")
			if isBase && baseName == "_global" && len(members) > 0 && areMembers && strings.TrimSpace(lines[lineIndex+2]) == "avm1.not();" && strings.TrimSpace(lines[lineIndex+3]) == "avm1.not();" && isBranch {
				path := strings.Join(members, ".")
				aliases = map[string]string{"register1": path, "register2": path + ".prototype"}
				guardLabel = branchLabel
			}
		}
		aliasesByLine[lineIndex] = aliases
		if guardLabel != "" && strings.TrimSpace(lines[lineIndex]) == guardLabel+":" {
			aliases = make(map[string]string)
			guardLabel = ""
		}
	}
	return aliasesByLine
}

func expandTypeScriptMemberFunctions(source string) (string, error) {
	lines := strings.Split(source, "\n")
	expandedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		expanded, consumedLines, isFunction, err := expandedTypeScriptMemberFunction(lines[lineIndex:])
		if err != nil {
			return "", fmt.Errorf("memberFunction[%d]: %w", lineIndex+1, err)
		}
		if isFunction {
			expandedLines = append(expandedLines, expanded...)
			lineIndex += consumedLines
			continue
		}
		expandedLines = append(expandedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(expandedLines, "\n"), nil
}

func expandedTypeScriptMemberFunction(lines []string) ([]string, int, bool, error) {
	if len(lines) < 4 {
		return nil, 0, false, nil
	}
	metadataMatches := typeScriptMemberFunctionMetadataPattern.FindStringSubmatch(strings.TrimSpace(lines[0]))
	functionMatches := typeScriptMemberFunctionPattern.FindStringSubmatch(strings.TrimSpace(lines[1]))
	if len(metadataMatches) != 4 || len(functionMatches) != 3 {
		return nil, 0, false, nil
	}
	target, err := strconv.Unquote(metadataMatches[1])
	if err != nil {
		return nil, 0, true, fmt.Errorf("target: %w", err)
	}
	functionName, err := strconv.Unquote(metadataMatches[2])
	if err != nil {
		return nil, 0, true, fmt.Errorf("functionName: %w", err)
	}
	functionEnd := matchingTypeScriptBrace(lines, 1)
	if functionEnd < 0 || strings.TrimSpace(lines[functionEnd]) != "};" {
		return nil, 0, true, fmt.Errorf("functionScope")
	}
	pathParts := strings.Split(target, ".")
	register, err := strconv.ParseUint(strings.TrimPrefix(pathParts[0], "register"), 10, 8)
	if err != nil {
		return nil, 0, true, fmt.Errorf("register: %w", err)
	}
	expanded := []string{fmt.Sprintf("avm1.pushRegister(%d);", register)}
	for _, member := range pathParts[1:] {
		expanded = append(expanded, fmt.Sprintf("avm1.pushConstant(%q);", member), "avm1.getMember();")
	}
	visibleParts := strings.Split(functionMatches[1], ".")
	propertyName := visibleParts[len(visibleParts)-1]
	expanded = append(expanded,
		fmt.Sprintf("avm1.pushConstant(%q);", propertyName),
		metadataMatches[3],
		fmt.Sprintf("function %s(%s): void {", functionName, functionMatches[2]),
	)
	registerNames := memberFunctionRegisterNames(metadataMatches[3])
	parameterRegisters := make(map[string]string, len(registerNames))
	for registerName, parameterName := range registerNames {
		parameterRegisters[parameterName] = registerName
	}
	for _, bodyLine := range lines[2:functionEnd] {
		bodyLine = replaceTypeScriptIdentifiers(bodyLine, parameterRegisters)
		expanded = append(expanded, "\t"+strings.TrimSpace(bodyLine))
	}
	expanded = append(expanded, "}", "avm1.setMember();")
	return expanded, functionEnd + 1, true, nil
}

func memberFunctionRegisterNames(metadata string) map[string]string {
	names := make(map[string]string)
	row, err := parseActionStatement(metadata)
	if err != nil {
		return names
	}
	name := rowFieldStringOrEmpty(row, 0)
	if name != "FUNCTION2" || len(row) != 7 {
		return names
	}
	flags, areFlags := rowFieldNumber(row, 2)
	if areFlags && flags&0x01 != 0 {
		names["register1"] = "this"
	}
	registers, areRegisters := row[3].([]any)
	parameters, areParameters := row[4].([]any)
	if !areRegisters || !areParameters || len(registers) != len(parameters) {
		return names
	}
	for parameterIndex, parameter := range parameters {
		register, isRegister := rowFieldNumber(registers, parameterIndex)
		parameterName, isParameter := parameter.(string)
		if !isRegister || !isParameter || !isTypeScriptIdentifier(parameterName) {
			continue
		}
		names[fmt.Sprintf("register%d", int(register))] = parameterName
	}
	return names
}

func replaceTypeScriptIdentifiers(line string, replacements map[string]string) string {
	if len(replacements) == 0 {
		return line
	}
	var replaced strings.Builder
	isQuoted := false
	isEscaped := false
	for characterIndex := 0; characterIndex < len(line); {
		character := line[characterIndex]
		if isEscaped {
			replaced.WriteByte(character)
			isEscaped = false
			characterIndex++
			continue
		}
		if isQuoted && character == '\\' {
			replaced.WriteByte(character)
			isEscaped = true
			characterIndex++
			continue
		}
		if character == '"' {
			replaced.WriteByte(character)
			isQuoted = !isQuoted
			characterIndex++
			continue
		}
		isIdentifierStart := character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		if !isQuoted && isIdentifierStart {
			identifierEnd := characterIndex + 1
			for identifierEnd < len(line) {
				identifierCharacter := line[identifierEnd]
				isIdentifier := identifierCharacter == '_' || identifierCharacter >= 'a' && identifierCharacter <= 'z' || identifierCharacter >= 'A' && identifierCharacter <= 'Z' || identifierCharacter >= '0' && identifierCharacter <= '9'
				if !isIdentifier {
					break
				}
				identifierEnd++
			}
			identifier := line[characterIndex:identifierEnd]
			if replacement := replacements[identifier]; replacement != "" {
				identifier = replacement
			}
			replaced.WriteString(identifier)
			characterIndex = identifierEnd
			continue
		}
		replaced.WriteByte(character)
		characterIndex++
	}
	return replaced.String()
}
