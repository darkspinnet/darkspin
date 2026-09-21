package game

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
)

var (
	ErrGameplayUserNotFound = errors.New("gameplay user not found")
	ErrGameplayGameNotFound = errors.New("gameplay game not found")
	ErrGameplaySlotNotFound = errors.New("gameplay slot not found")
	ErrGameplayTeamNotFound = errors.New("gameplay team not found")
	ErrGameplayNotReady     = errors.New("gameplay game not ready")
	ErrGameplayDifficulty   = errors.New("gameplay difficulty invalid")
	ErrGameplaySquadInvalid = errors.New("gameplay squad invalid")
)

const (
	MinimumCampaignDifficulty uint32 = 1
	MaximumCampaignDifficulty uint32 = 100
)

// ActiveUserFinder resolves only users authenticated in the current server
// process. Gameplay identity binding must not load an unauthenticated profile
// directly from durable storage.
type ActiveUserFinder interface {
	UserByID(int64) *sporenet.User
}

// GameplayBinding is the authorized game membership needed by a gameplay
// transport. It deliberately contains no RakNet representation details.
type GameplayBinding struct {
	Endpoint                   NetworkEndpoint
	GameID                     uint32
	Level                      string
	PlayerMask                 uint32
	UserID                     uint64
	AvatarID                   uint32
	AvatarLevel                uint32
	AvatarXP                   float32
	StartingAvatarXP           float32
	ChainProgression           uint32
	ChainLevelIndex            uint32
	IsOverdriveUnlocked        bool
	IsCatalystUnlocked         bool
	IsDiagonalCatalystUnlocked bool
	CatalystSlotCount          uint32
	IsReplay                   bool
	IsWarped                   bool
	IsCheckpointRestore        bool
	DNA                        uint32
	Difficulty                 uint32
	Mode                       Mode
	Slot                       uint16
	Team                       uint16
	MemberLimit                uint16
	ParticipantCount           uint16
	SquadID                    uint32
	Creatures                  [3]GameplayCreature
	ActivatedCreatures         []GameplayCreature
}

// RefreshReplay derives the build-103 HasBeatenThisLevel predicate from the
// authorized account progression and selected one-based chain slot. IsReplay
// remains server-internal and is not a Blaze or RakNet field.
func (e *GameplayBinding) RefreshReplay() {
	if e == nil {
		return
	}
	e.IsReplay = e.IsWarped || e.Mode == ModeChain && e.ChainLevelIndex != 0 &&
		e.ChainProgression >= e.ChainLevelIndex
}

// GameplayRoster is the transport-neutral player presentation retained by a
// zone so every member can establish the same set of hero objects.
type GameplayRoster struct {
	AvatarLevel         uint32
	AvatarXP            float32
	ChainProgression    uint32
	IsOverdriveUnlocked bool
	DNA                 uint32
	Creatures           [3]GameplayCreature
}

func (e GameplayBinding) Roster() GameplayRoster {
	return GameplayRoster{
		AvatarLevel: e.AvatarLevel, AvatarXP: e.AvatarXP,
		ChainProgression: e.ChainProgression, DNA: e.DNA,
		IsOverdriveUnlocked: e.IsOverdriveUnlocked,
		Creatures:           e.Creatures,
	}
}

// GameplayCreature is the transport-neutral selected hero identity needed to
// construct a gameplay roster. Appearance assets remain a RakNet adapter concern.
type GameplayCreature struct {
	ID                         uint32
	Name                       string
	Noun                       uint32
	Version                    uint32
	AppearanceVersion          uint32
	PassiveAbility             uint32
	ElementType                string
	ClassType                  string
	HitPoint                   float32
	PowerPoint                 float32
	GearScore                  float32
	FlattenedGearScore         float32
	FunctionalItemCount        uint32
	IgnoredFunctionalItemCount uint32
	FunctionalWeaponItemCount  uint32
	MinimumFunctionalItemLevel uint32
	MaximumFunctionalItemLevel uint32
	WeaponItemLevel            uint32
	WeaponDamageModifier       float32
	PartAttribute              [111]float32
	MinimumWeaponDamage        float32
	MaximumWeaponDamage        float32
	DamageProfile              DamageProfile
	HealingProfile             HealingProfile
	HealingTargetProfile       HealingTargetProfile
	TimingProfile              TimingProfile
	CriticalRating             float32
	PhysicalDefense            float32
	EnergyDefense              float32
	PetDamage                  float32
	PetHealthIncrease          float32
	RangeIncrease              float32
	AreaDurationIncrease       float32
	OverdriveDurationIncrease  float32
	LifeSteal                  float32
	AutoCrit                   float32
	CriticalDamageIncrease     float32
	PhysicalDamageReduction    float32
	EnergyDamageReduction      float32
	DamageReflection           float32
	PassiveAuraRadius          float32
	PassiveKillDamageIncrease  float32
	PassiveMaximumStack        uint32
	PassiveStackPerKill        uint32
	PassiveMovementIncrease    float32
	PassiveHealthAttackMax     float32
	PassiveHealthMovementMax   float32
	PassiveBehindDamage        float32
	PlayerExperienceIncrease   float32
}

// GameplayJoin authorizes a gameplay connection against Blaze-created game
// membership without mutating the user or game.
type GameplayJoin struct {
	userFinder           ActiveUserFinder
	gameManager          *Manager
	partCatalog          *PartCatalog
	tutorialEndPublisher TutorialEndPublisher
	inventoryPublisher   InventoryPublisher
	appearanceStore      AppearanceStore
}

func NewGameplayJoin(
	userFinder ActiveUserFinder, gameManager *Manager, partCatalog ...*PartCatalog,
) (*GameplayJoin, error) {
	if userFinder == nil {
		return nil, errors.New("create gameplay join: nil user finder")
	}
	if gameManager == nil {
		return nil, errors.New("create gameplay join: nil game manager")
	}
	operation := &GameplayJoin{userFinder: userFinder, gameManager: gameManager}
	if len(partCatalog) > 0 {
		operation.partCatalog = partCatalog[0]
	}
	return operation, nil
}

// ReserveCampaignLaunchHandoff protects the frozen co-op roster from the
// guest-side Blaze shell removal emitted as RakNet preparation begins.
func (o *GameplayJoin) ReserveCampaignLaunchHandoff(gameID uint32) error {
	if o == nil || o.gameManager == nil {
		return errors.New("campaign launch handoff unavailable")
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return fmt.Errorf("campaignHandoffGame: %w", ErrGameplayGameNotFound)
	}
	instance.ReserveLaunchHandoffRemovals()
	return nil
}

func (o *GameplayJoin) GenerateCampaignPart(
	creature GameplayCreature, difficulty uint32, accountLevel uint32, choice uint32,
) (sporenet.Part, error) {
	if o == nil || o.partCatalog == nil {
		return sporenet.Part{}, errors.New("campaign part catalog unavailable")
	}
	part, err := o.partCatalog.GenerateCampaignPart(
		creature.ClassType, creature.ElementType, max(uint32(1), difficulty),
		max(uint32(1), accountLevel), choice,
	)
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("campaignPartGenerate: %w", err)
	}
	return part, nil
}

func (o *GameplayJoin) GenerateCampaignRewardPart(
	creature GameplayCreature, difficulty uint32, accountLevel uint32, choice uint32,
	rarity sporenet.PartRarity,
) (sporenet.Part, error) {
	if o == nil || o.partCatalog == nil {
		return sporenet.Part{}, errors.New("campaign reward part catalog unavailable")
	}
	part, err := o.partCatalog.GenerateCampaignRewardPart(
		creature.ClassType, creature.ElementType, max(uint32(1), difficulty),
		max(uint32(1), accountLevel), choice, rarity,
	)
	if err != nil {
		return sporenet.Part{}, fmt.Errorf("campaignRewardPartGenerate: %w", err)
	}
	return part, nil
}

func (o *GameplayJoin) Execute(ctx context.Context, userID int64) (GameplayBinding, error) {
	if ctx == nil {
		return GameplayBinding{}, errors.New("gameplay join: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return GameplayBinding{}, fmt.Errorf("joinContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return GameplayBinding{}, fmt.Errorf("joinUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(user.CurrentGameID())
	if instance == nil {
		return GameplayBinding{}, fmt.Errorf("joinGame: %w", ErrGameplayGameNotFound)
	}
	if !instance.IsGameplayJoinable() {
		return GameplayBinding{}, fmt.Errorf("joinReady: %w", ErrGameplayNotReady)
	}
	slot, playerMask, isFound := instance.PlayerBinding(user.Account.ID)
	if !isFound {
		return GameplayBinding{}, fmt.Errorf("joinSlot: %w", ErrGameplaySlotNotFound)
	}
	team, isTeamFound := instance.PlayerTeam(user.Account.ID)
	if instance.Info.Mode == ModeArena && !isTeamFound {
		return GameplayBinding{}, fmt.Errorf("joinTeam: %w", ErrGameplayTeamNotFound)
	}
	endpoint := instance.Info.HostNetwork.External
	if endpoint == (NetworkEndpoint{}) {
		endpoint = instance.Info.HostNetwork.Internal
	}
	view := user.View()
	squadID := view.Account.DefaultDeckPVEID
	if instance.Info.Mode == ModeArena {
		squadID = view.Account.DefaultDeckPVPID
	}
	creatures := selectedGameplayCreatures(view, o.partCatalog, squadID)
	difficulty, err := resolveGameplayDifficulty(instance.Info)
	if err != nil {
		return GameplayBinding{}, fmt.Errorf("joinDifficulty: %w", err)
	}
	binding := GameplayBinding{
		Endpoint: endpoint, GameID: instance.ID, Level: instance.Info.Level,
		PlayerMask: playerMask, UserID: uint64(user.Account.ID),
		AvatarID:    user.Account.AvatarID,
		AvatarLevel: user.Account.Level, AvatarXP: float32(user.Account.XP),
		StartingAvatarXP:    float32(user.Account.XP),
		ChainProgression:    user.Account.ChainProgression,
		IsOverdriveUnlocked: user.Account.IsOverdriveUnlocked,
		DNA:                 user.Account.DNA, Difficulty: difficulty,
		Mode: instance.Info.Mode, Slot: slot, Team: team, MemberLimit: instance.Info.MaxPlayers,
		ParticipantCount:    instance.ParticipantCount(),
		IsWarped:            instance.Info.IsWarped,
		IsCheckpointRestore: instance.IsCheckpointRestore(),
		SquadID:             squadID,
		Creatures:           creatures,
		ActivatedCreatures:  activatedGameplayCreatures(view, o.partCatalog),
	}
	if binding.Mode == ModeChain && binding.Level != "" {
		selectedLevel, isSelected := o.gameManager.ChainLevelForSelection(binding.Difficulty)
		if binding.IsWarped && !isSelected {
			return GameplayBinding{}, fmt.Errorf(
				"joinWarpSelection[%d]: unavailable", binding.Difficulty,
			)
		} else if binding.IsWarped {
			binding.ChainLevelIndex = binding.Difficulty
		} else if isSelected {
			if !strings.EqualFold(selectedLevel, binding.Level) {
				return GameplayBinding{}, fmt.Errorf(
					"joinChainSelection[%d]: got %q, want %q",
					binding.Difficulty, binding.Level, selectedLevel,
				)
			}
			binding.ChainLevelIndex = binding.Difficulty
		} else {
			chainLevelIndex, isFound := o.gameManager.ChainLevelIndex(binding.Level)
			if !isFound {
				return GameplayBinding{}, fmt.Errorf("joinChainLevel: %q", binding.Level)
			}
			if binding.Difficulty != chainLevelIndex {
				return GameplayBinding{}, fmt.Errorf(
					"joinChainSelection[%d]: got %q at %d",
					binding.Difficulty, binding.Level, chainLevelIndex,
				)
			}
			binding.ChainLevelIndex = chainLevelIndex
		}
	}
	err = o.resolveAppearances(ctx, &binding, view)
	if err != nil {
		return GameplayBinding{}, fmt.Errorf("joinAppearance: %w", err)
	}
	binding.RefreshReplay()
	binding.IsCatalystUnlocked = binding.Mode == ModeChain &&
		(binding.ChainProgression >= 3 || binding.ChainLevelIndex >= 3)
	binding.IsDiagonalCatalystUnlocked = user.Account.UnlockDiagonalCatalysts != 0
	binding.CatalystSlotCount = min(uint32(9), max(uint32(3), user.Account.UnlockCatalysts))
	return binding, nil
}

// SelectCampaignSquad authorizes the squad selected by the client during the
// campaign preparation vote and replaces the provisional default squad.
func (o *GameplayJoin) SelectCampaignSquad(
	binding GameplayBinding, squadID uint32,
) (GameplayBinding, error) {
	if o == nil || o.userFinder == nil || binding.Mode != ModeChain || squadID == 0 {
		return GameplayBinding{}, ErrGameplaySquadInvalid
	}
	user := o.userFinder.UserByID(int64(binding.UserID))
	if user == nil {
		return GameplayBinding{}, fmt.Errorf("squadUser: %w", ErrGameplayUserNotFound)
	}
	view := user.View()
	var selectedSquad *sporenet.Squad
	for index := range view.Squads {
		squad := &view.Squads[index]
		if squad.ID != squadID {
			continue
		}
		selectedSquad = squad
		break
	}
	unlockedSquadCount := max(uint32(1), view.Account.UnlockPVEDecks)
	if selectedSquad == nil || selectedSquad.IsLocked ||
		selectedSquad.Slot > unlockedSquadCount ||
		!strings.EqualFold(selectedSquad.Category, "pve") {
		return GameplayBinding{}, ErrGameplaySquadInvalid
	}
	creatures := selectedGameplayCreatures(view, o.partCatalog, squadID)
	for index := range creatures {
		if creatures[index].ID == 0 {
			return GameplayBinding{}, ErrGameplaySquadInvalid
		}
	}
	binding.SquadID = squadID
	for index := range creatures {
		for _, activated := range binding.ActivatedCreatures {
			if creatures[index].ID == activated.ID {
				creatures[index].AppearanceVersion = activated.AppearanceVersion
				break
			}
		}
	}
	binding.Creatures = creatures
	activatedCreatures := activatedGameplayCreatures(view, o.partCatalog)
	for index := range activatedCreatures {
		for _, previous := range binding.ActivatedCreatures {
			if activatedCreatures[index].ID == previous.ID {
				activatedCreatures[index].AppearanceVersion = previous.AppearanceVersion
				break
			}
		}
	}
	binding.ActivatedCreatures = activatedCreatures
	return binding, nil
}

func resolveGameplayDifficulty(info Info) (uint32, error) {
	if info.Mode == ModeTutorial || info.Mode == ModeArena {
		return MinimumCampaignDifficulty, nil
	}
	rawDifficulty := info.Attributes["SelectedDifficulty"]
	if rawDifficulty == "" {
		return MinimumCampaignDifficulty, nil
	}
	parsedDifficulty, err := strconv.ParseUint(rawDifficulty, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("difficultyParse: %w", ErrGameplayDifficulty)
	}
	difficulty := uint32(parsedDifficulty)
	if difficulty < MinimumCampaignDifficulty || difficulty > MaximumCampaignDifficulty {
		return 0, fmt.Errorf("difficultyRange[%d]: %w", difficulty, ErrGameplayDifficulty)
	}
	return difficulty, nil
}

func selectedGameplayCreatures(
	view sporenet.UserView, partCatalog *PartCatalog, squadID uint32,
) [3]GameplayCreature {
	var selected [3]GameplayCreature
	var creatureID [3]uint32
	for _, squad := range view.Squads {
		if squad.ID == squadID {
			creatureID = squad.CreatureIDs
			break
		}
	}
	for position, id := range creatureID {
		for _, creature := range view.Creatures {
			if creature == nil || creature.ID != id {
				continue
			}
			selected[position] = gameplayCreature(view, creature, partCatalog)
			break
		}
	}
	return selected
}

func activatedGameplayCreatures(view sporenet.UserView, partCatalog *PartCatalog) []GameplayCreature {
	creatures := make([]GameplayCreature, 0, len(view.Creatures))
	for _, creature := range view.Creatures {
		if creature == nil || creature.Template == nil {
			continue
		}
		creatures = append(creatures, gameplayCreature(view, creature, partCatalog))
	}
	return creatures
}

func gameplayCreature(
	view sporenet.UserView, creature *sporenet.Creature, partCatalog *PartCatalog,
) GameplayCreature {
	selected := GameplayCreature{
		ID: creature.ID, Name: creature.Name(), Noun: creature.Noun(),
		Version:   creature.Version,
		GearScore: creature.GearScore, FlattenedGearScore: creature.FlattenedGearScore(),
	}
	if creature.Template != nil {
		selected.PassiveAbility = creature.Template.AbilityPassive
		selected.ElementType = strings.ToLower(creature.Template.ElementType)
		selected.ClassType = strings.ToLower(creature.Template.ClassType)
		selected.MinimumWeaponDamage = float32(creature.Template.WeaponMinDamage)
		selected.MaximumWeaponDamage = float32(creature.Template.WeaponMaxDamage)
	}
	partAttribute := [partAttributeCount]float32{}
	weaponDamageModifier := float32(0)
	equippedParts, ignoredPartCount := gameplayEquippedParts(view.Parts, creature.ID)
	selected.FunctionalItemCount = uint32(len(equippedParts))
	selected.IgnoredFunctionalItemCount = ignoredPartCount
	for index := range equippedParts {
		part := &equippedParts[index]
		partLevel := uint32(part.Level)
		if selected.MinimumFunctionalItemLevel == 0 ||
			partLevel < selected.MinimumFunctionalItemLevel {
			selected.MinimumFunctionalItemLevel = partLevel
		}
		selected.MaximumFunctionalItemLevel = max(
			selected.MaximumFunctionalItemLevel, partLevel,
		)
		definition, isDefinitionFound := partCatalog.ByRigblock(part.RigblockAssetID)
		if isDefinitionFound && definition.SlotType == "weapon" {
			selected.FunctionalWeaponItemCount++
			selected.WeaponItemLevel = max(selected.WeaponItemLevel, partLevel)
		}
		attributes, isFound := partCatalog.runtimeAttributes(part)
		if !isFound {
			continue
		}
		for attributeIndex, amount := range attributes {
			partAttribute[attributeIndex] += amount
		}
		modifier := partCatalog.WeaponDamageModifier(part)
		if modifier > 0 {
			weaponDamageModifier = max(weaponDamageModifier, modifier)
		}
	}
	// Persisted equipment records do not retain their editor slot. When every
	// equipped item has lost its catalog weapon classification, preserve the
	// playable loadout by treating its highest-level functional item as the
	// missing weapon.
	if weaponDamageModifier <= 0 && len(equippedParts) > 0 {
		selected.FunctionalWeaponItemCount = 1
		selected.WeaponItemLevel = selected.MaximumFunctionalItemLevel
		weaponDamageModifier = partCatalog.weaponDamageModifier(selected.WeaponItemLevel)
	}
	if weaponDamageModifier <= 0 {
		weaponDamageModifier = 1
	}
	selected.WeaponDamageModifier = weaponDamageModifier
	selected.MinimumWeaponDamage, selected.MaximumWeaponDamage = equippedWeaponDamageRange(
		selected.MinimumWeaponDamage, selected.MaximumWeaponDamage,
		weaponDamageModifier, partAttribute,
	)
	stats := gameplayCreatureStats(creature, partAttribute)
	for _, stat := range stats {
		switch stat.Name {
		case "HLTH":
			selected.HitPoint = float32(stat.Maximum)
		case "MANA":
			selected.PowerPoint = float32(stat.Maximum)
		case "CRTR":
			selected.CriticalRating = float32(stat.Maximum)
		case "PDEF":
			selected.PhysicalDefense = float32(stat.Maximum)
		case "EDEF":
			selected.EnergyDefense = float32(stat.Maximum)
		}
	}
	selected.PetDamage += partAttribute[63]
	selected.PetHealthIncrease += partAttribute[64]
	selected.RangeIncrease += partAttribute[67]
	selected.AreaDurationIncrease += partAttribute[95]
	selected.OverdriveDurationIncrease += partAttribute[70]
	selected.LifeSteal += partAttribute[35]
	selected.AutoCrit += partAttribute[19]
	selected.CriticalDamageIncrease += partAttribute[22]
	selected.PlayerExperienceIncrease += partAttribute[111]
	copy(selected.PartAttribute[:], partAttribute[:len(selected.PartAttribute)])
	if creature.Template != nil {
		primaryAttribute, isPrimaryAttributeFound := profilePrimaryAttribute(creature.Template, stats)
		selected.DamageProfile = damageProfile(
			float32(primaryAttribute), isPrimaryAttributeFound, partAttribute,
		)
		selected.HealingProfile = healingProfile(
			float32(primaryAttribute), isPrimaryAttributeFound, partAttribute,
		)
		applyPassiveProperties(
			&selected,
			creature.Template.AbilityProperty[creature.Template.AbilityPassive],
		)
		applyRecoveredPassiveProperties(&selected)
	}
	selected.HealingTargetProfile = healingTargetProfile(partAttribute)
	selected.TimingProfile = timingProfile(partAttribute)
	return selected
}

func equippedWeaponDamageRange(
	minimum float32, maximum float32, weaponDamageModifier float32,
	partAttribute [partAttributeCount]float32,
) (float32, float32) {
	minimum = float32(int32(minimum * weaponDamageModifier))
	maximum = float32(int32(maximum * weaponDamageModifier))
	minimum += partAttribute[104]
	maximum += partAttribute[105]
	minimum *= max(float32(0), 1+partAttribute[106])
	maximum *= max(float32(0), 1+partAttribute[107])
	minimum = max(float32(0), float32(int32(minimum)))
	maximum = max(minimum, float32(int32(maximum)))
	return minimum, maximum
}

const maximumGameplayFunctionalItemCount = 6

// gameplayEquippedParts applies the same six-functional-item boundary used by
// editor gear scoring. Legacy or malformed saves can retain more equipped
// rows; those rows must not silently grant combat stats that the editor never
// includes in the displayed loadout score.
func gameplayEquippedParts(
	parts []sporenet.Part, creatureID uint32,
) ([]sporenet.Part, uint32) {
	equippedParts := make([]sporenet.Part, 0, maximumGameplayFunctionalItemCount)
	seenPartIDs := make(map[uint64]struct{}, maximumGameplayFunctionalItemCount)
	ignoredPartCount := uint32(0)
	for _, part := range parts {
		if part.EquippedToCreatureID != creatureID || part.IsFlair {
			continue
		}
		if _, isSeen := seenPartIDs[part.ID]; isSeen {
			ignoredPartCount++
			continue
		}
		seenPartIDs[part.ID] = struct{}{}
		if len(equippedParts) >= maximumGameplayFunctionalItemCount {
			ignoredPartCount++
			continue
		}
		equippedParts = append(equippedParts, part)
	}
	return equippedParts, ignoredPartCount
}

// gameplayCreatureStats rebuilds mission core stats from immutable template
// values and the equipped functional-item vector. The editor snapshot remains
// a compatibility fallback, but it is not authoritative for gameplay because
// it can be stale when a mission is entered immediately after an equipment
// change.
func gameplayCreatureStats(
	creature *sporenet.Creature, partAttribute [partAttributeCount]float32,
) []sporenet.Stat {
	if creature == nil {
		return nil
	}
	if creature.Template == nil || len(creature.Template.Stats) == 0 {
		return creature.Stats
	}
	stats := append([]sporenet.Stat(nil), creature.Template.Stats...)
	for index := range stats {
		attributeIndex, isFound := gameplayCorePartAttribute(stats[index].Name)
		if !isFound {
			continue
		}
		maximum := max(float32(0), float32(stats[index].Maximum)+partAttribute[attributeIndex])
		stats[index].Maximum = uint32(maximum)
		stats[index].Current = stats[index].Maximum
	}
	return stats
}

func gameplayCorePartAttribute(name string) (int, bool) {
	switch name {
	case "STR":
		return 0, true
	case "DEX":
		return 1, true
	case "MIND":
		return 2, true
	case "HLTH":
		return 4, true
	case "MANA":
		return 5, true
	case "PDEF":
		return 7, true
	case "EDEF":
		return 9, true
	case "CRTR":
		return 10, true
	default:
		return 0, false
	}
}

func applyPassiveProperties(selected *GameplayCreature, properties []sporenet.AbilityProperty) {
	if selected == nil {
		return
	}
	for _, property := range properties {
		if property.AuthoredMinimum != property.AuthoredMaximum {
			continue
		}
		amount := float32(property.AuthoredMinimum)
		switch property.SourceName {
		case "criticalDamageIncrease":
			selected.CriticalDamageIncrease += amount
		case "physicalDamageReduction", "physicalDamageReductionPercent":
			if property.SourceTableName == "nModifier_CrushingDread" ||
				property.SourceTableName == "nModifier_ThornBark" {
				selected.PhysicalDamageReduction += amount
			}
		case "energyDamageReduction", "energyDamageReductionPercent":
			selected.EnergyDamageReduction += amount
		case "damageReflectionPercent":
			selected.DamageReflection += amount
		case "dotDamageIncrease":
			selected.DamageProfile.DamageOverTimeIncrease += amount
			selected.HealingProfile.HealingOverTimeIncrease += amount
		case "damageIncrease":
			if property.SourceTableName == "nModifier_GravityTempest_Passive" {
				selected.DamageProfile.ProjectileDamage += amount
			}
		case "speedIncrease":
			if property.SourceTableName == "nModifier_GravityTempest_Passive" {
				selected.TimingProfile.ProjectileSpeedIncrease += amount
			}
		case "auraRadius":
			selected.PassiveAuraRadius = max(selected.PassiveAuraRadius, amount)
		case "damageBonusPerSoul":
			selected.PassiveKillDamageIncrease = amount
		case "percentOfDefense":
			if property.SourceTableName == "nModifier_BinarySentinel_Passive" {
				selected.DamageProfile.BasicDefenseBoost += amount
			}
		case "maxSlots":
			if amount > 0 && amount == float32(uint32(amount)) {
				selected.PassiveMaximumStack = uint32(amount)
			}
		case "soulsPerKill":
			if amount > 0 && amount == float32(uint32(amount)) {
				selected.PassiveStackPerKill = uint32(amount)
			}
		}
	}
}

func applyRecoveredPassiveProperties(selected *GameplayCreature) {
	if selected == nil {
		return
	}
	switch selected.PassiveAbility {
	case util.HashID("LightningTempest_Passive"):
		selected.PassiveAuraRadius = max(selected.PassiveAuraRadius, 30)
	case util.HashID("VoodooTempestPassive"):
		selected.PassiveAuraRadius = max(selected.PassiveAuraRadius, 8)
	case util.HashID("TimeRavagerPassiveModifier"):
		selected.TimingProfile.AttackSpeed += 0.10
		selected.PassiveMovementIncrease += 0.06
		selected.PassiveAuraRadius = max(selected.PassiveAuraRadius, 12)
	case util.HashID("LightspeedTempestPassive"):
		selected.PassiveHealthAttackMax = 0.20
		selected.PassiveHealthMovementMax = 0.25
	case util.HashID("QuantumPositioning"):
		// Rank one applies a 100-percent Physical Defense change. The separate
		// Energy Defense change is gated by overdrive in the authored modifier.
		selected.PhysicalDefense *= 2
	case util.HashID("ShadowRavagerPassive"):
		selected.PassiveBehindDamage = 0.25
	}
}

// MarkTutorialComplete authorizes and records the gameplay-side completion
// boundary without mutating the persistent account.
func (o *GameplayJoin) MarkTutorialComplete(ctx context.Context, userID int64, gameID uint32) error {
	if ctx == nil {
		return errors.New("tutorial completion: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("completionContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return fmt.Errorf("completionUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return fmt.Errorf("completionGame: %w", ErrGameplayGameNotFound)
	}
	if instance.Info.Mode != ModeTutorial || instance.Info.Level != TutorialLevel {
		return errors.New("completionGame: not tutorial")
	}
	if !instance.MarkTutorialComplete(userID) {
		return fmt.Errorf("completionPlayer: %w", ErrGameplaySlotNotFound)
	}
	return nil
}

// RollbackTutorialComplete releases the gameplay marker when the server could
// not publish the terminal director transition.
func (o *GameplayJoin) RollbackTutorialComplete(ctx context.Context, userID int64, gameID uint32) error {
	if ctx == nil {
		return errors.New("tutorial rollback: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("rollbackContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return fmt.Errorf("rollbackUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return fmt.Errorf("rollbackGame: %w", ErrGameplayGameNotFound)
	}
	if !instance.RollbackTutorialComplete(userID) {
		return fmt.Errorf("rollbackPlayer: %w", ErrGameplaySlotNotFound)
	}
	return nil
}

// EndTutorial starts the client's normal post-game leave exchange. Keep the
// membership until RemovePlayer so its response can notify the departing user.
func (o *GameplayJoin) EndTutorial(ctx context.Context, userID int64, gameID uint32) error {
	if ctx == nil {
		return errors.New("tutorial end: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("endContext: %w", err)
	}
	if o == nil || o.userFinder == nil || o.gameManager == nil || gameID == 0 {
		return errors.New("tutorial end unavailable")
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return fmt.Errorf("endUser: %w", ErrGameplayUserNotFound)
	}
	if user.CurrentGameID() != gameID {
		return errors.New("endMembership: current game changed")
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return fmt.Errorf("endGame: %w", ErrGameplayGameNotFound)
	}
	if instance.Info.Mode != ModeTutorial || !IsTutorialLevel(instance.Info.Level) {
		return errors.New("endMode: not tutorial")
	}
	if !instance.IsTutorialComplete(userID) {
		return errors.New("endPlayer: tutorial incomplete")
	}
	if o.tutorialEndPublisher == nil {
		return errors.New("tutorial end publisher unavailable")
	}
	err = o.tutorialEndPublisher.PublishTutorialEnd(ctx, userID, gameID)
	if err != nil {
		return fmt.Errorf("tutorialEndPublish: %w", err)
	}
	instance.SetState(StatePostGame)
	return nil
}

// ConsumeEffectPreview authorizes and consumes a chat-requested presentation
// asset for the active gameplay member.
func (o *GameplayJoin) ConsumeEffectPreview(
	ctx context.Context, userID int64, gameID uint32,
) (EffectPreview, bool, error) {
	if ctx == nil {
		return EffectPreview{}, false, errors.New("effect preview: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return EffectPreview{}, false, fmt.Errorf("effectContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return EffectPreview{}, false, fmt.Errorf("effectUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return EffectPreview{}, false, fmt.Errorf("effectGame: %w", ErrGameplayGameNotFound)
	}
	preview, isFound := instance.ConsumeEffectPreview(userID)
	return preview, isFound, nil
}

// ConsumePlayerResourceCommand authorizes and consumes one developer resource mutation.
func (o *GameplayJoin) ConsumePlayerResourceCommand(
	ctx context.Context, userID int64, gameID uint32,
) (PlayerResourceCommand, bool, error) {
	if ctx == nil {
		return PlayerResourceCommand{}, false, errors.New("player resource: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return PlayerResourceCommand{}, false, fmt.Errorf("resourceContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return PlayerResourceCommand{}, false, fmt.Errorf("resourceUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return PlayerResourceCommand{}, false, fmt.Errorf("resourceGame: %w", ErrGameplayGameNotFound)
	}
	command, isFound := instance.ConsumePlayerResourceCommand(userID)
	return command, isFound, nil
}

// ConsumePlayerEventCommand authorizes and consumes one allowlisted developer event.
func (o *GameplayJoin) ConsumePlayerEventCommand(
	ctx context.Context, userID int64, gameID uint32, isScheduleAvailable bool,
) (PlayerEventCommand, bool, error) {
	if ctx == nil {
		return PlayerEventCommand{}, false, errors.New("player event: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return PlayerEventCommand{}, false, fmt.Errorf("eventContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return PlayerEventCommand{}, false, fmt.Errorf("eventUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return PlayerEventCommand{}, false, fmt.Errorf("eventGame: %w", ErrGameplayGameNotFound)
	}
	command, isFound := instance.ConsumePlayerEventCommand(userID, isScheduleAvailable)
	return command, isFound, nil
}

// ConsumePlayerLevelUpdate authorizes and consumes a live progression reflection.
func (o *GameplayJoin) ConsumePlayerLevelUpdate(
	ctx context.Context, userID int64, gameID uint32,
) (PlayerLevelUpdate, bool, error) {
	if ctx == nil {
		return PlayerLevelUpdate{}, false, errors.New("player level update: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return PlayerLevelUpdate{}, false, fmt.Errorf("levelContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return PlayerLevelUpdate{}, false, fmt.Errorf("levelUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return PlayerLevelUpdate{}, false, fmt.Errorf("levelGame: %w", ErrGameplayGameNotFound)
	}
	update, isFound := instance.ConsumePlayerLevelUpdate(userID)
	return update, isFound, nil
}

// RequestPlayerLevelUpdate queues one authorized live progression reflection
// for a member of the active game instance.
func (o *GameplayJoin) RequestPlayerLevelUpdate(
	userID int64, gameID uint32, update PlayerLevelUpdate,
) bool {
	if o == nil || userID <= 0 || gameID == 0 || update.Level == 0 {
		return false
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return false
	}
	return instance.RequestPlayerLevelUpdate(userID, update)
}

// ConsumePlayerDNAUpdate authorizes and consumes a live currency reflection.
func (o *GameplayJoin) ConsumePlayerDNAUpdate(
	ctx context.Context, userID int64, gameID uint32,
) (PlayerDNAUpdate, bool, error) {
	if ctx == nil {
		return PlayerDNAUpdate{}, false, errors.New("player DNA update: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return PlayerDNAUpdate{}, false, fmt.Errorf("dnaContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return PlayerDNAUpdate{}, false, fmt.Errorf("dnaUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return PlayerDNAUpdate{}, false, fmt.Errorf("dnaGame: %w", ErrGameplayGameNotFound)
	}
	update, isFound := instance.ConsumePlayerDNAUpdate(userID)
	return update, isFound, nil
}

// ConsumeItemPresentation authorizes and consumes a live persisted-item presentation.
func (o *GameplayJoin) ConsumeItemPresentation(
	ctx context.Context, userID int64, gameID uint32,
) (sporenet.Part, bool, error) {
	if ctx == nil {
		return sporenet.Part{}, false, errors.New("item presentation: nil context")
	}
	err := ctx.Err()
	if err != nil {
		return sporenet.Part{}, false, fmt.Errorf("itemContext: %w", err)
	}
	user := o.userFinder.UserByID(userID)
	if user == nil {
		return sporenet.Part{}, false, fmt.Errorf("itemUser: %w", ErrGameplayUserNotFound)
	}
	instance := o.gameManager.Game(gameID)
	if instance == nil {
		return sporenet.Part{}, false, fmt.Errorf("itemGame: %w", ErrGameplayGameNotFound)
	}
	part, isFound := instance.ConsumeItemPresentation(userID)
	return part, isFound, nil
}
