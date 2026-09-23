package sqlite

import (
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

type userIdentityRow struct {
	LoginName                   string `db:"login_name"`
	DisplayName                 string `db:"display_name"`
	CreateDT                    string `db:"create_dt"`
	LastConnectionDT            string `db:"last_connection_dt"`
	AvatarID                    uint32 `db:"avatar_id"`
	Level                       uint32 `db:"level"`
	XP                          uint32 `db:"xp"`
	ChainProgression            uint32 `db:"chain_progression"`
	OnboardingProgress          uint32 `db:"onboarding_progress"`
	IsTutorialCompletionPending int64  `db:"is_tutorial_completion_pending"`
}

type userRow struct {
	ID                          int64  `db:"id"`
	LoginName                   string `db:"login_name"`
	DisplayName                 string `db:"display_name"`
	Password                    string `db:"password"`
	CreateDT                    string `db:"create_dt"`
	LastConnectionDT            string `db:"last_connection_dt"`
	IsTutorialCompletionPending int64  `db:"is_tutorial_completion_pending"`
	IsAllAccessGranted          int64  `db:"is_all_access_granted"`
	IsOnlineAccessGranted       int64  `db:"is_online_access_granted"`
	IsOverdriveUnlocked         int64  `db:"is_overdrive_unlocked"`
	ChainProgression            uint32 `db:"chain_progression"`
	CreatureRewards             uint32 `db:"creature_reward"`
	CurrentGameID               uint32 `db:"current_game_id"`
	CurrentPlaygroupID          uint32 `db:"current_playgroup_id"`
	DefaultDeckPVEID            uint32 `db:"default_deck_pve_id"`
	DefaultDeckPVPID            uint32 `db:"default_deck_pvp_id"`
	Level                       uint32 `db:"level"`
	XP                          uint32 `db:"xp"`
	DNA                         uint32 `db:"dna"`
	AvatarID                    uint32 `db:"avatar_id"`
	NewPlayerInventory          uint32 `db:"new_player_inventory"`
	OnboardingProgress          uint32 `db:"onboarding_progress"`
	CashoutBonusTime            uint32 `db:"cashout_bonus_time"`
	StarLevel                   uint32 `db:"star_level"`
	UnlockCatalysts             uint32 `db:"unlock_catalyst"`
	UnlockDiagonalCatalysts     uint32 `db:"unlock_diagonal_catalyst"`
	UnlockInventory             uint32 `db:"unlock_inventory"`
	UnlockFuelTanks             uint32 `db:"unlock_fuel_tank"`
	UnlockPVEDecks              uint32 `db:"unlock_pve_deck"`
	UnlockPVPDecks              uint32 `db:"unlock_pvp_deck"`
	UnlockStats                 uint32 `db:"unlock_stat"`
	UnlockInventoryIdentify     uint32 `db:"unlock_inventory_identify"`
	UnlockEditorFlairSlots      uint32 `db:"unlock_editor_flair_slot"`
	Upsell                      uint32 `db:"upsell"`
	CapLevel                    uint32 `db:"cap_level"`
	CapProgression              uint32 `db:"cap_progression"`
}

type settingRow struct {
	UserID int64  `db:"user_id"`
	Key    string `db:"key"`
	Value  string `db:"value"`
}

type userEventRow struct {
	UserID     int64  `db:"user_id"`
	Key        string `db:"key"`
	MessageID  uint32 `db:"message_id"`
	Metadata   string `db:"metadata"`
	OccurredAt int64  `db:"occurred_at"`
	IsPublic   int64  `db:"is_public"`
}

type campaignExperienceRow struct {
	UserID       int64  `db:"user_id"`
	ResultID     uint64 `db:"result_id"`
	Amount       uint32 `db:"amount"`
	CumulativeXP uint32 `db:"cumulative_xp"`
	Level        uint32 `db:"level"`
}

type userAssociationRow struct {
	UserID       int64  `db:"user_id"`
	ListType     uint32 `db:"list_type"`
	MemberID     int64  `db:"member_id"`
	Name         string `db:"name"`
	AssociatedAt uint64 `db:"associated_at"`
}

type userStatRow struct {
	UserID             int64   `db:"user_id"`
	PVEPlayTimeSecond  uint64  `db:"pve_play_time_second"`
	PVEMinionKill      uint64  `db:"pve_minion_kill"`
	PVESpecialKill     uint64  `db:"pve_special_kill"`
	PVEBossKill        uint64  `db:"pve_boss_kill"`
	PVETotalKill       uint64  `db:"pve_total_kill"`
	PVEDeath           uint64  `db:"pve_death"`
	PVEDamageDealt     float64 `db:"pve_damage_dealt"`
	PVEDamageTaken     float64 `db:"pve_damage_taken"`
	PVEDamageMaximum   float64 `db:"pve_damage_maximum"`
	PVEHealing         float64 `db:"pve_healing"`
	PVEHealingReceived float64 `db:"pve_healing_received"`
	PVEHealingMaximum  float64 `db:"pve_healing_maximum"`
	PVPPlayTimeSecond  uint64  `db:"pvp_play_time_second"`
	PVPWin             uint64  `db:"pvp_win"`
	PVPLoss            uint64  `db:"pvp_loss"`
	PVPPlayerKill      uint64  `db:"pvp_player_kill"`
	PVPDeath           uint64  `db:"pvp_death"`
	PVPDamageDealt     float64 `db:"pvp_damage_dealt"`
	PVPDamageTaken     float64 `db:"pvp_damage_taken"`
	PVPDamageMaximum   float64 `db:"pvp_damage_maximum"`
	PVPHealing         float64 `db:"pvp_healing"`
	PVPHealingReceived float64 `db:"pvp_healing_received"`
	PVPHealingMaximum  float64 `db:"pvp_healing_maximum"`
}

func userStatRowFromRecord(record sporenet.UserRecord) userStatRow {
	stat := record.Stats
	return userStatRow{
		UserID:            record.Account.ID,
		PVEPlayTimeSecond: stat.PVEPlayTimeSecond,
		PVEMinionKill:     stat.PVEMinionKill, PVESpecialKill: stat.PVESpecialKill,
		PVEBossKill: stat.PVEBossKill, PVETotalKill: stat.PVETotalKill,
		PVEDeath: stat.PVEDeath, PVEDamageDealt: stat.PVEDamageDealt,
		PVEDamageTaken: stat.PVEDamageTaken, PVEDamageMaximum: stat.PVEDamageMaximum,
		PVEHealing: stat.PVEHealing, PVEHealingReceived: stat.PVEHealingReceived,
		PVEHealingMaximum: stat.PVEHealingMaximum,
		PVPPlayTimeSecond: stat.PVPPlayTimeSecond,
		PVPWin:            stat.PVPWin, PVPLoss: stat.PVPLoss,
		PVPPlayerKill: stat.PVPPlayerKill, PVPDeath: stat.PVPDeath,
		PVPDamageDealt: stat.PVPDamageDealt, PVPDamageTaken: stat.PVPDamageTaken,
		PVPDamageMaximum: stat.PVPDamageMaximum, PVPHealing: stat.PVPHealing,
		PVPHealingReceived: stat.PVPHealingReceived, PVPHealingMaximum: stat.PVPHealingMaximum,
	}
}

func (row userStatRow) stats() sporenet.PlayerStats {
	return sporenet.PlayerStats{
		PVEPlayTimeSecond: row.PVEPlayTimeSecond,
		PVEMinionKill:     row.PVEMinionKill, PVESpecialKill: row.PVESpecialKill,
		PVEBossKill: row.PVEBossKill, PVETotalKill: row.PVETotalKill,
		PVEDeath: row.PVEDeath, PVEDamageDealt: row.PVEDamageDealt,
		PVEDamageTaken: row.PVEDamageTaken, PVEDamageMaximum: row.PVEDamageMaximum,
		PVEHealing: row.PVEHealing, PVEHealingReceived: row.PVEHealingReceived,
		PVEHealingMaximum: row.PVEHealingMaximum,
		PVPPlayTimeSecond: row.PVPPlayTimeSecond,
		PVPWin:            row.PVPWin, PVPLoss: row.PVPLoss,
		PVPPlayerKill: row.PVPPlayerKill, PVPDeath: row.PVPDeath,
		PVPDamageDealt: row.PVPDamageDealt, PVPDamageTaken: row.PVPDamageTaken,
		PVPDamageMaximum: row.PVPDamageMaximum, PVPHealing: row.PVPHealing,
		PVPHealingReceived: row.PVPHealingReceived, PVPHealingMaximum: row.PVPHealingMaximum,
	}
}

type squadRow struct {
	ID       uint32 `db:"id"`
	UserID   int64  `db:"user_id"`
	Name     string `db:"name"`
	Category string `db:"category"`
	Slot     uint32 `db:"slot"`
	IsLocked int64  `db:"is_locked"`
}

type squadCreatureRow struct {
	UserID     int64  `db:"user_id"`
	SquadID    uint32 `db:"squad_id"`
	CreatureID uint32 `db:"creature_id"`
	Position   uint32 `db:"position"`
}

type creatureRow struct {
	ID           uint32  `db:"id"`
	UserID       int64   `db:"user_id"`
	TemplateName string  `db:"template_name"`
	Version      uint32  `db:"version"`
	GearScore    float32 `db:"gear_score"`
	ItemPoints   float32 `db:"item_point"`
	LargeImage   string  `db:"large_image_url"`
	ThumbImage   string  `db:"thumb_image_url"`
	CreatorID    int64   `db:"creator_id"`
}

type creatureStatRow struct {
	UserID     int64  `db:"user_id"`
	CreatureID uint32 `db:"creature_id"`
	Position   int    `db:"position"`
	Name       string `db:"name"`
	Maximum    uint32 `db:"maximum"`
	Current    uint32 `db:"current"`
}

type creatureAbilityStatRow struct {
	UserID     int64  `db:"user_id"`
	CreatureID uint32 `db:"creature_id"`
	Position   int    `db:"position"`
	AbilityKey string `db:"ability_key"`
	Token      string `db:"token"`
	TokenValue string `db:"token_value"`
}

type partRow struct {
	UserID                 int64               `db:"user_id"`
	Position               int                 `db:"position"`
	ItemID                 uint64              `db:"item_id"`
	ReferenceID            uint64              `db:"reference_id"`
	IsFlair                int64               `db:"is_flair"`
	Cost                   uint32              `db:"cost"`
	CreatureID             uint32              `db:"creature_id"`
	Level                  uint16              `db:"level"`
	MarketStatus           uint8               `db:"market_status"`
	Rarity                 sporenet.PartRarity `db:"rarity"`
	Status                 uint8               `db:"status"`
	Usage                  uint8               `db:"usage"`
	CreationDate           uint64              `db:"creation_date"`
	RigblockAssetID        uint16              `db:"rigblock_asset_id"`
	PrefixAssetID          uint16              `db:"prefix_asset_id"`
	PrefixSecondaryAssetID uint16              `db:"prefix_secondary_asset_id"`
	SuffixAssetID          uint16              `db:"suffix_asset_id"`
}

type extensionRow struct {
	UserID   int64  `db:"user_id"`
	Position int    `db:"position"`
	Name     string `db:"name"`
	Value    []byte `db:"value"`
}

func userRowFromRecord(record sporenet.UserRecord) userRow {
	account := record.Account
	lastConnectionDT := ""
	if !record.LastConnectionDT.IsZero() {
		lastConnectionDT = record.LastConnectionDT.UTC().Format(time.RFC3339Nano)
	}
	return userRow{
		ID: account.ID, LoginName: record.LoginName, DisplayName: record.DisplayName, Password: record.Password,
		CreateDT:                    record.CreateDT.UTC().Format(time.RFC3339Nano),
		LastConnectionDT:            lastConnectionDT,
		IsTutorialCompletionPending: boolInteger(record.IsTutorialCompletionPending),
		IsAllAccessGranted:          boolInteger(account.IsAllAccessGranted), IsOnlineAccessGranted: boolInteger(account.IsOnlineAccessGranted),
		IsOverdriveUnlocked: boolInteger(account.IsOverdriveUnlocked),
		ChainProgression:    account.ChainProgression, CreatureRewards: account.CreatureRewards,
		CurrentGameID: account.CurrentGameID, CurrentPlaygroupID: account.CurrentPlaygroupID,
		DefaultDeckPVEID: account.DefaultDeckPVEID, DefaultDeckPVPID: account.DefaultDeckPVPID,
		Level: account.Level, XP: account.XP, DNA: account.DNA, AvatarID: account.AvatarID,
		NewPlayerInventory: account.NewPlayerInventory, OnboardingProgress: account.OnboardingProgress,
		CashoutBonusTime: account.CashoutBonusTime, StarLevel: account.StarLevel,
		UnlockCatalysts: account.UnlockCatalysts, UnlockDiagonalCatalysts: account.UnlockDiagonalCatalysts,
		UnlockInventory: account.UnlockInventory, UnlockFuelTanks: account.UnlockFuelTanks,
		UnlockPVEDecks: account.UnlockPVEDecks, UnlockPVPDecks: account.UnlockPVPDecks,
		UnlockStats: account.UnlockStats, UnlockInventoryIdentify: account.UnlockInventoryIdentify,
		UnlockEditorFlairSlots: account.UnlockEditorFlairSlots, Upsell: account.Upsell,
		CapLevel: account.CapLevel, CapProgression: account.CapProgression,
	}
}

func (row userRow) account() sporenet.Account {
	return sporenet.Account{
		IsAllAccessGranted:    row.IsAllAccessGranted != 0,
		IsOnlineAccessGranted: row.IsOnlineAccessGranted != 0,
		IsOverdriveUnlocked:   row.IsOverdriveUnlocked != 0, ID: row.ID,
		ChainProgression: row.ChainProgression, CreatureRewards: row.CreatureRewards,
		CurrentGameID: row.CurrentGameID, CurrentPlaygroupID: row.CurrentPlaygroupID,
		DefaultDeckPVEID: row.DefaultDeckPVEID, DefaultDeckPVPID: row.DefaultDeckPVPID,
		Level: row.Level, XP: row.XP, DNA: row.DNA, AvatarID: row.AvatarID,
		NewPlayerInventory: row.NewPlayerInventory, OnboardingProgress: row.OnboardingProgress,
		CashoutBonusTime: row.CashoutBonusTime, StarLevel: row.StarLevel,
		UnlockCatalysts: row.UnlockCatalysts, UnlockDiagonalCatalysts: row.UnlockDiagonalCatalysts,
		UnlockInventory: row.UnlockInventory, UnlockFuelTanks: row.UnlockFuelTanks,
		UnlockPVEDecks: row.UnlockPVEDecks, UnlockPVPDecks: row.UnlockPVPDecks,
		UnlockStats: row.UnlockStats, UnlockInventoryIdentify: row.UnlockInventoryIdentify,
		UnlockEditorFlairSlots: row.UnlockEditorFlairSlots, Upsell: row.Upsell,
		CapLevel: row.CapLevel, CapProgression: row.CapProgression,
	}
}

func boolInteger(isTrue bool) int64 {
	if isTrue {
		return 1
	}
	return 0
}
