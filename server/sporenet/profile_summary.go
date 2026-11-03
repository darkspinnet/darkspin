package sporenet

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ProfileSummary contains scalar identity and lifetime counters only.
type ProfileSummary struct {
	Identity UserIdentity
	Stats    PlayerStats
}

// ProfileSummaryRepository avoids loading inventory for account observations.
type ProfileSummaryRepository interface {
	LoadProfileSummary(context.Context, int64) (ProfileSummary, error)
}

// SummaryFromRecord projects scalars without copying the record's collections.
func SummaryFromRecord(record UserRecord) ProfileSummary {
	return ProfileSummary{Identity: UserIdentity{
		ID: record.Account.ID, LoginName: record.LoginName, DisplayName: record.DisplayName,
		CreateDT: record.CreateDT, LastConnectionDT: record.LastConnectionDT,
		AvatarID: record.Account.AvatarID, Level: record.Account.Level, XP: record.Account.XP,
		ChainProgression:            record.Account.ChainProgression,
		IsTutorialCompleted:         record.Account.IsTutorialCompleted(),
		IsTutorialCompletionPending: record.IsTutorialCompletionPending,
	}, Stats: record.Stats}
}

// ProfileSummary reads current active counters or a narrow durable projection.
func (e *UserManager) ProfileSummary(ctx context.Context, userID int64) (ProfileSummary, error) {
	if ctx == nil {
		return ProfileSummary{}, errors.New("nil context")
	}
	err := ctx.Err()
	if err != nil {
		return ProfileSummary{}, fmt.Errorf("summaryContext: %w", err)
	}
	if userID <= 0 {
		return ProfileSummary{}, fmt.Errorf("summaryID: %w", ErrUserNotFound)
	}
	active := e.UserByID(userID)
	if active != nil {
		active.mu.RLock()
		summary := SummaryFromRecord(UserRecord{
			DisplayName: active.DisplayName, LoginName: active.LoginName,
			CreateDT: active.CreateDT, LastConnectionDT: active.LastConnectionDT,
			IsTutorialCompletionPending: active.IsTutorialCompletionPending,
			Account:                     active.Account, Stats: active.Stats,
		})
		if !active.playStartedAt.IsZero() {
			elapsed := time.Since(active.playStartedAt)
			if elapsed > 0 {
				summary.Stats.PVEPlayTimeSecond += uint64(elapsed / time.Second)
			}
		}
		active.mu.RUnlock()
		return summary, nil
	}
	if e.repository == nil {
		return ProfileSummary{}, errors.New("user repository unavailable")
	}
	repository, isSupported := e.repository.(ProfileSummaryRepository)
	if isSupported {
		summary, readErr := repository.LoadProfileSummary(ctx, userID)
		if readErr != nil {
			return ProfileSummary{}, fmt.Errorf("summaryRead: %w", readErr)
		}
		return summary, nil
	}
	// Legacy profile adapters can retain their established full-record read.
	repositoryByID, isSupported := e.repository.(UserProfileRepository)
	if !isSupported {
		return ProfileSummary{}, errors.New("profile summary repository unavailable")
	}
	record, err := repositoryByID.LoadByID(ctx, userID)
	if err != nil {
		return ProfileSummary{}, fmt.Errorf("summaryRecord: %w", err)
	}
	return SummaryFromRecord(record), nil
}
