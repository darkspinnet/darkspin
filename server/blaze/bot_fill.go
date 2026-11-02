package blaze

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/pvp"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// formBotFilledGame reserves real members first, then assigns the remaining
// presentation slots to socketless players. Campaign uses a configurable bot
// count; an Arena match fills every open slot to its requested team size.
func formBotFilledGame(
	gameManager *game.Manager, userDirectory activeUserDirectory,
	sessions []pvp.MatchmakingSession, criteria pvp.MatchmakingCriteria,
	campaignBotSize uint32, isSquadMirroringEnabled bool,
) (*game.Instance, error) {
	if len(sessions) == 0 || len(sessions) >= int(criteria.ExpectedPlayerCount) {
		return nil, errors.New("bot formation member count invalid")
	}
	switch criteria.GameType {
	case pvp.CampaignGameType:
		return formBotFilledCampaign(gameManager, userDirectory, sessions, criteria, campaignBotSize, isSquadMirroringEnabled)
	case pvp.ArenaGameType:
		return formBotFilledArena(gameManager, userDirectory, sessions, criteria)
	default:
		return nil, errors.New("bot formation game type unsupported")
	}
}

func formBotFilledCampaign(
	gameManager *game.Manager, userDirectory activeUserDirectory,
	sessions []pvp.MatchmakingSession, criteria pvp.MatchmakingCriteria, botSize uint32,
	isSquadMirroringEnabled bool,
) (*game.Instance, error) {
	if gameManager == nil || userDirectory == nil || botSize < 1 || botSize > 3 ||
		criteria.IsRanked || criteria.ExpectedPlayerCount != pvp.CampaignExpectedPlayerCount ||
		criteria.TeamSize != pvp.CampaignTeamSize {
		return nil, errors.New("campaign bot formation invalid")
	}
	selection, err := strconv.ParseUint(criteria.SelectedDifficulty, 10, 32)
	if err != nil || selection == 0 {
		return nil, errors.New("campaign bot selection invalid")
	}
	level, isFound := gameManager.ChainLevelForSelection(uint32(selection))
	if !isFound {
		return nil, errors.New("campaign bot level unavailable")
	}
	players := make([]*sporenet.User, 0, len(sessions))
	occupiedNames := make(map[string]bool, len(sessions)+int(botSize))
	warpLevel := ""
	for index, session := range sessions {
		player := userDirectory.UserByID(session.UserID)
		if player == nil || player.CurrentGameID() != 0 ||
			uint32(selection) > player.View().Account.ChainProgression+1 {
			return nil, fmt.Errorf("campaignBotMember[%d]: unavailable", index)
		}
		queuedLevel, isWarpFound := gameManager.CampaignWarp(player.Account.ID)
		if isWarpFound {
			if warpLevel != "" && !strings.EqualFold(warpLevel, queuedLevel) {
				return nil, errors.New("campaign bot warp destinations conflict")
			}
			warpLevel = queuedLevel
		}
		players = append(players, player)
		occupiedNames[strings.ToLower(player.DisplayName)] = true
	}
	instance := gameManager.Create()
	instance.Info.Mode = game.ModeChain
	instance.Info.Level = level
	instance.Info.Name = "Darkspin Campaign"
	instance.Info.Attributes["GameType"] = strconv.FormatUint(uint64(criteria.GameType), 10)
	instance.Info.Attributes["Ranked"] = "0"
	instance.Info.Attributes["SelectedDifficulty"] = criteria.SelectedDifficulty
	instance.Info.Attributes["ExpectedPlayerCount"] = strconv.Itoa(len(players))
	instance.Info.Attributes["TeamSize"] = "0"
	instance.Info.MaxPlayers = pvp.CampaignExpectedPlayerCount
	instance.Info.ExpectedPlayerCount = uint16(len(players))
	instance.Info.Capacity = []uint32{uint32(pvp.CampaignExpectedPlayerCount)}
	if warpLevel != "" {
		instance.Info.Level = warpLevel
		instance.Info.IsWarped = true
	}
	instance.ReserveAdmission()
	for index, player := range players {
		err = instance.AddPlayerAtSlot(player, uint16(index))
		if err != nil {
			gameManager.Remove(instance.ID)
			return nil, fmt.Errorf("campaignBotAddMember[%d]: %w", index, err)
		}
	}
	spareSquadsByPlayer := make([][]sporenet.Squad, len(players))
	if isSquadMirroringEnabled {
		for index, player := range players {
			spareSquadsByPlayer[index] = campaignSpareSquads(player)
		}
	}
	var templates []*sporenet.TemplateCreature
	templateSource, isTemplateAvailable := userDirectory.(interface {
		CreatureTemplates() []*sporenet.TemplateCreature
	})
	if isTemplateAvailable {
		templates = templateSource.CreatureTemplates()
	}
	usedBotSquadNames := make(map[string]bool)
	botCount := min(int(botSize), int(criteria.ExpectedPlayerCount)-len(players))
	for index := 0; index < botCount; index++ {
		slot := uint16(len(players) + index)
		ownerIndex := 0
		if !isSquadMirroringEnabled {
			ownerIndex = index % len(players)
		}
		var sourceSquad sporenet.Squad
		for playerIndex, squads := range spareSquadsByPlayer {
			if len(squads) == 0 {
				continue
			}
			ownerIndex = playerIndex
			sourceSquad = squads[0]
			spareSquadsByPlayer[playerIndex] = squads[1:]
			break
		}
		owner := players[ownerIndex]
		roster := game.GameplayRoster{}
		if !isSquadMirroringEnabled {
			roster, err = randomCampaignBotRoster(templates, owner, usedBotSquadNames)
			if err != nil {
				gameManager.Remove(instance.ID)
				return nil, fmt.Errorf("campaignRandomSquad[%d]: %w", slot, err)
			}
		}
		name := "AI Companion"
		if sourceSquad.ID != 0 {
			name = fmt.Sprintf("AI Squad %d", sourceSquad.Slot)
		} else if botCount > 1 {
			name = fmt.Sprintf("AI Companion %d", index+1)
		}
		name = uniqueBotDisplayName(name, occupiedNames)
		occupiedNames[strings.ToLower(name)] = true
		bot, botErr := instance.AddBot(game.BotRequest{
			OwnerUserID: uint64(owner.Account.ID), Slot: slot, Team: 1,
			DisplayName: name, AvatarID: randomBotAvatarID(instance, owner.Account.AvatarID),
			SourceSquadID: sourceSquad.ID, Roster: roster,
			IsSquadMirroringDisabled: !isSquadMirroringEnabled,
		})
		if botErr != nil {
			gameManager.Remove(instance.ID)
			return nil, fmt.Errorf("campaignBotAdd[%d]: %w", slot, botErr)
		}
		if !bot.IsServerBot {
			gameManager.Remove(instance.ID)
			return nil, errors.New("campaign bot identity unavailable")
		}
	}
	err = gameManager.SelectCampaignDestination(instance)
	if err != nil {
		gameManager.Remove(instance.ID)
		return nil, fmt.Errorf("campaignBotDestination: %w", err)
	}
	return instance, nil
}

// campaignSpareSquads orders unlocked, populated PvE slots after the player's
// deployed squad. GameplayJoin resolves their saved equipment and appearance.
func campaignSpareSquads(player *sporenet.User) []sporenet.Squad {
	if player == nil {
		return nil
	}
	view := player.View()
	ownedCreatureIDs := make(map[uint32]bool, len(view.Creatures))
	for _, creature := range view.Creatures {
		if creature != nil && creature.Template != nil {
			ownedCreatureIDs[creature.ID] = true
		}
	}
	squads := make([]sporenet.Squad, 0, len(view.Squads))
	for _, candidate := range view.Squads {
		if candidate.ID == 0 || candidate.ID == view.Account.DefaultDeckPVEID ||
			candidate.IsLockedFor(view.Account) ||
			(candidate.Category != "" && !strings.EqualFold(candidate.Category, "pve")) {
			continue
		}
		isPopulated := false
		for _, creatureID := range candidate.CreatureIDs {
			if ownedCreatureIDs[creatureID] {
				isPopulated = true
				break
			}
		}
		if isPopulated {
			squads = append(squads, candidate)
		}
	}
	sort.Slice(squads, func(left, right int) bool {
		return squads[left].Slot < squads[right].Slot
	})
	return squads
}

func randomCampaignBotRoster(
	templates []*sporenet.TemplateCreature, owner *sporenet.User,
	usedBotSquadNames map[string]bool,
) (game.GameplayRoster, error) {
	if owner == nil {
		return game.GameplayRoster{}, errors.New("campaign bot owner unavailable")
	}
	choices := game.AvailableBotSquads(templates, owner.Account.Level)
	if len(choices) == 0 {
		return game.GameplayRoster{}, nil
	}
	unusedChoices := make([]game.BotSquadChoice, 0, len(choices))
	for _, choice := range choices {
		if !usedBotSquadNames[strings.ToLower(choice.DisplayName)] {
			unusedChoices = append(unusedChoices, choice)
		}
	}
	if len(unusedChoices) != 0 {
		choices = unusedChoices
	}
	choice := choices[rand.IntN(len(choices))]
	selected, isComplete := game.BotSquadTemplates(templates, choice, owner.Account.Level)
	if !isComplete {
		return game.GameplayRoster{}, nil
	}
	roster, err := game.BotSquadRoster(selected, owner.Account.Level, float32(owner.Account.XP))
	if err != nil {
		return game.GameplayRoster{}, fmt.Errorf("botRoster: %w", err)
	}
	usedBotSquadNames[strings.ToLower(choice.DisplayName)] = true
	return roster, nil
}

type arenaBotMember struct {
	player  *sporenet.User
	partyID uint32
}

func formBotFilledArena(
	gameManager *game.Manager, userDirectory activeUserDirectory,
	sessions []pvp.MatchmakingSession, criteria pvp.MatchmakingCriteria,
) (*game.Instance, error) {
	if gameManager == nil || userDirectory == nil || criteria.IsRanked || !pvp.IsArenaCriteria(criteria) {
		return nil, errors.New("arena bot formation invalid")
	}
	teamMembers := [2][]arenaBotMember{}
	occupiedNames := make(map[string]bool, int(criteria.ExpectedPlayerCount))
	partyID := uint32(0)
	for index, session := range sessions {
		player := userDirectory.UserByID(session.UserID)
		if player == nil || player.CurrentGameID() != 0 {
			return nil, fmt.Errorf("arenaBotMember[%d]: unavailable", index)
		}
		if session.PartyID != 0 && partyID == 0 {
			partyID = session.PartyID
		}
		team := 0
		isPartyPlaced := false
		if session.PartyID != 0 {
			for teamIndex, members := range teamMembers {
				for _, member := range members {
					if member.partyID == session.PartyID {
						team = teamIndex
						isPartyPlaced = true
						break
					}
				}
			}
		}
		if !isPartyPlaced && len(teamMembers[1]) < len(teamMembers[0]) {
			team = 1
		}
		if len(teamMembers[team]) >= int(criteria.TeamSize) {
			return nil, errors.New("arena bot team capacity exceeded")
		}
		teamMembers[team] = append(teamMembers[team], arenaBotMember{player: player, partyID: session.PartyID})
		occupiedNames[strings.ToLower(player.DisplayName)] = true
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
	if partyID != 0 {
		instance.Info.PlaygroupID = strconv.FormatUint(uint64(partyID), 10)
		instance.Info.Attributes["PlaygroupKey"] = instance.Info.PlaygroupID
	}
	instance.Info.MaxPlayers = criteria.ExpectedPlayerCount
	instance.Info.ExpectedPlayerCount = criteria.ExpectedPlayerCount
	instance.Info.TeamCapacity = pvp.ArenaTeamCount
	instance.Info.Capacity = []uint32{uint32(criteria.ExpectedPlayerCount)}
	instance.ReserveAdmission()
	for teamIndex, members := range teamMembers {
		for memberIndex, member := range members {
			slot := uint16(teamIndex)*criteria.TeamSize + uint16(memberIndex)
			err := instance.AddPlayerAtSlot(member.player, slot)
			if err != nil {
				gameManager.Remove(instance.ID)
				return nil, fmt.Errorf("arenaBotAddMember[%d]: %w", slot, err)
			}
			if !instance.AssignPlayerTeam(member.player.Account.ID, uint16(teamIndex+1)) {
				gameManager.Remove(instance.ID)
				return nil, fmt.Errorf("arenaBotMemberTeam[%d]: unavailable", slot)
			}
		}
	}
	var templates []*sporenet.TemplateCreature
	templateSource, isAvailable := userDirectory.(interface {
		CreatureTemplates() []*sporenet.TemplateCreature
	})
	if isAvailable {
		templates = templateSource.CreatureTemplates()
	}
	usedNames := make(map[string]bool)
	for teamIndex, members := range teamMembers {
		for memberIndex := len(members); memberIndex < int(criteria.TeamSize); memberIndex++ {
			slot := uint16(teamIndex)*criteria.TeamSize + uint16(memberIndex)
			owner := teamMembers[0][0].player
			if len(teamMembers[teamIndex]) != 0 {
				owner = teamMembers[teamIndex][0].player
			}
			name := fmt.Sprintf("AI Opponent %d", slot)
			if teamIndex == 0 {
				name = fmt.Sprintf("AI Ally %d", slot)
			}
			roster := game.GameplayRoster{}
			choices := game.AvailableBotSquads(templates, owner.Account.Level)
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
						return nil, fmt.Errorf("arenaBotSquad[%d]: %w", slot, rosterErr)
					}
					name = choice.DisplayName
					usedNames[strings.ToLower(name)] = true
				}
			}
			name = uniqueBotDisplayName(name, occupiedNames)
			occupiedNames[strings.ToLower(name)] = true
			bot, botErr := instance.AddBot(game.BotRequest{
				OwnerUserID: uint64(owner.Account.ID), Slot: slot, Team: uint16(teamIndex + 1),
				DisplayName: name, Roster: roster, AvatarID: randomBotAvatarID(instance, owner.Account.AvatarID),
			})
			if botErr != nil {
				gameManager.Remove(instance.ID)
				return nil, fmt.Errorf("arenaBotAdd[%d]: %w", slot, botErr)
			}
			if !bot.IsServerBot {
				gameManager.Remove(instance.ID)
				return nil, errors.New("arena bot identity unavailable")
			}
		}
	}
	return instance, nil
}
