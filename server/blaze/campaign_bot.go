package blaze

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/pvp"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func isLocalCampaignBotFallback(request *Request, userID int64, criteria pvp.MatchmakingCriteria) bool {
	return criteria.GameType == pvp.CampaignGameType && !criteria.IsRanked &&
		criteria.SelectedDifficulty != "" && isLocalSoloMatchmakingPlayer(request, userID)
}

// Campaign bots are companions owned by the sole human, so reward and result
// membership remains the real player's ordinary campaign membership.
func formLocalCampaignBotGame(
	gameManager *game.Manager, owner *sporenet.User, session pvp.MatchmakingSession,
) (*game.Instance, error) {
	if gameManager == nil || owner == nil || owner.CurrentGameID() != 0 {
		return nil, errors.New("campaign bot owner invalid")
	}
	selection, err := strconv.ParseUint(session.Criteria.SelectedDifficulty, 10, 32)
	if err != nil || selection == 0 || uint32(selection) > owner.View().Account.ChainProgression+1 {
		return nil, errors.New("campaign bot selection invalid")
	}
	level, isFound := gameManager.ChainLevelForSelection(uint32(selection))
	if !isFound {
		return nil, errors.New("campaign bot level unavailable")
	}
	instance := gameManager.Create()
	instance.Info.Mode = game.ModeChain
	instance.Info.Level = level
	instance.Info.Name = "Darkspin Campaign"
	instance.Info.Attributes["GameType"] = strconv.FormatUint(uint64(pvp.CampaignGameType), 10)
	instance.Info.Attributes["Ranked"] = "0"
	instance.Info.Attributes["SelectedDifficulty"] = session.Criteria.SelectedDifficulty
	instance.Info.Attributes["ExpectedPlayerCount"] = "1"
	instance.Info.Attributes["TeamSize"] = "0"
	instance.Info.MaxPlayers = pvp.CampaignExpectedPlayerCount
	instance.Info.ExpectedPlayerCount = 1
	instance.Info.Capacity = []uint32{uint32(pvp.CampaignExpectedPlayerCount)}
	warpLevel, isWarpFound := gameManager.CampaignWarp(owner.Account.ID)
	if isWarpFound {
		instance.Info.Level = warpLevel
		instance.Info.IsWarped = true
	}
	instance.ReserveAdmission()
	err = instance.AddPlayerAtSlot(owner, 0)
	if err != nil {
		gameManager.Remove(instance.ID)
		return nil, fmt.Errorf("campaignBotOwner: %w", err)
	}
	bot, err := instance.AddBot(game.BotRequest{
		OwnerUserID: uint64(owner.Account.ID), Slot: 1, Team: 1,
		DisplayName: uniqueBotDisplayName("AI Companion", map[string]bool{strings.ToLower(owner.DisplayName): true}),
		AvatarID:    randomBotAvatarID(instance, owner.Account.AvatarID),
	})
	if err != nil {
		gameManager.Remove(instance.ID)
		return nil, fmt.Errorf("campaignBotAdd: %w", err)
	}
	if !bot.IsServerBot {
		gameManager.Remove(instance.ID)
		return nil, errors.New("campaign bot identity unavailable")
	}
	err = gameManager.SelectCampaignDestination(instance)
	if err != nil {
		gameManager.Remove(instance.ID)
		return nil, fmt.Errorf("campaignBotDestination: %w", err)
	}
	return instance, nil
}
