package game

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Keep synthetic identities exactly representable by the native Flash UI's
// double-precision numbers, including the low bits distinguishing bot slots.
const botUserIDMarker = uint64(1) << 52

// BotRequest describes one server-controlled roster slot. Its owner must be a
// real admitted player; bots never become authenticated or persistent users.
type BotRequest struct {
	OwnerUserID              uint64
	Slot                     uint16
	Team                     uint16
	DisplayName              string
	AvatarID                 uint32
	SourceSquadID            uint32
	IsSquadMirroringDisabled bool
	Roster                   GameplayRoster
	IsAppearanceResolved     bool
}

// Bot retains match-local player presentation independently of active users.
// UserID and SessionID are synthetic identities scoped to this game and slot.
type Bot struct {
	BotRequest
	UserID      uint64
	SessionID   uint32
	IsServerBot bool
}

// BotSquadRoster creates match-local hero instances from installed templates.
// The IDs are replaced with generated appearance identities before publication.
func BotSquadRoster(templates [3]*sporenet.TemplateCreature, level uint32, xp float32) (GameplayRoster, error) {
	roster := GameplayRoster{AvatarLevel: level, AvatarXP: xp}
	for index, template := range templates {
		if template == nil || template.Noun == 0 {
			return GameplayRoster{}, fmt.Errorf("botTemplate[%d]: unavailable", index)
		}
		creature := sporenet.NewCreature(template)
		creature.ID = uint32(index + 1)
		roster.Creatures[index] = gameplayCreature(sporenet.UserView{}, creature, nil)
	}
	return roster, nil
}

// BotAvatar resolves only an exact match-local synthetic identity. Bots never
// need persistent accounts for the client's ordinary portrait HTTP request.
func (e *Manager) BotAvatar(userID uint64) (uint32, bool) {
	if e == nil || userID&botUserIDMarker == 0 {
		return 0, false
	}
	instance := e.Game(uint32(userID >> 16))
	if instance == nil {
		return 0, false
	}
	bot, isFound := instance.Bot(uint16(userID))
	if !isFound || bot.UserID != userID {
		return 0, false
	}
	return bot.AvatarID, true
}

// AddBot reserves a distinct roster slot without changing human readiness,
// ownership, progression, or the active-user authentication directory.
func (e *Instance) AddBot(req BotRequest) (Bot, error) {
	if e == nil || req.OwnerUserID == 0 || req.Slot >= MaxGamePlayers ||
		strings.TrimSpace(req.DisplayName) == "" {
		return Bot{}, errors.New("bot request invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Info.State == StateInGame || e.Info.State == StatePostGame || e.Info.State == StateDestructing {
		return Bot{}, errors.New("bot admission closed")
	}
	if _, isOwnerFound := e.players[int64(req.OwnerUserID)]; !isOwnerFound {
		return Bot{}, errors.New("bot owner unavailable")
	}
	if e.Info.MaxPlayers != 0 && req.Slot >= e.Info.MaxPlayers {
		return Bot{}, errors.New("bot slot out of range")
	}
	if req.Team == 0 || e.Info.Mode == ModeArena &&
		(e.Info.TeamCapacity == 0 || req.Team > e.Info.TeamCapacity) {
		return Bot{}, errors.New("bot team invalid")
	}
	for _, slot := range e.slots {
		if slot == req.Slot {
			return Bot{}, errors.New("bot slot occupied by player")
		}
	}
	if _, isBotFound := e.bots[req.Slot]; isBotFound {
		return Bot{}, errors.New("bot slot occupied")
	}
	bot := Bot{
		BotRequest:  req,
		UserID:      botUserIDMarker | uint64(e.ID)<<16 | uint64(req.Slot),
		SessionID:   uint32(0xf0000000) | (e.ID&0x03ffffff)<<2 | uint32(req.Slot),
		IsServerBot: true,
	}
	if e.bots == nil {
		e.bots = make(map[uint16]Bot)
	}
	e.bots[req.Slot] = bot
	return bot, nil
}

// Bots returns detached descriptors in their stable player-slot order.
func (e *Instance) Bots() []Bot {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	bots := make([]Bot, 0, len(e.bots))
	for _, bot := range e.bots {
		bots = append(bots, bot)
	}
	e.mu.RUnlock()
	sort.Slice(bots, func(first, second int) bool {
		return bots[first].Slot < bots[second].Slot
	})
	return bots
}

// PlayerBots returns bots represented by native player roster entries.
// Campaign AI also needs a player presentation entry, but human readiness and
// reward membership remain separate from this public roster.
func (e *Instance) PlayerBots() []Bot {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	isPlayerRoster := e.Info.Mode == ModeArena || e.Info.Mode == ModeChain
	e.mu.RUnlock()
	if !isPlayerRoster {
		return nil
	}
	return e.Bots()
}

// Bot returns the descriptor occupying one synthetic player slot.
func (e *Instance) Bot(slot uint16) (Bot, bool) {
	if e == nil {
		return Bot{}, false
	}
	e.mu.RLock()
	bot, isFound := e.bots[slot]
	e.mu.RUnlock()
	return bot, isFound
}

// RosterParticipantCount includes socketless bots for gameplay presentation
// and combat barriers. ParticipantCount retains the authenticated human count.
func (e *Instance) RosterParticipantCount() uint16 {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	count := uint16(len(e.players))
	if e.Info.Mode == ModeArena {
		count += uint16(len(e.bots))
	}
	e.mu.RUnlock()
	return count
}

// CombatParticipantCount includes campaign AI in enemy difficulty without
// making a socketless bot part of human loading and rejoin barriers.
func (e *Instance) CombatParticipantCount() uint16 {
	if e == nil {
		return 1
	}
	e.mu.RLock()
	count := uint16(len(e.players))
	if e.Info.Mode == ModeChain || e.Info.Mode == ModeArena {
		count += uint16(len(e.bots))
	}
	e.mu.RUnlock()
	return min(max(count, 1), MaxGamePlayers)
}

// BotBindings derives socketless actor membership from an authenticated owner.
// Bots use the owner's authorized world but never pass through user lookup.
func (e *GameplayJoin) BotBindings(
	ctx context.Context, owner GameplayBinding, meleeNouns ...map[uint32]bool,
) ([]GameplayBinding, error) {
	if e == nil || e.gameManager == nil || owner.IsServerBot || owner.UserID == 0 {
		return nil, errors.New("bot binding owner invalid")
	}
	instance := e.gameManager.Game(owner.GameID)
	if instance == nil || !instance.HasPlayer(int64(owner.UserID)) {
		return nil, errors.New("bot binding game unavailable")
	}
	bindings := make([]GameplayBinding, 0)
	usedSquadIDs := make(map[uint32]bool)
	for _, bot := range instance.Bots() {
		if bot.OwnerUserID != owner.UserID {
			continue
		}
		binding := owner
		binding.IsServerBot = true
		binding.BotOwnerUserID = owner.UserID
		binding.BotDisplayName = bot.DisplayName
		binding.BotSourceSquadID = bot.SourceSquadID
		binding.IsBotSquadMirroringDisabled = bot.IsSquadMirroringDisabled
		binding.UserID = bot.UserID
		binding.Slot = bot.Slot
		binding.Team = bot.Team
		if bot.AvatarID != 0 {
			binding.AvatarID = bot.AvatarID
		}
		if bot.Roster.Creatures[0].Noun != 0 {
			binding.AvatarLevel = bot.Roster.AvatarLevel
			binding.AvatarXP = bot.Roster.AvatarXP
			binding.StartingAvatarXP = bot.Roster.AvatarXP
			binding.ChainProgression = bot.Roster.ChainProgression
			binding.IsOverdriveUnlocked = bot.Roster.IsOverdriveUnlocked
			binding.DNA = bot.Roster.DNA
			binding.Creatures = bot.Roster.Creatures
		}
		isAppearanceResolved := bot.IsAppearanceResolved
		if binding.Mode == ModeChain && bot.Roster.Creatures[0].Noun == 0 && bot.IsSquadMirroringDisabled {
			binding.Creatures = randomCampaignBotCreatures(owner)
		}
		if binding.Mode == ModeChain && bot.Roster.Creatures[0].Noun == 0 && !bot.IsSquadMirroringDisabled {
			var usableNouns map[uint32]bool
			if len(meleeNouns) != 0 {
				usableNouns = meleeNouns[0]
			}
			squadID, creatures, isFound := e.campaignBotSquad(owner, usableNouns, usedSquadIDs, bot.SourceSquadID)
			if !isFound && bot.SourceSquadID != 0 {
				squadID, creatures, isFound = e.campaignBotSquad(owner, usableNouns, usedSquadIDs, 0)
			}
			if isFound {
				binding.SquadID = squadID
				binding.Creatures = creatures
				usedSquadIDs[squadID] = true
				isAppearanceResolved = true
			}
		}
		binding.ActivatedCreatures = nil
		binding.PlayerMask = instance.PlayerMask()
		binding.ParticipantCount = instance.RosterParticipantCount()
		binding.CombatParticipantCount = instance.CombatParticipantCount()
		if !isAppearanceResolved {
			err := e.resolveBotAppearances(ctx, &binding)
			if err != nil {
				return nil, fmt.Errorf("botLooks: %w", err)
			}
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

func randomCampaignBotCreatures(owner GameplayBinding) [3]GameplayCreature {
	creatures := owner.Creatures
	choices := append([]GameplayCreature(nil), owner.ActivatedCreatures...)
	rand.Shuffle(len(choices), func(first, second int) {
		choices[first], choices[second] = choices[second], choices[first]
	})
	for index := range creatures {
		if index >= len(choices) {
			break
		}
		creatures[index] = choices[index]
	}
	return creatures
}

// campaignBotSquad borrows a detached roster from the earliest unused,
// unlocked PvE slot. The player's saved squad and equipment remain untouched.
func (e *GameplayJoin) campaignBotSquad(
	owner GameplayBinding, meleeNouns map[uint32]bool, usedSquadIDs map[uint32]bool,
	preferredSquadID uint32,
) (uint32, [3]GameplayCreature, bool) {
	user := e.userFinder.UserByID(int64(owner.UserID))
	if user == nil {
		return 0, [3]GameplayCreature{}, false
	}
	view := user.View()
	selectedSlot := ^uint32(0)
	selectedID := uint32(0)
	selectedCreatures := [3]GameplayCreature{}
	for _, candidate := range view.Squads {
		if candidate.ID == owner.SquadID || usedSquadIDs[candidate.ID] ||
			(preferredSquadID != 0 && candidate.ID != preferredSquadID) || candidate.Slot >= selectedSlot ||
			candidate.IsLockedFor(view.Account) ||
			(candidate.Category != "" && !strings.EqualFold(candidate.Category, "pve")) {
			continue
		}
		compact := [3]GameplayCreature{}
		count := 0
		isCombatCapable := len(meleeNouns) == 0
		for _, creatureID := range candidate.CreatureIDs {
			if creatureID == 0 {
				continue
			}
			for _, creature := range owner.ActivatedCreatures {
				if creature.ID != creatureID || creature.Noun == 0 {
					continue
				}
				compact[count] = creature
				count++
				isCombatCapable = isCombatCapable || meleeNouns[creature.Noun]
				break
			}
		}
		if count == 0 || !isCombatCapable {
			continue
		}
		selectedSlot = candidate.Slot
		selectedID = candidate.ID
		selectedCreatures = compact
	}
	if selectedID == 0 {
		return 0, [3]GameplayCreature{}, false
	}
	return selectedID, selectedCreatures, true
}
