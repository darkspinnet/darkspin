package sqlite

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/lua51"
)

type luaChunkImport struct {
	id                   int64
	serverDataResourceID int64
	resourceName         string
	sourceName           string
	bytecodeSHA256       string
	bytecode             []byte
	strings              []string
}

type luaDependencyImport struct {
	name          string
	targetChunkID *int64
}

var luaModuleSources = map[string]string{
	"Abilities!template_ability_firstaggro.lua":        "Abilities/0x78AA702F.lua",
	"Abilities!template_ability_melee.lua":             "Abilities/0x7BF2D7DD.lua",
	"Abilities!template_ability_pointblankaoe.lua":     "Abilities/0x8C550D00.lua",
	"Abilities!template_ability_projectile.lua":        "Abilities/0xCE0FC9AA.lua",
	"Abilities!template_ability_targetedaoe.lua":       "Abilities/0x604A151C.lua",
	"Abilities!template_ability_toss.lua":              "Abilities/0xCDD518BA.lua",
	"0x3681d755!GlobalDefinitions.lua":                 "lua/0x2E64AA9E.lua",
	"Lua!AttributeUtils.lua":                           "lua/0x4C983F6A.lua",
	"Modifiers!modifier_drainsnare.lua":                "Modifiers/0x5CA36DD4.lua",
	"Modifiers!modifier_energysentinel_healdebuff.lua": "Modifiers/0x1E05F108.lua",
	"Modifiers!modifier_fireravagerburn.lua":           "Modifiers/0x71216062.lua",
	"Modifiers!modifier_knockbackwithimmunity.lua":     "Modifiers/0x9B80AB0C.lua",
	"Modifiers!modifier_LightningTempest_Basic.lua":    "Modifiers/0x768636B6.lua",
	"Modifiers!modifier_lightspeed_haste.lua":          "Modifiers/0xCFDB9908.lua",
	"Modifiers!modifier_plasmasentinel_burn.lua":       "Modifiers/0xECBD9819.lua",
	"Modifiers!modifier_shocked.lua":                   "Modifiers/0xEABB0526.lua",
	"Modifiers!modifier_timeravager_basic.lua":         "Modifiers/0xFBFDCB83.lua",
	"Modifiers!modifier_voodootempest_weaken.lua":      "Modifiers/0x71564167.lua",
	"Modifiers!template_chain_counter.lua":             "Modifiers/0x184258A8.lua",
	"Modifiers!template_modifier_dot.lua":              "Modifiers/0xD6C886B1.lua",
	"Modifiers!template_modifier_speed.lua":            "Modifiers/0xDD9C47AD.lua",
}

func writeLuaChunks(ctx context.Context, transaction *sql.Tx) error {
	chunks, err := loadLuaChunkImports(ctx, transaction)
	if err != nil {
		return fmt.Errorf("chunkLoad: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO lua_chunk
		(id, server_data_resource_id, source_name, bytecode_sha256, bytecode_size)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("chunkPrepare: %w", err)
	}
	for _, chunk := range chunks {
		_, err = statement.ExecContext(ctx, chunk.id, chunk.serverDataResourceID,
			chunk.sourceName, chunk.bytecodeSHA256, len(chunk.bytecode))
		if err != nil {
			_ = statement.Close()
			return fmt.Errorf("chunkInsert[%d]: %w", chunk.id, err)
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("chunkClose: %w", err)
	}
	err = writeLuaStringConstants(ctx, transaction, chunks)
	if err != nil {
		return fmt.Errorf("stringWrite: %w", err)
	}
	err = writeLuaStaticProperties(ctx, transaction, chunks)
	if err != nil {
		return fmt.Errorf("propertyWrite: %w", err)
	}
	err = writeLuaTokenBindings(ctx, transaction, chunks)
	if err != nil {
		return fmt.Errorf("tokenWrite: %w", err)
	}
	err = writeLuaModuleAliases(ctx, transaction, chunks)
	if err != nil {
		return fmt.Errorf("aliasWrite: %w", err)
	}

	dependencies := resolveLuaDependencies(chunks)
	statement, err = transaction.PrepareContext(ctx, `
		INSERT INTO lua_dependency
		(lua_chunk_id, ordinal, dependency_name, target_lua_chunk_id)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("dependencyPrepare: %w", err)
	}
	dependencyID := int64(1)
	for _, chunk := range chunks {
		for ordinal, dependency := range dependencies[chunk.id] {
			_, err = statement.ExecContext(ctx, chunk.id, ordinal, dependency.name, dependency.targetChunkID)
			if err != nil {
				_ = statement.Close()
				return fmt.Errorf("dependencyInsert[%d]: %w", dependencyID, err)
			}
			dependencyID++
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("dependencyClose: %w", err)
	}
	return nil
}

type luaStaticProperty struct {
	tableName    string
	propertyName string
	minimum      float64
	maximum      float64
	evidence     string
}

func writeLuaStaticProperties(ctx context.Context, transaction *sql.Tx, chunks []luaChunkImport) error {
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO lua_static_property
		(id, lua_chunk_id, table_name, property_name, minimum, maximum, evidence)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("propertyPrepare: %w", err)
	}
	propertyID := int64(1)
	for _, chunk := range chunks {
		inspection, inspectErr := lua51.Inspect(chunk.bytecode)
		if inspectErr != nil {
			_ = statement.Close()
			return fmt.Errorf("propertyInspect[%d]: %w", chunk.id, inspectErr)
		}
		properties := luaAbilityProperties(inspection)
		for _, property := range properties {
			_, err = statement.ExecContext(ctx, propertyID, chunk.id, property.tableName,
				property.propertyName, property.minimum, property.maximum, property.evidence)
			if err != nil {
				_ = statement.Close()
				return fmt.Errorf("propertyInsert[%d:%s]: %w", chunk.id, property.propertyName, err)
			}
			propertyID++
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("propertyClose: %w", err)
	}
	return nil
}

func luaAbilityProperties(chunk *lua51.Chunk) []luaStaticProperty {
	global := lua51.StaticGlobals(chunk)
	tableNames := make([]string, 0, len(global))
	for tableName, table := range global {
		if table.Kind == lua51.StaticTable &&
			(strings.HasPrefix(tableName, "nAbility_") || strings.HasPrefix(tableName, "nModifier_")) {
			tableNames = append(tableNames, tableName)
		}
	}
	sort.Strings(tableNames)
	properties := make([]luaStaticProperty, 0)
	for _, tableName := range tableNames {
		table := global[tableName]
		propertyNames := make([]string, 0, len(table.Fields))
		for propertyName := range table.Fields {
			propertyNames = append(propertyNames, propertyName)
		}
		sort.Strings(propertyNames)
		for _, propertyName := range propertyNames {
			minimum, maximum, evidence, isFound := luaNumericBounds(table.Fields[propertyName])
			if !isFound {
				continue
			}
			properties = append(properties, luaStaticProperty{
				tableName: tableName, propertyName: propertyName,
				minimum: minimum, maximum: maximum, evidence: evidence,
			})
		}
	}
	return properties
}

func luaNumericBounds(field lua51.StaticValue) (float64, float64, string, bool) {
	if field.Kind == lua51.StaticNumber {
		if math.IsNaN(field.Number) || math.IsInf(field.Number, 0) {
			return 0, 0, "", false
		}
		return field.Number, field.Number, "constant", true
	}
	if field.Kind != lua51.StaticTable || len(field.Entries) == 0 {
		return 0, 0, "", false
	}
	first := field.Entries[0]
	if first.Kind == lua51.StaticNumber {
		return first.Number, first.Number, "ranked constant", true
	}
	if first.Kind != lua51.StaticTable || len(first.Entries) == 0 || first.Entries[0].Kind != lua51.StaticNumber {
		return 0, 0, "", false
	}
	minimum := first.Entries[0].Number
	maximum := minimum
	if len(first.Entries) > 1 && first.Entries[1].Kind == lua51.StaticNumber {
		maximum = first.Entries[1].Number
	}
	if math.IsNaN(minimum) || math.IsInf(minimum, 0) || math.IsNaN(maximum) || math.IsInf(maximum, 0) {
		return 0, 0, "", false
	}
	return minimum, maximum, "ranked range", true
}

func writeLuaStringConstants(ctx context.Context, transaction *sql.Tx, chunks []luaChunkImport) error {
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO lua_string_constant
		(id, lua_chunk_id, ordinal, string_constant)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("stringPrepare: %w", err)
	}
	stringID := int64(1)
	for _, chunk := range chunks {
		for ordinal, stringConstant := range chunk.strings {
			_, err = statement.ExecContext(ctx, stringID, chunk.id, ordinal, stringConstant)
			if err != nil {
				_ = statement.Close()
				return fmt.Errorf("stringInsert[%d:%d]: %w", chunk.id, ordinal, err)
			}
			stringID++
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("stringClose: %w", err)
	}
	return nil
}

func writeLuaModuleAliases(ctx context.Context, transaction *sql.Tx, chunks []luaChunkImport) error {
	chunkIDBySource := make(map[string]int64, len(chunks))
	for _, chunk := range chunks {
		chunkIDBySource[normalizeLuaName(chunk.sourceName)] = chunk.id
	}
	moduleNames := make([]string, 0, len(luaModuleSources))
	for moduleName := range luaModuleSources {
		moduleNames = append(moduleNames, moduleName)
	}
	sort.Strings(moduleNames)
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO lua_module_alias
		(id, lua_chunk_id, module_name, evidence)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("aliasPrepare: %w", err)
	}
	for index, moduleName := range moduleNames {
		sourceName := luaModuleSources[moduleName]
		chunkID, isFound := chunkIDBySource[normalizeLuaName(sourceName)]
		if !isFound {
			continue
		}
		_, err = statement.ExecContext(ctx, index+1, chunkID, moduleName, "instruction-checked require target")
		if err != nil {
			_ = statement.Close()
			return fmt.Errorf("aliasInsert[%d]: %w", index, err)
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("aliasClose: %w", err)
	}
	return nil
}

func loadLuaChunkImports(ctx context.Context, transaction *sql.Tx) ([]luaChunkImport, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT server_data.content_source_resource_id,
		       server_data.resource_group,
		       server_data.resource_name,
		       server_data.decoded_payload
		FROM server_data
		WHERE server_data.is_compiled_lua=1
		ORDER BY server_data.content_source_resource_id`)
	if err != nil {
		return nil, fmt.Errorf("chunkQuery: %w", err)
	}
	chunks := make([]luaChunkImport, 0, 1029)
	for rows.Next() {
		var serverDataResourceID int64
		var resourceGroup string
		var resourceName string
		var compressedPayload []byte
		err = rows.Scan(&serverDataResourceID, &resourceGroup, &resourceName, &compressedPayload)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("chunkScan: %w", err)
		}
		bytecode, decodeErr := decompressContent(compressedPayload)
		if decodeErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("chunkDecode[%d]: %w", serverDataResourceID, decodeErr)
		}
		inspection, inspectErr := lua51.Inspect(bytecode)
		if inspectErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("chunkInspect[%d]: %w", serverDataResourceID, inspectErr)
		}
		sourceName := inspection.SourceName
		if sourceName == "" {
			sourceName = resourceGroup + "/" + resourceName
		}
		digest := sha256.Sum256(bytecode)
		chunks = append(chunks, luaChunkImport{
			id:                   int64(len(chunks) + 1),
			serverDataResourceID: serverDataResourceID,
			resourceName:         resourceGroup + "/" + resourceName,
			sourceName:           sourceName,
			bytecodeSHA256:       fmt.Sprintf("%x", digest),
			bytecode:             bytecode,
			strings:              inspection.Strings,
		})
	}
	err = rows.Err()
	if err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("chunkRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("chunkRowsClose: %w", err)
	}
	return chunks, nil
}

func resolveLuaDependencies(chunks []luaChunkImport) map[int64][]luaDependencyImport {
	targetIDs := make(map[string][]int64)
	for _, chunk := range chunks {
		for _, targetName := range luaSourceKeys(chunk.sourceName) {
			targetIDs[targetName] = append(targetIDs[targetName], chunk.id)
		}
	}
	dependencies := make(map[int64][]luaDependencyImport, len(chunks))
	aliasTargetIDs := make(map[string]int64, len(luaModuleSources))
	for moduleName, sourceName := range luaModuleSources {
		matches := targetIDs[normalizeLuaName(sourceName)]
		if len(matches) == 1 {
			aliasTargetIDs[normalizeLuaName(moduleName)] = matches[0]
		}
	}
	for _, chunk := range chunks {
		seen := make(map[string]struct{})
		for _, constant := range chunk.strings {
			if !isLuaDependency(constant) {
				continue
			}
			normalizedName := normalizeLuaName(constant)
			if normalizedName == "" || normalizedName == normalizeLuaName(chunk.sourceName) {
				continue
			}
			if _, isSeen := seen[normalizedName]; isSeen {
				continue
			}
			seen[normalizedName] = struct{}{}
			dependency := luaDependencyImport{name: constant}
			if targetChunkID, isFound := aliasTargetIDs[normalizedName]; isFound && targetChunkID != chunk.id {
				dependency.targetChunkID = &targetChunkID
			}
			for _, lookupName := range luaSourceKeys(constant) {
				if dependency.targetChunkID != nil {
					break
				}
				matches := targetIDs[lookupName]
				if len(matches) != 1 || matches[0] == chunk.id {
					continue
				}
				targetChunkID := matches[0]
				dependency.targetChunkID = &targetChunkID
				break
			}
			dependencies[chunk.id] = append(dependencies[chunk.id], dependency)
		}
		sort.Slice(dependencies[chunk.id], func(left, right int) bool {
			return dependencies[chunk.id][left].name < dependencies[chunk.id][right].name
		})
	}
	return dependencies
}

func isLuaDependency(name string) bool {
	normalizedName := normalizeLuaName(name)
	if normalizedName == "" {
		return false
	}
	if strings.Contains(normalizedName, ".lua") {
		return true
	}
	return strings.HasPrefix(normalizedName, "abilities/") ||
		strings.HasPrefix(normalizedName, "modifiers/") ||
		strings.HasPrefix(normalizedName, "behaviors/")
}

func luaSourceKeys(name string) []string {
	normalizedName := normalizeLuaName(name)
	if normalizedName == "" {
		return nil
	}
	keys := []string{normalizedName}
	baseName := filepath.Base(normalizedName)
	if baseName != normalizedName {
		keys = append(keys, baseName)
	}
	withoutExtension := strings.TrimSuffix(normalizedName, ".lua")
	if withoutExtension != normalizedName {
		keys = append(keys, withoutExtension)
	}
	return keys
}

func normalizeLuaName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "@")
	name = strings.TrimPrefix(name, "!")
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	return strings.ToLower(name)
}

func decompressContent(compressedPayload []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(compressedPayload))
	if err != nil {
		return nil, fmt.Errorf("zlibOpen: %w", err)
	}
	contents, err := io.ReadAll(r)
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("zlibRead: %w", err)
	}
	err = r.Close()
	if err != nil {
		return nil, fmt.Errorf("zlibClose: %w", err)
	}
	return contents, nil
}
