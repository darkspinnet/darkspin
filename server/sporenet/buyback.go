package sporenet

import (
	"context"
	"fmt"
	"time"
)

// The original server's buyback expiry is not present in the client. This is
// a server policy, measured from sale rather than acquisition or last login.
const buybackLifetime = 2 * time.Hour

// ExpireBuyback removes expired sold parts before the inventory baseline is
// published. Legacy sold parts have no reliable sale time and expire once.
func (e *UserManager) ExpireBuyback(ctx context.Context, user *User, now time.Time) error {
	if user == nil {
		return ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previousParts := user.Parts
	previousShop := user.PartShop
	parts := make([]Part, 0, len(previousParts))
	for _, part := range previousParts {
		if part.MarketStatus != PartMarketBuyback || part.BuybackExpiresAt > now.Unix() {
			parts = append(parts, part)
			continue
		}
		// An expired quote's identity must never become a different item.
		nextID := part.ID
		if nextID < ^uint64(0) {
			nextID++
		}
		user.PartShop.NextPartID = max(user.PartShop.NextPartID, nextID)
	}
	if len(parts) == len(previousParts) {
		user.mu.Unlock()
		return nil
	}
	user.Parts = parts
	user.mu.Unlock()
	err := e.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Parts = previousParts
		user.PartShop = previousShop
		user.mu.Unlock()
		return fmt.Errorf("buybackSave: %w", err)
	}
	return nil
}
