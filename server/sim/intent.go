// Package sim provides deterministic, per-session simulation of authored game
// scripts. It emits typed intents; protocol and persistence adapters remain
// outside this package.
package sim

import "time"

// Role is a stable authored object identity independent of a network object ID.
type Role string

// Phase is a named state in a scenario director.
type Phase string

// Command is a player or world command accepted by a phase.
type Command string

// Domain distinguishes simulation authority from presentation and control flow.
type Domain uint8

const (
	DomainControl Domain = iota
	DomainAuthority
	DomainPresentation
)

// Confidence records how strongly reverse-engineering evidence supports an
// instruction or native interpretation.
type Confidence uint8

const (
	ConfidenceUnknown Confidence = iota
	ConfidenceInferred
	ConfidenceBytecode
	ConfidenceNative
	ConfidenceLive
)

// Provenance identifies the packaged script and recovered instruction behind
// an intent.
type Provenance struct {
	LuaChunkID               int64
	BytecodeSHA256           string
	FunctionName             string
	InstructionOffset        uint32
	IsInstructionOffsetKnown bool
	NativeAddress            uint32
	Confidence               Confidence
}

// EventID is the runtime delivery identity of one emitted event. Epoch keeps
// reset sessions distinct even though their semantic trace sequence restarts.
type EventID struct {
	Epoch    uint64
	Sequence uint64
}

// EventMeta carries immutable event identity and evidence to typed adapters.
// Adapters that can apply an operation before returning an error use ID to
// make an explicit retry idempotent.
type EventMeta struct {
	ID         EventID
	At         time.Duration
	Phase      Phase
	Provenance Provenance
}

// Intent is a closed vocabulary emitted by the simulator.
type Intent interface {
	intentDomain() Domain
}

// DomainOf reports whether an intent controls simulation flow, changes
// authoritative state, or requests presentation.
func DomainOf(intent Intent) Domain {
	if intent == nil {
		return DomainControl
	}
	return intent.intentDomain()
}

// Event is one intent at an exact offset on the simulation clock.
type Event struct {
	ID         EventID
	Sequence   uint64
	At         time.Duration
	Phase      Phase
	Intent     Intent
	Provenance Provenance
	Scope      CancelScope
}

type WaitIntent struct{ Duration time.Duration }

func (WaitIntent) intentDomain() Domain { return DomainControl }

// ConditionalWaitIntent records a deterministic wait boundary whose resume is
// owned by an injected simulator fact rather than elapsed time alone.
type ConditionalWaitIntent struct {
	ConditionKind string
	Role          Role
	TargetRole    Role
	Timeout       time.Duration
	IsExpected    bool
}

func (ConditionalWaitIntent) intentDomain() Domain { return DomainControl }

type SpawnIntent struct {
	Role     Role
	NounName string
}

func (SpawnIntent) intentDomain() Domain { return DomainAuthority }

// CompanionSpawnIntent delegates placement and replicated creature ownership
// to the authoritative world while preserving the passive's authored assets.
type CompanionSpawnIntent struct {
	Role             Role
	OwnerRole        Role
	NounName         string
	SpawnRadius      float32
	OwnerDistance    float32
	SpawnEffectID    uint32
	SpawnAbilityID   uint32
	BurrowModifierID uint32
}

func (CompanionSpawnIntent) intentDomain() Domain { return DomainAuthority }

type DespawnIntent struct{ Role Role }

func (DespawnIntent) intentDomain() Domain { return DomainAuthority }

type VisibilityIntent struct {
	Role      Role
	IsVisible bool
}

func (VisibilityIntent) intentDomain() Domain { return DomainPresentation }

type AnimationIntent struct {
	Role          Role
	AnimationName string
}

func (AnimationIntent) intentDomain() Domain { return DomainPresentation }

type AnimationResetIntent struct{ Role Role }

func (AnimationResetIntent) intentDomain() Domain { return DomainPresentation }

type GraphicsStateIntent struct {
	Role  Role
	State uint32
}

func (GraphicsStateIntent) intentDomain() Domain { return DomainPresentation }

type CollisionStateKind string

const (
	CollisionStatePhysics    CollisionStateKind = "physics"
	CollisionStateNavigation CollisionStateKind = "navigation"
)

type PhysicsStateIntent struct {
	Role               Role
	CollisionKind      CollisionStateKind
	IsCollisionEnabled bool
}

func (PhysicsStateIntent) intentDomain() Domain { return DomainAuthority }

type InteractableUseIntent struct {
	Role     Role
	UseCount uint32
}

func (InteractableUseIntent) intentDomain() Domain { return DomainAuthority }

type LootDropIntent struct {
	SourceRole Role
	PlayerRole Role
	IsLoot     bool
	IsCrystal  bool
	IsOrb      bool
}

func (LootDropIntent) intentDomain() Domain { return DomainAuthority }

type EffectIntent struct {
	Role       Role
	ActorRole  Role
	EffectName string
	Facing     Position
	Slot       uint8
	IsStopped  bool
	IsCritical bool
}

func (EffectIntent) intentDomain() Domain { return DomainPresentation }

type PositionedEffectIntent struct {
	EffectName      string
	Position        Position
	Facing          Position
	IsFacingOmitted bool
}

func (PositionedEffectIntent) intentDomain() Domain { return DomainPresentation }

type CinematicIntent struct {
	FocusRole Role
	Duration  time.Duration
	Radius    float32
}

func (CinematicIntent) intentDomain() Domain { return DomainPresentation }

type DialogueIntent struct {
	Role       Role
	DialogueID uint32
}

func (DialogueIntent) intentDomain() Domain { return DomainPresentation }

// ClientEventKind is an allowlisted client-local presentation consequence.
type ClientEventKind string

const ClientEventPlayerUnlockedSecondCreature ClientEventKind = "player_unlocked_second_creature"
const ClientEventPlayerUnlockedSecondCreatureID uint32 = 1910684726

type ClientEventIntent struct {
	EventKind ClientEventKind
	EventID   uint32
}

func (ClientEventIntent) intentDomain() Domain { return DomainPresentation }

type ObjectiveIntent struct {
	ObjectiveID uint32
	IsActive    bool
}

func (ObjectiveIntent) intentDomain() Domain { return DomainAuthority }

// ObjectiveDataIntent preserves one authored SetObjectiveIntData mutation.
// PlayerIndex 255 is the native kAllPlayers selector. Flags remain opaque until
// their individual meanings are recovered from build 103.
type ObjectiveDataIntent struct {
	ObjectiveID uint32
	PlayerIndex uint8
	TokenIndex  uint8
	Integer     int32
	Flags       [2]bool
}

func (ObjectiveDataIntent) intentDomain() Domain { return DomainAuthority }

// UnlockKind is the allowlisted squad mutation requested by authored content.
type UnlockKind string

const UnlockNextAbility UnlockKind = "next_ability"
const UnlockSecondCreature UnlockKind = "second_creature"
const UnlockOverdrive UnlockKind = "overdrive"
const UnlockCrystals UnlockKind = "crystals"

type UnlockIntent struct {
	PlayerRole Role
	UnlockKind UnlockKind
	Count      uint32
}

func (UnlockIntent) intentDomain() Domain { return DomainAuthority }

// CrystalDropIntent is the authoritative request made by the recovered
// DropCrystals native. Pickup selection, creation, and launch remain owned by
// the pickup feature rather than being inferred inside the Lua VM.
type CrystalDropIntent struct {
	PlayerRole Role
}

func (CrystalDropIntent) intentDomain() Domain { return DomainAuthority }

// CrystalPickupIntent is the exact native request made by chunk 134 after its
// activation callback resolves the acting agent, player, and selected target.
// Admission and delayed release remain owned by the crystal pickup feature.
type CrystalPickupIntent struct {
	PlayerRole Role
	AgentRole  Role
	TargetRole Role
}

func (CrystalPickupIntent) intentDomain() Domain { return DomainAuthority }

type MovementIntent struct {
	Role   Role
	Target Role
	Speed  float32
}

func (MovementIntent) intentDomain() Domain { return DomainAuthority }

type TeleportIntent struct {
	Role        Role
	Destination Position
}

func (TeleportIntent) intentDomain() Domain { return DomainAuthority }

type TriggerVolumeIntent struct {
	Role   Role
	Center Position
	Radius float32
}

func (TriggerVolumeIntent) intentDomain() Domain { return DomainAuthority }

type DestroyTriggerVolumeIntent struct{ Role Role }

func (DestroyTriggerVolumeIntent) intentDomain() Domain { return DomainAuthority }

type LocomotionStopIntent struct {
	Role           Role
	Facing         Position
	TargetPosition Position
	IsTurn         bool
}

func (LocomotionStopIntent) intentDomain() Domain { return DomainAuthority }

type AttributeKind string

const AttributeImmobilized AttributeKind = "immobilized"
const AttributeIntangible AttributeKind = "intangible"
const AttributeInvisibleToSecurityTeleporter AttributeKind = "invisible_to_security_teleporter"

type AttributeModifierIntent struct {
	Role          Role
	AttributeKind AttributeKind
	Amount        float32
}

func (AttributeModifierIntent) intentDomain() Domain { return DomainAuthority }

type ModifierRequestIntent struct {
	TargetRole    Role
	InitiatorRole Role
	ModifierGUID  string
	Destination   Position
	Rank          uint32
}

func (ModifierRequestIntent) intentDomain() Domain { return DomainAuthority }

type CombatIntent struct {
	ActorRole   Role
	TargetRole  Role
	AbilityName string
}

func (CombatIntent) intentDomain() Domain { return DomainAuthority }

type TargetValidationIntent struct {
	ActorRole  Role
	TargetRole Role
	Stage      string
	IsValid    bool
}

func (TargetValidationIntent) intentDomain() Domain { return DomainControl }

type CooldownIntent struct {
	Role        Role
	AbilityName string
	Duration    time.Duration
}

func (CooldownIntent) intentDomain() Domain { return DomainAuthority }

// ResourceChangeIntent records an accepted actor-resource payment separately
// from cooldown and presentation. Delta is negative for a cost.
type ResourceChangeIntent struct {
	Role  Role
	Delta float32
}

func (ResourceChangeIntent) intentDomain() Domain { return DomainAuthority }

// AreaPulseIntent asks the authoritative world owner to resolve the live
// allies and enemies inside one authored area-of-effect tick.
type AreaPulseIntent struct {
	Role               Role
	ActorRole          Role
	Position           Position
	Radius             float32
	Tick               uint32
	DamageMinimum      float32
	DamageMaximum      float32
	DamageCoefficient  float32
	HealingMinimum     float32
	HealingMaximum     float32
	HealingCoefficient float32
	RootModifierID     uint32
	RootChance         float32
	IsFirstTargetOnly  bool
}

func (AreaPulseIntent) intentDomain() Domain { return DomainAuthority }

type AbilityReleaseIntent struct {
	Role        Role
	AbilityName string
}

func (AbilityReleaseIntent) intentDomain() Domain { return DomainControl }

type ProjectileLaunchIntent struct {
	Role                    Role
	ActorRole               Role
	TargetRole              Role
	NounName                string
	Position                Position
	Direction               Position
	AngularVelocity         Position
	Speed                   float32
	Acceleration            float32
	Distance                float32
	RangeIncrease           float32
	IsHoming                bool
	IsPiercing              bool
	IsOrientationRecomputed bool
}

func (ProjectileLaunchIntent) intentDomain() Domain { return DomainAuthority }

type ProjectileImpactIntent struct {
	Role       Role
	TargetRole Role
	Position   Position
	Facing     Position
	IsDirect   bool
}

func (ProjectileImpactIntent) intentDomain() Domain { return DomainAuthority }

type ProjectileMotionIntent struct {
	Role                      Role
	TargetRole                Role
	Direction                 Position
	AngularVelocity           Position
	Speed                     float32
	Acceleration              float32
	Distance                  float32
	RangeIncrease             float32
	HomingDelay               time.Duration
	IsHoming                  bool
	ExpectedGeometryCollision *Position
}

func (ProjectileMotionIntent) intentDomain() Domain { return DomainAuthority }

// DamageIntent records one accepted combat mutation independently from death
// presentation and corpse lifecycle scheduling.
type DamageIntent struct {
	ActorRole             Role
	TargetRole            Role
	DeltaHealth           float32
	IntegerHitPointChange int32
	IsKilling             bool
	IsCritical            bool
}

func (DamageIntent) intentDomain() Domain { return DomainAuthority }

// HitPointIntent records the resulting authoritative combatant HP state.
type HitPointIntent struct {
	Role      Role
	HitPoints float32
}

func (HitPointIntent) intentDomain() Domain { return DomainAuthority }

type DeathStateAction string

const (
	DeathTargetCleared     DeathStateAction = "target_cleared"
	DeathTargetRestored    DeathStateAction = "target_restored"
	DeathCorpseFading      DeathStateAction = "corpse_fading"
	DeathCorpseRestored    DeathStateAction = "corpse_restored"
	DeathWaitingForRevival DeathStateAction = "waiting_for_revival"
	DeathMarkedForDeletion DeathStateAction = "marked_for_deletion"
)

// DeathStateIntent represents Behavior_Death authority operations whose Lua
// wrappers do not directly send application packets.
type DeathStateIntent struct {
	Role   Role
	Action DeathStateAction
}

func (DeathStateIntent) intentDomain() Domain { return DomainAuthority }

type RewardIntent struct {
	PlayerRole Role
	RewardKind string
	Amount     int64
}

func (RewardIntent) intentDomain() Domain { return DomainAuthority }

// SequenceCompleteIntent marks authored script cleanup/completion. It does not
// mutate or remove the referenced world object.
type SequenceCompleteIntent struct{ Role Role }

func (SequenceCompleteIntent) intentDomain() Domain { return DomainControl }
