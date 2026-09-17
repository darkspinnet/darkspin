package sqlite

import (
	"context"
	"errors"
	"fmt"
)

// ChainLevelReferences returns the complete authored campaign order. Repeated
// level assets are retained because their ordinal is the progression identity.
func (s *Store) ChainLevelReferences(ctx context.Context) ([]string, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT ordinal, level_reference
		FROM chain_level
		ORDER BY ordinal`)
	if err != nil {
		return nil, fmt.Errorf("chainQuery: %w", err)
	}
	defer rows.Close()
	reference := make([]string, 0, len(build103ChainLevelReference))
	for rows.Next() {
		var ordinal int
		var levelReference string
		err = rows.Scan(&ordinal, &levelReference)
		if err != nil {
			return nil, fmt.Errorf("chainScan[%d]: %w", len(reference), err)
		}
		if ordinal != len(reference) {
			return nil, fmt.Errorf("chainOrdinal: got %d, want %d", ordinal, len(reference))
		}
		if levelReference == "" {
			return nil, fmt.Errorf("chainReference[%d]: empty", ordinal)
		}
		reference = append(reference, levelReference)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("chainRows: %w", err)
	}
	if len(reference) != len(build103ChainLevelReference) {
		return nil, fmt.Errorf("chainCount: got %d, want %d",
			len(reference), len(build103ChainLevelReference))
	}
	return reference, nil
}
