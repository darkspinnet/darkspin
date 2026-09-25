package sporenet

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const overdriveCampaignIndex uint32 = 5

const campaignCashOutBonusCooldown = 22 * time.Hour

var ErrCampaignCashOutBonusChanged = errors.New("campaign cashout bonus changed")

// CampaignExperience is one durable successful-zone XP receipt. ResultID is
// scoped by user and makes replay after a process restart an idempotent read.
type CampaignExperience struct {
	ResultID     uint64
	Amount       uint32
	CumulativeXP uint32
	Level        uint32
}

// CampaignCashOutReward is the bounded server-selected reward ledger committed
// after a completed chain is collected. ResultID is its durable idempotency
// identity.
type CampaignCashOutReward struct {
	ResultID                 uint64
	CompletedIndex           uint32
	ExpectedCashoutBonusTime uint32
	Parts                    []Part
	IsDailyBonusGranted      bool
}

// CampaignCashOutBonus is the current durable eligibility input for one
// cash-out transaction. CashoutBonusTime is the profile-visible last-claim
// timestamp and also guards generation against a concurrent claim.
type CampaignCashOutBonus struct {
	CashoutBonusTime uint32
	IsAvailable      bool
}

// CampaignCashOutReceipt is the immutable durable result used to construct
// repeatable cash-out presentation.
type CampaignCashOutReceipt struct {
	ResultID            uint64
	ChainProgression    uint32
	AccountExperience   uint32
	Parts               []Part
	IsDailyBonusGranted bool
	IsCommitted         bool
}

// CampaignCashOutBonusStatus returns whether the next successful cash-out may
// claim the profile's Daily Bonus. The build-103 profile counts down 79,200
// seconds from CashoutBonusTime, so a zero timestamp is immediately eligible.
func (m *UserManager) CampaignCashOutBonusStatus(
	ctx context.Context, userID int64,
) (CampaignCashOutBonus, error) {
	if ctx == nil {
		return CampaignCashOutBonus{}, errors.New("campaign cashout bonus context unavailable")
	}
	err := ctx.Err()
	if err != nil {
		return CampaignCashOutBonus{}, fmt.Errorf("cashOutBonusContext: %w", err)
	}
	user := m.UserByID(userID)
	if user == nil {
		return CampaignCashOutBonus{}, fmt.Errorf("bonusUser: %w", ErrInvalidUser)
	}
	user.mu.RLock()
	cashoutBonusTime := user.Account.CashoutBonusTime
	user.mu.RUnlock()
	return CampaignCashOutBonus{
		CashoutBonusTime: cashoutBonusTime,
		IsAvailable:      isCampaignCashOutBonusAvailable(cashoutBonusTime, time.Now()),
	}, nil
}

// UnlockOverdrive durably records the campaign Overdrive entitlement. The
// operation is idempotent because the 2-1 checkpoint can be revisited.
func (m *UserManager) UnlockOverdrive(ctx context.Context, userID int64) (bool, error) {
	user := m.UserByID(userID)
	if user == nil {
		return false, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	if user.Account.IsOverdriveUnlocked {
		user.mu.Unlock()
		return false, nil
	}
	previousAccount := user.Account
	user.Account.IsOverdriveUnlocked = true
	user.mu.Unlock()
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAccount(previousAccount)
		return false, fmt.Errorf("overdriveSave: %w", err)
	}
	return true, nil
}

// GrantCampaignExperience commits one zone-completion XP award exactly once.
// A repeated result identity returns the original cumulative receipt, including
// after the zone and server process have been recreated from a checkpoint.
func (m *UserManager) GrantCampaignExperience(
	ctx context.Context, userID int64, resultID uint64, amount uint32,
) (TutorialExperience, error) {
	if resultID == 0 {
		return TutorialExperience{}, errors.New("campaign experience result invalid")
	}
	user := m.UserByID(userID)
	if user == nil {
		return TutorialExperience{}, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	for _, receipt := range user.CampaignExperiences {
		if receipt.ResultID != resultID {
			continue
		}
		if receipt.Amount != amount || receipt.Level == 0 {
			user.mu.Unlock()
			return TutorialExperience{}, errors.New("campaign experience receipt conflict")
		}
		experience := TutorialExperience{
			CumulativeXP: receipt.CumulativeXP, Level: receipt.Level,
		}
		user.mu.Unlock()
		return experience, nil
	}
	previousAccount := user.Account
	previousReceipts := append([]CampaignExperience(nil), user.CampaignExperiences...)
	if amount > ^uint32(0)-user.Account.XP {
		user.mu.Unlock()
		return TutorialExperience{}, errors.New("campaign experience overflow")
	}
	user.Account.XP += amount
	user.Account.Level = accountLevelForExperience(user.Account.XP)
	user.Account.CreatureRewards += heroRewardEntitlementCount(user.Account.Level) -
		heroRewardEntitlementCount(previousAccount.Level)
	experience := TutorialExperience{
		CumulativeXP: user.Account.XP, Level: user.Account.Level,
	}
	user.CampaignExperiences = append(user.CampaignExperiences, CampaignExperience{
		ResultID: resultID, Amount: amount,
		CumulativeXP: experience.CumulativeXP, Level: experience.Level,
	})
	user.mu.Unlock()
	eventAppend := user.appendEvents(
		levelMilestoneEvents(previousAccount.Level, experience.Level)...,
	)
	previousEvents := eventAppend.previousEvents
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Account = previousAccount
		user.CampaignExperiences = previousReceipts
		user.mu.Unlock()
		user.restoreEvents(previousEvents)
		return TutorialExperience{}, fmt.Errorf("campaignExperienceSave: %w", err)
	}
	return experience, nil
}

// AdvanceChainProgression durably records the highest completed campaign
// index. Replays and completion of an earlier index are idempotent no-ops.
func (m *UserManager) AdvanceChainProgression(
	ctx context.Context, userID int64, completedIndex uint32,
) (uint32, bool, error) {
	if completedIndex == 0 {
		return 0, false, errors.New("chain progression index zero")
	}
	user := m.UserByID(userID)
	if user == nil {
		return 0, false, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previousAccount := user.Account
	isAdvanced := user.Account.ChainProgression < completedIndex
	if isAdvanced {
		user.Account.ChainProgression = completedIndex
	}
	isOverdriveAdvanced := completedIndex >= overdriveCampaignIndex &&
		!user.Account.IsOverdriveUnlocked
	if isOverdriveAdvanced {
		user.Account.IsOverdriveUnlocked = true
	}
	progression := user.Account.ChainProgression
	user.mu.Unlock()
	eventAppend := user.appendEvents(campaignCompletedEvent(completedIndex))
	if !isAdvanced && !isOverdriveAdvanced && !eventAppend.isChanged {
		return progression, false, nil
	}
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAccount(previousAccount)
		user.restoreEvents(eventAppend.previousEvents)
		return 0, false, fmt.Errorf("chainProgressionSave: %w", err)
	}
	return progression, isAdvanced, nil
}

// CommitCampaignCashOut atomically persists chain progression, a bounded
// reward ledger, and the result idempotency event through one repository save.
// A replay returns every previously committed item without another write.
func (m *UserManager) CommitCampaignCashOut(
	ctx context.Context, userID int64, reward CampaignCashOutReward,
) (CampaignCashOutReceipt, error) {
	if reward.ResultID == 0 || reward.CompletedIndex == 0 ||
		len(reward.Parts) == 0 || len(reward.Parts) > 4 {
		return CampaignCashOutReceipt{}, errors.New("campaign cashout reward invalid")
	}
	for _, part := range reward.Parts {
		if part.RigblockAssetID == 0 {
			return CampaignCashOutReceipt{}, errors.New("campaign cashout reward invalid")
		}
	}
	user := m.UserByID(userID)
	if user == nil {
		return CampaignCashOutReceipt{}, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	keyPrefix := fmt.Sprintf("campaign-cashout:%d:", reward.ResultID)
	user.mu.Lock()
	for _, event := range user.Events {
		if !strings.HasPrefix(event.Key, keyPrefix) {
			continue
		}
		partIDs, isDailyBonusGranted, parseErr := campaignCashOutEvent(
			strings.TrimPrefix(event.Key, keyPrefix),
		)
		if parseErr != nil {
			user.mu.Unlock()
			return CampaignCashOutReceipt{}, fmt.Errorf("campaignCashOutKey: %w", parseErr)
		}
		parts := make([]Part, 0, len(partIDs))
		for _, partID := range partIDs {
			isPartFound := false
			for _, part := range user.Parts {
				if part.ID != partID {
					continue
				}
				parts = append(parts, part)
				isPartFound = true
				break
			}
			if !isPartFound {
				user.mu.Unlock()
				return CampaignCashOutReceipt{}, errors.New("campaign cashout reward missing")
			}
		}
		receipt := CampaignCashOutReceipt{
			ResultID: reward.ResultID, ChainProgression: user.Account.ChainProgression,
			AccountExperience: user.Account.XP, Parts: parts, IsCommitted: true,
			IsDailyBonusGranted: isDailyBonusGranted,
		}
		user.mu.Unlock()
		return receipt, nil
	}
	now := time.Now()
	isDailyBonusAvailable := isCampaignCashOutBonusAvailable(
		user.Account.CashoutBonusTime, now,
	)
	if reward.ExpectedCashoutBonusTime != user.Account.CashoutBonusTime ||
		reward.IsDailyBonusGranted != isDailyBonusAvailable {
		user.mu.Unlock()
		return CampaignCashOutReceipt{}, ErrCampaignCashOutBonusChanged
	}
	if reward.IsDailyBonusGranted &&
		(now.Unix() <= 0 || now.Unix() > int64(^uint32(0))) {
		user.mu.Unlock()
		return CampaignCashOutReceipt{}, errors.New("campaign cashout bonus time invalid")
	}
	capacity := user.Account.UnlockInventoryIdentify
	ownedCount := uint64(0)
	for index := range user.Parts {
		if user.Parts[index].OccupiesInventorySlot() {
			ownedCount++
		}
	}
	if capacity == 0 || ownedCount+uint64(len(reward.Parts)) > uint64(capacity) {
		user.mu.Unlock()
		return CampaignCashOutReceipt{}, ErrInventoryFull
	}
	previousAccount := user.Account
	previousParts := append([]Part(nil), user.Parts...)
	previousEvents := append([]UserEvent(nil), user.Events...)
	nextPartID := uint64(1)
	for _, existing := range user.Parts {
		if existing.ID >= nextPartID {
			if existing.ID >= uint64(^uint32(0)) {
				user.mu.Unlock()
				return CampaignCashOutReceipt{}, ErrPartExists
			}
			nextPartID = existing.ID + 1
		}
	}
	if userID <= 0 || uint64(userID) > uint64(^uint32(0)>>1) ||
		nextPartID+uint64(len(reward.Parts))-1 > uint64(^uint32(0)) {
		user.mu.Unlock()
		return CampaignCashOutReceipt{}, ErrPartExists
	}
	parts := make([]Part, 0, len(reward.Parts))
	partIDs := make([]string, 0, len(reward.Parts))
	for index, rewardPart := range reward.Parts {
		part := rewardPart
		part.Normalize()
		part.ID = nextPartID + uint64(index)
		part.ReferenceID = uint64(userID)<<32 | part.ID
		if part.CreationDate == 0 {
			part.CreationDate = uint64(now.Unix())
		}
		parts = append(parts, part)
		partIDs = append(partIDs, strconv.FormatUint(part.ID, 10))
	}
	user.Parts = append(user.Parts, parts...)
	user.Account.ChainProgression = max(user.Account.ChainProgression, reward.CompletedIndex)
	if reward.IsDailyBonusGranted {
		user.Account.CashoutBonusTime = uint32(now.Unix())
	}
	if reward.CompletedIndex >= overdriveCampaignIndex {
		user.Account.IsOverdriveUnlocked = true
	}
	user.Events = append(user.Events, UserEvent{
		Key:       campaignCashOutEventKey(keyPrefix, partIDs, reward.IsDailyBonusGranted),
		MessageID: UserEventMessageMilestone,
		Metadata: fmt.Sprintf("Collected %d campaign rewards after mission %d.",
			len(parts), reward.CompletedIndex),
		OccurredAt: now.Unix(),
	})
	receipt := CampaignCashOutReceipt{
		ResultID: reward.ResultID, ChainProgression: user.Account.ChainProgression,
		AccountExperience: user.Account.XP, Parts: parts, IsCommitted: true,
		IsDailyBonusGranted: reward.IsDailyBonusGranted,
	}
	user.mu.Unlock()
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Account = previousAccount
		user.Parts = previousParts
		user.Events = previousEvents
		user.mu.Unlock()
		return CampaignCashOutReceipt{}, fmt.Errorf("campaignCashOutSave: %w", err)
	}
	return receipt, nil
}

func isCampaignCashOutBonusAvailable(cashoutBonusTime uint32, now time.Time) bool {
	if cashoutBonusTime == 0 {
		return true
	}
	currentTime := now.Unix()
	if currentTime <= 0 || currentTime < int64(cashoutBonusTime) {
		return false
	}
	return currentTime-int64(cashoutBonusTime) >=
		int64(campaignCashOutBonusCooldown/time.Second)
}

func campaignCashOutEventKey(
	keyPrefix string, partIDs []string, isDailyBonusGranted bool,
) string {
	dailyBonusFlag := "0"
	if isDailyBonusGranted {
		dailyBonusFlag = "1"
	}
	return fmt.Sprintf(
		"%sdaily=%s;parts=%s", keyPrefix, dailyBonusFlag, strings.Join(partIDs, ","),
	)
}

func campaignCashOutEvent(encodedEvent string) ([]uint64, bool, error) {
	if !strings.HasPrefix(encodedEvent, "daily=") {
		partIDs, err := campaignCashOutPartIDs(encodedEvent)
		if err != nil {
			return nil, false, fmt.Errorf("legacyParts: %w", err)
		}
		return partIDs, false, nil
	}
	dailyBonusToken, encodedPartIDs, isFound := strings.Cut(encodedEvent, ";parts=")
	if !isFound {
		return nil, false, errors.New("cashout event parts missing")
	}
	isDailyBonusGranted := false
	switch dailyBonusToken {
	case "daily=0":
	case "daily=1":
		isDailyBonusGranted = true
	default:
		return nil, false, errors.New("cashout event bonus invalid")
	}
	partIDs, err := campaignCashOutPartIDs(encodedPartIDs)
	if err != nil {
		return nil, false, fmt.Errorf("parts: %w", err)
	}
	return partIDs, isDailyBonusGranted, nil
}

func campaignCashOutPartIDs(encodedPartIDs string) ([]uint64, error) {
	partTokens := strings.Split(encodedPartIDs, ",")
	if len(partTokens) == 0 || len(partTokens) > 4 {
		return nil, errors.New("cashout part IDs invalid")
	}
	partIDs := make([]uint64, 0, len(partTokens))
	for _, partToken := range partTokens {
		partID, err := strconv.ParseUint(partToken, 10, 64)
		if err != nil || partID == 0 {
			return nil, errors.New("cashout part ID invalid")
		}
		partIDs = append(partIDs, partID)
	}
	return partIDs, nil
}
