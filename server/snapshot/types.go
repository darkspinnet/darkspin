// Package snapshot owns opt-in rolling RakNet and gameplay-state captures.
package snapshot

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	DefaultBufferDuration = 30 * time.Second
	DefaultDelay          = 30 * time.Second
	minimumDelay          = time.Second
	maximumDelay          = time.Hour
	maximumBufferDuration = 5 * time.Minute
	maximumBufferedByte   = 64 * 1024 * 1024
)

// Mode controls rolling collection and automatic anomaly capture.
type Mode string

const (
	ModeOff    Mode = "off"
	ModeManual Mode = "manual"
	ModeAuto   Mode = "auto"
)

// ParseMode validates one launcher or chat-selected snapshot mode.
func ParseMode(raw string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(raw)))
	switch mode {
	case ModeOff, ModeManual, ModeAuto:
		return mode, nil
	default:
		return "", errors.New("mode must be manual, auto, or off")
	}
}

// Actor identifies the player whose connection supplied snapshot context.
type Actor struct {
	UserID   int64  `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
	GameID   uint32 `json:"game_id,omitempty"`
	Remote   string `json:"remote,omitempty"`
}

// StateRequest asks gameplay for one authoritative, immutable keyframe.
type StateRequest struct {
	Actor Actor
}

// ObjectState is the common authoritative state needed to align server and
// client beliefs without serializing gameplay aggregates directly.
type ObjectState struct {
	Kind                         string     `json:"kind"`
	SubType                      uint32     `json:"sub_type,omitempty"`
	ObjectID                     uint32     `json:"object_id"`
	OwnerObjectID                uint32     `json:"owner_object_id,omitempty"`
	UserID                       uint64     `json:"user_id,omitempty"`
	PeerGeneration               uint64     `json:"peer_generation,omitempty"`
	NounName                     string     `json:"noun_name,omitempty"`
	AbilityName                  string     `json:"ability_name,omitempty"`
	Position                     [3]float32 `json:"position"`
	Rotation                     [3]float32 `json:"rotation,omitempty"`
	LinearVelocity               [3]float32 `json:"linear_velocity,omitempty"`
	Facing                       [3]float32 `json:"facing,omitempty"`
	GoalPosition                 [3]float32 `json:"goal_position,omitempty"`
	TargetPosition               [3]float32 `json:"target_position,omitempty"`
	HitPoint                     float32    `json:"hit_point,omitempty"`
	ManaPoint                    float32    `json:"mana_point,omitempty"`
	Amount                       uint32     `json:"amount,omitempty"`
	TargetObjectID               uint32     `json:"target_object_id,omitempty"`
	ActionRevision               uint64     `json:"action_revision,omitempty"`
	AbilityIndex                 uint32     `json:"ability_index,omitempty"`
	SyncStamp                    uint8      `json:"sync_stamp,omitempty"`
	Speed                        float32    `json:"speed,omitempty"`
	StopDistance                 float32    `json:"stop_distance,omitempty"`
	SlowMovementScale            float32    `json:"slow_movement_scale,omitempty"`
	RemainingDistance            float32    `json:"remaining_distance,omitempty"`
	RemainingDurationMS          int64      `json:"remaining_duration_ms,omitempty"`
	StunRemainingMS              int64      `json:"stun_remaining_ms,omitempty"`
	SleepRemainingMS             int64      `json:"sleep_remaining_ms,omitempty"`
	RootRemainingMS              int64      `json:"root_remaining_ms,omitempty"`
	SilenceRemainingMS           int64      `json:"silence_remaining_ms,omitempty"`
	FearRemainingMS              int64      `json:"fear_remaining_ms,omitempty"`
	FearSourceObjectID           uint32     `json:"fear_source_object_id,omitempty"`
	IsDefeated                   bool       `json:"is_defeated,omitempty"`
	IsPublished                  bool       `json:"is_published,omitempty"`
	IsTargetable                 bool       `json:"is_targetable"`
	IsActive                     bool       `json:"is_active,omitempty"`
	IsPursuing                   bool       `json:"is_pursuing,omitempty"`
	IsFrozen                     bool       `json:"is_frozen,omitempty"`
	IsGravityDeflected           bool       `json:"is_gravity_deflected,omitempty"`
	IsActionStarted              bool       `json:"is_action_started,omitempty"`
	IsFirstActionStarted         bool       `json:"is_first_action_started,omitempty"`
	IsSelfResurrectionTriggered  bool       `json:"is_self_resurrection_triggered,omitempty"`
	IsNavigationCollisionEnabled bool       `json:"is_navigation_collision_enabled"`
	ActionOwnerUserID            uint64     `json:"action_owner_user_id,omitempty"`
	ActionOwnerGeneration        uint64     `json:"action_owner_generation,omitempty"`
}

// ModifierState captures live server effects that can explain movement,
// resource, and timing differences even when they own no world object.
type ModifierState struct {
	InstanceID                uint32  `json:"instance_id"`
	GUID                      uint32  `json:"guid"`
	SourceObjectID            uint32  `json:"source_object_id"`
	TargetObjectID            uint32  `json:"target_object_id"`
	Rank                      uint32  `json:"rank"`
	DurationMS                int64   `json:"duration_ms"`
	Kind                      uint8   `json:"kind"`
	StackCount                uint32  `json:"stack_count,omitempty"`
	DamageBuff                float32 `json:"damage_buff,omitempty"`
	EnergyDamageBuff          float32 `json:"energy_damage_buff,omitempty"`
	EnergyDamageTakenIncrease float32 `json:"energy_damage_taken_increase,omitempty"`
	HealingReduction          float32 `json:"healing_reduction,omitempty"`
	AttackSpeed               float32 `json:"attack_speed,omitempty"`
	CooldownReduction         float32 `json:"cooldown_reduction,omitempty"`
	MovementSpeedBuff         float32 `json:"movement_speed_buff,omitempty"`
	IsChannel                 bool    `json:"is_channel,omitempty"`
	IsHaste                   bool    `json:"is_haste,omitempty"`
}

// PlayerControlState preserves server-side command admission and motion state
// that may diverge without creating a separate world object.
type PlayerControlState struct {
	ReportedPosition              [3]float32         `json:"reported_position"`
	MovementGoal                  [3]float32         `json:"movement_goal"`
	MotionPosition                [3]float32         `json:"motion_position"`
	MotionGoal                    [3]float32         `json:"motion_goal"`
	MotionVelocity                [3]float32         `json:"motion_velocity"`
	MotionSpeed                   float32            `json:"motion_speed,omitempty"`
	MotionRevision                uint64             `json:"motion_revision,omitempty"`
	MotionStartedAt               time.Time          `json:"motion_started_at,omitempty"`
	MotionSegmentAtMS             int64              `json:"motion_segment_at_ms,omitempty"`
	AttackFacing                  [3]float32         `json:"attack_facing"`
	AttackTargetPosition          [3]float32         `json:"attack_target_position"`
	AttackTargetObjectID          uint32             `json:"attack_target_object_id,omitempty"`
	AttackPoseRemainingMS         int64              `json:"attack_pose_remaining_ms,omitempty"`
	AbilityReleaseRemainingMS     int64              `json:"ability_release_remaining_ms,omitempty"`
	AbilityReleaseRevision        uint64             `json:"ability_release_revision,omitempty"`
	HeroSelectionRemainingMS      int64              `json:"hero_selection_remaining_ms,omitempty"`
	EnemySilenceRemainingMS       int64              `json:"enemy_silence_remaining_ms,omitempty"`
	EnemySleepRemainingMS         int64              `json:"enemy_sleep_remaining_ms,omitempty"`
	EnemyStunRemainingMS          int64              `json:"enemy_stun_remaining_ms,omitempty"`
	EnemyStunTargetObjectID       uint32             `json:"enemy_stun_target_object_id,omitempty"`
	EnemyRootRemainingMS          int64              `json:"enemy_root_remaining_ms,omitempty"`
	EnemyRootTargetObjectID       uint32             `json:"enemy_root_target_object_id,omitempty"`
	EnemyFearRemainingMS          int64              `json:"enemy_fear_remaining_ms,omitempty"`
	EnemyFearTargetObjectID       uint32             `json:"enemy_fear_target_object_id,omitempty"`
	OverdriveRemainingMS          int64              `json:"overdrive_remaining_ms,omitempty"`
	FollowTargetUserID            uint64             `json:"follow_target_user_id,omitempty"`
	NextProjectileObjectID        uint32             `json:"next_projectile_object_id,omitempty"`
	BasicSequence                 BasicSequenceState `json:"basic_sequence"`
	IsMotionMoving                bool               `json:"is_motion_moving"`
	IsAttackPoseActive            bool               `json:"is_attack_pose_active"`
	IsHeroSelectionPending        bool               `json:"is_hero_selection_pending"`
	IsHeroSelectionScheduled      bool               `json:"is_hero_selection_scheduled"`
	IsOverdrivePersistencePending bool               `json:"is_overdrive_persistence_pending"`
	IsOverdriveSpent              bool               `json:"is_overdrive_spent"`
	IsTrapperStealthed            bool               `json:"is_trapper_stealthed"`
}

type BasicSequenceState struct {
	Revision               uint64 `json:"revision,omitempty"`
	CooldownRemainingMS    int64  `json:"cooldown_remaining_ms,omitempty"`
	ContinueRemainingMS    int64  `json:"continue_remaining_ms,omitempty"`
	HeldGeneration         uint64 `json:"held_generation,omitempty"`
	Index                  int    `json:"index,omitempty"`
	StarterIndex           int    `json:"starter_index,omitempty"`
	IsHeld                 bool   `json:"is_held"`
	IsStarted              bool   `json:"is_started"`
	IsFullSequenceComplete bool   `json:"is_full_sequence_complete"`
}

type CooldownState struct {
	Key           uint64 `json:"key"`
	AbilityID     uint32 `json:"ability_id,omitempty"`
	RemainingMS   int64  `json:"remaining_ms,omitempty"`
	Revision      uint64 `json:"revision,omitempty"`
	IsHeroAbility bool   `json:"is_hero_ability"`
}

type ActionScheduleState struct {
	Kind              string `json:"kind"`
	ObjectID          uint32 `json:"object_id"`
	IsRunAttached     bool   `json:"is_run_attached"`
	IsCleanerAttached bool   `json:"is_cleaner_attached"`
}

type DeathState struct {
	ObjectID                     uint32  `json:"object_id"`
	SourceObjectID               uint32  `json:"source_object_id"`
	AnimationName                string  `json:"animation_name,omitempty"`
	ElapsedMS                    int64   `json:"elapsed_ms"`
	DeadlinesMS                  []int64 `json:"deadlines_ms,omitempty"`
	RemainingDeadlinesMS         []int64 `json:"remaining_deadlines_ms,omitempty"`
	PendingTaskCount             int     `json:"pending_task_count"`
	IsTargetCleared              bool    `json:"is_target_cleared"`
	IsImmobilized                bool    `json:"is_immobilized"`
	IsLocomotionStopped          bool    `json:"is_locomotion_stopped"`
	IsCorpseFading               bool    `json:"is_corpse_fading"`
	IsMarkedForDeletion          bool    `json:"is_marked_for_deletion"`
	IsPhysicsCollisionEnabled    bool    `json:"is_physics_collision_enabled"`
	IsNavigationCollisionEnabled bool    `json:"is_navigation_collision_enabled"`
}

type ObjectiveState struct {
	ObjectiveID uint32      `json:"objective_id"`
	States      [4]uint8    `json:"states"`
	Tokens      [4][3]int32 `json:"tokens"`
}

type RandomState struct {
	Kind        string   `json:"kind"`
	Words       []uint32 `json:"words"`
	Index       uint32   `json:"index"`
	DrawCount   uint64   `json:"draw_count"`
	StateSHA256 string   `json:"state_sha256"`
}

type SquadCharacterState struct {
	Index            uint32  `json:"index"`
	HitPoint         float32 `json:"hit_point"`
	ManaPoint        float32 `json:"mana_point"`
	MaximumHitPoint  float32 `json:"maximum_hit_point"`
	MaximumManaPoint float32 `json:"maximum_mana_point"`
	IsAvailable      bool    `json:"is_available"`
	IsDeployed       bool    `json:"is_deployed"`
}

type SquadState struct {
	DeployedCreatureIndex     uint32                `json:"deployed_creature_index"`
	DeployCooldownRemainingMS int64                 `json:"deploy_cooldown_remaining_ms,omitempty"`
	Characters                []SquadCharacterState `json:"characters,omitempty"`
	IsGameOver                bool                  `json:"is_game_over"`
	IsRestartReserved         bool                  `json:"is_restart_reserved"`
}

type CrystalSlotState struct {
	Index        int    `json:"index"`
	NounName     string `json:"noun_name,omitempty"`
	NounAsset    uint32 `json:"noun_asset,omitempty"`
	CrystalType  int32  `json:"crystal_type,omitempty"`
	CrystalLevel int32  `json:"crystal_level,omitempty"`
	Rarity       int32  `json:"rarity,omitempty"`
	IsOccupied   bool   `json:"is_occupied"`
}

type CrystalInventoryState struct {
	Slots              []CrystalSlotState `json:"slots"`
	IsDiagonalUnlocked bool               `json:"is_diagonal_unlocked"`
}

type AbilityRuntimeState struct {
	Kind          string `json:"kind"`
	Name          string `json:"name,omitempty"`
	AbilityID     uint32 `json:"ability_id,omitempty"`
	ObjectID      uint32 `json:"object_id,omitempty"`
	OwnerObjectID uint32 `json:"owner_object_id,omitempty"`
	RemainingMS   int64  `json:"remaining_ms,omitempty"`
	IsTriggered   bool   `json:"is_triggered,omitempty"`
}

type ZoneMemberState struct {
	UserID         uint64 `json:"user_id"`
	PeerGeneration uint64 `json:"peer_generation"`
	PlayerSlot     uint16 `json:"player_slot"`
	AbilityCount   uint32 `json:"ability_count"`
	IsReplay       bool   `json:"is_replay"`
	IsConnected    bool   `json:"is_connected"`
}

type TimelineTaskState struct {
	Key         string    `json:"key"`
	DueAt       time.Time `json:"due_at,omitempty"`
	RemainingMS int64     `json:"remaining_ms,omitempty"`
}

type EncounterStageState struct {
	Key            string `json:"key"`
	Stage          int    `json:"stage"`
	TerminalStages []int  `json:"terminal_stages"`
	IsPublished    bool   `json:"is_published"`
}

type HordeState struct {
	MarkerSetName   string   `json:"marker_set_name"`
	Phase           uint8    `json:"phase"`
	WaveOrdinal     int      `json:"wave_ordinal"`
	LiveObjectIDs   []uint32 `json:"live_object_ids,omitempty"`
	IsGateActive    bool     `json:"is_gate_active"`
	CompletionEvent string   `json:"completion_event,omitempty"`
}

type BossState struct {
	Phase                 uint8    `json:"phase"`
	LeaderObjectID        uint32   `json:"leader_object_id,omitempty"`
	LeaderHitPoint        float32  `json:"leader_hit_point,omitempty"`
	LiveObjectIDs         []uint32 `json:"live_object_ids,omitempty"`
	FirstWaveAddObjectIDs []uint32 `json:"first_wave_add_object_ids,omitempty"`
	PlanCount             int      `json:"plan_count"`
	IsInitialChain        bool     `json:"is_initial_chain"`
	IsLeaderDeferred      bool     `json:"is_leader_deferred"`
	IsSecondWaveRequested bool     `json:"is_second_wave_requested"`
	IsSecondWaveAdmitted  bool     `json:"is_second_wave_admitted"`
	IsBeamOutReserved     bool     `json:"is_beam_out_reserved"`
	IsBeamOutCommitted    bool     `json:"is_beam_out_committed"`
}

type SecurityState struct {
	ObjectIDs  [7]uint32 `json:"object_ids"`
	RouteIndex int       `json:"route_index"`
	Presented  [7]bool   `json:"presented"`
}

type InteractableState struct {
	ObjectID uint32 `json:"object_id"`
	Limit    int32  `json:"limit"`
	Count    int32  `json:"count"`
}

type DirectorPublicationState struct {
	Kind             string `json:"kind"`
	PublicationID    uint64 `json:"publication_id,omitempty"`
	MarkerSetOrdinal int    `json:"marker_set_ordinal"`
	MarkerSetName    string `json:"marker_set_name"`
	TriggerOrdinal   int    `json:"trigger_ordinal,omitempty"`
	TriggerMarkerID  uint32 `json:"trigger_marker_id,omitempty"`
	EventOrdinal     int    `json:"event_ordinal,omitempty"`
	EventName        string `json:"event_name,omitempty"`
	CallbackName     string `json:"callback_name,omitempty"`
	SourceObjectID   uint32 `json:"source_object_id,omitempty"`
	ListenerCount    int    `json:"listener_count"`
}

// SessionState describes one authoritative gameplay binding and its observed
// zone objects at the snapshot boundary.
type SessionState struct {
	Remote               string                     `json:"remote"`
	UserID               uint64                     `json:"user_id"`
	UserName             string                     `json:"user_name,omitempty"`
	GameID               uint32                     `json:"game_id"`
	SessionGeneration    uint64                     `json:"session_generation"`
	TransportGeneration  uint64                     `json:"transport_generation"`
	ZoneID               uint64                     `json:"zone_id,omitempty"`
	ZoneGeneration       uint64                     `json:"zone_generation,omitempty"`
	ZoneState            uint8                      `json:"zone_state,omitempty"`
	ProjectionRevision   uint64                     `json:"projection_revision,omitempty"`
	Stage                string                     `json:"stage"`
	PlayerSlot           uint16                     `json:"player_slot"`
	DeployedObjectID     uint32                     `json:"deployed_object_id"`
	PendingPacketCount   int                        `json:"pending_packet_count"`
	IsPendingOverflow    bool                       `json:"is_pending_overflow"`
	PendingPackets       []PendingPacketState       `json:"pending_packets,omitempty"`
	PlayerControl        PlayerControlState         `json:"player_control"`
	Cooldowns            []CooldownState            `json:"cooldowns,omitempty"`
	ActionSchedules      []ActionScheduleState      `json:"action_schedules,omitempty"`
	Deaths               []DeathState               `json:"deaths,omitempty"`
	Objectives           []ObjectiveState           `json:"objectives,omitempty"`
	TimelineKeys         []string                   `json:"timeline_keys,omitempty"`
	TimelineTasks        []TimelineTaskState        `json:"timeline_tasks,omitempty"`
	Randoms              []RandomState              `json:"randoms,omitempty"`
	Squad                SquadState                 `json:"squad"`
	CrystalInventory     CrystalInventoryState      `json:"crystal_inventory"`
	AbilityRuntimes      []AbilityRuntimeState      `json:"ability_runtimes,omitempty"`
	ZoneElapsedMS        int64                      `json:"zone_elapsed_ms,omitempty"`
	ZoneCompletionID     uint64                     `json:"zone_completion_id,omitempty"`
	ZoneLevel            string                     `json:"zone_level,omitempty"`
	ZoneDifficulty       uint32                     `json:"zone_difficulty,omitempty"`
	ZoneChainLevelIndex  uint32                     `json:"zone_chain_level_index,omitempty"`
	ZoneMemberLimit      uint16                     `json:"zone_member_limit,omitempty"`
	ClearedSpawnGroupIDs []uint32                   `json:"cleared_spawn_group_ids,omitempty"`
	IsPopulationPrimed   bool                       `json:"is_population_primed"`
	IsZoneRestored       bool                       `json:"is_zone_restored"`
	IsObjectiveComplete  bool                       `json:"is_objective_complete"`
	ZoneMembers          []ZoneMemberState          `json:"zone_members,omitempty"`
	DirectorPublications []DirectorPublicationState `json:"director_publications,omitempty"`
	EncounterStages      []EncounterStageState      `json:"encounter_stages,omitempty"`
	Hordes               []HordeState               `json:"hordes,omitempty"`
	Boss                 *BossState                 `json:"boss,omitempty"`
	Security             *SecurityState             `json:"security,omitempty"`
	Interactables        []InteractableState        `json:"interactables,omitempty"`
	Objects              []ObjectState              `json:"objects"`
	Modifiers            []ModifierState            `json:"modifiers,omitempty"`
}

// PendingPacketState preserves one gameplay packet that exists in server
// output state but has not yet reached the observed RakNet transport boundary.
type PendingPacketState struct {
	BatchID       uint64 `json:"batch_id"`
	PacketIndex   int    `json:"packet_index"`
	PacketID      uint8  `json:"packet_id,omitempty"`
	PacketName    string `json:"packet_name,omitempty"`
	ObjectID      uint32 `json:"object_id,omitempty"`
	PayloadSize   int    `json:"payload_size"`
	PayloadSHA256 string `json:"payload_sha256,omitempty"`
	PayloadHex    string `json:"payload_hex,omitempty"`
	IsCritical    bool   `json:"is_critical"`
}

// StateFrame is an authoritative server keyframe captured with an incident.
type StateFrame struct {
	CapturedAt time.Time      `json:"captured_at"`
	Actor      Actor          `json:"actor"`
	Sessions   []SessionState `json:"sessions"`
}

// StateProvider supplies gameplay-owned state without exposing internal
// aggregates to the diagnostic writer.
type StateProvider interface {
	SyncSnapshot(context.Context, StateRequest) (StateFrame, error)
}

// Notice is one automatic incident report suitable for an in-game chat echo.
type Notice struct {
	Actor   Actor
	Message string
}

// Notifier publishes automatic incident results to an active player.
type Notifier interface {
	NotifySnapshot(context.Context, Notice) error
}
