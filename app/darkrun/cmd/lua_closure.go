package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/lua51"
	"github.com/darkspinnet/darkspin/server/util"
)

// Prototype records preserve every function, including unnamed callbacks. A
// string reference is a review edge, not proof that a call executes at runtime.
type luaAuditPrototype struct {
	Path             string   `json:"path"`
	FirstLine        uint64   `json:"first_line"`
	LastLine         uint64   `json:"last_line"`
	InstructionCount int      `json:"instruction_count"`
	Strings          []string `json:"strings"`
}

type luaAuditSymbol struct {
	Name           string  `json:"name"`
	TargetChunkIDs []int64 `json:"candidate_chunk_ids"`
}

type luaAuditClosure struct {
	Ability          string   `json:"ability"`
	RootChunkIDs     []int64  `json:"root_chunk_ids"`
	RootEvidence     string   `json:"root_evidence"`
	ChunkIDs         []int64  `json:"review_chunk_ids"`
	PrototypeCount   int      `json:"prototype_count"`
	UnresolvedNames  []string `json:"unresolved_dependency_names"`
	BuiltinNames     []string `json:"compiler_builtin_names"`
	IsParityVerified bool     `json:"is_parity_verified"`
}

func luaAuditPrototypes(prototype *lua51.Prototype, path string) []luaAuditPrototype {
	if prototype == nil {
		return nil
	}
	texts := make([]string, 0)
	for _, constant := range prototype.Constants {
		if constant.Kind == lua51.ConstantString {
			texts = append(texts, constant.String)
		}
	}
	entries := []luaAuditPrototype{{
		Path: path, FirstLine: prototype.LineDefined, LastLine: prototype.LastLineDefined,
		InstructionCount: len(prototype.Instructions), Strings: texts,
	}}
	for index, child := range prototype.Prototypes {
		entries = append(entries, luaAuditPrototypes(child, fmt.Sprintf("%s.%d", path, index))...)
	}
	return entries
}

func linkLuaAuditSymbols(chunks []luaAuditChunk) {
	owners := make(map[string][]int64)
	ownersByFilename := make(map[string][]int64)
	for _, chunk := range chunks {
		for _, name := range chunk.Registrations {
			owners[name] = append(owners[name], chunk.ID)
		}
		group, filename, isQualified := strings.Cut(chunk.Source, "/")
		if isQualified && (group == "Abilities" || group == "Modifiers" || group == "behaviors") {
			ownersByFilename[strings.ToLower(filename)] = append(ownersByFilename[strings.ToLower(filename)], chunk.ID)
		}
		// Build 103 loads common helpers before the authored ability chunks.
		// These implicit runtime references are absent from require indexes.
		if chunk.Source == "lua/0x57572DAC.lua" && chunk.SHA256 == "59566e1528d3850efd8dc905aa6e12dd2cf113d06fa750bef35cbe82ed6a6126" {
			for _, name := range []string{"nAbility", "nAbilityFns", "nModifier", "nModifierFns"} {
				owners[name] = append(owners[name], chunk.ID)
			}
		}
		names := append([]string(nil), chunk.StaticAbilityTables...)
		names = append(names, chunk.StaticModifierTables...)
		names = append(names, chunk.StaticBehaviorTables...)
		for _, name := range names {
			owners[name] = append(owners[name], chunk.ID)
			_, shortName, isQualified := strings.Cut(name, "_")
			if isQualified {
				owners[shortName] = append(owners[shortName], chunk.ID)
			}
		}
	}
	for index := range chunks {
		chunk := &chunks[index]
		seenNames := make(map[string]bool)
		for _, name := range chunk.strings {
			if seenNames[name] {
				continue
			}
			seenNames[name] = true
			targetIDs := make([]int64, 0)
			candidateIDs := append([]int64(nil), owners[name]...)
			filename := fmt.Sprintf("0x%08x.lua", util.HashID(name))
			candidateIDs = append(candidateIDs, ownersByFilename[filename]...)
			seenIDs := make(map[int64]bool)
			for _, id := range candidateIDs {
				if id != chunk.ID && !seenIDs[id] {
					targetIDs = append(targetIDs, id)
					seenIDs[id] = true
				}
			}
			sort.Slice(targetIDs, func(left, right int) bool { return targetIDs[left] < targetIDs[right] })
			if len(targetIDs) != 0 {
				chunk.SymbolReferences = append(chunk.SymbolReferences, luaAuditSymbol{
					Name: name, TargetChunkIDs: targetIDs,
				})
			}
		}
		sort.Slice(chunk.SymbolReferences, func(left, right int) bool {
			return chunk.SymbolReferences[left].Name < chunk.SymbolReferences[right].Name
		})
	}
}

func luaAuditHeroClosures(chunks []luaAuditChunk, kits []luaAuditHeroKit) []luaAuditClosure {
	chunksByID := make(map[int64]luaAuditChunk, len(chunks))
	for _, chunk := range chunks {
		chunksByID[chunk.ID] = chunk
	}
	abilities := make(map[string][]int64)
	for _, kit := range kits {
		for _, ability := range kit.Abilities {
			ids := abilities[ability.AssetName]
			if ability.DefinitionChunkID != 0 {
				ids = append(ids, ability.DefinitionChunkID)
			}
			abilities[ability.AssetName] = ids
		}
	}
	closures := make([]luaAuditClosure, 0, len(abilities))
	for name, ids := range abilities {
		closure := luaAuditClosure{Ability: name, RootEvidence: "production definition provenance"}
		if len(ids) == 0 {
			closure.RootEvidence = "authored identity hash or exact table-name candidate; not execution proof"
			expectedFilename := fmt.Sprintf("0x%08X.lua", util.HashID(name))
			for _, chunk := range chunks {
				group, filename, isQualified := strings.Cut(chunk.Source, "/")
				isCandidate := isQualified && (group == "Abilities" || group == "Modifiers") &&
					strings.EqualFold(filename, expectedFilename)
				for _, registeredName := range chunk.Registrations {
					isCandidate = isCandidate || registeredName == name
				}
				for _, symbol := range append(append([]string(nil), chunk.StaticAbilityTables...), chunk.StaticModifierTables...) {
					isCandidate = isCandidate || symbol == "nAbility_"+name || symbol == "nModifier_"+name
				}
				if isCandidate {
					ids = append(ids, chunk.ID)
				}
			}
		}
		roots := make(map[int64]bool)
		for _, id := range ids {
			roots[id] = true
		}
		for id := range roots {
			closure.RootChunkIDs = append(closure.RootChunkIDs, id)
		}
		sort.Slice(closure.RootChunkIDs, func(left, right int) bool { return closure.RootChunkIDs[left] < closure.RootChunkIDs[right] })
		pendingIDs := append([]int64(nil), closure.RootChunkIDs...)
		seenIDs := make(map[int64]bool)
		unknownNames := make(map[string]bool)
		builtinNames := make(map[string]bool)
		for len(pendingIDs) > 0 {
			id := pendingIDs[len(pendingIDs)-1]
			pendingIDs = pendingIDs[:len(pendingIDs)-1]
			if seenIDs[id] {
				continue
			}
			seenIDs[id] = true
			chunk, isFound := chunksByID[id]
			if !isFound {
				unknownNames[fmt.Sprintf("chunk:%d", id)] = true
				continue
			}
			closure.ChunkIDs = append(closure.ChunkIDs, id)
			closure.PrototypeCount += len(chunk.Prototypes)
			for _, dependency := range chunk.Dependencies {
				if len(dependency.BuiltinModes) > 0 {
					builtinNames[dependency.Name] = true
				}
				switch {
				case dependency.RuntimeTargetChunkID != nil:
					pendingIDs = append(pendingIDs, *dependency.RuntimeTargetChunkID)
				case dependency.IndexedTargetChunkID != nil:
					pendingIDs = append(pendingIDs, *dependency.IndexedTargetChunkID)
				case dependency.CandidateChunkID != nil:
					pendingIDs = append(pendingIDs, *dependency.CandidateChunkID)
				case len(dependency.BuiltinModes) > 0:
					builtinNames[dependency.Name] = true
				default:
					unknownNames[dependency.Name] = true
				}
			}
			for _, symbol := range chunk.SymbolReferences {
				pendingIDs = append(pendingIDs, symbol.TargetChunkIDs...)
			}
		}
		for name := range unknownNames {
			closure.UnresolvedNames = append(closure.UnresolvedNames, name)
		}
		for name := range builtinNames {
			closure.BuiltinNames = append(closure.BuiltinNames, name)
		}
		sort.Strings(closure.UnresolvedNames)
		sort.Strings(closure.BuiltinNames)
		sort.Slice(closure.ChunkIDs, func(left, right int) bool { return closure.ChunkIDs[left] < closure.ChunkIDs[right] })
		closures = append(closures, closure)
	}
	sort.Slice(closures, func(left, right int) bool { return closures[left].Ability < closures[right].Ability })
	return closures
}
