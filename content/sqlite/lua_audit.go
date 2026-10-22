package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
)

// OpenReadOnly opens an existing content database without changing its schema,
// journal policy, or imported content.
func OpenReadOnly(ctx context.Context, path string) (store *Store, resultErr error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	parameters := url.Values{}
	parameters.Set("mode", "ro")
	parameters.Add("_pragma", "query_only(1)")
	parameters.Add("_pragma", "busy_timeout(5000)")
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(absolutePath)+"?"+parameters.Encode())
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	defer func() {
		if resultErr == nil {
			return
		}
		closeErr := database.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("databaseClose: %w", closeErr))
		}
	}()
	database.SetMaxOpenConns(4)
	err = database.PingContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("databasePing: %w", err)
	}
	return &Store{database: database}, nil
}

// LuaIndexChunk is indexed evidence only; none of these links prove execution.
type LuaIndexChunk struct {
	ID           int64
	Source       string
	SHA256       string
	Size         int
	Aliases      []string
	Dependencies []LuaIndexDependency
	Callbacks    []string
}

type LuaIndexDependency struct {
	Name          string
	TargetChunkID *int64
}

// LuaIndex returns every imported chunk and its indexed dependency evidence.
func (e *Store) LuaIndex(ctx context.Context) (chunks []LuaIndexChunk, resultErr error) {
	rows, err := e.database.QueryContext(ctx, `SELECT id, source_name, bytecode_sha256, bytecode_size FROM lua_chunk ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("chunkQuery: %w", err)
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("chunkClose: %w", closeErr))
		}
	}()
	chunks = make([]LuaIndexChunk, 0)
	for rows.Next() {
		var chunk LuaIndexChunk
		err = rows.Scan(&chunk.ID, &chunk.Source, &chunk.SHA256, &chunk.Size)
		if err != nil {
			return nil, fmt.Errorf("chunkScan: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("chunkRows: %w", err)
	}
	for index := range chunks {
		err = e.loadLuaIndexLinks(ctx, &chunks[index])
		if err != nil {
			return nil, fmt.Errorf("chunkLinks[%d]: %w", chunks[index].ID, err)
		}
	}
	return chunks, nil
}

func (e *Store) loadLuaIndexLinks(ctx context.Context, chunk *LuaIndexChunk) (resultErr error) {
	rows, err := e.database.QueryContext(ctx, `
		SELECT 'alias', module_name, NULL FROM lua_module_alias WHERE lua_chunk_id=?
		UNION ALL SELECT 'dependency', dependency_name, target_lua_chunk_id FROM lua_dependency WHERE lua_chunk_id=?
		UNION ALL SELECT 'callback', callback_name, NULL FROM level_script WHERE lua_chunk_id=?
		ORDER BY 1, 2`, chunk.ID, chunk.ID, chunk.ID)
	if err != nil {
		return fmt.Errorf("linkQuery: %w", err)
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("linkClose: %w", closeErr))
		}
	}()
	for rows.Next() {
		var kind, name string
		var targetChunkID *int64
		err = rows.Scan(&kind, &name, &targetChunkID)
		if err != nil {
			return fmt.Errorf("linkScan: %w", err)
		}
		switch kind {
		case "alias":
			chunk.Aliases = append(chunk.Aliases, name)
		case "dependency":
			chunk.Dependencies = append(chunk.Dependencies, LuaIndexDependency{Name: name, TargetChunkID: targetChunkID})
		case "callback":
			chunk.Callbacks = append(chunk.Callbacks, name)
		}
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("linkRows: %w", err)
	}
	return nil
}
