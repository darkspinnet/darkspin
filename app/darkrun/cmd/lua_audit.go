package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/lua51"
	contentstore "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	"github.com/darkspinnet/darkspin/server/zone/content/contentsqlite"
	"github.com/spf13/cobra"
)

type luaAuditReport struct {
	Database                         string            `json:"database"`
	Scope                            string            `json:"scope"`
	ChunkCount                       int               `json:"chunk_count"`
	InvalidChunkCount                int               `json:"invalid_chunk_count"`
	TotalDependencyCount             int               `json:"total_dependency_count"`
	IndexedUnresolvedDependencyCount int               `json:"indexed_unresolved_dependency_count"`
	UnknownDependencyCount           int               `json:"unknown_dependency_count"`
	ResourceResolvedDependencyCount  int               `json:"resource_resolved_dependency_count"`
	RuntimeUnresolvedDependencyCount int               `json:"runtime_unresolved_dependency_count"`
	CatalogError                     string            `json:"catalog_error,omitempty"`
	Chunks                           []luaAuditChunk   `json:"chunks"`
	HeroKits                         []luaAuditHeroKit `json:"hero_kits"`
	UnsupportedBasics                map[uint32]string `json:"unsupported_basics"`
	HeroClosures                     []luaAuditClosure `json:"hero_closures"`
}

type luaAuditChunk struct {
	ID                            int64                `json:"id"`
	Source                        string               `json:"source"`
	SHA256                        string               `json:"sha256"`
	Size                          int                  `json:"size"`
	ValidationError               string               `json:"validation_error,omitempty"`
	Aliases                       []string             `json:"aliases"`
	Dependencies                  []luaAuditDependency `json:"dependencies"`
	LinkedCallbacks               []string             `json:"linked_callbacks"`
	StaticAbilityTables           []string             `json:"static_ability_tables"`
	StaticModifierTables          []string             `json:"static_modifier_tables"`
	StaticBehaviorTables          []string             `json:"static_behavior_tables"`
	IsRegistrationConstantPresent bool                 `json:"is_registration_constant_present"`
	Prototypes                    []luaAuditPrototype  `json:"prototypes"`
	Registrations                 []string             `json:"registration_name_candidates"`
	SymbolReferences              []luaAuditSymbol     `json:"symbol_references"`
	strings                       []string
}

type luaAuditDependency struct {
	Name                 string   `json:"name"`
	IndexedTargetChunkID *int64   `json:"indexed_target_chunk_id"`
	RuntimeTargetChunkID *int64   `json:"runtime_target_chunk_id"`
	ResolutionError      string   `json:"resolution_error,omitempty"`
	BuiltinModes         []string `json:"builtin_modes"`
	IndexResolution      string   `json:"index_resolution"`
	CandidateChunkID     *int64   `json:"candidate_chunk_id,omitempty"`
	CandidateSource      string   `json:"candidate_source,omitempty"`
	CandidateEvidence    string   `json:"candidate_evidence,omitempty"`
}

type luaAuditHeroKit struct {
	Noun      uint32            `json:"noun"`
	Name      string            `json:"name"`
	Abilities []luaAuditAbility `json:"abilities"`
}

type luaAuditAbility struct {
	Slot              string          `json:"slot"`
	ID                uint32          `json:"id"`
	AssetName         string          `json:"asset_name"`
	DefinitionName    string          `json:"definition_name"`
	DefinitionKind    sim.AbilityKind `json:"definition_kind"`
	DefinitionChunkID int64           `json:"definition_chunk_id"`
	DefinitionSHA256  string          `json:"definition_sha256"`
	DefinitionState   string          `json:"definition_state"`
	CompileError      string          `json:"compile_error,omitempty"`
}

func newLuaAuditCommand() *cobra.Command {
	configPath := game.DefaultConfigFilename
	command := &cobra.Command{
		Use:   "lua-audit",
		Short: "Report all indexed Lua chunks and production hero definitions without starting a server",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			err := runLuaAudit(command.Context(), command.OutOrStdout(), configPath)
			if err != nil {
				return fmt.Errorf("auditRun: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&configPath, "config", game.DefaultConfigFilename, "path to darkspin.toml")
	return command
}

func runLuaAudit(ctx context.Context, output io.Writer, configPath string) (resultErr error) {
	if ctx == nil {
		return errors.New("nil context")
	}
	if output == nil {
		return errors.New("nil output")
	}
	if configPath == "" {
		return errors.New("empty config path")
	}
	absoluteConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("configPath: %w", err)
	}
	_, err = os.Stat(absoluteConfigPath)
	if err != nil {
		return fmt.Errorf("configStat: %w", err)
	}
	config, isConfigCreated, err := game.LoadConfig(absoluteConfigPath)
	if err != nil {
		return fmt.Errorf("configLoad: %w", err)
	}
	if isConfigCreated {
		return errors.New("audit requires an existing configuration")
	}
	driver := strings.ToLower(strings.TrimSpace(config.String(game.ConfigStorageDriver)))
	if driver != "" && driver != "sqlite" {
		return fmt.Errorf("driverUnsupported: %s", driver)
	}
	databasePath := filepath.Join(filepath.Dir(absoluteConfigPath), server.DarkspinDirectory,
		server.CacheDirectory, server.ContentDatabaseFilename)
	store, err := contentstore.OpenReadOnly(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("storeOpen: %w", err)
	}
	defer func() {
		closeErr := store.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("storeClose: %w", closeErr))
		}
	}()
	chunks, err := store.LuaIndex(ctx)
	if err != nil {
		return fmt.Errorf("indexRead: %w", err)
	}
	report := luaAuditReport{
		Database:   databasePath,
		Scope:      "Read-only indexed inventory and production catalog load. Arbitrary chunks are decoded, never executed. Static tables, literal registration operands and string constants are candidate evidence, not proof of runtime registration. Hero closure graphs include indexed dependencies, inferred symbol links and implicit common helpers; they over-approximate reachability and do not certify callback parity. Definition availability includes recovered Go contracts and does not verify activation, effects, passives, or real-client behavior. Builtin modes describe compiler-specific preload policies only. Indexed unresolved counts include all NULL targets; unknown counts exclude compiler-specific builtin names. Runtime targets separately include unique native resource-key recovery without changing the database or builtin execution policies. Unknown dependencies are informational; catalog or integrity failure produces a nonzero exit after the full report is written.",
		ChunkCount: len(chunks), Chunks: make([]luaAuditChunk, 0, len(chunks)),
		HeroKits: make([]luaAuditHeroKit, 0),
	}
	for _, chunk := range chunks {
		entry := inspectLuaAuditChunk(ctx, store, chunk)
		for index := range entry.Dependencies {
			dependency := &entry.Dependencies[index]
			if dependency.IndexedTargetChunkID != nil {
				continue
			}
			candidate := luaAuditDependencyCandidate(dependency.Name, chunks)
			if candidate != nil {
				dependency.CandidateChunkID = &candidate.ID
				dependency.CandidateSource = candidate.Source
				dependency.CandidateEvidence = "unique group and case-insensitive FNV-1 filename stem match; index remains unresolved"
			}
		}
		if entry.ValidationError != "" {
			report.InvalidChunkCount++
		}
		for _, dependency := range entry.Dependencies {
			report.TotalDependencyCount++
			if dependency.RuntimeTargetChunkID == nil {
				report.RuntimeUnresolvedDependencyCount++
			} else if dependency.IndexedTargetChunkID == nil {
				report.ResourceResolvedDependencyCount++
			}
			if dependency.IndexedTargetChunkID == nil {
				report.IndexedUnresolvedDependencyCount++
			}
			if dependency.IndexResolution == "unknown" {
				report.UnknownDependencyCount++
			}
		}
		report.Chunks = append(report.Chunks, entry)
	}
	programs, catalogErr := contentsqlite.Load(store)
	if catalogErr != nil {
		report.CatalogError = catalogErr.Error()
	} else {
		report.UnsupportedBasics = programs.PlayerBasicUnsupported
		report.HeroKits = luaAuditHeroKits(programs)
	}
	linkLuaAuditSymbols(report.Chunks)
	report.HeroClosures = luaAuditHeroClosures(report.Chunks, report.HeroKits)
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(report)
	if err != nil {
		return fmt.Errorf("reportEncode: %w", err)
	}
	var auditErr error
	if catalogErr != nil {
		auditErr = fmt.Errorf("catalogLoad: %w", catalogErr)
	}
	if report.InvalidChunkCount > 0 {
		integrityErr := fmt.Errorf("%d chunk inspections failed", report.InvalidChunkCount)
		auditErr = errors.Join(auditErr, fmt.Errorf("chunkIntegrity: %w", integrityErr))
	}
	if auditErr != nil {
		return fmt.Errorf("auditStatus: %w", auditErr)
	}
	return nil
}

func luaAuditDependencyCandidate(name string, chunks []contentstore.LuaIndexChunk) *contentstore.LuaIndexChunk {
	group, filename, isQualified := strings.Cut(name, "!")
	if !isQualified || !strings.EqualFold(filepath.Ext(filename), ".lua") {
		return nil
	}
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	expectedFilename := fmt.Sprintf("0x%08X.lua", util.HashID(stem))
	var candidate *contentstore.LuaIndexChunk
	for index := range chunks {
		sourceGroup, sourceFilename, isSourceQualified := strings.Cut(chunks[index].Source, "/")
		if !isSourceQualified || !strings.EqualFold(sourceFilename, expectedFilename) {
			continue
		}
		isGroupMatched := strings.EqualFold(group, sourceGroup)
		if !isGroupMatched && strings.HasPrefix(strings.ToLower(group), "0x") {
			groupHash, err := strconv.ParseUint(group[2:], 16, 32)
			if err != nil {
				continue
			}
			isGroupMatched = uint32(groupHash) == util.HashID(sourceGroup)
		}
		if !isGroupMatched {
			continue
		}
		if candidate != nil {
			return nil
		}
		candidate = &chunks[index]
	}
	return candidate
}

func inspectLuaAuditChunk(ctx context.Context, store *contentstore.Store, chunk contentstore.LuaIndexChunk) (entry luaAuditChunk) {
	defer func() {
		if recovered := recover(); recovered != nil {
			entry.ValidationError = fmt.Sprintf("inspectionPanic: %v", recovered)
		}
	}()
	entry = luaAuditChunk{
		ID: chunk.ID, Source: chunk.Source, SHA256: chunk.SHA256, Size: chunk.Size,
		Aliases: chunk.Aliases, LinkedCallbacks: chunk.Callbacks,
		Dependencies:         make([]luaAuditDependency, 0, len(chunk.Dependencies)),
		StaticAbilityTables:  make([]string, 0),
		StaticModifierTables: make([]string, 0), StaticBehaviorTables: make([]string, 0),
	}
	for _, dependency := range chunk.Dependencies {
		modes := sim.LuaBuiltinModuleModes(dependency.Name)
		resolution := "unknown"
		if dependency.TargetChunkID != nil {
			resolution = "indexed_chunk"
		} else if len(modes) > 0 {
			resolution = "compiler_specific_builtin"
		}
		entry.Dependencies = append(entry.Dependencies, luaAuditDependency{
			Name: dependency.Name, IndexedTargetChunkID: dependency.TargetChunkID,
			RuntimeTargetChunkID: dependency.RuntimeTargetChunkID,
			ResolutionError:      dependency.ResolutionError,
			BuiltinModes:         modes, IndexResolution: resolution,
		})
	}
	loadedChunk, err := store.LuaChunk(ctx, chunk.ID)
	if err != nil {
		entry.ValidationError = err.Error()
		return entry
	}
	inspection, err := lua51.Inspect(loadedChunk.Bytecode)
	if err != nil {
		entry.ValidationError = err.Error()
		return entry
	}
	entry.strings = inspection.Strings
	entry.Prototypes = luaAuditPrototypes(inspection.Main, "0")
	entry.Registrations = luaAuditRegistrations(inspection.Main)
	for _, constant := range inspection.Strings {
		if constant == "RegisterAbility" {
			entry.IsRegistrationConstantPresent = true
		}
	}
	for name, table := range lua51.StaticGlobals(inspection) {
		if table.Kind != lua51.StaticTable {
			continue
		}
		switch {
		case strings.HasPrefix(name, "nAbility_"):
			entry.StaticAbilityTables = append(entry.StaticAbilityTables, name)
		case strings.HasPrefix(name, "nModifier_"):
			entry.StaticModifierTables = append(entry.StaticModifierTables, name)
		case strings.HasPrefix(name, "nBehavior_"):
			entry.StaticBehaviorTables = append(entry.StaticBehaviorTables, name)
		}
	}
	sort.Strings(entry.StaticAbilityTables)
	sort.Strings(entry.StaticModifierTables)
	sort.Strings(entry.StaticBehaviorTables)
	return entry
}

func luaAuditHeroKits(programs zonecontent.Programs) []luaAuditHeroKit {
	kits := make([]luaAuditHeroKit, 0, len(programs.HeroKits))
	slotNames := []string{"passive", "basic", "random", "special_one", "special_two"}
	for _, kit := range programs.HeroKits {
		entry := luaAuditHeroKit{Noun: kit.Noun, Name: kit.Name, Abilities: make([]luaAuditAbility, 0, len(slotNames))}
		for index, slotName := range slotNames {
			ability := kit.Abilities[zonecontent.HeroAbilitySlot(index)]
			state := "definition_unavailable"
			if index == int(zonecontent.HeroAbilityPassive) {
				state = "passive_identity_only_not_compiled_by_hero_catalog"
			} else if ability.CompileError == "" && ability.Definition.Name != "" {
				state = "production_definition_available_behavior_unverified"
			}
			entry.Abilities = append(entry.Abilities, luaAuditAbility{
				Slot: slotName, ID: ability.ID, AssetName: ability.AssetName,
				DefinitionName: ability.Definition.Name, DefinitionKind: ability.Definition.Kind,
				DefinitionChunkID: ability.Definition.Provenance.LuaChunkID,
				DefinitionSHA256:  ability.Definition.Provenance.BytecodeSHA256,
				DefinitionState:   state, CompileError: ability.CompileError,
			})
		}
		kits = append(kits, entry)
	}
	sort.Slice(kits, func(left, right int) bool { return kits[left].Noun < kits[right].Noun })
	return kits
}
