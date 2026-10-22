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
	"strings"

	"github.com/darkspinnet/darkspin/content/lua51"
	contentstore "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/sim"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	"github.com/darkspinnet/darkspin/server/zone/content/contentsqlite"
	"github.com/spf13/cobra"
)

type luaAuditReport struct {
	Database               string            `json:"database"`
	Scope                  string            `json:"scope"`
	ChunkCount             int               `json:"chunk_count"`
	InvalidChunkCount      int               `json:"invalid_chunk_count"`
	UnknownDependencyCount int               `json:"unknown_dependency_count"`
	CatalogError           string            `json:"catalog_error,omitempty"`
	Chunks                 []luaAuditChunk   `json:"chunks"`
	HeroKits               []luaAuditHeroKit `json:"hero_kits"`
	UnsupportedBasics      map[uint32]string `json:"unsupported_basics"`
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
	IsRegistrationConstantPresent bool                 `json:"is_registration_constant_present"`
}

type luaAuditDependency struct {
	Name                 string   `json:"name"`
	IndexedTargetChunkID *int64   `json:"indexed_target_chunk_id"`
	BuiltinModes         []string `json:"builtin_modes"`
	IndexResolution      string   `json:"index_resolution"`
}

type luaAuditHeroKit struct {
	Noun      uint32            `json:"noun"`
	Name      string            `json:"name"`
	Abilities []luaAuditAbility `json:"abilities"`
}

type luaAuditAbility struct {
	Slot            string          `json:"slot"`
	ID              uint32          `json:"id"`
	AssetName       string          `json:"asset_name"`
	DefinitionName  string          `json:"definition_name"`
	DefinitionKind  sim.AbilityKind `json:"definition_kind"`
	DefinitionState string          `json:"definition_state"`
	CompileError    string          `json:"compile_error,omitempty"`
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
		Scope:      "Read-only indexed inventory and production catalog load. Arbitrary chunks are decoded, never executed. Static tables and string constants are evidence, not registrations. Definition availability includes recovered Go contracts and does not verify activation, effects, passives, or real-client behavior. Builtin modes describe compiler-specific preload policies only.",
		ChunkCount: len(chunks), Chunks: make([]luaAuditChunk, 0, len(chunks)),
		HeroKits: make([]luaAuditHeroKit, 0),
	}
	for _, chunk := range chunks {
		entry := inspectLuaAuditChunk(ctx, store, chunk)
		if entry.ValidationError != "" {
			report.InvalidChunkCount++
		}
		for _, dependency := range entry.Dependencies {
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
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(report)
	if err != nil {
		return fmt.Errorf("reportEncode: %w", err)
	}
	return nil
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
		Dependencies:        make([]luaAuditDependency, 0, len(chunk.Dependencies)),
		StaticAbilityTables: make([]string, 0),
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
			BuiltinModes: modes, IndexResolution: resolution,
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
	for _, constant := range inspection.Strings {
		if constant == "RegisterAbility" {
			entry.IsRegistrationConstantPresent = true
		}
	}
	for name, table := range lua51.StaticGlobals(inspection) {
		if table.Kind == lua51.StaticTable && strings.HasPrefix(name, "nAbility_") {
			entry.StaticAbilityTables = append(entry.StaticAbilityTables, name)
		}
	}
	sort.Strings(entry.StaticAbilityTables)
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
				DefinitionState: state, CompileError: ability.CompileError,
			})
		}
		kits = append(kits, entry)
	}
	sort.Slice(kits, func(left, right int) bool { return kits[left].Noun < kits[right].Noun })
	return kits
}
