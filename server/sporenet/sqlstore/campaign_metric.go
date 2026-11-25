package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	activitysql "github.com/darkspinnet/darkspin/server/activity/sqlstore"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jmoiron/sqlx"
)

// Only newly durable XP receipts bearing authoritative completion metadata count.
// Import/Create writes their existing receipt IDs without emitting activity, so
// later profile saves, copies, restores, and pre-upgrade receipts cannot backfill wins.
func recordCampaignCompletions(ctx context.Context, tx *sqlx.Tx, record sporenet.UserRecord) error {
	isPending := false
	for _, receipt := range record.CampaignExperiences {
		if receipt.CampaignNumber != 0 {
			isPending = true
			break
		}
	}
	if !isPending {
		return nil
	}
	resultIDs := make([]uint64, 0)
	err := tx.SelectContext(ctx, &resultIDs, tx.Rebind(`SELECT result_id FROM campaign_experience WHERE user_id = ?`), record.Account.ID)
	if err != nil {
		return fmt.Errorf("completionPrevious: %w", err)
	}
	previousIDs := make(map[uint64]struct{}, len(resultIDs))
	for _, resultID := range resultIDs {
		previousIDs[resultID] = struct{}{}
	}
	for _, receipt := range record.CampaignExperiences {
		if receipt.CampaignNumber == 0 {
			continue
		}
		_, isSaved := previousIDs[receipt.ResultID]
		if isSaved {
			continue
		}
		err = activitysql.RecordCampaignCompletion(ctx, tx, activity.CampaignCompletion{
			UserID: record.Account.ID, ResultID: receipt.ResultID, CampaignNumber: receipt.CampaignNumber, LevelNumber: receipt.CampaignLevelNumber, At: time.Unix(receipt.CompletedAtUnix, 0).UTC(),
		})
		if err != nil {
			return fmt.Errorf("completionRecord: %w", err)
		}
	}
	return nil
}
