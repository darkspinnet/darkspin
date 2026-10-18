package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ImportStage struct {
	Name     string
	Duration time.Duration
}

// ImportStages reports the last measured duration for each import phase.
func (e *Store) ImportStages(ctx context.Context) (stages []ImportStage, resultErr error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("import stage store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT stage_name, duration_ns
		FROM content_import_stage ORDER BY stage_name`)
	if err != nil {
		return nil, fmt.Errorf("stageQuery: %w", err)
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("stageClose: %w", closeErr))
		}
	}()
	stages = make([]ImportStage, 0)
	for rows.Next() {
		var stage ImportStage
		var durationNS int64
		err = rows.Scan(&stage.Name, &durationNS)
		if err != nil {
			return nil, fmt.Errorf("stageScan: %w", err)
		}
		stage.Duration = time.Duration(durationNS)
		stages = append(stages, stage)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("stageRows: %w", err)
	}
	return stages, nil
}

func writeImportStages(ctx context.Context, transaction *sql.Tx, timings []BuildStageTiming) (resultErr error) {
	statement, err := transaction.PrepareContext(ctx, `INSERT INTO content_import_stage
		(stage_name, duration_ns) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("stagePrepare: %w", err)
	}
	defer func() {
		closeErr := statement.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("stageClose: %w", closeErr))
		}
	}()
	for _, timing := range timings {
		if !timing.IsComplete {
			return fmt.Errorf("stageIncomplete: %s", timing.Phase)
		}
		_, err = statement.ExecContext(ctx, timing.Phase, timing.Duration.Nanoseconds())
		if err != nil {
			return fmt.Errorf("stageInsert[%s]: %w", timing.Phase, err)
		}
	}
	return nil
}
