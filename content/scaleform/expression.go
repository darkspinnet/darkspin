package scaleform

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type renderedExpression struct {
	source       string
	stringValue  *string
	integerValue *int
}

func liftRenderedExpressions(source string) (string, error) {
	lines := strings.Split(source, "\n")
	liftedLines := make([]string, 0, len(lines))
	for lineIndex := 0; lineIndex < len(lines); {
		statement, consumedLines, isExpression := renderedExpressionAssignment(lines[lineIndex:])
		if isExpression {
			liftedLines = append(liftedLines, statement)
			lineIndex += consumedLines
			continue
		}
		liftedLines = append(liftedLines, lines[lineIndex])
		lineIndex++
	}
	return strings.Join(liftedLines, "\n"), nil
}

func renderedExpressionAssignment(lines []string) (string, int, bool) {
	if len(lines) < 4 {
		return "", 0, false
	}
	indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], "\t"))]
	stack := make([]renderedExpression, 0, 12)
	for lineIndex, sourceLine := range lines {
		if !strings.HasPrefix(sourceLine, indent) || strings.HasPrefix(strings.TrimPrefix(sourceLine, indent), "\t") {
			return "", 0, false
		}
		line := strings.TrimSpace(sourceLine)
		if line == "avm1.setMember();" {
			if len(stack) != 3 || stack[1].stringValue == nil || !isTypeScriptReferencePath(stack[0].source) || !isTypeScriptIdentifier(*stack[1].stringValue) {
				return "", 0, false
			}
			return fmt.Sprintf("%s%s.%s = %s;", indent, stack[0].source, *stack[1].stringValue, stack[2].source), lineIndex + 1, true
		}
		if line == "avm1.setVariable();" {
			if len(stack) != 2 || stack[0].stringValue == nil || !isTypeScriptIdentifier(*stack[0].stringValue) {
				return "", 0, false
			}
			return fmt.Sprintf("%s%s = %s;", indent, *stack[0].stringValue, stack[1].source), lineIndex + 1, true
		}
		if line == "avm1.callMethod();" || line == "avm1.newMethod();" {
			if len(stack) < 3 {
				return "", 0, false
			}
			method := stack[len(stack)-1]
			object := stack[len(stack)-2]
			count := stack[len(stack)-3]
			if method.stringValue == nil || count.integerValue == nil || *count.integerValue < 0 || *count.integerValue > len(stack)-3 || !isTypeScriptIdentifier(*method.stringValue) || !isTypeScriptReferencePath(object.source) {
				return "", 0, false
			}
			argumentCount := *count.integerValue
			argumentStart := len(stack) - 3 - argumentCount
			arguments := make([]string, 0, argumentCount)
			for argumentIndex := len(stack) - 4; argumentIndex >= argumentStart; argumentIndex-- {
				arguments = append(arguments, stack[argumentIndex].source)
			}
			callSource := fmt.Sprintf("%s.%s(%s)", object.source, *method.stringValue, strings.Join(arguments, ", "))
			if line == "avm1.newMethod();" {
				callSource = "new " + callSource
			}
			stack = append(stack[:argumentStart], renderedExpression{source: callSource})
			continue
		}
		members, areMembers := renderedMembers(line)
		if areMembers {
			if len(stack) == 0 {
				return "", 0, false
			}
			stack[len(stack)-1] = renderedExpression{source: stack[len(stack)-1].source + "." + strings.Join(members, ".")}
			continue
		}
		operator := renderedBinaryOperator(line)
		if operator != "" {
			if len(stack) < 2 {
				return "", 0, false
			}
			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = append(stack[:len(stack)-2], renderedExpression{source: fmt.Sprintf("(%s %s %s)", left.source, operator, right.source)})
			continue
		}
		expression, isExpression := renderedPushedExpression(line)
		if !isExpression {
			return "", 0, false
		}
		stack = append(stack, expression)
	}
	return "", 0, false
}

func renderedPushedExpression(line string) (renderedExpression, bool) {
	if variable, isVariable := renderedVariable(line); isVariable && isTypeScriptReferencePath(variable) {
		return renderedExpression{source: variable}, true
	}
	if register, isRegister := renderedRegister(line); isRegister {
		return renderedExpression{source: register}, true
	}
	if constant, isConstant := renderedConstant(line); isConstant {
		return renderedExpression{source: strconv.Quote(constant), stringValue: &constant}, true
	}
	row, err := parseActionStatement(line)
	if err != nil {
		return renderedExpression{}, false
	}
	source, isSource := simplePushedSource(row)
	if !isSource {
		return renderedExpression{}, false
	}
	expression := renderedExpression{source: source}
	name := rowFieldStringOrEmpty(row, 0)
	if name == "PUSH" && len(row) == 2 {
		item, isItem := row[1].([]any)
		if isItem {
			kind := rowFieldStringOrEmpty(item, 0)
			integer, isInteger := rowFieldNumber(item, 1)
			if kind == "INT" && isInteger {
				expression.integerValue = &integer
			}
			if kind == "DOUBLE" && isInteger && integer == 0 {
				expression.integerValue = &integer
			}
		}
	}
	return expression, true
}

func renderedBinaryOperator(line string) string {
	switch line {
	case "avm1.add();", "avm1.add2();":
		return "+"
	case "avm1.subtract();":
		return "-"
	case "avm1.multiply();":
		return "*"
	case "avm1.divide();":
		return "/"
	default:
		return ""
	}
}

func parseTypeScriptExpression(source string, constants []string) ([][]any, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, errors.New("expressionEmpty")
	}
	for hasOuterParentheses(source) {
		source = strings.TrimSpace(source[1 : len(source)-1])
	}
	if strings.HasPrefix(source, "new ") {
		rows, err := parseTypeScriptExpression(strings.TrimSpace(strings.TrimPrefix(source, "new ")), constants)
		if err != nil {
			return nil, fmt.Errorf("constructor: %w", err)
		}
		if len(rows) == 0 {
			return nil, errors.New("constructorEmpty")
		}
		name := rowFieldStringOrEmpty(rows[len(rows)-1], 0)
		if name != "CALL_METHOD" {
			return nil, errors.New("constructorCall")
		}
		rows[len(rows)-1] = []any{"NEW_METHOD"}
		return rows, nil
	}
	operatorIndex, operator := topLevelBinaryOperator(source)
	if operatorIndex >= 0 {
		leftRows, err := parseTypeScriptExpression(source[:operatorIndex], constants)
		if err != nil {
			return nil, fmt.Errorf("left: %w", err)
		}
		rightRows, err := parseTypeScriptExpression(source[operatorIndex+3:], constants)
		if err != nil {
			return nil, fmt.Errorf("right: %w", err)
		}
		return append(append(leftRows, rightRows...), []any{operator}), nil
	}
	callIndex := topLevelCallOpen(source)
	if callIndex > 0 && strings.HasSuffix(source, ")") {
		path := source[:callIndex]
		pathParts := strings.Split(path, ".")
		if !isTypeScriptReferencePath(path) {
			return nil, fmt.Errorf("callPath: %q", path)
		}
		arguments, err := splitMemberCallArguments(source[callIndex+1 : len(source)-1])
		if err != nil {
			return nil, fmt.Errorf("callArguments: %w", err)
		}
		rows := make([][]any, 0)
		for argumentIndex := len(arguments) - 1; argumentIndex >= 0; argumentIndex-- {
			argumentRows, expressionErr := parseTypeScriptExpression(arguments[argumentIndex], constants)
			if expressionErr != nil {
				return nil, fmt.Errorf("callArgument[%d]: %w", argumentIndex, expressionErr)
			}
			rows = append(rows, argumentRows...)
		}
		if len(arguments) == 0 {
			rows = append(rows, []any{"PUSH", []any{"DOUBLE", float64(0)}})
		} else {
			rows = append(rows, []any{"PUSH", []any{"INT", float64(len(arguments))}})
		}
		if len(pathParts) == 1 {
			functionItem := namedPushItem(pathParts[0], constants)
			rows = append(rows, []any{"PUSH", functionItem}, []any{"CALL_FUNCTION"})
			return rows, nil
		}
		receiverRows, err := parseReferencePath(pathParts[:len(pathParts)-1], constants)
		if err != nil {
			return nil, fmt.Errorf("callReceiver: %w", err)
		}
		rows = append(rows, receiverRows...)
		methodItem := namedPushItem(pathParts[len(pathParts)-1], constants)
		rows = append(rows, []any{"PUSH", methodItem}, []any{"CALL_METHOD"})
		return rows, nil
	}
	if isTypeScriptReferencePath(source) {
		return parseReferencePath(strings.Split(source, "."), constants)
	}
	item, err := parseAssignmentValue(source)
	if err != nil {
		return nil, fmt.Errorf("literal: %w", err)
	}
	return [][]any{{"PUSH", item}}, nil
}

func parseReferencePath(parts []string, constants []string) ([][]any, error) {
	if len(parts) == 0 || !isTypeScriptReferencePath(strings.Join(parts, ".")) {
		return nil, errors.New("referencePath")
	}
	rows := make([][]any, 0)
	if strings.HasPrefix(parts[0], "register") {
		register, err := strconv.ParseUint(strings.TrimPrefix(parts[0], "register"), 10, 8)
		if err != nil {
			return nil, fmt.Errorf("register: %w", err)
		}
		rows = append(rows, []any{"PUSH", []any{"REGISTER", float64(register)}})
	} else {
		baseItem := namedPushItem(parts[0], constants)
		rows = append(rows, []any{"PUSH", baseItem}, []any{"GET_VARIABLE"})
	}
	for _, member := range parts[1:] {
		memberItem := namedPushItem(member, constants)
		rows = append(rows, []any{"PUSH", memberItem}, []any{"GET_MEMBER"})
	}
	return rows, nil
}

func hasOuterParentheses(source string) bool {
	if len(source) < 2 || source[0] != '(' || source[len(source)-1] != ')' {
		return false
	}
	depth := 0
	for index, character := range source {
		switch character {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && index != len(source)-1 {
				return false
			}
		}
	}
	return depth == 0
}

func topLevelBinaryOperator(source string) (int, string) {
	depth := 0
	isQuoted := false
	isEscaped := false
	additiveIndex := -1
	additiveOperator := ""
	multiplicativeIndex := -1
	multiplicativeOperator := ""
	for index := 0; index < len(source); index++ {
		character := source[index]
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
		switch character {
		case ')':
			depth--
		case '(':
			depth++
		}
		if depth == 0 && index >= 1 && index+1 < len(source) && source[index-1] == ' ' && source[index+1] == ' ' {
			switch character {
			case '+':
				additiveIndex, additiveOperator = index-1, "ADD2"
			case '-':
				additiveIndex, additiveOperator = index-1, "SUBTRACT"
			case '*':
				multiplicativeIndex, multiplicativeOperator = index-1, "MULTIPLY"
			case '/':
				multiplicativeIndex, multiplicativeOperator = index-1, "DIVIDE"
			}
		}
	}
	if additiveIndex >= 0 {
		return additiveIndex, additiveOperator
	}
	if multiplicativeIndex >= 0 {
		return multiplicativeIndex, multiplicativeOperator
	}
	return -1, ""
}

func topLevelCallOpen(source string) int {
	depth := 0
	isQuoted := false
	isEscaped := false
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
		switch character {
		case '(':
			if depth == 0 {
				return index
			}
			depth++
		case ')':
			depth--
		}
	}
	return -1
}
