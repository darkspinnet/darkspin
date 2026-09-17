package npc

import (
	"errors"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type BossIdentity struct {
	DisplayName   string
	AffixNames    [game.MaxCampaignNPCAffixCount]string
	ModifierNames [game.MaxCampaignNPCAffixCount + 1]string
	AuraRadius    float32
	IsKnown       bool
}

// SpawnIntroduction identifies when and how an ordinary campaign actor first
// becomes visible. Zero preserves the proximity-driven population fallback.
type SpawnIntroduction uint8

const (
	SpawnIntroductionProximity SpawnIntroduction = iota
	SpawnIntroductionFloorWarp
	SpawnIntroductionDormant
	SpawnIntroductionAmbush
)

type SpawnPlan struct {
	ObjectID           uint32
	OwnerObjectID      uint32
	NounName           string
	AuthoredNounName   string
	Position           game.Vec3
	Rotation           game.Vec3
	Experience         uint32
	LocusID            uint32
	Kind               sim.DirectorLocusKind
	IsCaptain          bool
	IsElite            bool
	IsBoss             bool
	IsFixture          bool
	IsRewardSuppressed bool
	MarkerSetName      string
	Introduction       SpawnIntroduction
	NPCProfile         game.CampaignNPCProfile
	BossIdentity       BossIdentity
	ActionProfile      ActionProfile
	IsActionKnown      bool
}

func (e SpawnPlan) Clone() SpawnPlan {
	e.ActionProfile = e.ActionProfile.Clone()
	return e
}

func (e SpawnPlan) IsEqual(other SpawnPlan) bool {
	return e.ObjectID == other.ObjectID &&
		e.OwnerObjectID == other.OwnerObjectID &&
		e.NounName == other.NounName &&
		e.AuthoredNounName == other.AuthoredNounName &&
		e.Position == other.Position &&
		e.Rotation == other.Rotation &&
		e.Experience == other.Experience &&
		e.LocusID == other.LocusID &&
		e.Kind == other.Kind &&
		e.IsCaptain == other.IsCaptain &&
		e.IsElite == other.IsElite &&
		e.IsBoss == other.IsBoss &&
		e.IsFixture == other.IsFixture &&
		e.IsRewardSuppressed == other.IsRewardSuppressed &&
		e.MarkerSetName == other.MarkerSetName &&
		e.Introduction == other.Introduction &&
		e.NPCProfile == other.NPCProfile &&
		e.BossIdentity == other.BossIdentity &&
		e.ActionProfile.IsEqual(other.ActionProfile) &&
		e.IsActionKnown == other.IsActionKnown
}

type ActionOwner struct {
	UserID         uint64
	PeerGeneration uint64
}

// Faction identifies which side an NPC or target belongs to. The two current
// factions deliberately leave room for player-aligned companions and future
// non-player allies without treating every NPC as an enemy.
type Faction uint8

const (
	FactionUnknown Faction = iota
	FactionPlayerAligned
	FactionNonPlayerAligned
)

type Target struct {
	ObjectID        uint32
	Position        game.Vec3
	FootprintRadius float32
	Faction         Faction
	Owner           ActionOwner
	IsAlive         bool
}

type Snapshot struct {
	Plan                            SpawnPlan
	Origin                          game.Vec3
	Facing                          game.Vec3
	Faction                         Faction
	HitPoint                        float32
	ManaPoint                       float32
	TargetObjectID                  uint32
	TargetFaction                   Faction
	TargetOwner                     ActionOwner
	ActionOwner                     ActionOwner
	ActionGeneration                uint64
	IsDefeated                      bool
	IsActionStarted                 bool
	IsFirstActionStarted            bool
	IsPublished                     bool
	IsShieldTriggered               bool
	IsShieldActive                  bool
	IsSpawnStealthActive            bool
	IsTurtleTriggered               bool
	IsTurtleActive                  bool
	IsSelfResurrectionTriggered     bool
	IsCorruptorStageTwo             bool
	IsNavigationCollisionEnabled    bool
	IsInvisibleToSecurityTeleporter bool
	status                          status
}

type DamageResult struct {
	ObjectID                   uint32
	DeletedOwnedObjectIDs      []uint32
	LocusID                    uint32
	MarkerSetName              string
	PreviousHealth             float32
	HitPoint                   float32
	Damage                     float32
	AbsorbedDamage             float32
	RemainingLocusActorCount   int
	RemainingMarkerSetCount    int
	RemainingActorCount        int
	IsDefeated                 bool
	IsShieldStarted            bool
	IsSpawnStealthRevealed     bool
	IsTurtleStarted            bool
	IsSelfResurrectionStarted  bool
	IsCorruptorStageTwoStarted bool
	IsAreaShiftStarted         bool
	IsDamageImmune             bool
	IsAbsorptionShieldHit      bool
	IsAbsorptionShieldBroken   bool
	IsAbsorptionShieldRenewed  bool
	IsLocusCleared             bool
	AreAllNPCsDefeated         bool
	PassiveHeals               []PassiveHeal
	PassiveEnrages             []PassiveEnrage
}

type PassiveHeal struct {
	SourceObjectID uint32
	Target         Snapshot
	Amount         float32
}

type PassiveEnrage struct {
	Target    Snapshot
	BodyScale float32
}

// DamageEvent is one resolved nonlethal NPC damage outcome for member
// projection. Presentation adapters decide how to encode damage and immune
// feedback for each client.
type DamageEvent struct {
	SourceObjectID uint32
	TargetObjectID uint32
	Position       game.Vec3
	Damage         float32
	HitPoint       float32
	IsCritical     bool
	IsDamageImmune bool
}

type ActionEventKind uint8

const (
	ActionEventAggro ActionEventKind = iota + 1
	ActionEventPursuit
	ActionEventPursuitStep
	ActionEventPursuitRedirect
	ActionEventAttack
	ActionEventCancel
	ActionEventReturn
	ActionEventBossActive
)

type ActionEvent struct {
	Kind      ActionEventKind
	Plan      FirstActionPlan
	Timestamp uint64
}

type ForcedMovementEvent struct {
	Plan        AttackPlan
	Destination game.Vec3
	Timestamp   uint64
}

type DeathEventKind uint8

const (
	DeathDamage DeathEventKind = iota + 1
	DeathHitPoint
	DeathTargetable
	DeathAnimation
	DeathEffect
	DeathMovementStop
	DeathCollision
	DeathDelete
	DeathGraphics
	DeathPositionedEffect
)

// DeathEvent is one protocol-independent presentation mutation emitted by the
// retained death simulation.
type DeathEvent struct {
	Kind               DeathEventKind
	SourceObjectID     uint32
	TargetObjectID     uint32
	Damage             float32
	HitPoint           float32
	IntegerHitPoint    int32
	Timestamp          uint64
	AnimationName      string
	EffectName         string
	EffectSlot         uint8
	GraphicsState      uint32
	Position           game.Vec3
	IsCritical         bool
	IsTargetable       bool
	IsEffectStopped    bool
	IsCollisionEnabled bool
}

func ValidateSpawnPlan(plan SpawnPlan, objectIDLimit uint32) error {
	profile := plan.NPCProfile
	if objectIDLimit == 0 || plan.ObjectID == 0 || plan.ObjectID >= objectIDLimit ||
		plan.NounName == "" || !profile.IsKnown || profile.HitPoint <= 0 ||
		profile.PowerPoint < 0 || !zonegeometry.IsFinite(plan.Position) ||
		!zonegeometry.IsFinite(plan.Rotation) {
		return errors.New("invalid plan")
	}
	if plan.Introduction > SpawnIntroductionAmbush {
		return errors.New("invalid introduction")
	}
	if plan.IsBoss {
		if !IsBossIdentityValid(plan.BossIdentity) {
			return errors.New("invalid boss identity")
		}
	} else if plan.BossIdentity.IsKnown {
		return errors.New("unexpected boss identity")
	}
	for _, stat := range []float32{
		profile.HitPoint, profile.PowerPoint, profile.Strength, profile.Dexterity,
		profile.Mind, profile.DodgeRating, profile.ResistRating, profile.CriticalRating,
		profile.DifficultyDamageMultiplier,
	} {
		if math.IsNaN(float64(stat)) || math.IsInf(float64(stat), 0) || stat < 0 {
			return errors.New("invalid profile")
		}
	}
	return nil
}
