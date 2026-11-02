package blaze

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/pvp"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Only a single local authenticated player uses the initial bot fallback.
// A remote, ranked, campaign or populated server keeps its ordinary queue.
func isLocalArenaBotFallback(request *Request, userID int64, criteria pvp.MatchmakingCriteria) bool {
	if request == nil || request.Session == nil || request.Session.server == nil ||
		criteria.IsRanked || !pvp.IsArenaCriteria(criteria) {
		return false
	}
	return isLocalSoloMatchmakingPlayer(request, userID)
}

func isLocalSoloMatchmakingPlayer(request *Request, userID int64) bool {
	if request == nil || request.Session == nil || request.Session.server == nil {
		return false
	}
	peerIP := net.ParseIP(sessionRemoteIP(request.Session))
	if peerIP == nil || !peerIP.IsLoopback() {
		return false
	}
	server := request.Session.server
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.sessionsByUser[userID]) != 1 {
		return false
	}
	for candidateID, sessions := range server.sessionsByUser {
		if candidateID != userID && len(sessions) != 0 {
			return false
		}
	}
	return true
}

func formLocalArenaBotGame(
	gameManager *game.Manager, owner *sporenet.User, session pvp.MatchmakingSession,
	templates []*sporenet.TemplateCreature,
) (*game.Instance, error) {
	criteria := session.Criteria
	if gameManager == nil || owner == nil || owner.CurrentGameID() != 0 ||
		criteria.IsRanked || !pvp.IsArenaCriteria(criteria) {
		return nil, errors.New("local bot formation invalid")
	}
	instance := gameManager.Create()
	instance.Info.Mode = game.ModeArena
	instance.Info.Level = game.RandomArenaLevel()
	instance.Info.Name = "Darkspin Arena"
	instance.Info.Attributes["GameType"] = strconv.FormatUint(uint64(criteria.GameType), 10)
	instance.Info.Attributes["Ranked"] = "0"
	instance.Info.Attributes["SelectedDifficulty"] = criteria.SelectedDifficulty
	instance.Info.Attributes["ExpectedPlayerCount"] = strconv.FormatUint(uint64(criteria.ExpectedPlayerCount), 10)
	instance.Info.Attributes["TeamSize"] = strconv.FormatUint(uint64(criteria.TeamSize), 10)
	if session.PartyID != 0 {
		instance.Info.PlaygroupID = strconv.FormatUint(uint64(session.PartyID), 10)
		instance.Info.Attributes["PlaygroupKey"] = instance.Info.PlaygroupID
	}
	instance.Info.MaxPlayers = criteria.ExpectedPlayerCount
	instance.Info.ExpectedPlayerCount = criteria.ExpectedPlayerCount
	instance.Info.TeamCapacity = pvp.ArenaTeamCount
	instance.Info.Capacity = []uint32{uint32(criteria.ExpectedPlayerCount)}
	instance.ReserveAdmission()
	err := instance.AddPlayerAtSlot(owner, 0)
	if err != nil {
		gameManager.Remove(instance.ID)
		return nil, fmt.Errorf("botOwnerAdd: %w", err)
	}
	if !instance.AssignPlayerTeam(owner.Account.ID, 1) {
		gameManager.Remove(instance.ID)
		return nil, errors.New("local bot owner team unavailable")
	}
	choices := game.AvailableBotSquads(templates, owner.Account.Level)
	usedNames := make(map[string]bool)
	occupiedNames := map[string]bool{strings.ToLower(owner.DisplayName): true}
	for slot := uint16(1); slot < criteria.ExpectedPlayerCount; slot++ {
		team := slot/criteria.TeamSize + 1
		role := "Opponent"
		if team == 1 {
			role = "Ally"
		}
		name := fmt.Sprintf("AI %s %d", role, slot)
		roster := game.GameplayRoster{}
		eligible := make([]game.BotSquadChoice, 0, len(choices))
		for _, choice := range choices {
			if !usedNames[strings.ToLower(choice.DisplayName)] {
				eligible = append(eligible, choice)
			}
		}
		if len(eligible) != 0 {
			choice := eligible[rand.IntN(len(eligible))]
			selected, isComplete := game.BotSquadTemplates(templates, choice, owner.Account.Level)
			if isComplete {
				var rosterErr error
				roster, rosterErr = game.BotSquadRoster(selected, owner.Account.Level, float32(owner.Account.XP))
				if rosterErr != nil {
					gameManager.Remove(instance.ID)
					return nil, fmt.Errorf("botSquad[%d]: %w", slot, rosterErr)
				}
				name = choice.DisplayName
				usedNames[strings.ToLower(name)] = true
			}
		}
		name = uniqueBotDisplayName(name, occupiedNames)
		occupiedNames[strings.ToLower(name)] = true
		bot, botErr := instance.AddBot(game.BotRequest{
			OwnerUserID: uint64(owner.Account.ID), Slot: slot, Team: team,
			DisplayName: name, Roster: roster, AvatarID: randomBotAvatarID(instance, owner.Account.AvatarID),
		})
		if botErr != nil {
			gameManager.Remove(instance.ID)
			return nil, fmt.Errorf("botRoster[%d]: %w", slot, botErr)
		}
		if !bot.IsServerBot {
			gameManager.Remove(instance.ID)
			return nil, errors.New("local bot identity unavailable")
		}
	}
	return instance, nil
}

func uniqueBotDisplayName(name string, occupiedNames map[string]bool) string {
	if !occupiedNames[strings.ToLower(name)] {
		return name
	}
	for suffix := 1; ; suffix++ {
		candidate := fmt.Sprintf("%s%d", name, suffix)
		if !occupiedNames[strings.ToLower(candidate)] {
			return candidate
		}
	}
}
