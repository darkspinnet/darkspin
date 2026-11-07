package game

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// PartDetailConverter resolves active mission identities before the HTTP
// adapter's legacy account-item namespace normalization can discard them.
type PartDetailConverter interface {
	ConvertPartsToDetail(context.Context, *sporenet.User, []uint64) error
}

func (e *API) convertPartsToDetail(ctx context.Context, user *sporenet.User, partIDs []uint64) error {
	if e.partDetailConverter != nil {
		err := e.partDetailConverter.ConvertPartsToDetail(ctx, user, partIDs)
		if err != nil {
			return fmt.Errorf("detailConvert: %w", err)
		}
		return nil
	}
	canonicalizePartStatusIDs(partIDs)
	changedParts, err := e.userManager.ConvertPartsToDetail(ctx, user, partIDs)
	if err != nil {
		return fmt.Errorf("detailOwned: %w", err)
	}
	// The legacy response only acknowledges the operation; the account feature
	// owns its changed records and durable save.
	if len(changedParts) != len(partIDs) {
		return fmt.Errorf("detailCount: converted %d of %d parts", len(changedParts), len(partIDs))
	}
	return nil
}
