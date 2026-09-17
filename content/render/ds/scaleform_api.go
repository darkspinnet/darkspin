package ds

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/scaleform"
)

type scaleformMethodObservation struct {
	argumentTypes []map[string]bool
	minimumArity  int
}

type scaleformSourceCollector struct {
	sources [][]byte
}

var scaleformCallPattern = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\.([A-Za-z_][A-Za-z0-9_]*)\((.*)\);\s*$`)
var scaleformGlobalPathPattern = regexp.MustCompile(`\b_global\.([A-Za-z_][A-Za-z0-9_]*)`)
var scaleformMemberPathPattern = regexp.MustCompile(`\b(?:_global\.)?[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+`)

func finalizeScaleformTypeScriptWorkspace(destinationPath string) error {
	sourceRoot := filepath.Join(destinationPath, "ui", "scaleform")
	fi, err := os.Stat(sourceRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sourceStat: %w", err)
	}
	if !fi.IsDir() {
		return nil
	}
	sources, err := readScaleformTypeScriptSources(sourceRoot)
	if err != nil {
		return fmt.Errorf("sourceRead: %w", err)
	}
	globalNames, err := scaleformTypeScriptGlobals(filepath.Join(destinationPath, "scaleform.d.ts"), sources)
	if err != nil {
		return fmt.Errorf("globalInfer: %w", err)
	}
	apis := inferScaleformTypeScriptAPIs(sources, globalNames)
	payload := scaleform.TypeScriptDeclarations(globalNames, apis)
	err = os.WriteFile(filepath.Join(destinationPath, "scaleform.d.ts"), payload, 0o644)
	if err != nil {
		return fmt.Errorf("declarationWrite: %w", err)
	}
	return nil
}

func readScaleformTypeScriptSources(sourceRoot string) ([][]byte, error) {
	collector := &scaleformSourceCollector{}
	err := filepath.WalkDir(sourceRoot, collector.visit)
	if err != nil {
		return nil, fmt.Errorf("sourceWalk: %w", err)
	}
	return collector.sources, nil
}

func (e *scaleformSourceCollector) visit(path string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return fmt.Errorf("walkPath: %w", walkErr)
	}
	if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ts") {
		return nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("typescriptRead: %w", err)
	}
	e.sources = append(e.sources, payload)
	return nil
}

func inferScaleformTypeScriptAPIs(sources [][]byte, globalNames []string) []scaleform.TypeScriptAPI {
	globalRoots := scaleformGlobalRoots(sources, globalNames)
	observations := make(map[string]map[string]*scaleformMethodObservation)
	memberPaths := make(map[string]bool)
	for _, source := range sources {
		for _, line := range strings.Split(string(source), "\n") {
			observeScaleformMemberPaths(memberPaths, globalRoots, line)
			matches := scaleformCallPattern.FindStringSubmatch(line)
			if len(matches) != 4 {
				continue
			}
			receiverPath := normalizeScaleformAPIPath(matches[1])
			if !isScaleformAPIPath(receiverPath, globalRoots) {
				continue
			}
			arguments, isValid := splitScaleformTypeScriptArguments(matches[3])
			if !isValid {
				continue
			}
			observeScaleformMethod(observations, receiverPath, matches[2], arguments)
		}
	}
	return renderScaleformAPIs(observations, memberPaths)
}

func observeScaleformMemberPaths(memberPaths map[string]bool, globalRoots map[string]bool, line string) {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "//") {
		return
	}
	unquotedLine := typeScriptUnquoted([]byte(line))
	for _, indexes := range scaleformMemberPathPattern.FindAllStringIndex(unquotedLine, -1) {
		path := normalizeScaleformAPIPath(unquotedLine[indexes[0]:indexes[1]])
		if !isScaleformAPIPath(path, globalRoots) {
			continue
		}
		remainder := strings.TrimSpace(unquotedLine[indexes[1]:])
		if strings.HasPrefix(remainder, "(") || strings.HasPrefix(remainder, "= function") {
			separatorIndex := strings.LastIndexByte(path, '.')
			if separatorIndex < 0 {
				continue
			}
			path = path[:separatorIndex]
		}
		if strings.Contains(path, ".") {
			memberPaths[path] = true
		}
	}
}

func scaleformGlobalRoots(sources [][]byte, globalNames []string) map[string]bool {
	roots := map[string]bool{
		"Key":       true,
		"Mouse":     true,
		"Selection": true,
		"Stage":     true,
		"System":    true,
		"flash":     true,
		"gfx":       true,
		"maxis":     true,
	}
	for _, globalName := range globalNames {
		roots[globalName] = true
	}
	for _, source := range sources {
		unquotedSource := typeScriptUnquoted(source)
		for _, matches := range scaleformGlobalPathPattern.FindAllStringSubmatch(unquotedSource, -1) {
			roots[matches[1]] = true
		}
	}
	return roots
}

func normalizeScaleformAPIPath(path string) string {
	return strings.TrimPrefix(path, "_global.")
}

func isScaleformAPIPath(path string, globalRoots map[string]bool) bool {
	parts := strings.Split(path, ".")
	if len(parts) == 0 || !globalRoots[parts[0]] {
		return false
	}
	if parts[0] == "avm1" || parts[0] == "Object" || parts[0] == "ExternalInterface" || parts[0] == "_root" || strings.HasPrefix(parts[0], "register") {
		return false
	}
	return true
}

func splitScaleformTypeScriptArguments(content string) ([]string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, true
	}
	var arguments []string
	start := 0
	depth := 0
	isQuoted := false
	isEscaped := false
	for index, character := range content {
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
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				return nil, false
			}
		case ',':
			if depth == 0 {
				arguments = append(arguments, strings.TrimSpace(content[start:index]))
				start = index + 1
			}
		}
	}
	if isQuoted || depth != 0 {
		return nil, false
	}
	arguments = append(arguments, strings.TrimSpace(content[start:]))
	return arguments, true
}

func observeScaleformMethod(observations map[string]map[string]*scaleformMethodObservation, receiverPath, methodName string, arguments []string) {
	methods := observations[receiverPath]
	if methods == nil {
		methods = make(map[string]*scaleformMethodObservation)
		observations[receiverPath] = methods
	}
	observation := methods[methodName]
	if observation == nil {
		observation = &scaleformMethodObservation{minimumArity: len(arguments)}
		methods[methodName] = observation
	}
	if len(arguments) < observation.minimumArity {
		observation.minimumArity = len(arguments)
	}
	for len(observation.argumentTypes) < len(arguments) {
		observation.argumentTypes = append(observation.argumentTypes, make(map[string]bool))
	}
	for argumentIndex, argument := range arguments {
		observation.argumentTypes[argumentIndex][scaleformTypeScriptArgumentType(argument)] = true
	}
}

func scaleformTypeScriptArgumentType(argument string) string {
	if strings.HasPrefix(argument, "\"") && strings.HasSuffix(argument, "\"") {
		return "string"
	}
	if argument == "true" || argument == "false" {
		return "boolean"
	}
	if argument == "null" {
		return "null"
	}
	_, err := strconv.ParseFloat(argument, 64)
	if err == nil {
		return "number"
	}
	return "AVM1Value"
}

func renderScaleformAPIs(observations map[string]map[string]*scaleformMethodObservation, memberPaths map[string]bool) []scaleform.TypeScriptAPI {
	propertiesByPath := make(map[string]map[string]bool)
	for memberPath := range memberPaths {
		ensureScaleformAPIPath(propertiesByPath, memberPath)
	}
	for receiverPath := range observations {
		ensureScaleformAPIPath(propertiesByPath, receiverPath)
	}
	paths := make([]string, 0, len(propertiesByPath))
	for path := range propertiesByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	apis := make([]scaleform.TypeScriptAPI, 0, len(paths))
	for _, path := range paths {
		api := scaleform.TypeScriptAPI{Path: path}
		for property := range propertiesByPath[path] {
			api.Properties = append(api.Properties, property)
		}
		sort.Strings(api.Properties)
		methodNames := make([]string, 0, len(observations[path]))
		for methodName := range observations[path] {
			methodNames = append(methodNames, methodName)
		}
		sort.Strings(methodNames)
		for _, methodName := range methodNames {
			api.Methods = append(api.Methods, renderScaleformMethod(methodName, observations[path][methodName]))
		}
		apis = append(apis, api)
	}
	return apis
}

func ensureScaleformAPIPath(propertiesByPath map[string]map[string]bool, path string) {
	parts := strings.Split(path, ".")
	for partIndex := 1; partIndex < len(parts); partIndex++ {
		parentPath := strings.Join(parts[:partIndex], ".")
		if propertiesByPath[parentPath] == nil {
			propertiesByPath[parentPath] = make(map[string]bool)
		}
		propertiesByPath[parentPath][parts[partIndex]] = true
	}
	if propertiesByPath[path] == nil {
		propertiesByPath[path] = make(map[string]bool)
	}
}

func renderScaleformMethod(methodName string, observation *scaleformMethodObservation) scaleform.TypeScriptMethod {
	method := scaleform.TypeScriptMethod{Name: methodName, ReturnType: scaleformMethodReturnType(methodName)}
	for argumentIndex, argumentTypes := range observation.argumentTypes {
		method.Parameters = append(method.Parameters, scaleform.TypeScriptParameter{
			Name:       scaleformParameterName(methodName, argumentIndex),
			Type:       joinScaleformArgumentTypes(argumentTypes),
			IsOptional: argumentIndex >= observation.minimumArity,
		})
	}
	return method
}

func scaleformMethodReturnType(methodName string) string {
	voidMethods := map[string]bool{
		"addEventListener":    true,
		"addListener":         true,
		"gotoAndPlay":         true,
		"gotoAndStop":         true,
		"loadClip":            true,
		"loadMovie":           true,
		"playSound":           true,
		"removeEventListener": true,
		"removeListener":      true,
		"setFocus":            true,
		"unloadClip":          true,
	}
	if voidMethods[methodName] {
		return "void"
	}
	return "AVM1Value"
}

func scaleformParameterName(methodName string, argumentIndex int) string {
	knownNames := map[string][]string{
		"addEventListener":    {"type", "listener"},
		"gotoAndPlay":         {"frame"},
		"gotoAndStop":         {"frame"},
		"loadClip":            {"path", "target"},
		"loadMovie":           {"path", "level"},
		"playSound":           {"name", "eventID"},
		"removeEventListener": {"type", "listener"},
		"setFocus":            {"target"},
	}
	names := knownNames[methodName]
	if argumentIndex < len(names) {
		return names[argumentIndex]
	}
	return fmt.Sprintf("argument%d", argumentIndex+1)
}

func joinScaleformArgumentTypes(argumentTypes map[string]bool) string {
	order := []string{"string", "number", "boolean", "null", "AVM1Value"}
	types := make([]string, 0, len(argumentTypes))
	for _, argumentType := range order {
		if argumentTypes[argumentType] {
			types = append(types, argumentType)
		}
	}
	if len(types) == 0 {
		return "AVM1Value"
	}
	return strings.Join(types, " | ")
}
