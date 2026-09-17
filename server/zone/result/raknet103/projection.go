package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonepreview "github.com/darkspinnet/darkspin/server/zone/preview"
	zoneresult "github.com/darkspinnet/darkspin/server/zone/result"
)

const (
	maximumCashOutRewards   = 4
	continueUnlockedOrdinal = 5
)

type CashOutReceiptRequest struct {
	Receipt            zoneresult.CashOutReceipt
	CompletedIndex     uint32
	PlanetsCompleted   uint8
	MedalCounts        [4]zoneresult.MedalCount
	StartingExperience uint32
	FinalExperience    uint32
}

func VotingEntry(gameTime uint64) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.GameStateMessage{
		Data: raknet.GameStateData{
			GameTime: gameTime, State: raknet.GameChainVoting,
			Type: raknet.GameTypeChain,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("votingStateMarshal: %w", err)
	}
	return packet, nil
}

func VotingTransition() ([]byte, error) {
	packet, err := raknet.MarshalApplication(
		raknet.ChainGameTransitionMessage{Subtype: 0},
	)
	if err != nil {
		return nil, fmt.Errorf("votingTransitionMarshal: %w", err)
	}
	return packet, nil
}

func Vote(snapshot zoneresult.Snapshot) ([]byte, error) {
	if snapshot.ResultID == 0 || snapshot.Level == "" ||
		snapshot.CompletedIndex == 0 || snapshot.NextLevel == "" ||
		snapshot.Phase != zoneresult.ChainVoting ||
		snapshot.EnemyNouns == [6]uint32{} ||
		snapshot.PlanetsCompleted == 0 {
		return nil, errors.New("vote snapshot invalid")
	}
	nextDifficulty := snapshot.CompletedIndex + 1
	if snapshot.IsTerminal {
		nextDifficulty = snapshot.CompletedIndex
	}
	presentation := zonepreview.CampaignPresentation(
		snapshot.CompletedIndex, true,
	)
	currentVoice := presentation.CurrentVoice
	if currentVoice == 0 {
		currentVoice = presentation.CurrentMovie
	}
	message := raknet.ChainVoteMessage{
		CurrentLevel:       util.HashID(snapshot.Level + ".Level"),
		NextDifficulty:     nextDifficulty,
		TimeRemaining:      float32(zoneresult.VoteDuration.Seconds()),
		PlanetsRepresented: snapshot.PlanetsCompleted,
		EnemyNouns:         snapshot.EnemyNouns,
		FirstPresentation: [3]uint32{
			0,
			presentation.CurrentMovie,
			currentVoice,
		},
		FirstVoice:    presentation.CurrentVoice,
		UnlockOrdinal: continueUnlockedOrdinal,
		NextPresentation: [3]uint32{
			0,
			presentation.NextMovie,
			presentation.NextMovie,
		},
		NextLevel: util.HashID(snapshot.NextLevel + ".Level"),
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("voteMarshal: %w", err)
	}
	return packet, nil
}

func CashOutRoute() ([]byte, error) {
	packet, err := raknet.MarshalApplication(
		raknet.ChainVoteRouteMessage{IsCashOut: false},
	)
	if err != nil {
		return nil, fmt.Errorf("cashOutRouteMarshal: %w", err)
	}
	return packet, nil
}

func CashOutReceipt(req CashOutReceiptRequest) ([]byte, error) {
	receipt := req.Receipt
	if !receipt.IsCommitted || receipt.ResultID == 0 ||
		receipt.ChainProgression == 0 || len(receipt.Parts) == 0 ||
		len(receipt.Parts) > maximumCashOutRewards ||
		receipt.AccountExperience > math.MaxInt32 ||
		req.StartingExperience > math.MaxInt32 || req.FinalExperience > math.MaxInt32 ||
		req.FinalExperience < req.StartingExperience ||
		req.CompletedIndex == 0 || req.PlanetsCompleted == 0 {
		return nil, errors.New("cash out receipt invalid")
	}
	message := raknet.ChainCashOutMessage{
		PlanetsCompleted:    int32(req.PlanetsCompleted),
		StartingExperiences: [4]int32{int32(req.StartingExperience)},
		FinalExperiences:    [4]int32{int32(req.FinalExperience)},
	}
	for playerIndex, medalCount := range req.MedalCounts {
		if medalCount.Gold > math.MaxInt32 || medalCount.Silver > math.MaxInt32 ||
			medalCount.Bronze > math.MaxInt32 {
			return nil, fmt.Errorf("cashOutMedals[%d]: overflow", playerIndex)
		}
		message.GoldMedalCounts[playerIndex] = int32(medalCount.Gold)
		message.SilverMedalCounts[playerIndex] = int32(medalCount.Silver)
		message.BronzeMedalCounts[playerIndex] = int32(medalCount.Bronze)
		isDailyBonusGranted := playerIndex == 0 && receipt.IsDailyBonusGranted
		rarityBands := zoneresult.CashOutRarityBands(
			req.CompletedIndex, req.PlanetsCompleted, medalCount,
			isDailyBonusGranted,
		)
		message.UpperRarityBoundaries[playerIndex] = int32(rarityBands.RareChance)
		message.LowerRarityBoundaries[playerIndex] = int32(rarityBands.PurifiedChance)
		message.AreCashoutBonusesGiven[playerIndex] = isDailyBonusGranted
	}
	for index, part := range receipt.Parts {
		if part.RigblockAssetHash == 0 {
			return nil, fmt.Errorf("cashOutReceiptPart[%d]: invalid", index)
		}
		roll := int32(zoneresult.CashOutRewardRoll(receipt.ResultID, index))
		message.Rewards[0][index] = raknet.ChainCashOutReward{
			Rigblock: part.RigblockAssetHash, Suffix: part.SuffixAssetHash,
			Prefix:  part.PrefixAssetHash,
			Prefix2: part.PrefixSecondaryAssetHash,
			Level:   int32(part.Level), Rarity: int32(part.Rarity), Roll: roll,
		}
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("cashOutMarshal: %w", err)
	}
	return packet, nil
}
