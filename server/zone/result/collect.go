package result

import (
	"context"
	"errors"
	"fmt"
)

const maximumCashOutBonusAttempts = 3

var ErrCashOutBonusChanged = errors.New("campaign cashout bonus changed")

type RewardSubject struct {
	ClassType    string
	ElementType  string
	AccountLevel uint32
	Alternates   []RewardSubject
}

type RewardPart struct {
	RigblockAssetID          uint16
	SuffixAssetID            uint16
	PrefixAssetID            uint16
	PrefixSecondaryAssetID   uint16
	RigblockAssetHash        uint32
	SuffixAssetHash          uint32
	PrefixAssetHash          uint32
	PrefixSecondaryAssetHash uint32
	Level                    uint16
	Rarity                   uint8
	Cost                     uint32
}

type Reward struct {
	ResultID                 uint64
	CompletedIndex           uint32
	ExpectedCashoutBonusTime uint32
	Parts                    []RewardPart
	IsDailyBonusGranted      bool
}

type CashOutBonusStatus struct {
	CashoutBonusTime uint32
	IsAvailable      bool
}

type PartGenerator interface {
	GenerateCampaignPart(RewardSubject, uint32, uint32, RewardTier) (RewardPart, error)
}

type RewardStore interface {
	CashOutBonus(context.Context, int64) (CashOutBonusStatus, error)
	CommitCampaignReward(context.Context, int64, Reward) (CashOutReceipt, error)
}

type CollectCommand struct {
	UserID  int64
	Subject RewardSubject
}

type CollectResult struct {
	Receipt     CashOutReceipt
	RewardLevel uint32
	IsReplay    bool
}

// CollectCashOut serializes reward generation and durable persistence through
// the participant's result session. Protocol encoding remains an adapter
// concern and can safely retry from the committed receipt.
func CollectCashOut(
	ctx context.Context,
	session *Session,
	generator PartGenerator,
	store RewardStore,
	command CollectCommand,
) (CollectResult, error) {
	if ctx == nil {
		return CollectResult{}, errors.New("campaign cashout context unavailable")
	}
	err := ctx.Err()
	if err != nil {
		return CollectResult{}, fmt.Errorf("cashOutContext: %w", err)
	}
	if session == nil {
		return CollectResult{}, errors.New("campaign cashout session unavailable")
	}
	snapshot := session.Snapshot()
	if snapshot.CashOutReceipt.IsCommitted {
		return CollectResult{
			Receipt: snapshot.CashOutReceipt, IsReplay: true,
		}, nil
	}
	if generator == nil || store == nil {
		return CollectResult{}, errors.New("campaign cashout dependency unavailable")
	}
	if command.UserID <= 0 || uint64(command.UserID) != snapshot.UserID ||
		command.Subject.ClassType == "" || command.Subject.ElementType == "" ||
		command.Subject.AccountLevel == 0 {
		return CollectResult{}, errors.New("campaign cashout command invalid")
	}
	snapshot, isReserved := session.ReserveCashOutCommit()
	if !isReserved {
		return CollectResult{}, errors.New("campaign cashout commit busy")
	}
	defer session.RollbackCashOutCommit(snapshot.ResultID)
	rewardCount := RewardCount(snapshot.PlanetsCompleted)
	rewardLevel := RewardLevel(snapshot.Difficulty, snapshot.PlanetsCompleted)
	for attempt := 0; attempt < maximumCashOutBonusAttempts; attempt++ {
		bonusStatus, statusErr := store.CashOutBonus(ctx, command.UserID)
		if statusErr != nil {
			return CollectResult{}, fmt.Errorf("cashOutBonus: %w", statusErr)
		}
		rarityBands := CashOutRarityBands(
			snapshot.CompletedIndex, snapshot.PlanetsCompleted,
			snapshot.MedalCounts[0], bonusStatus.IsAvailable,
		)
		rewardParts := make([]RewardPart, 0, rewardCount)
		for rewardIndex := 0; rewardIndex < rewardCount; rewardIndex++ {
			roll := CashOutRewardRoll(snapshot.ResultID, rewardIndex)
			tier := CashOutRewardTier(roll, rarityBands)
			if tier == RewardTierUnknown {
				return CollectResult{}, fmt.Errorf("cashOutRoll[%d]: invalid", rewardIndex)
			}
			part, generateErr := generator.GenerateCampaignPart(
				command.Subject,
				rewardLevel,
				uint32(snapshot.ResultID)+uint32(rewardIndex)*0x103,
				tier,
			)
			if generateErr != nil {
				return CollectResult{}, fmt.Errorf(
					"cashOutPart[%d]: %w", rewardIndex, generateErr,
				)
			}
			rewardParts = append(rewardParts, part)
		}
		receipt, commitErr := store.CommitCampaignReward(
			ctx,
			command.UserID,
			Reward{
				ResultID: snapshot.ResultID, CompletedIndex: snapshot.CompletedIndex,
				ExpectedCashoutBonusTime: bonusStatus.CashoutBonusTime,
				Parts:                    rewardParts,
				IsDailyBonusGranted:      bonusStatus.IsAvailable,
			},
		)
		if errors.Is(commitErr, ErrCashOutBonusChanged) {
			continue
		}
		if commitErr != nil {
			return CollectResult{}, fmt.Errorf("cashOutStore: %w", commitErr)
		}
		if receipt.ResultID != snapshot.ResultID || !receipt.IsCommitted ||
			len(receipt.Parts) == 0 {
			return CollectResult{}, errors.New("campaign cashout receipt invalid")
		}
		if !session.CommitCashOut(receipt) {
			return CollectResult{}, errors.New("campaign cashout receipt rejected")
		}
		return CollectResult{Receipt: receipt, RewardLevel: rewardLevel}, nil
	}
	return CollectResult{}, ErrCashOutBonusChanged
}
