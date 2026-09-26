package sporenet

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type MissionLoot struct {
	CompletionID            uint64
	Parts                   []Part
	LimitedEditionPity      LimitedEditionPity
	IsLimitedEditionPending bool
}

// CommitMissionLoot grants all pickups only at successful mission completion.
// The receipt shares the item save, so retries and restored checkpoints cannot
// duplicate the grant, even after the awarded items have been sold.
func (e *UserManager) CommitMissionLoot(ctx context.Context, userID int64, req MissionLoot) error {
	if req.CompletionID == 0 || len(req.Parts) == 0 {
		return errors.New("mission loot request invalid")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("lootContext: %w", err)
	}
	user := e.UserByID(userID)
	if user == nil {
		return fmt.Errorf("lootUser: %w", ErrInvalidUser)
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	key := fmt.Sprintf("mission-loot:%d", req.CompletionID)
	for _, event := range user.Events {
		if event.Key == key {
			user.mu.Unlock()
			return nil
		}
	}
	ownedCount := uint64(0)
	nextPartID := uint64(1)
	for _, part := range user.Parts {
		if part.OccupiesInventorySlot() {
			ownedCount++
		}
		if part.ID >= uint64(^uint32(0)) {
			user.mu.Unlock()
			return fmt.Errorf("lootIdentity: %w", ErrPartExists)
		}
		nextPartID = max(nextPartID, part.ID+1)
	}
	if ownedCount+uint64(len(req.Parts)) > uint64(user.Account.UnlockInventoryIdentify) {
		user.mu.Unlock()
		return fmt.Errorf("lootCapacity: %w", ErrInventoryFull)
	}
	if userID <= 0 || uint64(userID) > uint64(^uint32(0)>>1) ||
		nextPartID+uint64(len(req.Parts))-1 > uint64(^uint32(0)) {
		user.mu.Unlock()
		return fmt.Errorf("lootReference: %w", ErrPartExists)
	}
	now := time.Now().Unix()
	parts := make([]Part, 0, len(req.Parts))
	for index, part := range req.Parts {
		if part.RigblockAssetID == 0 || part.EquippedToCreatureID != 0 || part.MarketStatus != PartMarketOwned {
			user.mu.Unlock()
			return errors.New("mission loot item invalid")
		}
		part.Normalize()
		part.ID = nextPartID + uint64(index)
		part.ReferenceID = uint64(userID)<<32 | part.ID
		part.CreationDate = uint64(now)
		parts = append(parts, part)
	}
	previousParts := append([]Part(nil), user.Parts...)
	previousEvents := append([]UserEvent(nil), user.Events...)
	previousAccount := user.Account
	user.Parts = append(user.Parts, parts...)
	if req.IsLimitedEditionPending {
		user.Account.LimitedEditionMissCount = req.LimitedEditionPity.MissCount
		user.Account.LimitedEditionUsedMask = req.LimitedEditionPity.UsedMask
	}
	user.Events = append(user.Events, UserEvent{
		Key: key, MessageID: UserEventMessageMilestone,
		Metadata: fmt.Sprintf("Recovered %d mission items.", len(parts)), OccurredAt: now,
	})
	user.mu.Unlock()
	err = e.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Parts = previousParts
		user.Events = previousEvents
		user.Account = previousAccount
		user.mu.Unlock()
		return fmt.Errorf("lootSave: %w", err)
	}
	return nil
}
