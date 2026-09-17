package content

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
)

const campaignPlayerCompatibilityFootprint = float32(0.8)

// HeroAbilitySlot identifies one authored slot in a playable hero kit. The
// names match the source creature template rather than the client's action-bar
// index, which is a transport concern.
type HeroAbilitySlot uint8

const (
	HeroAbilityPassive HeroAbilitySlot = iota
	HeroAbilityBasic
	HeroAbilityRandom
	HeroAbilitySpecialOne
	HeroAbilitySpecialTwo
)

// HeroAbility retains both the immutable content identity and any definition
// understood by the constrained Lua compiler. An empty Definition with a
// CompileError remains useful evidence; it must not silently become a generic
// ability at runtime.
type HeroAbility struct {
	ID           uint32
	AssetName    string
	Definition   sim.AbilityDefinition
	CompileError string
}

// HeroKit is the five-slot authored ability kit for one playable hero noun.
type HeroKit struct {
	Noun      uint32
	Name      string
	Abilities map[HeroAbilitySlot]HeroAbility
}

type Programs struct {
	ChainLevel             []string
	ArenaLevels            map[string]ArenaLevel
	Critical               sim.CriticalTuning
	Difficulty             sim.DifficultyCombatTuning
	PlayerBasicAbility     map[uint32]sim.AbilityDefinition
	PlayerBasicUnsupported map[uint32]string
	HeroKits               map[uint32]HeroKit
	NonPlayerHitPoint      map[uint32]float32
	NonPlayerCritical      map[uint32]sim.CriticalProfile
	NounPhysics            map[string]NounPhysics
	NounPhysicsByID        map[uint32]NounPhysics
	NPCDeathAnimations     map[string]string
	ProjectileHalfExtent   sim.Position
	QuadraFirstAggro       sim.Program
	LightningBasic         sim.AbilityDefinition
	ElectronSphere         sim.AbilityDefinition
	SupportHealerBasic     sim.AbilityDefinition
	SupportHealerPassive   sim.SummonPassiveDefinition
	SupportHealerPetBasic  sim.AbilityDefinition
	SentryDroneLaser       sim.AbilityDefinition
	FireTempestPetBasic    sim.AbilityDefinition
	BeastPetBasic          sim.AbilityDefinition
	PlasmaSentinelPetBasic sim.AbilityDefinition
	PoisonMelee            sim.AbilityDefinition
	PoisonCloud            sim.AbilityDefinition
	PlasmaLightning        sim.AbilityDefinition
	TailZap                sim.AbilityDefinition
	BurstShot              sim.AbilityDefinition
	InteractWithObelisk    sim.AbilityDefinition
	InteractHealthObelisk  sim.AbilityDefinition
	IntroAbilitySecond     MarkerProgram
	IntroHealthAndPower    sim.MarkerTrigger
	IntroOverdrive         MarkerProgram
	IntroSecondCreature    MarkerProgram
	SecurityTeleporter     sim.LuaBytecode
	TeleporterModifier     sim.LuaBytecode
	BossTeleporter         zoneteleport.Simulation
	InvisibleBehavior      sim.Program
	SpawnModifier          sim.Program
	SoloSupportUnlock      sim.Program
	SupportUnlock          sim.Program
	OverdriveUnlock        sim.Program
	CatalystUnlock         sim.Program
	CrystalPickup          sim.Program
	ObjectiveInitializers  []sim.Program
	ObjectiveInput         []sim.LuaObjectiveInput
	CrystalDefinitions     []sim.CrystalDefinition
	CrystalLevelOffsets    []sim.CrystalLevelOffset
}

// ArenaSpawn is one content-authored player placement and facing.
type ArenaSpawn struct {
	Position        sim.Position
	RotationDegrees float32
}

// ArenaLevel retains the four authored spawn groups for one PVP map. Groups
// one and two are staging lobbies; groups three and four are combat starts.
type ArenaLevel struct {
	SpawnGroups [4][]ArenaSpawn
}

// ArenaSpawnGroup returns one authored one-based spawn group.
func (e Programs) ArenaSpawnGroup(levelName string, group uint16) []ArenaSpawn {
	if group == 0 || group > 4 {
		return nil
	}
	level, isFound := e.ArenaLevels[levelName]
	if !isFound {
		return nil
	}
	return level.SpawnGroups[group-1]
}

// HeroAbility resolves an authored hero slot without applying a wire-index or
// compatibility fallback.
func (e Programs) HeroAbility(noun uint32, slot HeroAbilitySlot) (HeroAbility, bool) {
	kit, isFound := e.HeroKits[noun]
	if !isFound {
		return HeroAbility{}, false
	}
	ability, isFound := kit.Abilities[slot]
	return ability, isFound
}

type NounPhysics = zoneobject.NounPhysics

// NPCDeathPhysics overlays animation selection without changing combat geometry.
func (e Programs) NPCDeathPhysics(nounName string) NounPhysics {
	physics := e.NounPhysics[nounName]
	nounKey := strings.ToLower(nounName)
	animationName := e.NPCDeathAnimations[nounKey]
	if animationName == "" {
		// Named captains use the same noun rig as their ordinary ranked family.
		animationName = e.NPCDeathAnimations[strings.ReplaceAll(nounKey, "_captain", "")]
	}
	if animationName != "" {
		physics.OrdinaryDeathAnimation = animationName
	}
	return physics
}

type MarkerProgram struct {
	Program      sim.Program
	Trigger      sim.MarkerTrigger
	CallbackName string
}

func (e Programs) FootprintRadius(assetName string) (float32, error) {
	noun, isFound := e.NounPhysics[assetName]
	if !isFound || noun.FootprintRadius <= 0 {
		return 0, fmt.Errorf("nounFootprint: %s", assetName)
	}
	return noun.FootprintRadius, nil
}

func (e Programs) FootprintRadiusByNoun(noun uint32) (float32, error) {
	if noun == 0 {
		return 0, errors.New("nounFootprint: invalid noun")
	}
	for assetName, physics := range e.NounPhysics {
		if util.HashID(assetName) == noun && physics.FootprintRadius > 0 {
			return physics.FootprintRadius, nil
		}
	}
	if _, isPlayerCreature := e.PlayerBasicAbility[noun]; isPlayerCreature {
		return campaignPlayerCompatibilityFootprint, nil
	}
	return 0, fmt.Errorf("nounFootprint: %#x", noun)
}

func (e Programs) BasicAttackCenterRange(
	assetName string, abilityRange float32,
) (float32, error) {
	attackerFootprint, err := e.FootprintRadius(assetName)
	if err != nil {
		return 0, fmt.Errorf("attackerFootprint: %w", err)
	}
	playerFootprint, err := e.FootprintRadius("PC_EL_Rogue.Noun")
	if err != nil {
		return 0, fmt.Errorf("playerFootprint: %w", err)
	}
	if abilityRange <= 0 {
		return 0, errors.New("abilityRange: invalid")
	}
	return abilityRange + attackerFootprint + playerFootprint, nil
}

func (e Programs) SummonCompanionAttackCenterRange(
	assetName string, abilityRange float32,
) (float32, error) {
	companionFootprint, err := e.FootprintRadius("HelperMelee.Noun")
	if err != nil {
		return 0, fmt.Errorf("companionFootprint: %w", err)
	}
	targetFootprint, err := e.FootprintRadius(assetName)
	if err != nil {
		return 0, fmt.Errorf("targetFootprint: %w", err)
	}
	if abilityRange <= 0 {
		return 0, errors.New("abilityRange: invalid")
	}
	return abilityRange + companionFootprint + targetFootprint, nil
}

func (e Programs) ProjectileGeometry(
	zOverride float32,
) (zoneability.ProjectileCollisionGeometry, error) {
	player, isFound := e.NounPhysics["PC_EL_Rogue.Noun"]
	if !isFound || e.ProjectileHalfExtent.X <= 0 || e.ProjectileHalfExtent.Y <= 0 ||
		e.ProjectileHalfExtent.Z <= 0 {
		return zoneability.ProjectileCollisionGeometry{},
			errors.New("projectileGeometry: missing")
	}
	halfExtent := e.ProjectileHalfExtent
	if zOverride > 0 {
		halfExtent.Z = zOverride
	}
	return zoneability.ProjectileCollisionGeometry{
		ProjectileHalfExtent: halfExtent,
		TargetMinimum:        player.BoundMinimum,
		TargetMaximum:        player.BoundMaximum,
	}, nil
}
