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
	maximumCashOutRewards = 4
	// Native sentinel five bypasses the continuation limit; it does not
	// establish the server's separate five-planet run policy.
	bypassContinuationLimit = int32(5)
)

type CashOutReceiptRequest struct {
	PlayerSlot         uint16
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
	packet, err := VoteWithCountdown(snapshot, float32(zoneresult.VoteDuration.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("voteCountdown: %w", err)
	}
	return packet, nil
}

func VoteWithCountdown(snapshot zoneresult.Snapshot, seconds float32) ([]byte, error) {
	if seconds <= 0 || math.IsNaN(float64(seconds)) || math.IsInf(float64(seconds), 0) {
		return nil, errors.New("vote countdown invalid")
	}
	if snapshot.ResultID == 0 || snapshot.Level == "" ||
		snapshot.CompletedIndex == 0 || snapshot.NextLevel == "" ||
		snapshot.Phase != zoneresult.ChainVoting ||
		snapshot.EnemyNouns == [6]uint32{} ||
		snapshot.PlanetsCompleted == 0 {
		return nil, errors.New("vote snapshot invalid")
	}
	nextDifficulty := snapshot.CompletedIndex + 1
	continuationLimit := bypassContinuationLimit
	if snapshot.IsTerminal {
		// The client prices completed loot from NextDifficulty-1. Stop
		// continuation through its independent limit, preserving that preview.
		continuationLimit = 0
	}
	presentation := zonepreview.CampaignPresentation(
		snapshot.CompletedIndex, true,
	)
	// Build 103 sub_448F70 plays record+0x45 when progression equals
	// record+4 minus one. Narration therefore introduces the offered level;
	// using the completed level repeats its introduction one mission late.
	nextPresentation := zonepreview.CampaignPresentation(nextDifficulty, false)
	currentVoice := presentation.CurrentVoice
	if currentVoice == 0 {
		currentVoice = presentation.CurrentMovie
	}
	message := raknet.ChainVoteMessage{
		// Build 103 labels these two wire slots opposite their recovered field
		// names: the current card reads NextLevel and the continue card reads
		// CurrentLevel.
		CurrentLevel:       util.HashID(snapshot.NextLevel + ".Level"),
		NextDifficulty:     nextDifficulty,
		TimeRemaining:      seconds,
		PlanetsRepresented: snapshot.PlanetsCompleted,
		EnemyNouns:         snapshot.EnemyNouns,
		FirstPresentation: [3]uint32{
			0,
			presentation.CurrentMovie,
			currentVoice,
		},
		FirstVoice:        nextPresentation.CurrentVoice,
		ContinuationLimit: continuationLimit,
		NextPresentation: [3]uint32{
			// Build 103 sub_449090 reads record+0xd9 as its preferred
			// interlevel camera; the following fields cannot start it.
			presentation.NextMovie,
			presentation.NextMovie,
			presentation.NextMovie,
		},
		NextLevel: util.HashID(snapshot.Level + ".Level"),
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
		int(req.PlayerSlot) >= len(req.MedalCounts) ||
		receipt.ChainProgression == 0 || len(receipt.Parts) == 0 ||
		len(receipt.Parts) > maximumCashOutRewards ||
		receipt.AccountExperience > math.MaxInt32 ||
		req.StartingExperience > math.MaxInt32 || req.FinalExperience > math.MaxInt32 ||
		req.FinalExperience < req.StartingExperience ||
		req.CompletedIndex == 0 || req.PlanetsCompleted == 0 {
		return nil, errors.New("cash out receipt invalid")
	}
	message := raknet.ChainCashOutMessage{
		PlanetsCompleted: int32(req.PlanetsCompleted),
	}
	// Native cCashOutUI retains all four participant rows in slot order.
	// A recipient's personal receipt must populate that participant's row.
	message.StartingExperiences[req.PlayerSlot] = int32(req.StartingExperience)
	message.FinalExperiences[req.PlayerSlot] = int32(req.FinalExperience)
	for playerIndex, medalCount := range req.MedalCounts {
		if medalCount.Gold > math.MaxInt32 || medalCount.Silver > math.MaxInt32 ||
			medalCount.Bronze > math.MaxInt32 {
			return nil, fmt.Errorf("cashOutMedals[%d]: overflow", playerIndex)
		}
		message.GoldMedalCounts[playerIndex] = int32(medalCount.Gold)
		message.SilverMedalCounts[playerIndex] = int32(medalCount.Silver)
		message.BronzeMedalCounts[playerIndex] = int32(medalCount.Bronze)
		isDailyBonusGranted := playerIndex == int(req.PlayerSlot) && receipt.IsDailyBonusGranted
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
		message.Rewards[req.PlayerSlot][index] = raknet.ChainCashOutReward{
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
