package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type squadShareSnapshot struct {
	FormatVersion  uint32                 `json:"formatVersion"`
	GeneratedAt    string                 `json:"generatedAt"`
	Crogenitors    []squadShareCrogenitor `json:"crogenitors"`
	AppearanceNote string                 `json:"appearanceNote"`
}

type squadShareCrogenitor struct {
	DisplayName                 string            `json:"displayName"`
	Level                       uint32            `json:"level"`
	Experience                  uint32            `json:"experience"`
	CampaignProgression         uint32            `json:"campaignProgression"`
	HighestCampaignUnlocked     uint32            `json:"highestCampaignUnlocked"`
	DefaultPVEDeckID            uint32            `json:"defaultPveDeckId"`
	DefaultPVPDeckID            uint32            `json:"defaultPvpDeckId"`
	IsTutorialCompleted         bool              `json:"isTutorialCompleted"`
	IsTutorialCompletionPending bool              `json:"isTutorialCompletionPending"`
	Unlocks                     squadShareUnlocks `json:"unlocks"`
	Squads                      []squadShareSquad `json:"squads"`
}

type squadShareUnlocks struct {
	IsAllAccessGranted    bool   `json:"isAllAccessGranted"`
	IsOnlineAccessGranted bool   `json:"isOnlineAccessGranted"`
	IsOverdriveUnlocked   bool   `json:"isOverdriveUnlocked"`
	Catalysts             uint32 `json:"catalysts"`
	DiagonalCatalysts     uint32 `json:"diagonalCatalysts"`
	Inventory             uint32 `json:"inventory"`
	FuelTanks             uint32 `json:"fuelTanks"`
	PVEDecks              uint32 `json:"pveDecks"`
	PVPDecks              uint32 `json:"pvpDecks"`
	Stats                 uint32 `json:"stats"`
	InventoryIdentify     uint32 `json:"inventoryIdentify"`
	EditorFlairSlots      uint32 `json:"editorFlairSlots"`
}

type squadShareSquad struct {
	Name     string               `json:"name"`
	Category string               `json:"category"`
	Slot     uint32               `json:"slot"`
	IsLocked bool                 `json:"isLocked"`
	Members  []squadShareCreature `json:"members"`
}

type squadShareCreature struct {
	CreatureID         uint32                  `json:"creatureId"`
	TemplateName       string                  `json:"templateName"`
	TemplateNoun       uint32                  `json:"templateNoun"`
	IsTemplateResolved bool                    `json:"isTemplateResolved"`
	ElementType        string                  `json:"elementType,omitempty"`
	ClassType          string                  `json:"classType,omitempty"`
	Version            uint32                  `json:"version"`
	GearScore          float32                 `json:"gearScore"`
	ItemPoints         float32                 `json:"itemPoints"`
	Abilities          []squadShareAbility     `json:"abilities"`
	Stats              []squadShareStat        `json:"stats"`
	AbilityStats       []squadShareAbilityStat `json:"abilityStats"`
	Equipment          []squadSharePart        `json:"equipment"`
	Appearance         squadShareAppearance    `json:"appearance"`
}

type squadShareAbility struct {
	Slot string `json:"slot"`
	ID   uint32 `json:"id"`
}

type squadShareStat struct {
	Name    string `json:"name"`
	Maximum uint32 `json:"maximum"`
	Current uint32 `json:"current"`
}

type squadShareAbilityStat struct {
	Key   string `json:"key"`
	Token string `json:"token"`
	Value string `json:"value"`
}

type squadSharePart struct {
	ID                     uint64 `json:"id"`
	ReferenceID            uint64 `json:"referenceId"`
	Level                  uint16 `json:"level"`
	Rarity                 uint8  `json:"rarity"`
	IsFlair                bool   `json:"isFlair"`
	RigblockAssetID        uint16 `json:"rigblockAssetId"`
	PrefixAssetID          uint16 `json:"prefixAssetId"`
	PrefixSecondaryAssetID uint16 `json:"prefixSecondaryAssetId"`
	SuffixAssetID          uint16 `json:"suffixAssetId"`
	Status                 uint8  `json:"status"`
	Usage                  uint8  `json:"usage"`
}

type squadShareAppearance struct {
	Version            uint32 `json:"version"`
	IsCustom           bool   `json:"isCustom"`
	PreviewPNGBase64   string `json:"previewPngBase64,omitempty"`
	IsPreviewAvailable bool   `json:"isPreviewAvailable"`
}

const maximumSquadShareProfiles = 20
const maximumSquadSharePreviewSize = 256 * 1024

// GetSquadSnapshot returns selected local Crogenitors as a credential-free
// payload intended for a player to review and share manually.
func (e *App) GetSquadSnapshot(profileLoginNames []string) (
	snapshot squadShareSnapshot, resultErr error,
) {
	if len(profileLoginNames) == 0 || len(profileLoginNames) > maximumSquadShareProfiles {
		return squadShareSnapshot{}, errors.New("select between 1 and 20 local Crogenitors")
	}
	ctx := e.ctx
	if ctx == nil {
		ctx = context.TODO()
	}
	basePath, err := executableDirectory()
	if err != nil {
		return squadShareSnapshot{}, fmt.Errorf("snapshotBase: %w", err)
	}
	paths, err := resolveSpinnerPaths(basePath)
	if err != nil {
		return squadShareSnapshot{}, fmt.Errorf("snapshotPaths: %w", err)
	}
	repository, err := e.openProfileRepositoryAt(ctx, paths.userPath)
	if err != nil {
		return squadShareSnapshot{}, fmt.Errorf("snapshotRepository: %w", err)
	}
	defer func() {
		closeErr := repository.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("snapshotRepositoryClose: %w", closeErr))
		}
	}()
	contentStore, err := contentsqlite.New(ctx, paths.contentPath)
	if err != nil {
		return squadShareSnapshot{}, fmt.Errorf("snapshotContent: %w", err)
	}
	defer func() {
		closeErr := contentStore.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("snapshotContentClose: %w", closeErr))
		}
	}()
	templates, err := contentStore.CreatureTemplates(ctx)
	if err != nil {
		return squadShareSnapshot{}, fmt.Errorf("snapshotTemplates: %w", err)
	}
	templatesByName := make(map[string]contentsqlite.CreatureTemplate, len(templates))
	for _, template := range templates {
		templatesByName[template.Name] = template
	}
	snapshot = squadShareSnapshot{
		FormatVersion:  1,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Crogenitors:    make([]squadShareCrogenitor, 0, len(profileLoginNames)),
		AppearanceNote: "Saved appearance preview PNGs are included when available. Darkspinner local profiles do not retain editable 3D body geometry.",
	}
	seenProfileNames := make(map[string]struct{}, len(profileLoginNames))
	for profileIndex, profileLoginName := range profileLoginNames {
		profileLoginName = strings.TrimSpace(profileLoginName)
		err = validateProfileIdentity(profileLoginName)
		if err != nil {
			return squadShareSnapshot{}, fmt.Errorf("snapshotIdentity[%d]: %w", profileIndex, err)
		}
		if _, isDuplicate := seenProfileNames[profileLoginName]; isDuplicate {
			return squadShareSnapshot{}, fmt.Errorf("snapshotIdentity[%d]: duplicate Crogenitor", profileIndex)
		}
		seenProfileNames[profileLoginName] = struct{}{}
		record, err := repository.LoadByLoginName(ctx, profileLoginName)
		if err != nil {
			return squadShareSnapshot{}, fmt.Errorf("snapshotProfile[%d]: %w", profileIndex, err)
		}
		highestCampaignUnlocked := record.Account.ChainProgression
		if highestCampaignUnlocked < ^uint32(0) {
			highestCampaignUnlocked++
		}
		crogenitor := squadShareCrogenitor{
			DisplayName: record.DisplayName,
			Level:       record.Account.Level, Experience: record.Account.XP,
			CampaignProgression:         record.Account.ChainProgression,
			HighestCampaignUnlocked:     highestCampaignUnlocked,
			DefaultPVEDeckID:            record.Account.DefaultDeckPVEID,
			DefaultPVPDeckID:            record.Account.DefaultDeckPVPID,
			IsTutorialCompleted:         record.Account.IsTutorialCompleted(),
			IsTutorialCompletionPending: record.IsTutorialCompletionPending,
			Unlocks: squadShareUnlocks{
				IsAllAccessGranted:    record.Account.IsAllAccessGranted,
				IsOnlineAccessGranted: record.Account.IsOnlineAccessGranted,
				IsOverdriveUnlocked:   record.Account.IsOverdriveUnlocked,
				Catalysts:             record.Account.UnlockCatalysts,
				DiagonalCatalysts:     record.Account.UnlockDiagonalCatalysts,
				Inventory:             record.Account.UnlockInventory,
				FuelTanks:             record.Account.UnlockFuelTanks,
				PVEDecks:              record.Account.UnlockPVEDecks,
				PVPDecks:              record.Account.UnlockPVPDecks,
				Stats:                 record.Account.UnlockStats,
				InventoryIdentify:     record.Account.UnlockInventoryIdentify,
				EditorFlairSlots:      record.Account.UnlockEditorFlairSlots,
			},
			Squads: make([]squadShareSquad, 0, len(record.Squads)),
		}
		for _, squad := range record.Squads {
			squadShare := squadShareSquad{
				Name: squad.Name, Category: squad.Category, Slot: squad.Slot,
				IsLocked: squad.IsLockedFor(record.Account),
				Members:  make([]squadShareCreature, 0, len(squad.CreatureIDs)),
			}
			for _, creatureID := range squad.CreatureIDs {
				creature := recordCreature(record.Creatures, creatureID)
				if creature == nil {
					continue
				}
				creatureShare, err := makeSquadShareCreature(paths.runtimePath, creature, templatesByName, record.Parts)
				if err != nil {
					return squadShareSnapshot{}, fmt.Errorf("snapshotCreature[%d]: %w", creatureID, err)
				}
				squadShare.Members = append(squadShare.Members, creatureShare)
			}
			crogenitor.Squads = append(crogenitor.Squads, squadShare)
		}
		snapshot.Crogenitors = append(snapshot.Crogenitors, crogenitor)
	}
	return snapshot, nil
}

func recordCreature(creatures []*sporenet.Creature, creatureID uint32) *sporenet.Creature {
	for _, creature := range creatures {
		if creature != nil && creature.ID == creatureID {
			return creature
		}
	}
	return nil
}

func makeSquadShareCreature(
	runtimePath string, creature *sporenet.Creature,
	templatesByName map[string]contentsqlite.CreatureTemplate, parts []sporenet.Part,
) (squadShareCreature, error) {
	template, isTemplateResolved := templatesByName[creature.TemplateName]
	creatureShare := squadShareCreature{
		CreatureID: creature.ID, TemplateName: creature.TemplateName,
		Version: creature.Version, GearScore: creature.GearScore,
		ItemPoints: creature.ItemPoints, IsTemplateResolved: isTemplateResolved,
		Abilities:    make([]squadShareAbility, 0, 5),
		Stats:        make([]squadShareStat, 0, len(creature.Stats)),
		AbilityStats: make([]squadShareAbilityStat, 0, len(creature.AbilityStats)),
		Equipment:    make([]squadSharePart, 0),
		Appearance:   squadShareAppearance{Version: creature.Version, IsCustom: creature.Version > 1},
	}
	if isTemplateResolved {
		creatureShare.TemplateNoun = template.ID
		creatureShare.ElementType = template.ElementType
		creatureShare.ClassType = template.ClassType
		creatureShare.Abilities = append(creatureShare.Abilities,
			squadShareAbility{Slot: "passive", ID: template.AbilityPassive},
			squadShareAbility{Slot: "basic", ID: template.AbilityBasic},
			squadShareAbility{Slot: "special1", ID: template.AbilitySpecial1},
			squadShareAbility{Slot: "special2", ID: template.AbilitySpecial2},
			squadShareAbility{Slot: "random", ID: template.AbilityRandom},
		)
	}
	for _, stat := range creature.Stats {
		creatureShare.Stats = append(creatureShare.Stats, squadShareStat{
			Name: stat.Name, Maximum: stat.Maximum, Current: stat.Current,
		})
	}
	for _, abilityStat := range creature.AbilityStats {
		creatureShare.AbilityStats = append(creatureShare.AbilityStats, squadShareAbilityStat{
			Key: abilityStat.Key, Token: abilityStat.Token, Value: abilityStat.Value,
		})
	}
	for _, part := range parts {
		if part.EquippedToCreatureID != creature.ID {
			continue
		}
		creatureShare.Equipment = append(creatureShare.Equipment, squadSharePart{
			ID: part.ID, ReferenceID: part.ReferenceID, Level: part.Level,
			Rarity: uint8(part.Rarity), IsFlair: part.IsFlair,
			RigblockAssetID: part.RigblockAssetID, PrefixAssetID: part.PrefixAssetID,
			PrefixSecondaryAssetID: part.PrefixSecondaryAssetID,
			SuffixAssetID:          part.SuffixAssetID, Status: part.Status, Usage: part.Usage,
		})
	}
	previewPNG, isPreviewAvailable, err := squadShareAppearancePreview(runtimePath, creature)
	if err != nil {
		return squadShareCreature{}, fmt.Errorf("creaturePreview: %w", err)
	}
	creatureShare.Appearance.PreviewPNGBase64 = previewPNG
	creatureShare.Appearance.IsPreviewAvailable = isPreviewAvailable
	return creatureShare, nil
}

func squadShareAppearancePreview(runtimePath string, creature *sporenet.Creature) (string, bool, error) {
	if creature.Version <= 1 || creature.ThumbImageURL == "" {
		return "", false, nil
	}
	name := fmt.Sprintf("%d_%d_thumb.png", creature.ID, creature.Version)
	uri, err := url.Parse(creature.ThumbImageURL)
	if err != nil || uri.Path != "/creature_png/"+name {
		// Legacy or unrecognized image references are omitted from shared payloads.
		return "", false, nil
	}
	imagePath := filepath.Join(runtimePath, "creature_png", name)
	fi, err := os.Lstat(imagePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("previewStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() <= 0 || fi.Size() > maximumSquadSharePreviewSize {
		return "", false, nil
	}
	contents, err := os.ReadFile(imagePath)
	if err != nil {
		return "", false, fmt.Errorf("previewRead: %w", err)
	}
	return base64.StdEncoding.EncodeToString(contents), true, nil
}
