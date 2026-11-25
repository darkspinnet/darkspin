package ds

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/lua51"
)

type luaResourceKey struct {
	group    uint32
	instance uint64
}

type luaResourceName struct {
	Name       string
	Group      string
	Evidence   string
	Symbols    []string
	References []string
	Candidates []string
}

type luaNameCandidate struct {
	name     string
	evidence string
}

type luaNameLookup struct {
	namesByKey      map[luaResourceKey]luaResourceName
	candidatesByKey map[luaResourceKey]map[string]luaNameCandidate
	groupsByID      map[uint32]string
}

// These group identities are shared with the build-103 ServerData importer.
var luaGroupsByID = map[uint32]string{
	0x02f98b67: "HintTemplates",
	0x32b7d77a: "luadebug",
	0x3681d755: "lua",
	0x3b01d7f6: "LabsTuning",
	0x7153bbb1: "Abilities",
	0x982920ff: "LuaTestScripts",
	0xc130a42a: "behaviors",
	0xc953ece0: "LuaClient",
	0xd6a2b6e8: "PopupTip",
	0xf6dc1ab3: "NounDefinitionTemplates",
	0xfc0ff8f5: "Modifiers",
}

// packageLuaNames recovers names from package-local evidence without executing
// Lua. A noun or ability may share a script; resource identity remains the
// group/instance pair rather than any inferred gameplay owner.
func packageLuaNames(ctx context.Context, reader *dbpf.Reader, resources []dbpf.Resource, names map[uint32]string) (map[luaResourceKey]luaResourceName, error) {
	lookup := luaNameLookup{
		namesByKey:      make(map[luaResourceKey]luaResourceName),
		candidatesByKey: make(map[luaResourceKey]map[string]luaNameCandidate),
		groupsByID:      make(map[uint32]string),
	}
	for groupID, groupName := range luaGroupsByID {
		lookup.groupsByID[groupID] = groupName
	}
	for _, resource := range resources {
		if resource.Entry.Type != luaResourceType {
			continue
		}
		key := luaResourceKey{group: resource.Entry.Group, instance: resource.Entry.Instance}
		lookup.namesByKey[key] = luaResourceName{}
		lookup.candidatesByKey[key] = make(map[string]luaNameCandidate)
		if resource.Entry.Instance <= uint64(^uint32(0)) {
			name := luaNameStem(names[uint32(resource.Entry.Instance)])
			if name != "" {
				lookup.namesByKey[key] = luaResourceName{Name: name, Evidence: "registry"}
			}
		}
	}
	for ordinal, resource := range resources {
		if resource.Entry.Type != luaResourceType {
			continue
		}
		err := ctx.Err()
		if err != nil {
			return nil, fmt.Errorf("lookupContext: %w", err)
		}
		r, err := reader.Open(resource.Entry)
		if err != nil {
			return nil, fmt.Errorf("luaOpen[%d]: %w", ordinal, err)
		}
		payload, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("luaRead[%d]: %w", ordinal, err)
		}
		chunk, inspectErr := lua51.Inspect(payload)
		if inspectErr != nil {
			// Naming must not prevent preservation of an unsupported resource.
			continue
		}
		key := luaResourceKey{group: resource.Entry.Group, instance: resource.Entry.Instance}
		lookup.observeChunk(key, chunk)
	}
	lookup.resolve()
	return lookup.namesByKey, nil
}

func (e *luaNameLookup) observeChunk(key luaResourceKey, chunk *lua51.Chunk) {
	naming := e.namesByKey[key]
	symbols := make(map[string]bool)
	collectLuaDefinedSymbols(chunk.Main, symbols)
	naming.Symbols = sortedLuaNames(symbols)
	references := make(map[string]bool)
	sourceName := strings.TrimPrefix(chunk.SourceName, "@")
	e.addCandidate(key, luaNameStem(sourceName), "source-name")
	groupID, groupName, stem, isQualified, isReference := luaScriptReference(sourceName, key.group)
	if isReference && isQualified && groupID == key.group {
		e.addCandidate(key, stem, "source-name")
		if groupName != "" && e.groupsByID[groupID] == "" {
			e.groupsByID[groupID] = groupName
		}
	}
	for _, symbol := range naming.Symbols {
		e.addCandidate(key, symbol, "symbol-hash")
		e.addCandidate(key, strings.TrimPrefix(symbol, "n"), "symbol-hash")
	}
	for _, text := range chunk.Strings {
		groupID, groupName, stem, isQualified, isReference := luaScriptReference(text, key.group)
		if !isReference {
			continue
		}
		references[text] = true
		if isQualified && groupName != "" {
			if e.groupsByID[groupID] == "" {
				e.groupsByID[groupID] = groupName
			}
		}
		target := luaResourceKey{group: groupID, instance: uint64(luaNameHash(stem))}
		evidence := "module-reference"
		if !isQualified {
			evidence = "unqualified-reference"
		}
		e.addCandidate(target, stem, evidence)
	}
	naming.References = sortedLuaNames(references)
	e.namesByKey[key] = naming
}

func (e *luaNameLookup) addCandidate(key luaResourceKey, stem, evidence string) {
	candidates, isFound := e.candidatesByKey[key]
	if !isFound || stem == "" || uint64(luaNameHash(stem)) != key.instance {
		return
	}
	nameKey := strings.ToLower(stem)
	prior, isFound := candidates[nameKey]
	if isFound {
		priorRank := luaNameEvidenceRank(prior.evidence)
		rank := luaNameEvidenceRank(evidence)
		if priorRank < rank || priorRank == rank && prior.name <= stem {
			return
		}
	}
	candidates[nameKey] = luaNameCandidate{name: stem, evidence: evidence}
}

func (e *luaNameLookup) resolve() {
	for key, naming := range e.namesByKey {
		naming.Group = e.groupsByID[key.group]
		if naming.Group == "" {
			naming.Group = fmt.Sprintf("0x%08X", key.group)
		}
		candidates := e.candidatesByKey[key]
		for _, candidate := range candidates {
			naming.Candidates = append(naming.Candidates, candidate.name)
		}
		sort.Strings(naming.Candidates)
		if naming.Name != "" {
			e.namesByKey[key] = naming
			continue
		}
		switch len(candidates) {
		case 0:
			naming.Evidence = "unresolved"
			if len(naming.Symbols) == 1 {
				naming.Name = naming.Symbols[0]
				naming.Evidence = "defined-global"
			}
		case 1:
			for _, candidate := range candidates {
				naming.Name = candidate.name
				naming.Evidence = candidate.evidence
			}
		default:
			naming.Evidence = "ambiguous"
		}
		e.namesByKey[key] = naming
	}
}

func collectLuaDefinedSymbols(prototype *lua51.Prototype, symbols map[string]bool) {
	if prototype == nil {
		return
	}
	for index := 0; index < len(prototype.Instructions); index++ {
		instruction := prototype.Instructions[index]
		if instruction.Opcode() == lua51.OpcodeSetList && instruction.C() == 0 {
			// The following word is an extended table index, not an opcode.
			index++
			continue
		}
		if instruction.Opcode() != lua51.OpcodeSetGlobal || int(instruction.Bx()) >= len(prototype.Constants) {
			continue
		}
		constant := prototype.Constants[instruction.Bx()]
		if constant.Kind != lua51.ConstantString {
			continue
		}
		name := constant.String
		if luaNameStem(name) == "" {
			continue
		}
		if strings.HasPrefix(name, "nAbility_") || strings.HasPrefix(name, "nModifier_") ||
			strings.HasPrefix(name, "nBehavior_") || strings.HasPrefix(name, "nDirector_") {
			symbols[name] = true
		}
	}
	for _, child := range prototype.Prototypes {
		collectLuaDefinedSymbols(child, symbols)
	}
}

func luaScriptReference(text string, ownGroup uint32) (uint32, string, string, bool, bool) {
	if strings.ContainsAny(text, " \t\r\n\\@:") || !strings.HasSuffix(strings.ToLower(text), ".lua") {
		return 0, "", "", false, false
	}
	groupName, filename, isQualified := strings.Cut(text, "!")
	if !isQualified {
		groupName, filename, isQualified = strings.Cut(text, "/")
	}
	groupID := ownGroup
	if !isQualified {
		filename = text
		groupName = ""
	} else {
		if strings.HasPrefix(strings.ToLower(groupName), "0x") {
			parsedID, err := strconv.ParseUint(groupName[2:], 16, 32)
			if err != nil {
				return 0, "", "", false, false
			}
			groupID = uint32(parsedID)
			groupName = ""
		} else {
			if luaNameStem(groupName) == "" {
				return 0, "", "", false, false
			}
			groupID = luaNameHash(groupName)
		}
	}
	if strings.ContainsAny(filename, "/!") {
		return 0, "", "", false, false
	}
	stem := luaNameStem(filename)
	if stem == "" || strings.HasPrefix(strings.ToLower(stem), "0x") {
		return 0, "", "", false, false
	}
	return groupID, groupName, stem, isQualified, true
}

func luaNameEvidenceRank(evidence string) int {
	switch evidence {
	case "source-name":
		return 0
	case "module-reference":
		return 1
	case "symbol-hash":
		return 2
	default:
		return 3
	}
}

func luaNameStem(name string) string {
	if strings.HasPrefix(name, "@") {
		return ""
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if strings.EqualFold(filepath.Ext(name), ".lua") {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if name == "" || name == "." || name == ".." || strings.HasPrefix(strings.ToLower(name), "0x") {
		return ""
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-' || character == '~' {
			continue
		}
		return ""
	}
	return name
}

func luaNameHash(name string) uint32 {
	hash := uint32(0x811c9dc5)
	for index := 0; index < len(name); index++ {
		character := name[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		hash = hash*0x01000193 ^ uint32(character)
	}
	return hash
}

func sortedLuaNames(names map[string]bool) []string {
	sortedNames := make([]string, 0, len(names))
	for name := range names {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)
	return sortedNames
}
