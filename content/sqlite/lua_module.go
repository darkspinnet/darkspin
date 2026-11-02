package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type luaResourceKey struct {
	group    uint32
	instance uint32
}

// Native require (8F1280) parses a resource key through 7ADD40. Its group
// and extension-free instance use literal hex IDs or the case-folded FNV-1
// hash in 7AD4E0. Restrict recovery to ordinary qualified Lua references;
// special resource-key syntax and unqualified names retain existing policy.
func luaModuleResourceKey(name string) (luaResourceKey, bool) {
	group, filename, isQualified := strings.Cut(name, "!")
	if !isQualified {
		group, filename, isQualified = strings.Cut(name, "/")
	}
	if !isQualified || group == "" || strings.ContainsAny(group+filename, "@\\! /\t\r\n") {
		return luaResourceKey{}, false
	}
	stem, extension, hasExtension := strings.Cut(filename, ".")
	if !hasExtension || stem == "" || !strings.EqualFold(extension, "lua") {
		return luaResourceKey{}, false
	}
	groupID, isGroupValid := luaResourceID(group)
	instanceID, isInstanceValid := luaResourceID(stem)
	return luaResourceKey{group: groupID, instance: instanceID}, isGroupValid && isInstanceValid
}

func luaResourceID(name string) (uint32, bool) {
	if strings.HasPrefix(strings.ToLower(name), "0x") {
		id, err := strconv.ParseUint(name[2:], 16, 32)
		if err != nil {
			return 0, false
		}
		return uint32(id), true
	}
	for _, character := range name {
		if character > 127 {
			return 0, false
		}
	}
	return hashID(name), true
}

func resolveLuaResource(name string, resourceIDs map[luaResourceKey][]int64) (int64, error) {
	key, isQualified := luaModuleResourceKey(name)
	if !isQualified {
		return 0, nil
	}
	ids := resourceIDs[key]
	if len(ids) > 1 {
		return 0, fmt.Errorf("ambiguous Lua resource %q: %v", name, ids)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

func (e *Store) luaResourceIDs(ctx context.Context) (resourceIDs map[luaResourceKey][]int64, resultErr error) {
	rows, err := e.database.QueryContext(ctx, `
		SELECT lua_chunk.id, content_source_resource.group_id, content_source_resource.instance_id
		FROM lua_chunk JOIN server_data
		ON server_data.content_source_resource_id=lua_chunk.server_data_resource_id
		JOIN content_source_resource ON content_source_resource.id=server_data.content_source_resource_id`)
	if err != nil {
		return nil, fmt.Errorf("resourceQuery: %w", err)
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("resourceClose: %w", closeErr))
		}
	}()
	resourceIDs = make(map[luaResourceKey][]int64)
	for rows.Next() {
		var id int64
		var groupID uint32
		var instanceID uint64
		err = rows.Scan(&id, &groupID, &instanceID)
		if err != nil {
			return nil, fmt.Errorf("resourceScan: %w", err)
		}
		// Native Lua keys carry a 32-bit instance. Do not truncate a wider
		// DBPF identity into an unrelated module.
		if instanceID > uint64(^uint32(0)) {
			continue
		}
		key := luaResourceKey{group: groupID, instance: uint32(instanceID)}
		resourceIDs[key] = append(resourceIDs[key], id)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("resourceRows: %w", err)
	}
	return resourceIDs, nil
}

func (e *Store) luaModuleDependencies(ctx context.Context) (dependencies map[int64][]LuaIndexDependency, resultErr error) {
	rows, err := e.database.QueryContext(ctx, `SELECT lua_chunk_id, dependency_name, target_lua_chunk_id FROM lua_dependency ORDER BY lua_chunk_id, ordinal`)
	if err != nil {
		return nil, fmt.Errorf("dependencyQuery: %w", err)
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("dependencyClose: %w", closeErr))
		}
	}()
	dependencies = make(map[int64][]LuaIndexDependency)
	for rows.Next() {
		var id int64
		var dependency LuaIndexDependency
		err = rows.Scan(&id, &dependency.Name, &dependency.TargetChunkID)
		if err != nil {
			return nil, fmt.Errorf("dependencyScan: %w", err)
		}
		dependencies[id] = append(dependencies[id], dependency)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("dependencyRows: %w", err)
	}
	return dependencies, nil
}

// Resolve only the requested root's closure. Loading supplies immutable bytes;
// compiler-specific preloads and actual require calls still control execution.
func (e *Store) luaResolvedModules(ctx context.Context, rootID int64) (map[string]LuaChunk, error) {
	resourceIDs, err := e.luaResourceIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("moduleResources: %w", err)
	}
	dependencies, err := e.luaModuleDependencies(ctx)
	if err != nil {
		return nil, fmt.Errorf("moduleDependencies: %w", err)
	}
	modules := make(map[string]LuaChunk)
	chunksByID := make(map[int64]*LuaChunk)
	visitedIDs := make(map[int64]bool)
	pendingIDs := []int64{rootID}
	for len(pendingIDs) > 0 {
		id := pendingIDs[0]
		pendingIDs = pendingIDs[1:]
		if visitedIDs[id] {
			continue
		}
		visitedIDs[id] = true
		for _, dependency := range dependencies[id] {
			targetID := int64(0)
			if dependency.TargetChunkID != nil {
				targetID = *dependency.TargetChunkID
			} else {
				targetID, err = resolveLuaResource(dependency.Name, resourceIDs)
				if err != nil {
					return nil, fmt.Errorf("moduleResolve: %w", err)
				}
			}
			if targetID == 0 {
				continue
			}
			chunk := chunksByID[targetID]
			if chunk == nil {
				chunk, err = e.LuaChunk(ctx, targetID)
				if err != nil {
					return nil, fmt.Errorf("moduleLoad: %w", err)
				}
				chunksByID[targetID] = chunk
			}
			existing, isExisting := modules[dependency.Name]
			if isExisting && existing.ID != targetID {
				return nil, fmt.Errorf("ambiguous Lua module %q: %d/%d", dependency.Name, existing.ID, targetID)
			}
			modules[dependency.Name] = *chunk
			pendingIDs = append(pendingIDs, targetID)
		}
	}
	return modules, nil
}
