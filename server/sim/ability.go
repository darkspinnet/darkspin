package sim

import (
	"errors"
	"fmt"
	"time"
)

type AbilityKind string

type AbilityStatusKind string

type ProjectileBurstTargeting string

type MeleeHitPolicy struct {
	EffectName     string
	ModifierID     uint32
	ModifierChance uint32
}

// MeleeDamagePolicy describes one independently selected weapon-damage range
// and its paired impact presentation for authored variable-damage basics.
type MeleeDamagePolicy struct {
	MinimumMultiplier float32
	MaximumMultiplier float32
	EffectName        string
}

const (
	AbilityKindMelee            AbilityKind = "melee"
	AbilityKindProjectile       AbilityKind = "projectile"
	AbilityKindProjectileBurst  AbilityKind = "projectile_burst"
	AbilityKindCursorArea       AbilityKind = "cursor_area"
	AbilityKindPointBlank       AbilityKind = "point_blank"
	AbilityKindCone             AbilityKind = "cone"
	AbilityKindModifier         AbilityKind = "modifier"
	AbilityKindModifierArea     AbilityKind = "modifier_area"
	AbilityKindChannelArea      AbilityKind = "channel_area"
	AbilityKindSummonBuff       AbilityKind = "summon_buff"
	AbilityKindTrap             AbilityKind = "trap"
	AbilityKindToss             AbilityKind = "toss"
	AbilityKindCloudLob         AbilityKind = "cloud_lob"
	AbilityKindAreaHealing      AbilityKind = "area_healing"
	AbilityKindTargetedAOE      AbilityKind = "targeted_aoe"
	AbilityKindChannelDrain     AbilityKind = "channel_drain"
	AbilityKindTimedArea        AbilityKind = "timed_area"
	AbilityKindStatusArea       AbilityKind = "status_area"
	AbilityKindInfection        AbilityKind = "infection"
	AbilityKindHealingTicks     AbilityKind = "healing_ticks"
	AbilityKindAuraArea         AbilityKind = "aura_area"
	AbilityKindProjectileStatus AbilityKind = "projectile_status"
	AbilityKindCharge           AbilityKind = "charge"
	AbilityKindTeleportArea     AbilityKind = "teleport_area"
	AbilityKindRepulsion        AbilityKind = "repulsion"
	AbilityKindQuantumBlink     AbilityKind = "quantum_blink"
	AbilityKindReactiveSummon   AbilityKind = "reactive_summon"
	AbilityKindInteractable     AbilityKind = "interactable"
	AbilityKindTeleportStrike   AbilityKind = "teleport_strike"
)

const (
	AbilityStatusKindFear                  AbilityStatusKind = "fear"
	AbilityStatusKindBanish                AbilityStatusKind = "banish"
	AbilityStatusKindCurse                 AbilityStatusKind = "curse"
	AbilityStatusKindSilence               AbilityStatusKind = "silence"
	AbilityStatusKindSleep                 AbilityStatusKind = "sleep"
	AbilityStatusKindSlow                  AbilityStatusKind = "slow"
	AbilityStatusKindStun                  AbilityStatusKind = "stun"
	AbilityStatusKindTaunt                 AbilityStatusKind = "taunt"
	AbilityStatusKindPhysicalVulnerability AbilityStatusKind = "physical_vulnerability"
)

const (
	ProjectileBurstTargetingRetargetChannel ProjectileBurstTargeting = "retarget_channel"
	ProjectileBurstTargetingCursorArea      ProjectileBurstTargeting = "cursor_area"
	ProjectileBurstTargetingRadial          ProjectileBurstTargeting = "radial"
	ProjectileBurstTargetingArc             ProjectileBurstTargeting = "arc"
)

type TossAbilityBehavior string

const (
	TossAbilityBehaviorTrapper TossAbilityBehavior = "trapper"
	TossAbilityBehaviorVoodoo  TossAbilityBehavior = "voodoo"
)

type TossAbilityDefinition struct {
	Behavior                  TossAbilityBehavior
	NearAnimationName         string
	FarAnimationName          string
	ProjectileNoun            string
	ProjectileEffectName      string
	ImpactEffectName          string
	StrongImpactEffectName    string
	CloseRange                float32
	Height                    float32
	CloseHeight               float32
	FlightTime                time.Duration
	Speed                     float32
	CloseSpeed                float32
	Radius                    float32
	Angle                     float32
	MinimumDamagePercent      float32
	DamageMultiplier          float32
	CurseFearDamageMultiplier float32
	BounceRange               float32
	BounceCount               uint32
	BounceRestitution         float32
	CloseBounceRestitution    float32
	Offset                    Position
	CloseOffset               Position
	MaximumTargetCount        uint32
	GrenadeTimer              time.Duration
	IsMaximumTargetCountFound bool
	IsAlwaysTargetFarRange    bool
	IsStrikesGroundOnly       bool
	IsStopBounceOnCreatures   bool
	IsCloseOffsetXFound       bool
	IsCloseOffsetYFound       bool
	IsCloseOffsetZFound       bool
}

type CloudLobAbilityDefinition struct {
	ProjectileNoun         string
	CloudNoun              string
	ProjectileEffectName   string
	ImpactEffectName       string
	CloudEffectName        string
	CloudPulseEffectName   string
	CloudHitEffectName     string
	PoisonStatusEffectName string
	LeftOffset             Position
	RightOffset            Position
	FlightTime             time.Duration
	Height                 float32
	CloudRadius            float32
	CloudDuration          time.Duration
	TickDuration           time.Duration
	TickCount              uint32
	PoisonDamageType       uint32
	PoisonDamageSource     uint32
}

// LightningSecondaryAbilityDefinition is Electron Sphere's projectile-owned
// secondary scan. These operands come from the registered root ability rather
// than the shared projectile template.
type LightningSecondaryAbilityDefinition struct {
	MinimumDamage     float32
	MaximumDamage     float32
	DamageCoefficient float32
	Radius            float32
	BaseDelay         time.Duration
	RandomDelay       time.Duration
	Chance            float32
	MaximumCandidates uint32
	EffectName        string
	HitEffectName     string
}

// AbilityDefinition is the rank-one authored projection of a packaged Lua
// ability registration. It contains content data, not runtime combat state.
type AbilityDefinition struct {
	IsFacingSuppressed              bool
	IsFacingPolicyKnown             bool
	Name                            string
	Namespace                       string
	Kind                            AbilityKind
	Cooldown                        time.Duration
	Range                           float32
	AnimationName                   string
	OutAnimationName                string
	AnimationNames                  []string
	AnimationProjectileOffsets      [][]Position
	AnimationProjectileAngles       [][]float32
	AnimationAdditionalTargetCounts []uint32
	AnimationDamageMultipliers      []float32
	RandomMeleeDamagePolicies       []MeleeDamagePolicy
	AdditionalTargetRange           float32
	AdditionalTargetAngle           float32
	HitDelay                        time.Duration
	TeleportDelay                   time.Duration
	HitDelays                       []time.Duration
	AnimationHitDelays              [][]time.Duration
	IsMultiHitAnimation             bool
	ReleaseDelay                    time.Duration
	ReleaseDelays                   []time.Duration
	MinimumDamage                   float32
	MaximumDamage                   float32
	IsWeaponDamageRange             bool
	SecondaryMinimumDamage          float32
	SecondaryMaximumDamage          float32
	SecondaryDamageCoefficient      float32
	MinimumDamagePerTick            float32
	MaximumDamagePerTick            float32
	DamageCoefficient               float32
	DescriptorMask                  uint32
	DamageType                      uint32
	DamageSource                    uint32
	IsDescriptorFound               bool
	IsDamageTypeFound               bool
	IsDamageSourceFound             bool
	HitEffectName                   string
	MeleeHitPolicies                []MeleeHitPolicy
	Distance                        float32
	Speed                           float32
	Acceleration                    float32
	DamagePerSpeedUnit              float32
	MovementSpeedMultiplier         float32
	Radius                          float32
	IsAreaRadiusScaled              bool
	IsAreaDurationScaled            bool
	Angle                           float32
	MinimumDamagePercent            float32
	IsFixedDamageScale              bool
	LargeTargetDamageMultiplier     float32
	TrailEffectName                 string
	ProjectileNoun                  string
	ImpactEffectName                string
	MissEffectName                  string
	IsTrackBetweenShots             bool
	IsAlwaysUseCursorPosition       bool
	IsInitialPulse                  bool
	IsInitialTargetOnly             bool
	IsTargeted                      bool
	IsShouldPursue                  bool
	BonusDamageMultiplier           float32
	ManaCost                        float32
	ManaCoefficient                 float32
	Duration                        time.Duration
	TickDuration                    time.Duration
	MinimumHealingPerTick           float32
	MaximumHealingPerTick           float32
	MinimumFinalHealing             float32
	MaximumFinalHealing             float32
	HealingCoefficient              float32
	DamageHealingFraction           float32
	HealingRadius                   float32
	LifeSteal                       float32
	NumberOfTicks                   uint32
	ShotCount                       uint32
	FiringRate                      time.Duration
	FiringRateRandomness            time.Duration
	ShotHitChance                   float32
	BurstTargeting                  ProjectileBurstTargeting
	TimeToDestroyBuffs              time.Duration
	EffectNouns                     []string
	MuzzleEffectName                string
	RootModifierID                  uint32
	SecondaryModifierID             uint32
	SpreadModifierID                uint32
	SecondaryAnimationName          string
	RootChance                      float32
	SpawnNoun                       string
	IsForwardPlacement              bool
	PlacementDistance               float32
	AbsorbEffectName                string
	HealEffectName                  string
	GrowthEffectNames               []string
	GraphicsState                   uint32
	ActivationEffectName            string
	AllyEffectName                  string
	EnemyEffectName                 string
	LootDelay                       time.Duration
	FinalWaitDelay                  time.Duration
	IsLootDrop                      bool
	IsCrystalDrop                   bool
	IsOrbDrop                       bool
	IsNoGlobalCooldown              bool
	StatusKind                      AbilityStatusKind
	StatusDuration                  time.Duration
	StatusMovementScale             float32
	StatusAttackScale               float32
	StatusDamageBuff                float32
	StatusDamageReduction           float32
	StatusPhysicalIncrease          float32
	StatusEnergyIncrease            float32
	Toss                            TossAbilityDefinition
	CloudLob                        CloudLobAbilityDefinition
	LightningSecondary              LightningSecondaryAbilityDefinition
	Provenance                      Provenance
}

func abilityDefinitionFromLua(table *luaTable, provenance Provenance) (AbilityDefinition, error) {
	if table == nil {
		return AbilityDefinition{}, errors.New("nil ability table")
	}
	definition := AbilityDefinition{Name: provenance.FunctionName, Provenance: provenance}
	if definition.Name == "" {
		return AbilityDefinition{}, errors.New("empty registration name")
	}
	var err error
	definition.Namespace, err = luaAbilityString(table, "namespace")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("namespace: %w", err)
	}
	definition.IsNoGlobalCooldown, err = luaAbilityBoolean(table, "noGlobalCooldown")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("noGlobalCooldown: %w", err)
	}
	definition.IsShouldPursue, err = luaAbilityBoolean(table, "shouldPursue")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("shouldPursue: %w", err)
	}
	if table.has(stringLuaKey("cooldown")) {
		cooldown, cooldownErr := luaAbilityRankNumber(table, "cooldown", 1)
		if cooldownErr != nil {
			return AbilityDefinition{}, fmt.Errorf("cooldown: %w", cooldownErr)
		}
		definition.Cooldown = luaSeconds(cooldown)
	} else if !definition.IsNoGlobalCooldown {
		return AbilityDefinition{}, errors.New("cooldown: missing")
	}
	if table.has(stringLuaKey("range")) {
		rangeAmount, rangeErr := luaAbilityRankNumber(table, "range", 1)
		if rangeErr != nil {
			return AbilityDefinition{}, fmt.Errorf("range: %w", rangeErr)
		}
		definition.Range = float32(rangeAmount)
	}
	if table.has(stringLuaKey("manaCost")) {
		manaCost, manaErr := luaAbilityRankNumber(table, "manaCost", 1)
		if manaErr != nil {
			return AbilityDefinition{}, fmt.Errorf("manaCost: %w", manaErr)
		}
		definition.ManaCost = float32(manaCost)
	}
	if table.has(stringLuaKey("manaCoefficient")) {
		manaCoefficient, manaCoefficientErr := luaAbilityRankNumber(table, "manaCoefficient", 1)
		if manaCoefficientErr != nil {
			return AbilityDefinition{}, fmt.Errorf("manaCoefficient: %w", manaCoefficientErr)
		}
		definition.ManaCoefficient = float32(manaCoefficient)
	}
	if table.has(stringLuaKey("nearAnimation")) && table.has(stringLuaKey("farAnimation")) {
		definition, err = tossAbilityDefinitionFromLua(table, definition)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("toss: %w", err)
		}
		return definition, nil
	}
	var hitDelay []float64
	var animationHitDelay [][]float64
	var releaseDelay []float64
	animationSequence := table.get(stringLuaKey("animationSequence"))
	if animationSequence.kind == luaTableValue && animationSequence.table != nil {
		definition.AnimationNames, animationHitDelay, releaseDelay, err =
			luaAbilityAnimationSequence(animationSequence.table)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("animationSequence: %w", err)
		}
		hitDelay = make([]float64, len(animationHitDelay))
		definition.AnimationHitDelays = make([][]time.Duration, len(animationHitDelay))
		for animationIndex, animationHit := range animationHitDelay {
			hitDelay[animationIndex] = animationHit[0]
			definition.AnimationHitDelays[animationIndex] = make([]time.Duration, len(animationHit))
			for hitIndex, delay := range animationHit {
				definition.AnimationHitDelays[animationIndex][hitIndex] = luaSeconds(delay)
			}
			definition.IsMultiHitAnimation = definition.IsMultiHitAnimation || len(animationHit) > 1
		}
	} else {
		animationKey := "animationstate"
		if !table.has(stringLuaKey(animationKey)) && table.has(stringLuaKey("cast_animation")) {
			animationKey = "cast_animation"
		}
		if table.has(stringLuaKey(animationKey)) {
			definition.AnimationNames, err = luaAbilityStrings(table, animationKey)
			if err != nil {
				return AbilityDefinition{}, fmt.Errorf("animation: %w", err)
			}
		} else {
			// Some passive-owned projectiles, including SentryDroneLaser, use
			// their muzzle event for presentation and author no cast animation.
			definition.AnimationNames = []string{""}
		}
		hitDelayKey := "timetohit"
		if !table.has(stringLuaKey(hitDelayKey)) && table.has(stringLuaKey("timeToHit")) {
			hitDelayKey = "timeToHit"
		}
		if table.has(stringLuaKey(hitDelayKey)) {
			hitDelay, err = luaAbilityRankNumbers(table, hitDelayKey, 1)
			if err != nil {
				return AbilityDefinition{}, fmt.Errorf("hitDelay: %w", err)
			}
		} else {
			hitDelay = []float64{0}
		}
		if table.has(stringLuaKey("timetorelease")) {
			release, releaseErr := luaAbilityRankNumber(table, "timetorelease", 1)
			if releaseErr != nil {
				return AbilityDefinition{}, fmt.Errorf("releaseDelay: %w", releaseErr)
			}
			releaseDelay = []float64{release}
		} else {
			releaseDelay = []float64{0}
		}
	}
	definition.AnimationName = definition.AnimationNames[0]
	definition.HitDelays = make([]time.Duration, len(hitDelay))
	if animationSequence.kind == luaTableValue {
		for index, delay := range hitDelay {
			definition.HitDelays[index] = luaSeconds(delay)
		}
	} else {
		var cumulativeHitDelay time.Duration
		for index, delay := range hitDelay {
			cumulativeHitDelay += luaSeconds(delay)
			definition.HitDelays[index] = cumulativeHitDelay
		}
	}
	definition.HitDelay = definition.HitDelays[0]
	definition.ReleaseDelays = make([]time.Duration, len(releaseDelay))
	for index, delay := range releaseDelay {
		definition.ReleaseDelays[index] = luaSeconds(delay)
	}
	definition.ReleaseDelay = definition.ReleaseDelays[0]
	if table.has(stringLuaKey("damage")) {
		definition.MinimumDamage, definition.MaximumDamage, err = luaAbilityDamage(table, 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("damage: %w", err)
		}
	}
	if table.has(stringLuaKey("damageCoefficient")) {
		definition.DamageCoefficient, err = luaAbilityRankFloat32(table, "damageCoefficient", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("damageCoefficient: %w", err)
		}
	}
	definition.DescriptorMask, definition.IsDescriptorFound, err =
		luaAbilityOptionalUint32(table, "descriptors")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("descriptor: %w", err)
	}
	definition.DamageType, definition.IsDamageTypeFound, err =
		luaAbilityOptionalUint32(table, "damageType")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("damageType: %w", err)
	}
	definition.DamageSource, definition.IsDamageSourceFound, err =
		luaAbilityOptionalUint32(table, "damageSource")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("damageSource: %w", err)
	}
	if table.has(stringLuaKey("angle")) && table.has(stringLuaKey("radius")) &&
		(definition.MinimumDamage > 0 || definition.MaximumDamage > 0) {
		definition.Kind = AbilityKindCone
		definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("coneRadius: %w", err)
		}
		definition.Angle, err = luaAbilityRankFloat32(table, "angle", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("coneAngle: %w", err)
		}
		definition.MinimumDamagePercent, _, err = luaAbilityOptionalRankFloat32(
			table, "minimumDamagePercent", 1,
		)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("coneMinimumDamage: %w", err)
		}
		return definition, nil
	}
	if table.has(stringLuaKey("timeToDestroyBuffs")) {
		definition.Kind = AbilityKindModifierArea
		definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("modifierAreaRadius: %w", err)
		}
		duration, durationErr := luaAbilityRankNumber(table, "timeToDestroyBuffs", 1)
		if durationErr != nil {
			return AbilityDefinition{}, fmt.Errorf("modifierAreaDuration: %w", durationErr)
		}
		definition.TimeToDestroyBuffs = luaSeconds(duration)
		definition.AllyEffectName, err = luaAbilityString(table, "allyHit")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("modifierAreaAllyEffect: %w", err)
		}
		definition.EnemyEffectName, err = luaAbilityString(table, "enemyHit")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("modifierAreaEnemyEffect: %w", err)
		}
		return definition, nil
	}
	if table.has(stringLuaKey("modifier")) &&
		table.has(stringLuaKey("effectModifier")) &&
		table.has(stringLuaKey("slam_animation")) {
		definition.Kind = AbilityKindChannelArea
		definition.RootModifierID, _, err = luaAbilityOptionalUint32(table, "modifier")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("channelModifier: %w", err)
		}
		definition.SecondaryModifierID, _, err =
			luaAbilityOptionalUint32(table, "effectModifier")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("channelEffectModifier: %w", err)
		}
		definition.SecondaryAnimationName, err = luaAbilityString(table, "slam_animation")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("channelSlamAnimation: %w", err)
		}
		definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("channelRadius: %w", err)
		}
		return definition, nil
	}
	if table.has(stringLuaKey("modifier")) &&
		table.has(stringLuaKey("enrageAnimationState")) {
		definition.Kind = AbilityKindSummonBuff
		definition.RootModifierID, _, err = luaAbilityOptionalUint32(table, "modifier")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("summonBuffModifier: %w", err)
		}
		definition.SecondaryAnimationName, err =
			luaAbilityString(table, "enrageAnimationState")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("summonBuffAnimation: %w", err)
		}
		return definition, nil
	}
	// Ride the Lightning authors a modifier for its transit effect, but its
	// out-animation identifies the complete ability as a teleport strike. Keep
	// this narrower shape ahead of the generic self-modifier fallback.
	if table.has(stringLuaKey("outanimationstate")) && table.has(stringLuaKey("modifier")) {
		definition.OutAnimationName, err = luaAbilityString(table, "outanimationstate")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("outAnimation: %w", err)
		}
		definition.Kind = AbilityKindTeleportStrike
		return definition, nil
	}
	if table.has(stringLuaKey("modifier")) &&
		!table.has(stringLuaKey("distance")) &&
		!table.has(stringLuaKey("object")) {
		definition.Kind = AbilityKindModifier
		definition.RootModifierID, _, err = luaAbilityOptionalUint32(table, "modifier")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("modifierID: %w", err)
		}
		if table.has(stringLuaKey("effect")) {
			definition.ActivationEffectName, err = luaAbilityString(table, "effect")
			if err != nil {
				return AbilityDefinition{}, fmt.Errorf("modifierEffect: %w", err)
			}
		}
		return definition, nil
	}
	if definition.Name == "Sprout" {
		definition, err = cloudLobAbilityDefinitionFromLua(table, definition)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("cloudLob: %w", err)
		}
		return definition, nil
	}
	if table.has(stringLuaKey("gfxstate")) {
		definition.Kind = AbilityKindInteractable
		graphicsState := table.get(stringLuaKey("gfxstate"))
		if graphicsState.kind != luaNumber || graphicsState.number <= 0 ||
			graphicsState.number > float64(^uint32(0)) {
			return AbilityDefinition{}, fmt.Errorf("graphicsStateKind: %s", graphicsState.kind)
		}
		definition.GraphicsState = uint32(graphicsState.number)
		definition.ActivationEffectName, err = luaAbilityString(table, "effect")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("activationEffect: %w", err)
		}
		if table.has(stringLuaKey("timeUntilLootDrops")) {
			lootDelay, lootDelayErr := luaAbilityRankNumber(table, "timeUntilLootDrops", 1)
			if lootDelayErr != nil {
				return AbilityDefinition{}, fmt.Errorf("lootDelay: %w", lootDelayErr)
			}
			definition.LootDelay = luaSeconds(lootDelay)
		}
		finalWaitDelay, finalWaitErr := luaAbilityRankNumber(table, "finalWaitTime", 1)
		if finalWaitErr != nil {
			return AbilityDefinition{}, fmt.Errorf("finalWaitDelay: %w", finalWaitErr)
		}
		definition.FinalWaitDelay = luaSeconds(finalWaitDelay)
		switch definition.Name {
		case "InteractWithObelisk":
			definition.IsLootDrop = true
			definition.IsCrystalDrop = true
		case "InteractHealthObelisk":
			definition.IsOrbDrop = true
		default:
			return AbilityDefinition{}, fmt.Errorf("interactableDrop: %s", definition.Name)
		}
		return definition, nil
	}

	if table.has(stringLuaKey("treeNoun")) {
		definition.Kind = AbilityKindAreaHealing
		definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("healingRadius: %w", err)
		}
		duration, durationErr := luaAbilityRankNumber(table, "abilityDuration", 1)
		if durationErr != nil {
			return AbilityDefinition{}, fmt.Errorf("healingDuration: %w", durationErr)
		}
		definition.Duration = luaSeconds(duration)
		tickDuration, tickErr := luaAbilityRankNumber(table, "tickDuration", 1)
		if tickErr != nil {
			return AbilityDefinition{}, fmt.Errorf("healingTickDuration: %w", tickErr)
		}
		definition.TickDuration = luaSeconds(tickDuration)
		definition.MinimumHealingPerTick, definition.MaximumHealingPerTick, err =
			luaAbilityRankInterval(table, "healingpertick", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("healingPerTick: %w", err)
		}
		definition.MinimumFinalHealing, definition.MaximumFinalHealing, err =
			luaAbilityRankInterval(table, "healing", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("totalHealing: %w", err)
		}
		definition.HealingCoefficient, err = luaAbilityRankFloat32(table, "healingCoefficient", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("healingCoefficient: %w", err)
		}
		definition.SpawnNoun, err = luaAbilityString(table, "treeNoun")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("treeNoun: %w", err)
		}
		definition.AbsorbEffectName, err = luaAbilityString(table, "absorbEvent")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("absorbEvent: %w", err)
		}
		definition.HealEffectName, err = luaAbilityString(table, "healEvent")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("healEvent: %w", err)
		}
		definition.GrowthEffectNames, err = luaAbilityStrings(table, "growthEvents")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("growthEvents: %w", err)
		}
		return definition, nil
	}

	if table.has(stringLuaKey("numticks")) {
		definition.Kind = AbilityKindTargetedAOE
		definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("targetedRadius: %w", err)
		}
		tickDuration, tickErr := luaAbilityRankNumber(table, "tickDuration", 1)
		if tickErr != nil {
			return AbilityDefinition{}, fmt.Errorf("targetedTickDuration: %w", tickErr)
		}
		definition.TickDuration = luaSeconds(tickDuration)
		numberOfTicks, tickCountErr := luaAbilityRankNumber(table, "numticks", 1)
		if tickCountErr != nil {
			return AbilityDefinition{}, fmt.Errorf("targetedTickCount: %w", tickCountErr)
		}
		if numberOfTicks <= 0 || numberOfTicks > float64(^uint32(0)) ||
			float64(uint32(numberOfTicks)) != numberOfTicks {
			return AbilityDefinition{}, errors.New("targetedTickCount: invalid")
		}
		definition.NumberOfTicks = uint32(numberOfTicks)
		var isDamagePerTickFound bool
		definition.MinimumDamagePerTick, definition.MaximumDamagePerTick,
			isDamagePerTickFound, err = luaAbilityOptionalRankInterval(
			table, "damagepertick", 1,
		)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("targetedDamagePerTick: %w", err)
		}
		var isHealingPerTickFound bool
		definition.MinimumHealingPerTick, definition.MaximumHealingPerTick,
			isHealingPerTickFound, err = luaAbilityOptionalRankInterval(
			table, "healingpertick", 1,
		)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("targetedHealingPerTick: %w", err)
		}
		definition.HealingCoefficient, _, err = luaAbilityOptionalRankFloat32(
			table, "healingCoefficient", 1,
		)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("targetedHealingCoefficient: %w", err)
		}
		if table.has(stringLuaKey("effectObject")) {
			effectNoun, effectErr := luaAbilityRankString(table, "effectObject", 1)
			if effectErr != nil {
				return AbilityDefinition{}, fmt.Errorf("targetedEffectObject: %w", effectErr)
			}
			definition.EffectNouns = []string{effectNoun}
		}
		if table.has(stringLuaKey("muzzleEffect")) {
			definition.MuzzleEffectName, err = luaAbilityString(table, "muzzleEffect")
			if err != nil {
				return AbilityDefinition{}, fmt.Errorf("targetedMuzzleEffect: %w", err)
			}
		}
		definition.RootModifierID, _, err = luaAbilityOptionalUint32(table, "rootModifier")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("targetedRootModifier: %w", err)
		}
		definition.RootChance, _, err = luaAbilityOptionalRankFloat32(table, "rootChance", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("targetedRootChance: %w", err)
		}
		if !isDamagePerTickFound && !isHealingPerTickFound &&
			definition.MinimumDamage == 0 && definition.MaximumDamage == 0 &&
			definition.RootModifierID == 0 {
			return AbilityDefinition{}, errors.New("targetedPayload: missing")
		}
		return definition, nil
	}
	definition.IsAlwaysUseCursorPosition, err = luaAbilityBoolean(table, "alwaysUseCursorPos")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("alwaysUseCursorPosition: %w", err)
	}
	if definition.IsAlwaysUseCursorPosition {
		definition.Kind = AbilityKindCursorArea
		definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("cursorAreaRadius: %w", err)
		}
		definition.HitEffectName, err = luaAbilityString(table, "hitEvent")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("cursorAreaHitEvent: %w", err)
		}
		return definition, nil
	}
	// The melee template also inherits generic targeted and bonus-damage fields.
	// Prefer its authored hit arc before considering the broader point-blank
	// shape, otherwise basic attacks such as Wraith's Pummel compile as specials.
	isMelee := table.has(stringLuaKey("hitArcLength"))
	if isMelee {
		definition.Kind = AbilityKindMelee
	}

	if !isMelee && table.has(stringLuaKey("targeted")) &&
		table.has(stringLuaKey("bonusDamageMultiplier")) {
		definition.IsTargeted, err = luaAbilityBoolean(table, "targeted")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("pointBlankTargeted: %w", err)
		}
		if definition.IsTargeted {
			return AbilityDefinition{}, errors.New("pointBlankTargeted: true")
		}
		definition.Kind = AbilityKindPointBlank
		definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("pointBlankRadius: %w", err)
		}
		definition.BonusDamageMultiplier, err = luaAbilityRankFloat32(table, "bonusDamageMultiplier", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("pointBlankBonusDamage: %w", err)
		}
		definition.ImpactEffectName, err = luaAbilityString(table, "impactEvent")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("pointBlankImpactEvent: %w", err)
		}
		return definition, nil
	}
	if definition.Name == "DeathsEmbrace" && table.has(stringLuaKey("radius")) {
		definition.Kind = AbilityKindPointBlank
		definition.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("pointBlankRadius: %w", err)
		}
		definition.ImpactEffectName, err = luaAbilityString(table, "debuffEffect")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("pointBlankDebuffEffect: %w", err)
		}
		statusDuration, statusDurationErr := luaAbilityRankNumber(table, "debuffDuration", 1)
		if statusDurationErr != nil {
			return AbilityDefinition{}, fmt.Errorf("pointBlankDebuffDuration: %w", statusDurationErr)
		}
		definition.StatusKind = AbilityStatusKindFear
		definition.StatusDuration = luaSeconds(statusDuration)
		return definition, nil
	}

	if table.has(stringLuaKey("hitEffects")) || table.has(stringLuaKey("hitModifiers")) ||
		table.has(stringLuaKey("modifierChances")) {
		effectName, policyErr := luaAbilityStrings(table, "hitEffects")
		if policyErr != nil {
			return AbilityDefinition{}, fmt.Errorf("meleeHitEffects: %w", policyErr)
		}
		modifierID, policyErr := luaAbilitySequenceNumbers(table, "hitModifiers")
		if policyErr != nil {
			return AbilityDefinition{}, fmt.Errorf("meleeHitModifiers: %w", policyErr)
		}
		modifierChance, policyErr := luaAbilitySequenceNumbers(table, "modifierChances")
		if policyErr != nil {
			return AbilityDefinition{}, fmt.Errorf("meleeModifierChances: %w", policyErr)
		}
		if len(effectName) != len(modifierID) || len(effectName) != len(modifierChance) ||
			len(effectName) == 0 || len(definition.AnimationNames)%len(effectName) != 0 {
			return AbilityDefinition{}, errors.New("melee hit policy: mismatched")
		}
		definition.MeleeHitPolicies = make([]MeleeHitPolicy, len(effectName))
		for index := range effectName {
			chance := modifierChance[index]
			assetID := modifierID[index]
			if chance < 0 || chance > 100 || chance != float64(uint32(chance)) {
				return AbilityDefinition{}, fmt.Errorf("melee modifier chance[%d]: %g", index, chance)
			}
			if assetID <= 0 || assetID > float64(^uint32(0)) || assetID != float64(uint32(assetID)) {
				return AbilityDefinition{}, fmt.Errorf("melee modifier ID[%d]: %g", index, assetID)
			}
			definition.MeleeHitPolicies[index] = MeleeHitPolicy{
				EffectName: effectName[index], ModifierID: uint32(assetID),
				ModifierChance: uint32(chance),
			}
		}
		definition.Kind = AbilityKindMelee
		definition.HitEffectName = definition.MeleeHitPolicies[0].EffectName
		return definition, nil
	}
	hitEffect := table.get(stringLuaKey("hitEffect"))
	if hitEffect.kind == luaString {
		definition.Kind = AbilityKindMelee
		definition.HitEffectName = hitEffect.text
		return definition, nil
	}
	if isMelee {
		return definition, nil
	}
	definition.Kind = AbilityKindProjectile
	if table.has(stringLuaKey("totalShots")) {
		definition.Kind = AbilityKindProjectileBurst
		totalShots, totalShotsErr := luaAbilityRankNumber(table, "totalShots", 1)
		if totalShotsErr != nil {
			return AbilityDefinition{}, fmt.Errorf("totalShots: %w", totalShotsErr)
		}
		if totalShots <= 0 || totalShots > float64(^uint32(0)) ||
			float64(uint32(totalShots)) != totalShots {
			return AbilityDefinition{}, errors.New("totalShots: invalid")
		}
		definition.ShotCount = uint32(totalShots)
		firingRate, firingRateErr := luaAbilityRankNumber(table, "firingRate", 1)
		if firingRateErr != nil {
			return AbilityDefinition{}, fmt.Errorf("firingRate: %w", firingRateErr)
		}
		definition.FiringRate = luaSeconds(firingRate)
		firingRandomness, isRandomnessFound, randomnessErr :=
			luaAbilityOptionalRankFloat32(table, "firingRateRandomness", 1)
		if randomnessErr != nil {
			return AbilityDefinition{}, fmt.Errorf("firingRateRandomness: %w", randomnessErr)
		}
		if isRandomnessFound {
			definition.FiringRateRandomness = luaSeconds(float64(firingRandomness))
		}
		definition.ShotHitChance, _, err = luaAbilityOptionalRankFloat32(
			table, "chanceShotHits", 1,
		)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("chanceShotHits: %w", err)
		}
		if definition.IsAlwaysUseCursorPosition {
			definition.BurstTargeting = ProjectileBurstTargetingCursorArea
		} else {
			definition.BurstTargeting = ProjectileBurstTargetingRetargetChannel
		}
	}
	distance, err := luaAbilityRankNumber(table, "distance", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("distance: %w", err)
	}
	definition.Distance = float32(distance)
	speed, err := luaAbilityRankNumber(table, "speed", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("speed: %w", err)
	}
	definition.Speed = float32(speed)
	acceleration, isAccelerationFound, err := luaAbilityOptionalRankFloat32(
		table, "acceleration", 1,
	)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("acceleration: %w", err)
	}
	if isAccelerationFound {
		definition.Acceleration = acceleration
	}
	damagePerSpeedUnit, isDamagePerSpeedUnitFound, err := luaAbilityOptionalRankFloat32(
		table, "damagePerSpeedUnit", 1,
	)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("damagePerSpeedUnit: %w", err)
	}
	if isDamagePerSpeedUnitFound {
		definition.DamagePerSpeedUnit = damagePerSpeedUnit
	}
	radius, err := luaAbilityRankNumber(table, "radius", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("radius: %w", err)
	}
	definition.Radius = float32(radius)
	if table.has(stringLuaKey("trail")) {
		definition.TrailEffectName, err = luaAbilityString(table, "trail")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("trail: %w", err)
		}
	}
	definition.ProjectileNoun, err = luaAbilityString(table, "object")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("object: %w", err)
	}
	impactEffect := table.get(stringLuaKey("impactEvent"))
	if impactEffect.kind == luaString {
		definition.ImpactEffectName = impactEffect.text
	} else if impactEffect.kind == luaNumber && impactEffect.number == 0 {
		// The shared projectile template uses zero when a specialized ability
		// publishes its authored impact through explodeEvent instead.
		definition.ImpactEffectName, err = luaAbilityString(table, "explodeEvent")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("explodeEvent: %w", err)
		}
	} else {
		return AbilityDefinition{}, fmt.Errorf("impactEventKind: %s", impactEffect.kind)
	}
	onHitEffect := table.get(stringLuaKey("onHitEvent"))
	if onHitEffect.kind == luaString {
		definition.HitEffectName = onHitEffect.text
	} else if onHitEffect.kind == luaNumber && onHitEffect.number == 0 {
		// The shared projectile template uses numeric zero as its absent asset sentinel.
	} else if onHitEffect.kind != luaNil {
		return AbilityDefinition{}, fmt.Errorf("onHitEventKind: %s", onHitEffect.kind)
	}
	missEffect := table.get(stringLuaKey("missEvent"))
	if missEffect.kind == luaString {
		definition.MissEffectName = missEffect.text
	} else if missEffect.kind == luaNumber && missEffect.number == 0 {
		// The shared template uses numeric zero as its absent asset sentinel.
	} else if missEffect.kind != luaNil {
		return AbilityDefinition{}, fmt.Errorf("missEventKind: %s", missEffect.kind)
	}
	if table.get(stringLuaKey("faceTargetOnCreate")).kind != luaNil {
		isFacingEnabled, facingErr := luaAbilityBoolean(table, "faceTargetOnCreate")
		if facingErr != nil {
			return AbilityDefinition{}, fmt.Errorf("faceTargetOnCreate: %w", facingErr)
		}
		definition.IsFacingSuppressed = !isFacingEnabled
		definition.IsFacingPolicyKnown = true
	}
	definition.IsTrackBetweenShots, err = luaAbilityRankBoolean(table, "trackBetweenShots", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("trackBetweenShots: %w", err)
	}
	if definition.Name == "SupportHealerBasic" {
		definition.DamageHealingFraction, err =
			luaAbilityRankFloat32(table, "percentToHeal", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("damageHealingFraction: %w", err)
		}
		if definition.DamageHealingFraction <= 0 ||
			definition.DamageHealingFraction > 1 {
			return AbilityDefinition{}, errors.New("damageHealingFraction: invalid")
		}
		definition.HealingRadius, err = luaAbilityRankFloat32(table, "healRadius", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("healingRadius: %w", err)
		}
		if definition.HealingRadius <= 0 {
			return AbilityDefinition{}, errors.New("healingRadius: invalid")
		}
		definition.HealingCoefficient, err =
			luaAbilityRankFloat32(table, "healCoefficient", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("healingCoefficient: %w", err)
		}
	}
	if definition.Name == "PlasmaRandom_LightningBall" {
		definition, err = lightningSecondaryAbilityDefinitionFromLua(table, definition)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("lightningSecondary: %w", err)
		}
	}
	return definition, nil
}

func lightningSecondaryAbilityDefinitionFromLua(
	table *luaTable, definition AbilityDefinition,
) (AbilityDefinition, error) {
	var err error
	secondary := LightningSecondaryAbilityDefinition{}
	secondary.MinimumDamage, secondary.MaximumDamage, err =
		luaAbilityRankInterval(table, "lightningDamage", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("damage: %w", err)
	}
	secondary.DamageCoefficient, err =
		luaAbilityRankFloat32(table, "lightningDamageCoefficient", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("coefficient: %w", err)
	}
	secondary.Radius, err = luaAbilityRankFloat32(table, "lightningRange", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("radius: %w", err)
	}
	baseDelay, err := luaAbilityRankNumber(table, "lightningTiming", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("baseDelay: %w", err)
	}
	randomDelay, err := luaAbilityRankNumber(table, "lightningRandomTiming", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("randomDelay: %w", err)
	}
	secondary.BaseDelay = luaSeconds(baseDelay)
	secondary.RandomDelay = luaSeconds(randomDelay)
	secondary.Chance, err =
		luaAbilityRankFloat32(table, "lightningRandomChanceExtraTargets", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("chance: %w", err)
	}
	maximumCandidates, err := luaAbilityRankNumber(table, "lightningNumTargets", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("maximumCandidates: %w", err)
	}
	if maximumCandidates <= 0 || maximumCandidates > float64(^uint32(0)) ||
		float64(uint32(maximumCandidates)) != maximumCandidates {
		return AbilityDefinition{}, errors.New("maximumCandidates: invalid")
	}
	secondary.MaximumCandidates = uint32(maximumCandidates)
	secondary.EffectName, err = luaAbilityString(table, "lightningEffect")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("effect: %w", err)
	}
	secondary.HitEffectName, err = luaAbilityString(table, "lightningHitEffect")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("hitEffect: %w", err)
	}
	if secondary.MinimumDamage <= 0 ||
		secondary.MaximumDamage < secondary.MinimumDamage ||
		secondary.DamageCoefficient < 0 || secondary.Radius <= 0 ||
		secondary.BaseDelay <= 0 || secondary.RandomDelay < 0 ||
		secondary.Chance < 0 || secondary.Chance > 1 {
		return AbilityDefinition{}, errors.New("invalid operands")
	}
	definition.LightningSecondary = secondary
	return definition, nil
}

func cloudLobAbilityDefinitionFromLua(
	table *luaTable, definition AbilityDefinition,
) (AbilityDefinition, error) {
	definition.Kind = AbilityKindCloudLob
	// SproutPoison assigns the build-103 nDamageTypes.Life and
	// nDamageSources.Energy enum members in this root chunk.
	definition.CloudLob.PoisonDamageType = 2
	definition.CloudLob.PoisonDamageSource = 1
	var err error
	definition.CloudLob.ProjectileNoun, err = luaAbilityString(table, "projectileObj")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("projectileNoun: %w", err)
	}
	definition.CloudLob.CloudNoun, err = luaAbilityString(table, "jobObj")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("cloudNoun: %w", err)
	}
	effectDestination := map[string]*string{
		"projectile":        &definition.CloudLob.ProjectileEffectName,
		"projectile_impact": &definition.CloudLob.ImpactEffectName,
		"cloud":             &definition.CloudLob.CloudEffectName,
		"cloud_pulse":       &definition.CloudLob.CloudPulseEffectName,
		"cloud_hit":         &definition.CloudLob.CloudHitEffectName,
		"poison_status":     &definition.CloudLob.PoisonStatusEffectName,
	}
	for key, destination := range effectDestination {
		*destination, err = luaAbilityString(table, key)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("%s: %w", key, err)
		}
	}
	definition.CloudLob.LeftOffset.X, err = luaAbilityRankFloat32(table, "projOffsetLeftX", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("leftOffsetX: %w", err)
	}
	definition.CloudLob.LeftOffset.Y, err = luaAbilityRankFloat32(table, "projOffsetLeftY", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("leftOffsetY: %w", err)
	}
	definition.CloudLob.LeftOffset.Z, err = luaAbilityRankFloat32(table, "projOffsetLeftZ", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("leftOffsetZ: %w", err)
	}
	definition.CloudLob.RightOffset.X, err = luaAbilityRankFloat32(table, "projOffsetRightX", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("rightOffsetX: %w", err)
	}
	definition.CloudLob.RightOffset.Y, err = luaAbilityRankFloat32(table, "projOffsetRightY", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("rightOffsetY: %w", err)
	}
	definition.CloudLob.RightOffset.Z, err = luaAbilityRankFloat32(table, "projOffsetRightZ", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("rightOffsetZ: %w", err)
	}
	flightTime, flightTimeErr := luaAbilityRankNumber(table, "flightTime", 1)
	if flightTimeErr != nil {
		return AbilityDefinition{}, fmt.Errorf("flightTime: %w", flightTimeErr)
	}
	definition.CloudLob.FlightTime = luaSeconds(flightTime)
	definition.CloudLob.Height, err = luaAbilityRankFloat32(table, "height", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("height: %w", err)
	}
	definition.CloudLob.CloudRadius, err = luaAbilityRankFloat32(table, "cloudRadius", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("cloudRadius: %w", err)
	}
	cloudDuration, cloudDurationErr := luaAbilityRankNumber(table, "cloudDuration", 1)
	if cloudDurationErr != nil {
		return AbilityDefinition{}, fmt.Errorf("cloudDuration: %w", cloudDurationErr)
	}
	definition.CloudLob.CloudDuration = luaSeconds(cloudDuration)
	tickDuration, tickDurationErr := luaAbilityRankNumber(table, "dotPeriod", 1)
	if tickDurationErr != nil {
		return AbilityDefinition{}, fmt.Errorf("tickDuration: %w", tickDurationErr)
	}
	definition.CloudLob.TickDuration = luaSeconds(tickDuration)
	tickCount, tickCountErr := luaAbilityRankNumber(table, "dotNumTicks", 1)
	if tickCountErr != nil {
		return AbilityDefinition{}, fmt.Errorf("tickCount: %w", tickCountErr)
	}
	if tickCount <= 0 || tickCount > float64(^uint32(0)) || float64(uint32(tickCount)) != tickCount {
		return AbilityDefinition{}, errors.New("tickCount: invalid")
	}
	definition.CloudLob.TickCount = uint32(tickCount)
	return definition, nil
}

func luaAbilityOptionalUint32(table *luaTable, key string) (uint32, bool, error) {
	value := table.get(stringLuaKey(key))
	if value.kind == luaNil {
		return 0, false, nil
	}
	if value.kind != luaNumber || value.number < 0 || value.number > float64(^uint32(0)) ||
		value.number != float64(uint32(value.number)) {
		return 0, false, fmt.Errorf("%s: %g", value.kind, value.number)
	}
	return uint32(value.number), true, nil
}

func tossAbilityDefinitionFromLua(
	table *luaTable, definition AbilityDefinition,
) (AbilityDefinition, error) {
	definition.Kind = AbilityKindToss
	var err error
	definition.Toss.NearAnimationName, err = luaAbilityString(table, "nearAnimation")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("nearAnimation: %w", err)
	}
	definition.Toss.FarAnimationName, err = luaAbilityString(table, "farAnimation")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("farAnimation: %w", err)
	}
	hitDelay, err := luaAbilityRankNumber(table, "timetohit", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("hitDelay: %w", err)
	}
	releaseDelay, err := luaAbilityRankNumber(table, "timetorelease", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("releaseDelay: %w", err)
	}
	definition.AnimationName = definition.Toss.NearAnimationName
	definition.AnimationNames = []string{definition.AnimationName}
	definition.HitDelay = luaSeconds(hitDelay)
	definition.HitDelays = []time.Duration{definition.HitDelay}
	definition.ReleaseDelay = luaSeconds(releaseDelay)
	definition.ReleaseDelays = []time.Duration{definition.ReleaseDelay}
	switch definition.Name {
	case "Trapper_Grenade":
		definition.Toss.Behavior = TossAbilityBehaviorTrapper
		definition.Toss.DamageMultiplier, err = luaAbilityRankFloat32(table, "damageMultiplier", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("damageMultiplier: %w", err)
		}
	case "VoodooTempestBasic":
		definition.Toss.Behavior = TossAbilityBehaviorVoodoo
	default:
		return AbilityDefinition{}, fmt.Errorf("behavior: %s", definition.Name)
	}
	definition.Toss.Radius, err = luaAbilityRankFloat32(table, "radius", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("radius: %w", err)
	}
	definition.Toss.CloseRange, err = luaAbilityRankFloat32(table, "closeRange", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("closeRange: %w", err)
	}
	definition.Toss.Height, err = luaAbilityRankFloat32(table, "height", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("height: %w", err)
	}
	definition.Toss.CloseHeight, _, err = luaAbilityOptionalRankFloat32(table, "closeHeight", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("closeHeight: %w", err)
	}
	flightTime, _, err := luaAbilityOptionalRankFloat32(table, "flightTime", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("flightTime: %w", err)
	}
	definition.Toss.FlightTime = luaSeconds(float64(flightTime))
	definition.Toss.Speed, _, err = luaAbilityOptionalRankFloat32(table, "speed", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("speed: %w", err)
	}
	definition.Toss.CloseSpeed, _, err = luaAbilityOptionalRankFloat32(table, "closeSpeed", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("closeSpeed: %w", err)
	}
	definition.Toss.ProjectileNoun, err = luaAbilityString(table, "object")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("projectileNoun: %w", err)
	}
	definition.Toss.ProjectileEffectName, err = luaAbilityString(table, "projectileFX")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("projectileEffect: %w", err)
	}
	if table.has(stringLuaKey("impactFX")) {
		definition.Toss.ImpactEffectName, err = luaAbilityString(table, "impactFX")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("impactEffect: %w", err)
		}
	}
	if definition.Toss.Behavior == TossAbilityBehaviorVoodoo {
		definition.Toss.ImpactEffectName, err = luaAbilityString(table, "normalImpact")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("normalImpact: %w", err)
		}
		definition.Toss.StrongImpactEffectName, err = luaAbilityString(table, "strongImpact")
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("strongImpact: %w", err)
		}
		definition.Toss.CurseFearDamageMultiplier, err =
			luaAbilityRankFloat32(table, "cursedTargetDamageMultiplier", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("curseFearDamageMultiplier: %w", err)
		}
	}
	definition.Toss.BounceRange, _, err = luaAbilityOptionalRankFloat32(table, "bounceRange", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("bounceRange: %w", err)
	}
	bounceCount, isBounceCountFound, err := luaAbilityOptionalRankFloat32(table, "bounceNum", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("bounceCount: %w", err)
	}
	if isBounceCountFound {
		if bounceCount < 0 || bounceCount > float32(^uint32(0)) || bounceCount != float32(uint32(bounceCount)) {
			return AbilityDefinition{}, errors.New("bounceCount: invalid")
		}
		definition.Toss.BounceCount = uint32(bounceCount)
	}
	definition.Toss.BounceRestitution, _, err =
		luaAbilityOptionalRankFloat32(table, "bounceRestitution", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("bounceRestitution: %w", err)
	}
	definition.Toss.CloseBounceRestitution, _, err =
		luaAbilityOptionalRankFloat32(table, "closeBounceRestitution", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("closeBounceRestitution: %w", err)
	}
	definition.Toss.Offset.X, _, err = luaAbilityOptionalRankFloat32(table, "offsetX", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("offsetX: %w", err)
	}
	definition.Toss.Offset.Y, _, err = luaAbilityOptionalRankFloat32(table, "offsetY", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("offsetY: %w", err)
	}
	definition.Toss.Offset.Z, _, err = luaAbilityOptionalRankFloat32(table, "offsetZ", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("offsetZ: %w", err)
	}
	definition.Toss.CloseOffset.X, definition.Toss.IsCloseOffsetXFound, err =
		luaAbilityOptionalRankFloat32(table, "closeOffsetX", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("closeOffsetX: %w", err)
	}
	definition.Toss.CloseOffset.Y, definition.Toss.IsCloseOffsetYFound, err =
		luaAbilityOptionalRankFloat32(table, "closeOffsetY", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("closeOffsetY: %w", err)
	}
	definition.Toss.CloseOffset.Z, definition.Toss.IsCloseOffsetZFound, err =
		luaAbilityOptionalRankFloat32(table, "closeOffsetZ", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("closeOffsetZ: %w", err)
	}
	maximumTargetCount, isMaximumTargetCountFound, err :=
		luaAbilityOptionalRankFloat32(table, "maxTargets", 1)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("maximumTargetCount: %w", err)
	}
	if isMaximumTargetCountFound {
		if maximumTargetCount < 1 || maximumTargetCount > float32(^uint32(0)) ||
			maximumTargetCount != float32(uint32(maximumTargetCount)) {
			return AbilityDefinition{}, errors.New("maximumTargetCount: invalid")
		}
		definition.Toss.MaximumTargetCount = uint32(maximumTargetCount)
		definition.Toss.IsMaximumTargetCountFound = true
	}
	definition.Toss.IsAlwaysTargetFarRange, err = luaAbilityBoolean(table, "alwaysTargetFarRange")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("alwaysTargetFarRange: %w", err)
	}
	definition.Toss.IsStrikesGroundOnly, err = luaAbilityBoolean(table, "strikesGroundOnly")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("strikesGroundOnly: %w", err)
	}
	definition.Toss.IsStopBounceOnCreatures, err = luaAbilityBoolean(table, "stopBounceOnCreatures")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("stopBounceOnCreatures: %w", err)
	}
	definition.DescriptorMask, definition.IsDescriptorFound, err =
		luaAbilityOptionalUint32(table, "descriptors")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("descriptor: %w", err)
	}
	definition.DamageType, definition.IsDamageTypeFound, err =
		luaAbilityOptionalUint32(table, "damageType")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("damageType: %w", err)
	}
	definition.DamageSource, definition.IsDamageSourceFound, err =
		luaAbilityOptionalUint32(table, "damageSource")
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("damageSource: %w", err)
	}
	if table.has(stringLuaKey("damageCoefficient")) {
		definition.DamageCoefficient, err = luaAbilityRankFloat32(table, "damageCoefficient", 1)
		if err != nil {
			return AbilityDefinition{}, fmt.Errorf("damageCoefficient: %w", err)
		}
	}
	if definition.Toss.Behavior == TossAbilityBehaviorTrapper {
		grenadeTimer, timerErr := luaAbilityRankNumber(table, "grenadeTimer", 1)
		if timerErr != nil {
			return AbilityDefinition{}, fmt.Errorf("grenadeTimer: %w", timerErr)
		}
		definition.Toss.GrenadeTimer = luaSeconds(grenadeTimer)
	}
	return definition, nil
}

func luaAbilityOptionalRankFloat32(
	table *luaTable, key string, rank int,
) (float32, bool, error) {
	if !table.has(stringLuaKey(key)) {
		return 0, false, nil
	}
	number, err := luaAbilityRankNumber(table, key, rank)
	if err != nil {
		return 0, false, fmt.Errorf("rankNumber: %w", err)
	}
	return float32(number), true, nil
}

func luaAbilityOptionalRankInterval(
	table *luaTable, key string, rank int,
) (float32, float32, bool, error) {
	if !table.has(stringLuaKey(key)) {
		return 0, 0, false, nil
	}
	minimum, maximum, err := luaAbilityRankInterval(table, key, rank)
	if err != nil {
		return 0, 0, false, fmt.Errorf("rankInterval: %w", err)
	}
	return minimum, maximum, true, nil
}

func luaAbilityBoolean(table *luaTable, key string) (bool, error) {
	field := table.get(stringLuaKey(key))
	if field.kind == luaNil {
		return false, nil
	}
	if field.kind == luaBoolean {
		return field.isTrue, nil
	}
	if field.kind == luaNumber && (field.number == 0 || field.number == 1) {
		return field.number == 1, nil
	}
	return false, fmt.Errorf("fieldKind: %s", field.kind)
}

func luaAbilityRankFloat32(table *luaTable, key string, rank int) (float32, error) {
	number, err := luaAbilityRankNumber(table, key, rank)
	if err != nil {
		return 0, fmt.Errorf("rankNumber: %w", err)
	}
	return float32(number), nil
}

func luaAbilityRankInterval(table *luaTable, key string, rank int) (float32, float32, error) {
	field := table.get(stringLuaKey(key))
	if field.kind == luaNumber && field.number >= 0 {
		amount := float32(field.number)
		return amount, amount, nil
	}
	if field.kind != luaTableValue || field.table == nil {
		return 0, 0, fmt.Errorf("fieldKind: %s", field.kind)
	}
	rankField := field.table.get(numberLuaKey(float64(rank)))
	if rankField.kind == luaNumber && rankField.number >= 0 {
		amount := float32(rankField.number)
		return amount, amount, nil
	}
	if rankField.kind != luaTableValue || rankField.table == nil {
		return 0, 0, fmt.Errorf("rankKind[%d]: %s", rank, rankField.kind)
	}
	minimum := rankField.table.get(numberLuaKey(1))
	maximum := rankField.table.get(numberLuaKey(2))
	if minimum.kind != luaNumber || maximum.kind != luaNumber ||
		minimum.number < 0 || maximum.number < minimum.number {
		return 0, 0, errors.New("invalid interval")
	}
	return float32(minimum.number), float32(maximum.number), nil
}

func luaAbilityAnimationSequence(table *luaTable) ([]string, [][]float64, []float64, error) {
	animationName := make([]string, 0, 5)
	hitDelay := make([][]float64, 0, 5)
	releaseDelay := make([]float64, 0, 5)
	for index := 1; ; index++ {
		item := table.get(numberLuaKey(float64(index)))
		if item.kind == luaNil {
			break
		}
		if item.kind != luaTableValue || item.table == nil {
			return nil, nil, nil, fmt.Errorf("itemKind[%d]: %s", index, item.kind)
		}
		animation := item.table.get(stringLuaKey("animationstate"))
		hit := item.table.get(stringLuaKey("hit"))
		release := item.table.get(stringLuaKey("release"))
		if animation.kind != luaString || animation.text == "" ||
			release.kind != luaNumber || release.number < 0 {
			return nil, nil, nil, fmt.Errorf("itemFields[%d]: %s/%s/%s", index, animation.kind, hit.kind, release.kind)
		}
		itemHitDelay := make([]float64, 0, 2)
		if hit.kind == luaNumber && hit.number >= 0 {
			itemHitDelay = append(itemHitDelay, hit.number)
		} else if hit.kind == luaTableValue && hit.table != nil {
			for hitIndex := 1; ; hitIndex++ {
				delay := hit.table.get(numberLuaKey(float64(hitIndex)))
				if delay.kind == luaNil {
					break
				}
				if delay.kind != luaNumber || delay.number < 0 ||
					(len(itemHitDelay) != 0 && delay.number <= itemHitDelay[len(itemHitDelay)-1]) {
					return nil, nil, nil, fmt.Errorf("itemHit[%d:%d]: %s", index, hitIndex, delay.kind)
				}
				itemHitDelay = append(itemHitDelay, delay.number)
			}
		}
		if len(itemHitDelay) == 0 || itemHitDelay[len(itemHitDelay)-1] > release.number {
			return nil, nil, nil, fmt.Errorf("itemHit[%d]: invalid", index)
		}
		animationName = append(animationName, animation.text)
		hitDelay = append(hitDelay, itemHitDelay)
		releaseDelay = append(releaseDelay, release.number)
	}
	if len(animationName) == 0 {
		return nil, nil, nil, errors.New("empty animation sequence")
	}
	return animationName, hitDelay, releaseDelay, nil
}

func luaAbilityStrings(table *luaTable, key string) ([]string, error) {
	field := table.get(stringLuaKey(key))
	if field.kind == luaString && field.text != "" {
		return []string{field.text}, nil
	}
	if field.kind != luaTableValue || field.table == nil {
		return nil, fmt.Errorf("fieldKind: %s", field.kind)
	}
	text := make([]string, 0, 5)
	for index := 1; ; index++ {
		item := field.table.get(numberLuaKey(float64(index)))
		if item.kind == luaNil {
			break
		}
		if item.kind != luaString || item.text == "" {
			return nil, fmt.Errorf("itemKind[%d]: %s", index, item.kind)
		}
		text = append(text, item.text)
	}
	if len(text) == 0 {
		return nil, errors.New("empty string sequence")
	}
	return text, nil
}

func luaAbilityRankString(table *luaTable, key string, rank int) (string, error) {
	field := table.get(stringLuaKey(key))
	if field.kind == luaString && field.text != "" {
		return field.text, nil
	}
	if field.kind != luaTableValue || field.table == nil {
		return "", fmt.Errorf("fieldKind: %s", field.kind)
	}
	rankField := field.table.get(numberLuaKey(float64(rank)))
	if rankField.kind != luaString || rankField.text == "" {
		return "", fmt.Errorf("rankKind[%d]: %s", rank, rankField.kind)
	}
	return rankField.text, nil
}

func luaAbilitySequenceNumbers(table *luaTable, key string) ([]float64, error) {
	field := table.get(stringLuaKey(key))
	if field.kind != luaTableValue || field.table == nil {
		return nil, fmt.Errorf("fieldKind: %s", field.kind)
	}
	number := make([]float64, 0, 5)
	for index := 1; ; index++ {
		item := field.table.get(numberLuaKey(float64(index)))
		if item.kind == luaNil {
			break
		}
		if item.kind != luaNumber || item.number < 0 {
			return nil, fmt.Errorf("itemKind[%d]: %s", index, item.kind)
		}
		number = append(number, item.number)
	}
	if len(number) == 0 {
		return nil, errors.New("empty number sequence")
	}
	return number, nil
}

func luaAbilityRankBoolean(table *luaTable, key string, rank int) (bool, error) {
	field := table.get(stringLuaKey(key))
	if field.kind == luaNil {
		return false, nil
	}
	if field.kind == luaTableValue && field.table != nil {
		field = field.table.get(numberLuaKey(float64(rank)))
	}
	if field.kind == luaBoolean {
		return field.isTrue, nil
	}
	if field.kind == luaNumber && (field.number == 0 || field.number == 1) {
		return field.number == 1, nil
	}
	return false, fmt.Errorf("fieldKind: %s", field.kind)
}

func luaAbilityRankNumbers(table *luaTable, key string, rank int) ([]float64, error) {
	field := table.get(stringLuaKey(key))
	if field.kind == luaNumber {
		return []float64{field.number}, nil
	}
	if field.kind != luaTableValue || field.table == nil {
		return nil, fmt.Errorf("fieldKind: %s", field.kind)
	}
	rankField := field.table.get(numberLuaKey(float64(rank)))
	if rankField.kind == luaNumber {
		return []float64{rankField.number}, nil
	}
	if rankField.kind != luaTableValue || rankField.table == nil {
		return nil, fmt.Errorf("rankKind[%d]: %s", rank, rankField.kind)
	}
	delay := make([]float64, 0, 4)
	for index := 1; ; index++ {
		shotField := rankField.table.get(numberLuaKey(float64(index)))
		if shotField.kind == luaNil {
			break
		}
		if shotField.kind != luaNumber || shotField.number < 0 {
			return nil, fmt.Errorf("shotKind[%d]: %s", index, shotField.kind)
		}
		delay = append(delay, shotField.number)
	}
	if len(delay) == 0 {
		return nil, errors.New("empty shot delay")
	}
	return delay, nil
}

func luaAbilityRankNumber(table *luaTable, key string, rank int) (float64, error) {
	field := table.get(stringLuaKey(key))
	if field.kind == luaNumber {
		return field.number, nil
	}
	if field.kind != luaTableValue || field.table == nil {
		return 0, fmt.Errorf("fieldKind: %s", field.kind)
	}
	rankField := field.table.get(numberLuaKey(float64(rank)))
	if rankField.kind != luaNumber {
		return 0, fmt.Errorf("rankKind[%d]: %s", rank, rankField.kind)
	}
	return rankField.number, nil
}

func luaAbilityString(table *luaTable, key string) (string, error) {
	field := table.get(stringLuaKey(key))
	if field.kind != luaString || field.text == "" {
		return "", fmt.Errorf("fieldKind: %s", field.kind)
	}
	return field.text, nil
}

func luaAbilityDamage(table *luaTable, rank int) (float32, float32, error) {
	field := table.get(stringLuaKey("damage"))
	if field.kind != luaTableValue || field.table == nil {
		return 0, 0, fmt.Errorf("fieldKind: %s", field.kind)
	}
	rankField := field.table.get(numberLuaKey(float64(rank)))
	if rankField.kind != luaTableValue || rankField.table == nil {
		return 0, 0, fmt.Errorf("rankKind[%d]: %s", rank, rankField.kind)
	}
	minimum := rankField.table.get(numberLuaKey(1))
	maximum := rankField.table.get(numberLuaKey(2))
	if minimum.kind != luaNumber || maximum.kind != luaNumber || minimum.number <= 0 || maximum.number < minimum.number {
		return 0, 0, errors.New("invalid damage interval")
	}
	return float32(minimum.number), float32(maximum.number), nil
}
