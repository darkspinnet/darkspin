package sporenet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const deletedProfileRetention = 120 * 24 * time.Hour

// RestoreDeletedProfile recovers one archived profile through the dedicated-server
// administrative port. Its caller must enforce the dedicated-server boundary.
// It preserves the original identity unless an alternate name is supplied.
func RestoreDeletedProfile(
	ctx context.Context, repository UserRecoveryRepository,
	userID int64, loginName string,
) (string, error) {
	err := contextError(ctx)
	if err != nil {
		return "", fmt.Errorf("restoreContext: %w", err)
	}
	if repository == nil {
		return "", errors.New("recovery repository unavailable")
	}
	if userID <= 0 {
		return "", errors.New("archived user ID must be positive")
	}
	loginName = strings.TrimSpace(loginName)
	if loginName != "" {
		err = validateCopiedUserIdentity(loginName)
		if err != nil {
			return "", fmt.Errorf("restoreIdentity: %w", err)
		}
	}
	restoredLoginName, err := repository.RestoreDeletedProfile(ctx, userID, loginName)
	if err != nil {
		return "", fmt.Errorf("restoreProfile: %w", err)
	}
	return restoredLoginName, nil
}

// PurgeExpiredProfiles applies the fixed retention policy before server startup.
func PurgeExpiredProfiles(
	ctx context.Context, repository UserPurgeRepository, now time.Time,
) (int64, error) {
	err := contextError(ctx)
	if err != nil {
		return 0, fmt.Errorf("purgeContext: %w", err)
	}
	if repository == nil {
		return 0, errors.New("purge repository unavailable")
	}
	if now.IsZero() {
		return 0, errors.New("purge time is required")
	}
	purgedCount, err := repository.PurgeDeletedProfiles(ctx, now.UTC().Add(-deletedProfileRetention))
	if err != nil {
		return 0, fmt.Errorf("purgeProfile: %w", err)
	}
	return purgedCount, nil
}
