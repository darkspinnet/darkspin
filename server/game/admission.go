package game

import (
	"context"
	"fmt"
)

type accountAdmissionAuthorizer interface {
	AuthorizeSession(context.Context, int64) error
}

func authorizeGameplayAccount(ctx context.Context, userFinder ActiveUserFinder, userID int64) error {
	authorizer, isSupported := userFinder.(accountAdmissionAuthorizer)
	if !isSupported {
		return nil
	}
	err := authorizer.AuthorizeSession(ctx, userID)
	if err != nil {
		return fmt.Errorf("gameplayAccess: %w", err)
	}
	return nil
}
