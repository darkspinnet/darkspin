package sporenet

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	storage "github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/zone/result"
)

// Progression commits one campaign reward transaction to account storage.
type Progression interface {
	CampaignCashOutBonusStatus(
		ctx context.Context,
		userID int64,
	) (storage.CampaignCashOutBonus, error)
	CommitCampaignCashOut(
		ctx context.Context,
		userID int64,
		reward storage.CampaignCashOutReward,
	) (storage.CampaignCashOutReceipt, error)
}

// PartGenerator adapts the gameplay part catalog to result reward generation.
type PartGenerator struct {
	GameplayJoin *game.GameplayJoin
}

func (e PartGenerator) GenerateCampaignPart(
	subject result.RewardSubject,
	level uint32,
	choice uint32,
	tier result.RewardTier,
) (result.RewardPart, error) {
	if e.GameplayJoin == nil {
		return result.RewardPart{}, errors.New("campaign part generator unavailable")
	}
	rarity, err := campaignRewardRarity(tier)
	if err != nil {
		return result.RewardPart{}, fmt.Errorf("partRarity: %w", err)
	}
	candidates := append([]result.RewardSubject{subject}, subject.Alternates...)
	eligible := make([]result.RewardSubject, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ClassType != "" && candidate.ElementType != "" {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return result.RewardPart{}, errors.New("campaign reward subject unavailable")
	}
	subject = eligible[choice%uint32(len(eligible))]
	part, err := e.GameplayJoin.GenerateCampaignRewardPart(
		game.GameplayCreature{
			ClassType:   subject.ClassType,
			ElementType: subject.ElementType,
		},
		level,
		subject.AccountLevel,
		choice,
		rarity,
	)
	if err != nil {
		return result.RewardPart{}, fmt.Errorf("partGenerate: %w", err)
	}
	return rewardPart(part), nil
}

func campaignRewardRarity(tier result.RewardTier) (storage.PartRarity, error) {
	switch tier {
	case result.RewardTierSpecial:
		return storage.PartUnique, nil
	case result.RewardTierRarified:
		return storage.PartRareUnique, nil
	case result.RewardTierPurified:
		return storage.PartEpicUnique, nil
	default:
		return storage.PartBasic, errors.New("campaign reward tier invalid")
	}
}

// RewardStore adapts account progression persistence to the result feature.
type RewardStore struct {
	Progression Progression
}

func (e RewardStore) CashOutBonus(
	ctx context.Context, userID int64,
) (result.CashOutBonusStatus, error) {
	if e.Progression == nil {
		return result.CashOutBonusStatus{}, errors.New("campaign reward store unavailable")
	}
	bonus, err := e.Progression.CampaignCashOutBonusStatus(ctx, userID)
	if err != nil {
		return result.CashOutBonusStatus{}, fmt.Errorf("bonusStatus: %w", err)
	}
	return result.CashOutBonusStatus{
		CashoutBonusTime: bonus.CashoutBonusTime,
		IsAvailable:      bonus.IsAvailable,
	}, nil
}

func (e RewardStore) CommitCampaignReward(
	ctx context.Context,
	userID int64,
	req result.Reward,
) (result.CashOutReceipt, error) {
	if e.Progression == nil {
		return result.CashOutReceipt{}, errors.New("campaign reward store unavailable")
	}
	parts := make([]storage.Part, 0, len(req.Parts))
	for _, part := range req.Parts {
		parts = append(parts, storedPart(part))
	}
	receipt, err := e.Progression.CommitCampaignCashOut(
		ctx,
		userID,
		storage.CampaignCashOutReward{
			ResultID:                 req.ResultID,
			CompletedIndex:           req.CompletedIndex,
			ExpectedCashoutBonusTime: req.ExpectedCashoutBonusTime,
			Parts:                    parts,
			IsDailyBonusGranted:      req.IsDailyBonusGranted,
		},
	)
	if errors.Is(err, storage.ErrCampaignCashOutBonusChanged) {
		return result.CashOutReceipt{}, fmt.Errorf(
			"rewardBonus: %w", result.ErrCashOutBonusChanged,
		)
	}
	if err != nil {
		return result.CashOutReceipt{}, fmt.Errorf("rewardCommit: %w", err)
	}
	return cashOutReceipt(receipt), nil
}

func rewardPart(part storage.Part) result.RewardPart {
	return result.RewardPart{
		RigblockAssetID:          part.RigblockAssetID,
		SuffixAssetID:            part.SuffixAssetID,
		PrefixAssetID:            part.PrefixAssetID,
		PrefixSecondaryAssetID:   part.PrefixSecondaryAssetID,
		RigblockAssetHash:        part.RigblockAssetHash,
		SuffixAssetHash:          part.SuffixAssetHash,
		PrefixAssetHash:          part.PrefixAssetHash,
		PrefixSecondaryAssetHash: part.PrefixSecondaryAssetHash,
		Level:                    part.Level,
		Rarity:                   uint8(part.Rarity),
		Cost:                     part.Cost,
	}
}

func storedPart(part result.RewardPart) storage.Part {
	return storage.Part{
		RigblockAssetID:          part.RigblockAssetID,
		SuffixAssetID:            part.SuffixAssetID,
		PrefixAssetID:            part.PrefixAssetID,
		PrefixSecondaryAssetID:   part.PrefixSecondaryAssetID,
		RigblockAssetHash:        part.RigblockAssetHash,
		SuffixAssetHash:          part.SuffixAssetHash,
		PrefixAssetHash:          part.PrefixAssetHash,
		PrefixSecondaryAssetHash: part.PrefixSecondaryAssetHash,
		Level:                    part.Level,
		Rarity:                   storage.PartRarity(part.Rarity),
		Cost:                     part.Cost,
	}
}

func cashOutReceipt(receipt storage.CampaignCashOutReceipt) result.CashOutReceipt {
	parts := make([]result.CashOutPart, 0, len(receipt.Parts))
	for _, part := range receipt.Parts {
		parts = append(parts, result.CashOutPart{
			RigblockAssetHash:        part.RigblockAssetHash,
			SuffixAssetHash:          part.SuffixAssetHash,
			PrefixAssetHash:          part.PrefixAssetHash,
			PrefixSecondaryAssetHash: part.PrefixSecondaryAssetHash,
			Level:                    part.Level,
			Rarity:                   uint8(part.Rarity),
		})
	}
	return result.CashOutReceipt{
		ResultID:            receipt.ResultID,
		ChainProgression:    receipt.ChainProgression,
		AccountExperience:   receipt.AccountExperience,
		Parts:               parts,
		IsDailyBonusGranted: receipt.IsDailyBonusGranted,
		IsCommitted:         receipt.IsCommitted,
	}
}
