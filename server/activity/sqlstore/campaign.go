package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

// RecordCampaignCompletion shares the profile reward transaction. Its first-win
// baseline survives chart retention; anonymous global totals survive profile purge.
func RecordCampaignCompletion(ctx context.Context, tx *sqlx.Tx, req activity.CampaignCompletion) error {
	if req.UserID <= 0 || req.ResultID == 0 || req.ResultID > math.MaxInt64 || req.CampaignNumber == 0 || req.LevelNumber == 0 || req.LevelNumber > 4 || req.At.IsZero() || req.At.Unix() <= 0 {
		return errors.New("invalid campaign completion")
	}
	if tx.DriverName() == "pgx" || tx.DriverName() == "postgres" {
		err := execStatement(ctx, tx, `SELECT pg_advisory_xact_lock(1936748398, 3)`)
		if err != nil {
			return fmt.Errorf("campaignLock: %w", err)
		}
	}
	var previous campaignCompletionRow
	err := tx.GetContext(ctx, &previous, tx.Rebind(`SELECT campaign_number, level_number FROM activity_campaign_completion WHERE user_id = ? AND result_id = ?`), req.UserID, req.ResultID)
	if err == nil {
		if previous.CampaignNumber != req.CampaignNumber || previous.LevelNumber != req.LevelNumber {
			return errors.New("campaign completion identity conflict")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("campaignPrevious: %w", err)
	}
	var firstID uint64
	err = tx.GetContext(ctx, &firstID, tx.Rebind(`INSERT INTO activity_campaign_first_completion (user_id, campaign_number, level_number, result_id)
 VALUES (?, ?, ?, ?) ON CONFLICT (user_id, campaign_number, level_number) DO NOTHING RETURNING result_id`), req.UserID, req.CampaignNumber, req.LevelNumber, req.ResultID)
	isReplay := int64(0)
	if errors.Is(err, sql.ErrNoRows) {
		isReplay = 1
	} else if err != nil {
		return fmt.Errorf("campaignFirst: %w", err)
	} else if firstID != req.ResultID {
		return errors.New("campaign first completion identity conflict")
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_campaign_completion (user_id, result_id, campaign_number, level_number, completed_at, is_replay)
 VALUES (?, ?, ?, ?, ?, ?)`, req.UserID, req.ResultID, req.CampaignNumber, req.LevelNumber, req.At.UTC().Unix(), isReplay)
	if err != nil {
		return fmt.Errorf("campaignReceipt: %w", err)
	}
	err = completeMission(ctx, tx, req)
	if err != nil {
		return fmt.Errorf("campaignMission: %w", err)
	}
	err = recordCampaignSample(ctx, tx, req, isReplay)
	if err != nil {
		return fmt.Errorf("campaignSample: %w", err)
	}
	err = recordCampaignLevel(ctx, tx, req, isReplay)
	if err != nil {
		return fmt.Errorf("campaignLevel: %w", err)
	}
	return nil
}

type campaignCompletionRow struct {
	CampaignNumber uint32 `db:"campaign_number"`
	LevelNumber    uint32 `db:"level_number"`
}
