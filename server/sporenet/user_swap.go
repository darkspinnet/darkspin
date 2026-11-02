package sporenet

import (
	"context"
	"errors"
	"fmt"
)

type ProfileNameSwapRequest struct {
	DeletedUserID int64
	ActiveUserID  int64
}

type ProfileNameSwap struct {
	DeletedUserID    int64
	ActiveUserID     int64
	DeletedLoginName string
	ActiveLoginName  string
}

// SwapProfileNames changes identities without moving progress or deletion state.
// Its administrative caller must enforce the dedicated-server boundary.
func SwapProfileNames(
	ctx context.Context, repository UserNameSwapRepository, req ProfileNameSwapRequest,
) (ProfileNameSwap, error) {
	err := contextError(ctx)
	if err != nil {
		return ProfileNameSwap{}, fmt.Errorf("swapContext: %w", err)
	}
	if repository == nil {
		return ProfileNameSwap{}, errors.New("profile name swap repository unavailable")
	}
	if req.DeletedUserID <= 0 || req.ActiveUserID <= 0 || req.DeletedUserID == req.ActiveUserID {
		return ProfileNameSwap{}, errors.New("swap requires two different positive account IDs")
	}
	result, err := repository.SwapProfileNames(ctx, req)
	if err != nil {
		return ProfileNameSwap{}, fmt.Errorf("swapProfile: %w", err)
	}
	return result, nil
}
