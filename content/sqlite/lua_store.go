package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
)

// LuaChunk is one decoded native Lua 5.1 payload from content.db.
type LuaChunk struct {
	ID       int64
	Source   string
	SHA256   string
	Bytecode []byte
}

// MarkerLuaJob is one uniquely linked marker callback and its authored trigger
// policy. Linking does not itself authorize execution; callers must require
// IsServerOnly and apply scenario phase policy.
type MarkerLuaJob struct {
	LevelID           int64
	EventID           int64
	MarkerID          uint32
	PositionX         float32
	PositionY         float32
	PositionZ         float32
	Radius            float32
	CallbackName      string
	LuaChunkID        int64
	IsTriggerOnceOnly bool
	IsServerOnly      bool
}

// MarkerLuaJob loads one exact authored marker-to-Lua link.
func (s *Store) MarkerLuaJob(ctx context.Context, levelName string, markerID uint32) (MarkerLuaJob, error) {
	if ctx == nil {
		return MarkerLuaJob{}, errors.New("nil context")
	}
	if levelName == "" {
		return MarkerLuaJob{}, errors.New("empty level name")
	}
	var job MarkerLuaJob
	var storedMarkerID int64
	var isTriggerOnceOnly int
	var isServerOnly int
	err := s.database.QueryRowContext(ctx, `
		SELECT level.id, level_event.id, marker.marker_id,
		       marker.position_x, marker.position_y, marker.position_z,
		       level_event.trigger_radius, level_event.callback_name,
		       level_script.lua_chunk_id, level_event.is_trigger_once_only,
		       level_event.is_server_only
		FROM level
		JOIN level_alias ON level_alias.level_id=level.id
		JOIN level_marker_set ON level_marker_set.level_id=level.id
		JOIN marker ON marker.level_marker_set_id=level_marker_set.id
		JOIN level_event ON level_event.marker_id=marker.id
		JOIN level_script ON level_script.level_event_id=level_event.id
		WHERE level_alias.alias=? AND marker.marker_id=?
		  AND level_event.event_kind='triggerVolume'
		  AND level_event.event_slot='luaCallbackOnEnter'`, levelName, int64(markerID)).Scan(
		&job.LevelID, &job.EventID, &storedMarkerID,
		&job.PositionX, &job.PositionY, &job.PositionZ,
		&job.Radius, &job.CallbackName, &job.LuaChunkID,
		&isTriggerOnceOnly, &isServerOnly,
	)
	if err != nil {
		return MarkerLuaJob{}, fmt.Errorf("markerJobQuery[%s:%d]: %w", levelName, markerID, err)
	}
	if storedMarkerID < 0 || storedMarkerID > math.MaxUint32 {
		return MarkerLuaJob{}, fmt.Errorf("markerID: %d", storedMarkerID)
	}
	job.MarkerID = uint32(storedMarkerID)
	job.IsTriggerOnceOnly = isTriggerOnceOnly != 0
	job.IsServerOnly = isServerOnly != 0
	return job, nil
}

// IsLuaCatalogAvailable reports whether this content database includes the
// optional decoded Lua catalog used by the constrained simulator.
func (s *Store) IsLuaCatalogAvailable(ctx context.Context) (bool, error) {
	if s == nil || s.database == nil {
		return false, errors.New("nil store")
	}
	if ctx == nil {
		return false, errors.New("nil context")
	}
	var count int
	err := s.database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='lua_chunk'`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("catalogQuery: %w", err)
	}
	return count == 1, nil
}

// LuaChunk reads and verifies one immutable Lua chunk.
func (s *Store) LuaChunk(ctx context.Context, id int64) (*LuaChunk, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if id <= 0 {
		return nil, errors.New("invalid chunk ID")
	}
	var chunk LuaChunk
	var bytecodeSize int
	var compressedBytecode []byte
	err := s.database.QueryRowContext(ctx, `
		SELECT lua_chunk.id, lua_chunk.source_name, lua_chunk.bytecode_sha256,
		       lua_chunk.bytecode_size, server_data.decoded_payload
		FROM lua_chunk
		JOIN server_data
		  ON server_data.content_source_resource_id=lua_chunk.server_data_resource_id
		WHERE lua_chunk.id=?`, id).Scan(
		&chunk.ID, &chunk.Source, &chunk.SHA256, &bytecodeSize, &compressedBytecode)
	if err != nil {
		return nil, fmt.Errorf("chunkQuery[%d]: %w", id, err)
	}
	chunk.Bytecode, err = decompressContent(compressedBytecode)
	if err != nil {
		return nil, fmt.Errorf("chunkDecode[%d]: %w", id, err)
	}
	if len(chunk.Bytecode) != bytecodeSize {
		return nil, fmt.Errorf("chunkSize[%d]: got %d, want %d", id, len(chunk.Bytecode), bytecodeSize)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(chunk.Bytecode))
	if digest != chunk.SHA256 {
		return nil, fmt.Errorf("chunkHash[%d]: got %s, want %s", id, digest, chunk.SHA256)
	}
	return &chunk, nil
}

// LuaChunkByStringConstant resolves the one chunk containing an exact decoded
// Lua string constant. The caller must still validate the constant's semantic
// role, such as the name passed to nAbility.RegisterAbility.
func (s *Store) LuaChunkByStringConstant(ctx context.Context, stringConstant string) (*LuaChunk, error) {
	chunks, err := s.LuaChunksByStringConstant(ctx, stringConstant)
	if err != nil {
		return nil, fmt.Errorf("stringChunks: %w", err)
	}
	if len(chunks) != 1 {
		return nil, fmt.Errorf("stringChunkAmbiguous: %s", stringConstant)
	}
	return chunks[0], nil
}

// LuaChunksByStringConstant returns every chunk containing an exact decoded
// string. Registration loaders can then disambiguate references by compiling
// candidates and validating the registered identity.
func (s *Store) LuaChunksByStringConstant(ctx context.Context, stringConstant string) ([]*LuaChunk, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if stringConstant == "" {
		return nil, errors.New("empty string constant")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT DISTINCT lua_chunk.id
		FROM lua_string_constant
		JOIN lua_chunk ON lua_chunk.id=lua_string_constant.lua_chunk_id
		WHERE lua_string_constant.string_constant=? COLLATE NOCASE
		ORDER BY lua_chunk.id`, stringConstant)
	if err != nil {
		return nil, fmt.Errorf("stringChunkQuery[%s]: %w", stringConstant, err)
	}
	defer rows.Close()
	chunkIDs := make([]int64, 0, 4)
	for rows.Next() {
		var chunkID int64
		err = rows.Scan(&chunkID)
		if err != nil {
			return nil, fmt.Errorf("stringChunkScan[%s]: %w", stringConstant, err)
		}
		chunkIDs = append(chunkIDs, chunkID)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("stringChunkRows[%s]: %w", stringConstant, err)
	}
	if len(chunkIDs) == 0 {
		return nil, fmt.Errorf("stringChunkMissing: %s", stringConstant)
	}
	chunks := make([]*LuaChunk, 0, len(chunkIDs))
	for index, chunkID := range chunkIDs {
		chunk, loadErr := s.LuaChunk(ctx, chunkID)
		if loadErr != nil {
			return nil, fmt.Errorf("stringChunkLoad[%s:%d]: %w", stringConstant, index, loadErr)
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// LuaModules reads the complete exact require closure linked to a root chunk.
func (s *Store) LuaModules(ctx context.Context, rootID int64) (map[string]LuaChunk, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if rootID <= 0 {
		return nil, errors.New("invalid root chunk ID")
	}
	rows, err := s.database.QueryContext(ctx, `
		WITH RECURSIVE module_dependency(dependency_name, target_lua_chunk_id, path) AS (
			SELECT dependency_name, target_lua_chunk_id,
			       ',' || CAST(target_lua_chunk_id AS TEXT) || ','
			FROM lua_dependency
			WHERE lua_chunk_id=? AND target_lua_chunk_id IS NOT NULL
			UNION ALL
			SELECT dependency.dependency_name, dependency.target_lua_chunk_id,
			       module_dependency.path || CAST(dependency.target_lua_chunk_id AS TEXT) || ','
			FROM module_dependency
			JOIN lua_dependency AS dependency
			  ON dependency.lua_chunk_id=module_dependency.target_lua_chunk_id
			WHERE dependency.target_lua_chunk_id IS NOT NULL
			  AND INSTR(module_dependency.path,
			      ',' || CAST(dependency.target_lua_chunk_id AS TEXT) || ',')=0
		)
		SELECT DISTINCT module_dependency.dependency_name, lua_chunk.id, lua_chunk.source_name,
		       lua_chunk.bytecode_sha256, lua_chunk.bytecode_size, server_data.decoded_payload
		FROM module_dependency
		JOIN lua_chunk ON lua_chunk.id=module_dependency.target_lua_chunk_id
		JOIN server_data
		  ON server_data.content_source_resource_id=lua_chunk.server_data_resource_id
		ORDER BY module_dependency.dependency_name`, rootID)
	if err != nil {
		return nil, fmt.Errorf("moduleQuery[%d]: %w", rootID, err)
	}
	defer rows.Close()
	modules := make(map[string]LuaChunk)
	for rows.Next() {
		var moduleName string
		var chunk LuaChunk
		var bytecodeSize int
		var compressedBytecode []byte
		err = rows.Scan(&moduleName, &chunk.ID, &chunk.Source, &chunk.SHA256,
			&bytecodeSize, &compressedBytecode)
		if err != nil {
			return nil, fmt.Errorf("moduleScan[%d]: %w", rootID, err)
		}
		chunk.Bytecode, err = decompressContent(compressedBytecode)
		if err != nil {
			return nil, fmt.Errorf("moduleDecode[%d]: %w", chunk.ID, err)
		}
		if len(chunk.Bytecode) != bytecodeSize {
			return nil, fmt.Errorf("moduleSize[%d]: got %d, want %d", chunk.ID, len(chunk.Bytecode), bytecodeSize)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(chunk.Bytecode))
		if digest != chunk.SHA256 {
			return nil, fmt.Errorf("moduleHash[%d]: got %s, want %s", chunk.ID, digest, chunk.SHA256)
		}
		existing, isExisting := modules[moduleName]
		if isExisting && existing.ID != chunk.ID {
			return nil, fmt.Errorf("moduleAmbiguous[%s]: %d/%d", moduleName, existing.ID, chunk.ID)
		}
		modules[moduleName] = chunk
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("moduleRows[%d]: %w", rootID, err)
	}
	return modules, nil
}
