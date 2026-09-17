package chat

import (
	"context"
	"time"
)

// BugContextRequest identifies the authenticated player whose live gameplay
// state should be frozen into a diagnostic archive.
type BugContextRequest struct {
	Sender Participant
	GameID uint32
}

// BugContext is the transport-neutral live state captured at report time.
type BugContext struct {
	CapturedAt          time.Time      `json:"captured_at"`
	IsGameplayActive    bool           `json:"is_gameplay_active"`
	UserID              int64          `json:"user_id"`
	UserName            string         `json:"user_name"`
	GameID              uint32         `json:"game_id"`
	SessionGeneration   uint64         `json:"session_generation,omitempty"`
	TransportGeneration uint64         `json:"transport_generation,omitempty"`
	SessionStage        string         `json:"session_stage,omitempty"`
	ZoneGeneration      uint64         `json:"zone_generation,omitempty"`
	ZoneState           uint32         `json:"zone_state,omitempty"`
	PlayerSlot          uint16         `json:"player_slot"`
	SquadID             uint32         `json:"squad_id,omitempty"`
	MemberLimit         uint16         `json:"member_limit"`
	ParticipantCount    uint16         `json:"participant_count"`
	CrogenitorLevel     uint32         `json:"crogenitor_level,omitempty"`
	CampaignProgression uint32         `json:"campaign_progression,omitempty"`
	AbilityCount        uint32         `json:"ability_count,omitempty"`
	IsReplay            bool           `json:"is_replay,omitempty"`
	IsCheckpointRestore bool           `json:"is_checkpoint_restore,omitempty"`
	IsOverdriveUnlocked bool           `json:"is_overdrive_unlocked,omitempty"`
	IsDungeonSetupReady bool           `json:"is_dungeon_setup_ready,omitempty"`
	IsDungeonSetupFinal bool           `json:"is_dungeon_setup_final,omitempty"`
	Mission             BugMission     `json:"mission"`
	Hero                BugHero        `json:"active_hero"`
	Location            BugLocation    `json:"location"`
	SquadHeroes         []BugSquadHero `json:"squad_heroes,omitempty"`
}

type BugMission struct {
	Label      string `json:"label,omitempty"`
	Asset      string `json:"asset,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Index      uint32 `json:"index,omitempty"`
	Difficulty uint32 `json:"difficulty,omitempty"`
}

type BugHero struct {
	Name              string  `json:"name,omitempty"`
	CreatureID        uint32  `json:"creature_id,omitempty"`
	NounID            uint32  `json:"noun_id,omitempty"`
	ObjectID          uint32  `json:"object_id,omitempty"`
	SquadIndex        uint32  `json:"squad_index"`
	Level             uint32  `json:"level,omitempty"`
	HitPoint          float32 `json:"hit_point,omitempty"`
	MaximumHitPoint   float32 `json:"maximum_hit_point,omitempty"`
	PowerPoint        float32 `json:"power_point,omitempty"`
	MaximumPowerPoint float32 `json:"maximum_power_point,omitempty"`
	IsDefeated        bool    `json:"is_defeated,omitempty"`
}

type BugLocation struct {
	X                float32 `json:"x"`
	Y                float32 `json:"y"`
	Z                float32 `json:"z"`
	NearestMarkerSet string  `json:"nearest_marker_set,omitempty"`
	NearestMarker    string  `json:"nearest_marker,omitempty"`
	MarkerDistance   float32 `json:"marker_distance,omitempty"`
}

type BugSquadHero struct {
	Index                      uint32  `json:"index"`
	Name                       string  `json:"name,omitempty"`
	CreatureID                 uint32  `json:"creature_id,omitempty"`
	NounID                     uint32  `json:"noun_id,omitempty"`
	FunctionalItemCount        uint32  `json:"functional_item_count"`
	IgnoredFunctionalItemCount uint32  `json:"ignored_functional_item_count,omitempty"`
	FunctionalWeaponItemCount  uint32  `json:"functional_weapon_item_count"`
	MinimumFunctionalItemLevel uint32  `json:"minimum_functional_item_level,omitempty"`
	MaximumFunctionalItemLevel uint32  `json:"maximum_functional_item_level,omitempty"`
	WeaponItemLevel            uint32  `json:"weapon_item_level,omitempty"`
	WeaponDamageModifier       float32 `json:"weapon_damage_modifier,omitempty"`
	GearScore                  float32 `json:"gear_score,omitempty"`
	FlattenedGearScore         float32 `json:"flattened_gear_score,omitempty"`
	HitPoint                   float32 `json:"hit_point,omitempty"`
	MaximumHitPoint            float32 `json:"maximum_hit_point,omitempty"`
	PowerPoint                 float32 `json:"power_point,omitempty"`
	MaximumPowerPoint          float32 `json:"maximum_power_point,omitempty"`
	IsAvailable                bool    `json:"is_available"`
	IsDefeated                 bool    `json:"is_defeated,omitempty"`
	MinimumWeaponDamage        float32 `json:"minimum_weapon_damage,omitempty"`
	MaximumWeaponDamage        float32 `json:"maximum_weapon_damage,omitempty"`
	PrimaryAttribute           float32 `json:"primary_attribute,omitempty"`
	DamageBuff                 float32 `json:"damage_buff,omitempty"`
	ProjectileDamage           float32 `json:"projectile_damage,omitempty"`
	EnergyDamageBuff           float32 `json:"energy_damage_buff,omitempty"`
	EnergyDamage               float32 `json:"energy_damage,omitempty"`
	DirectAttackDamage         float32 `json:"direct_attack_damage,omitempty"`
	DirectAttackDamagePercent  float32 `json:"direct_attack_damage_percent,omitempty"`
	CriticalRating             float32 `json:"critical_rating,omitempty"`
	AutoCrit                   float32 `json:"auto_crit,omitempty"`
	CriticalDamageIncrease     float32 `json:"critical_damage_increase,omitempty"`
	AttackSpeed                float32 `json:"attack_speed,omitempty"`
	CooldownReduction          float32 `json:"cooldown_reduction,omitempty"`
}

// BugContextProvider freezes current gameplay state independently of the
// report's rolling log window.
type BugContextProvider interface {
	BugContext(context.Context, BugContextRequest) (BugContext, error)
}
