package sqlite

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
)

const bfxNavigationType = uint32(0x1999ae0b)

func (s *Store) LevelNavigation(ctx context.Context, levelName string) ([]byte, error) {
	if s == nil || s.database == nil || levelName == "" {
		return nil, errors.New("level navigation invalid")
	}
	var decodedSize int
	var compression string
	var compressed []byte
	err := s.database.QueryRowContext(ctx, `
		SELECT level_navigation.decoded_size, level_navigation.decoded_compression,
		       level_navigation.decoded_payload
		FROM level
		JOIN level_navigation ON level_navigation.level_id=level.id
		JOIN level_alias ON level_alias.level_id=level.id
		WHERE level_alias.alias=? COLLATE NOCASE
		LIMIT 1`,
		levelName,
	).Scan(&decodedSize, &compression, &compressed)
	if err != nil {
		return nil, fmt.Errorf("navigationRead: %w", err)
	}
	if compression != "zlib" || decodedSize <= 0 {
		return nil, errors.New("level navigation encoding invalid")
	}
	r, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("navigationZlib: %w", err)
	}
	decoded, err := io.ReadAll(r)
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("navigationDecode: %w", err)
	}
	err = r.Close()
	if err != nil {
		return nil, fmt.Errorf("navigationClose: %w", err)
	}
	if len(decoded) != decodedSize {
		return nil, fmt.Errorf("navigationSize: got %d, want %d", len(decoded), decodedSize)
	}
	return decoded, nil
}

func (s *Store) LevelNavigationNames(ctx context.Context) ([]string, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("level navigation store invalid")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT level.name
		FROM level
		JOIN level_navigation ON level_navigation.level_id=level.id
		ORDER BY level.name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("navigationNamesQuery: %w", err)
	}
	names := make([]string, 0)
	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("navigationNamesScan: %w", err)
		}
		names = append(names, name)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("navigationNamesRows: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("navigationNamesClose: %w", closeErr)
	}
	return names, nil
}

func (s *Store) ResolveLevelNavigationName(
	ctx context.Context, levelAlias string,
) (string, error) {
	if s == nil || s.database == nil || levelAlias == "" {
		return "", errors.New("level navigation alias invalid")
	}
	var name string
	err := s.database.QueryRowContext(ctx, `
		SELECT level.name
		FROM level
		JOIN level_navigation ON level_navigation.level_id=level.id
		JOIN level_alias ON level_alias.level_id=level.id
		WHERE level_alias.alias=? COLLATE NOCASE
		LIMIT 1`, levelAlias).Scan(&name)
	if err != nil {
		return "", fmt.Errorf("navigationNameResolve: %w", err)
	}
	return name, nil
}
