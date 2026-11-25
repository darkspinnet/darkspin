package lua51

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/yuin/gopher-lua/ast"
	"github.com/yuin/gopher-lua/parse"
)

// Definitions describes external names observed in Lua sources without running
// them. Observations establish neither a native signature nor a runtime type.
// Source-owned globals and Lua standard-library names are deliberately omitted.
func Definitions(sources [][]byte) ([]byte, error) {
	collector := definitionCollector{
		paths:      make(map[string]bool),
		calls:      make(map[string]bool),
		ownedPaths: make(map[string]bool),
	}
	for index, source := range sources {
		statements, err := parse.Parse(bytes.NewReader(source), fmt.Sprintf("source[%d]", index))
		if err != nil {
			return nil, fmt.Errorf("sourceParse[%d]: %w", index, err)
		}
		collector.statements(statements, &definitionScope{names: make(map[string]bool)})
	}
	return collector.render(), nil
}

type definitionCollector struct {
	paths      map[string]bool
	calls      map[string]bool
	ownedPaths map[string]bool
}

type definitionScope struct {
	parent *definitionScope
	names  map[string]bool
}

func (e *definitionScope) isLocal(name string) bool {
	for scope := e; scope != nil; scope = scope.parent {
		if scope.names[name] {
			return true
		}
	}
	return false
}

func (e *definitionScope) child(names ...string) *definitionScope {
	child := &definitionScope{parent: e, names: make(map[string]bool)}
	for _, name := range names {
		child.names[name] = true
	}
	return child
}

func definitionPath(expression ast.Expr, scope *definitionScope) string {
	switch expression := expression.(type) {
	case *ast.IdentExpr:
		if scope.isLocal(expression.Value) || !isDefinitionIdentifier(expression.Value) {
			return ""
		}
		return expression.Value
	case *ast.AttrGetExpr:
		key, isString := expression.Key.(*ast.StringExpr)
		if !isString || !isDefinitionIdentifier(key.Value) {
			return ""
		}
		parent := definitionPath(expression.Object, scope)
		if parent == "" {
			return ""
		}
		return parent + "." + key.Value
	default:
		return ""
	}
}

func isDefinitionIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' {
			continue
		}
		if index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return !strings.Contains(" and break do else elseif end false for function if in local nil not or repeat return then true until while ", " "+name+" ")
}

func (e *definitionCollector) expressions(expressions []ast.Expr, scope *definitionScope) {
	for _, expression := range expressions {
		e.expression(expression, scope)
	}
}

func (e *definitionCollector) expression(expression ast.Expr, scope *definitionScope) {
	path := definitionPath(expression, scope)
	if path != "" {
		e.paths[path] = true
	}
	switch expression := expression.(type) {
	case *ast.AttrGetExpr:
		e.expression(expression.Object, scope)
		e.expression(expression.Key, scope)
	case *ast.FuncCallExpr:
		path := definitionPath(expression.Func, scope)
		if expression.Receiver != nil {
			path = definitionPath(expression.Receiver, scope)
			if path != "" && isDefinitionIdentifier(expression.Method) {
				path += "." + expression.Method
			} else {
				path = ""
			}
		}
		if path != "" {
			e.paths[path] = true
			e.calls[path] = true
		}
		e.expression(expression.Func, scope)
		e.expression(expression.Receiver, scope)
		e.expressions(expression.Args, scope)
	case *ast.TableExpr:
		for _, field := range expression.Fields {
			e.expression(field.Key, scope)
			e.expression(field.Value, scope)
		}
	case *ast.FunctionExpr:
		e.function(expression, scope, false)
	case *ast.LogicalOpExpr:
		e.expression(expression.Lhs, scope)
		e.expression(expression.Rhs, scope)
	case *ast.RelationalOpExpr:
		e.expression(expression.Lhs, scope)
		e.expression(expression.Rhs, scope)
	case *ast.StringConcatOpExpr:
		e.expression(expression.Lhs, scope)
		e.expression(expression.Rhs, scope)
	case *ast.ArithmeticOpExpr:
		e.expression(expression.Lhs, scope)
		e.expression(expression.Rhs, scope)
	case *ast.UnaryMinusOpExpr:
		e.expression(expression.Expr, scope)
	case *ast.UnaryNotOpExpr:
		e.expression(expression.Expr, scope)
	case *ast.UnaryLenOpExpr:
		e.expression(expression.Expr, scope)
	}
}

func (e *definitionCollector) function(function *ast.FunctionExpr, scope *definitionScope, isMethod bool) {
	child := scope.child(function.ParList.Names...)
	if isMethod {
		child.names["self"] = true
	}
	if function.ParList.HasVargs {
		child.names["arg"] = true
	}
	e.statements(function.Stmts, child)
}

func (e *definitionCollector) own(expression ast.Expr, scope *definitionScope) {
	path := definitionPath(expression, scope)
	if path != "" {
		e.ownedPaths[path] = true
	}
}

func (e *definitionCollector) statements(statements []ast.Stmt, scope *definitionScope) {
	for _, statement := range statements {
		switch statement := statement.(type) {
		case *ast.AssignStmt:
			e.expressions(statement.Rhs, scope)
			for _, expression := range statement.Lhs {
				e.own(expression, scope)
				e.expression(expression, scope)
			}
		case *ast.LocalAssignStmt:
			// Lua local-function declarations include the function's own name
			// in its body, whereas ordinary initializer expressions do not.
			for _, expression := range statement.Exprs {
				function, isFunction := expression.(*ast.FunctionExpr)
				// GopherLua records LastLine only for local-function sugar.
				// In `local f = function() return f end`, f in the body
				// still refers to the surrounding scope.
				if isFunction && statement.LastLine() > 0 {
					e.function(function, scope.child(statement.Names...), false)
					continue
				}
				e.expression(expression, scope)
			}
			for _, name := range statement.Names {
				scope.names[name] = true
			}
		case *ast.FuncCallStmt:
			e.expression(statement.Expr, scope)
		case *ast.DoBlockStmt:
			e.statements(statement.Stmts, scope.child())
		case *ast.WhileStmt:
			e.expression(statement.Condition, scope)
			e.statements(statement.Stmts, scope.child())
		case *ast.RepeatStmt:
			child := scope.child()
			e.statements(statement.Stmts, child)
			e.expression(statement.Condition, child)
		case *ast.IfStmt:
			e.expression(statement.Condition, scope)
			e.statements(statement.Then, scope.child())
			e.statements(statement.Else, scope.child())
		case *ast.NumberForStmt:
			e.expression(statement.Init, scope)
			e.expression(statement.Limit, scope)
			e.expression(statement.Step, scope)
			e.statements(statement.Stmts, scope.child(statement.Name))
		case *ast.GenericForStmt:
			e.expressions(statement.Exprs, scope)
			e.statements(statement.Stmts, scope.child(statement.Names...))
		case *ast.FuncDefStmt:
			e.expression(statement.Name.Func, scope)
			e.expression(statement.Name.Receiver, scope)
			path := definitionPath(statement.Name.Func, scope)
			if statement.Name.Receiver != nil {
				path = definitionPath(statement.Name.Receiver, scope)
				if path != "" {
					path += "." + statement.Name.Method
				}
			}
			if path != "" {
				e.ownedPaths[path] = true
			}
			e.function(statement.Func, scope, statement.Name.Receiver != nil)
		case *ast.ReturnStmt:
			e.expressions(statement.Exprs, scope)
		}
	}
}

const definitionStandardRoots = " _G _VERSION assert collectgarbage coroutine debug dofile error gcinfo getfenv getmetatable io ipairs load loadfile loadstring math module newproxy next os package pairs pcall print rawequal rawget rawset require select setfenv setmetatable string table tonumber tostring type unpack xpcall "

func (e *definitionCollector) render() []byte {
	paths := make(map[string]bool)
	parents := make(map[string]bool)
	for path := range e.paths {
		parts := strings.Split(path, ".")
		if e.isOwnedPath(parts) || strings.Contains(definitionStandardRoots, " "+parts[0]+" ") {
			continue
		}
		for count := 1; count <= len(parts); count++ {
			prefix := strings.Join(parts[:count], ".")
			paths[prefix] = true
			if count < len(parts) {
				parents[prefix] = true
			}
		}
	}
	orderedPaths := make([]string, 0, len(paths))
	for path := range paths {
		orderedPaths = append(orderedPaths, path)
	}
	sort.Strings(orderedPaths)
	var output strings.Builder
	output.WriteString("---@meta\n\n-- Generated by darkrun convert from static observations of converted Lua.\n")
	output.WriteString("-- Editor hints only; do not load this file into the game.\n")
	output.WriteString("-- Names are observed references, not confirmed native API contracts.\n")
	output.WriteString("-- Argument, return, and field types remain unknown; source-owned globals are omitted.\n\n")
	for _, path := range orderedPaths {
		if parents[path] {
			output.WriteString("-- Editor namespace for observed members; runtime representation is unknown.\n" + path + " = {}\n\n")
			continue
		}
		if e.calls[path] {
			output.WriteString("-- Observed call; signature is unknown.\n---@param ... unknown\n---@return unknown\nfunction " + path + "(...) end\n\n")
			continue
		}
		output.WriteString("---@type unknown\n" + path + " = nil\n\n")
	}
	return []byte(output.String())
}

func (e *definitionCollector) isOwnedPath(parts []string) bool {
	for count := 1; count <= len(parts); count++ {
		if e.ownedPaths[strings.Join(parts[:count], ".")] {
			return true
		}
	}
	return false
}
