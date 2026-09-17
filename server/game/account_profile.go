package game

import (
	"context"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// publicAccountProfile is the allowlisted account data exposed to another
// authenticated player. It deliberately excludes credentials, settings,
// currency, purchase records, and active session identifiers.
type publicAccountProfile struct {
	blazeID               int64
	displayName           string
	isTutorialCompleted   bool
	chainProgression      uint32
	creatureReward        uint32
	avatarID              uint32
	level                 uint32
	xp                    uint32
	onboardingProgress    uint32
	cashoutBonusTime      uint32
	starLevel             uint32
	unlockCatalyst        uint32
	unlockFuelTank        uint32
	unlockInventory       uint32
	unlockPVPDeck         uint32
	unlockStats           uint32
	unlockEditorFlairSlot uint32
}

func newPublicAccountProfile(view sporenet.UserView) publicAccountProfile {
	account := view.Account
	return publicAccountProfile{
		blazeID: account.ID, displayName: view.DisplayName,
		isTutorialCompleted: account.IsTutorialCompleted(),
		chainProgression:    account.ChainProgression, creatureReward: account.CreatureRewards,
		avatarID: account.AvatarID,
		level:    account.Level, xp: account.XP, onboardingProgress: account.OnboardingProgress,
		cashoutBonusTime: account.CashoutBonusTime, starLevel: account.StarLevel,
		unlockCatalyst: account.UnlockCatalysts, unlockFuelTank: account.UnlockFuelTanks,
		unlockInventory: account.UnlockInventoryIdentify, unlockPVPDeck: account.UnlockPVPDecks,
		unlockStats:           account.UnlockStats,
		unlockEditorFlairSlot: account.UnlockEditorFlairSlots,
	}
}

func (profile publicAccountProfile) xmlNode() string {
	return xmlNode("account",
		xmlText("blaze_id", strconv.FormatInt(profile.blazeID, 10)),
		xmlText("id", strconv.FormatInt(profile.blazeID, 10)),
		xmlText("name", profile.displayName),
		xmlText("tutorial_completed", boolText(profile.isTutorialCompleted)),
		xmlText("chain_progression", number(profile.chainProgression)),
		xmlText("creature_rewards", number(profile.creatureReward)),
		xmlText("avatar_id", number(profile.avatarID)),
		xmlText("level", number(profile.level)),
		xmlText("xp", number(profile.xp)),
		xmlText("new_player_progress", number(profile.onboardingProgress)),
		xmlText("cashout_bonus_time", number(profile.cashoutBonusTime)),
		xmlText("star_level", number(profile.starLevel)),
		xmlText("unlock_catalysts", number(profile.unlockCatalyst)),
		xmlText("unlock_fuel_tanks", number(profile.unlockFuelTank)),
		xmlText("unlock_inventory", number(profile.unlockInventory)),
		xmlText("unlock_pvp_decks", number(profile.unlockPVPDeck)),
		xmlText("unlock_stats", number(profile.unlockStats)),
		xmlText("unlock_editor_flair_slots", number(profile.unlockEditorFlairSlot)),
	)
}

func (a *API) accountProfileTarget(
	ctx context.Context, actor *sporenet.User, values interface{ Get(string) string },
) (sporenet.UserView, bool) {
	if actor == nil {
		return sporenet.UserView{}, false
	}
	rawID := strings.TrimSpace(values.Get("id"))
	name := strings.TrimSpace(values.Get("name"))
	if rawID == "" && name == "" {
		return actor.View(), true
	}
	if rawID != "" && name != "" {
		return sporenet.UserView{}, false
	}
	accountID := int64(0)
	if rawID != "" {
		var err error
		accountID, err = strconv.ParseInt(rawID, 10, 64)
		if err != nil || accountID <= 0 {
			return sporenet.UserView{}, false
		}
	}
	view, err := a.userManager.PublicProfile(ctx, accountID, name)
	if err != nil {
		return sporenet.UserView{}, false
	}
	return view, true
}
