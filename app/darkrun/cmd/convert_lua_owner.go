package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type luaOwnerResourceKey struct {
	group    uint32
	instance uint64
}

type luaIndexOwner struct {
	NounID       uint32 `json:"noun_id"`
	NounName     string `json:"noun_name"`
	Slot         string `json:"slot"`
	AbilityAsset string `json:"ability_asset"`
	Evidence     string `json:"evidence"`
}

// Owner links are observed asset references, not recovered Lua filenames or
// proof that a chunk implements a creature's ability. Shared helpers can have
// several owners; the index retains all matching relationships.
func enrichLuaIndexOwners(ctx context.Context, destinationPath, contentDBPath string) error {
	if contentDBPath == "" {
		return nil
	}
	fi, err := os.Stat(destinationPath)
	if err != nil {
		return fmt.Errorf("destinationStat: %w", err)
	}
	if !fi.IsDir() {
		return errors.New("Lua owner enrichment requires a DS directory")
	}
	indexPath := filepath.Join(destinationPath, "lua-index.json")
	payload, err := os.ReadFile(indexPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("indexRead: %w", err)
	}
	indexFields := make(map[string]json.RawMessage)
	err = json.Unmarshal(payload, &indexFields)
	if err != nil {
		return fmt.Errorf("indexDecode: %w", err)
	}
	var version int
	err = json.Unmarshal(indexFields["version"], &version)
	if err != nil {
		return fmt.Errorf("versionDecode: %w", err)
	}
	if version != 1 {
		return fmt.Errorf("versionUnsupported: %d", version)
	}
	var resources []map[string]json.RawMessage
	err = json.Unmarshal(indexFields["resources"], &resources)
	if err != nil {
		return fmt.Errorf("resourceDecode: %w", err)
	}
	ownersByResource, err := readLuaIndexOwners(ctx, contentDBPath)
	if err != nil {
		return fmt.Errorf("ownerRead: %w", err)
	}
	for ordinal, resourceFields := range resources {
		group, groupErr := luaOwnerIdentity(resourceFields["group"], 32)
		if groupErr != nil {
			return fmt.Errorf("groupDecode[%d]: %w", ordinal, groupErr)
		}
		instance, instanceErr := luaOwnerIdentity(resourceFields["instance"], 64)
		if instanceErr != nil {
			return fmt.Errorf("instanceDecode[%d]: %w", ordinal, instanceErr)
		}
		owners := ownersByResource[luaOwnerResourceKey{group: uint32(group), instance: instance}]
		if owners == nil {
			owners = []luaIndexOwner{}
		}
		resourceFields["owners"], err = json.Marshal(owners)
		if err != nil {
			return fmt.Errorf("ownerEncode[%d]: %w", ordinal, err)
		}
	}
	indexFields["resources"], err = json.Marshal(resources)
	if err != nil {
		return fmt.Errorf("resourceEncode: %w", err)
	}
	payload, err = json.MarshalIndent(indexFields, "", "  ")
	if err != nil {
		return fmt.Errorf("indexEncode: %w", err)
	}
	err = replaceLuaOwnerIndex(ctx, indexPath, append(payload, '\n'))
	if err != nil {
		return fmt.Errorf("indexReplace: %w", err)
	}
	return nil
}

func luaOwnerIdentity(payload json.RawMessage, width int) (uint64, error) {
	var identity string
	err := json.Unmarshal(payload, &identity)
	if err != nil {
		return 0, fmt.Errorf("identityDecode: %w", err)
	}
	id, err := strconv.ParseUint(identity, 0, width)
	if err != nil {
		return 0, fmt.Errorf("identityParse: %w", err)
	}
	return id, nil
}

func readLuaIndexOwners(ctx context.Context, contentDBPath string) (ownersByResource map[luaOwnerResourceKey][]luaIndexOwner, resultErr error) {
	absolutePath, err := filepath.Abs(contentDBPath)
	if err != nil {
		return nil, fmt.Errorf("databasePath: %w", err)
	}
	parameters := url.Values{}
	parameters.Set("mode", "ro")
	parameters.Add("_pragma", "busy_timeout(5000)")
	databasePath := filepath.ToSlash(absolutePath)
	if filepath.VolumeName(absolutePath) != "" && !strings.HasPrefix(databasePath, "/") {
		// A drive letter belongs in the URI path, never its authority.
		databasePath = "/" + databasePath
	}
	databaseURL := url.URL{Scheme: "file", Path: databasePath, RawQuery: parameters.Encode()}
	database, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	defer func() {
		closeErr := database.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("databaseClose: %w", closeErr))
		}
	}()
	database.SetMaxOpenConns(1)
	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT content_source_resource.group_id,
		       content_source_resource.instance_id, creature_template.id,
		       creature_template.name, creature_template_ability.slot,
		       creature_template_ability.asset_name
		FROM creature_template_ability
		JOIN creature_template
		  ON creature_template.id = creature_template_ability.creature_template_id
		JOIN lua_string_constant
		  ON lua_string_constant.string_constant = creature_template_ability.asset_name COLLATE NOCASE
		JOIN lua_chunk ON lua_chunk.id = lua_string_constant.lua_chunk_id
		JOIN content_source_resource
		  ON content_source_resource.id = lua_chunk.server_data_resource_id
		ORDER BY content_source_resource.group_id, content_source_resource.instance_id,
		         creature_template.id, creature_template_ability.slot,
		         creature_template_ability.asset_name`)
	if err != nil {
		return nil, fmt.Errorf("ownerQuery: %w", err)
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("ownerClose: %w", closeErr))
		}
	}()
	ownersByResource = make(map[luaOwnerResourceKey][]luaIndexOwner)
	for rows.Next() {
		var key luaOwnerResourceKey
		owner := luaIndexOwner{Evidence: "ability-asset-reference"}
		err = rows.Scan(&key.group, &key.instance, &owner.NounID, &owner.NounName, &owner.Slot, &owner.AbilityAsset)
		if err != nil {
			return nil, fmt.Errorf("ownerScan: %w", err)
		}
		ownersByResource[key] = append(ownersByResource[key], owner)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("ownerRows: %w", err)
	}
	return ownersByResource, nil
}

func replaceLuaOwnerIndex(ctx context.Context, indexPath string, payload []byte) (resultErr error) {
	w, err := os.CreateTemp(filepath.Dir(indexPath), ".lua-index-*")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	temporaryPath := w.Name()
	defer func() {
		removeErr := os.Remove(temporaryPath)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("temporaryRemove: %w", removeErr))
		}
	}()
	_, writeErr := w.Write(payload)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("temporaryWrite: %w", errors.Join(writeErr, closeErr))
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("replaceContext: %w", err)
	}
	err = os.Rename(temporaryPath, indexPath)
	if err != nil {
		return fmt.Errorf("indexRename: %w", err)
	}
	return nil
}
