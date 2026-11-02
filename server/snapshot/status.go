package snapshot

// HeroSpeedState separates the equipment baseline, live bonuses and debuffs
// from the expected native slow attributes. PlayerControl carries motion speed.
type HeroSpeedState struct {
	ObjectID                uint32  `json:"object_id"`
	EquipmentAttackSpeed    float32 `json:"equipment_attack_speed"`
	EquipmentMovementSpeed  float32 `json:"equipment_movement_speed"`
	PassiveMovementIncrease float32 `json:"passive_movement_increase"`
	DebuffAttackSpeed       float32 `json:"debuff_attack_speed"`
	DebuffMovementSpeed     float32 `json:"debuff_movement_speed"`
	ExpectedSlowAttribute23 float32 `json:"expected_slow_attribute_23"`
	ExpectedSlowAttribute48 float32 `json:"expected_slow_attribute_48"`
}

// DamageTickState captures enemy DOT runs separately from the shared modifier
// inventory. Duration is the configured duration, not a remaining-time estimate.
type DamageTickState struct {
	ModifierName            string  `json:"modifier_name"`
	GUID                    uint32  `json:"guid"`
	InstanceID              uint32  `json:"instance_id"`
	SourceObjectID          uint32  `json:"source_object_id"`
	TargetObjectID          uint32  `json:"target_object_id"`
	AbilityName             string  `json:"ability_name"`
	Revision                uint64  `json:"revision"`
	StackCount              uint32  `json:"stack_count"`
	MinimumTickDamage       float32 `json:"minimum_tick_damage"`
	MaximumTickDamage       float32 `json:"maximum_tick_damage"`
	TickDurationMS          int64   `json:"tick_duration_ms"`
	DurationMS              int64   `json:"duration_ms"`
	DamageType              uint8   `json:"damage_type"`
	DamageSource            uint8   `json:"damage_source"`
	IsInModifierInventory   bool    `json:"is_in_modifier_inventory"`
	IsTransferGUIDSupported bool    `json:"is_transfer_guid_supported"`
}

// GeyserState preserves fixture admission and presentation bookkeeping. A
// recorded cycle means the server prepared it, not that the client rendered it.
type GeyserState struct {
	MarkerID          uint32     `json:"marker_id"`
	ObjectID          uint32     `json:"object_id"`
	Position          [3]float32 `json:"position"`
	Phase             string     `json:"phase"`
	Cycle             uint64     `json:"cycle"`
	WarningCycle      uint64     `json:"warning_cycle"`
	SpoutCycle        uint64     `json:"spout_cycle"`
	IsActive          bool       `json:"is_active"`
	IsWarningRecorded bool       `json:"is_warning_recorded"`
	IsSpoutRecorded   bool       `json:"is_spout_recorded"`
}
