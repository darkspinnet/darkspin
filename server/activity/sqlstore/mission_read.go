package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
)

type missionSampleRow struct {
	DayAt           int64 `db:"day_at"`
	StartedCount    int64 `db:"started_count"`
	CompletedCount  int64 `db:"completed_count"`
	UnresolvedCount int64 `db:"unresolved_count"`
}

func readMissionSamples(ctx context.Context, reader campaignSampleReader, req activity.Window, userID int64) (time.Time, []activity.MissionSample, error) {
	var boundary int64
	err := reader.GetContext(ctx, &boundary, `SELECT COALESCE(MAX(started_at), 0) FROM activity_collection WHERE id = 11`)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("missionStart: %w", err)
	}
	samples := make([]activity.MissionSample, 0)
	if boundary == 0 {
		return time.Time{}, samples, nil
	}
	start := req.Since.UTC().Truncate(24 * time.Hour).Unix()
	rows := make([]missionSampleRow, 0)
	if userID > 0 {
		// Unresolved counts refer to start-day cohorts as of the requested end.
		// Completion-day counts are a separate series; a success can cross midnight.
		err = reader.SelectContext(ctx, &rows, reader.Rebind(`SELECT day_at, SUM(started_count) AS started_count,
 SUM(completed_count) AS completed_count, SUM(unresolved_count) AS unresolved_count FROM (
 SELECT (started_at / 86400) * 86400 AS day_at, COUNT(*) AS started_count,
 0 AS completed_count, SUM(CASE WHEN completed_at = 0 OR completed_at > ? THEN 1 ELSE 0 END) AS unresolved_count
 FROM activity_mission WHERE user_id = ? AND started_at >= ? AND started_at >= ? AND started_at <= ? GROUP BY 1
 UNION ALL
 SELECT (completed_at / 86400) * 86400 AS day_at, 0 AS started_count,
 COUNT(*) AS completed_count, 0 AS unresolved_count FROM activity_mission
 WHERE user_id = ? AND started_at >= ? AND completed_at >= ? AND completed_at >= ? AND completed_at <= ? GROUP BY 1
 ) AS mission GROUP BY day_at ORDER BY day_at`), req.Until.Unix(), userID, boundary, start, req.Until.Unix(),
			userID, boundary, boundary, start, req.Until.Unix())
	} else {
		err = reader.SelectContext(ctx, &rows, reader.Rebind(`SELECT day_at, started_count, completed_count,
 started_count - resolved_start_count AS unresolved_count FROM activity_mission_day
 WHERE day_at >= ? AND day_at <= ? ORDER BY day_at`), start, req.Until.Unix())
	}
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("missionRead: %w", err)
	}
	for _, row := range rows {
		samples = append(samples, activity.MissionSample{At: time.Unix(row.DayAt, 0).UTC(),
			StartedCount: row.StartedCount, CompletedCount: row.CompletedCount, UnresolvedCount: row.UnresolvedCount})
	}
	return time.Unix(boundary, 0).UTC(), samples, nil
}
