package contentsqlite

import (
	"context"
	"errors"
	"fmt"

	contentdata "github.com/darkspinnet/darkspin/content"
	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
)

type lootRigblockStore interface {
	LootRigblocks(context.Context) ([]contentsqlite.LootRigblock, error)
	LootAffixes(context.Context) ([]contentsqlite.LootAffix, error)
	LootTuning(context.Context) (contentdata.LootTuning, error)
}

func LoadPartCatalog(ctx context.Context, store lootRigblockStore) (*game.PartCatalog, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if store == nil {
		return nil, errors.New("nil loot store")
	}
	rigblocks, err := store.LootRigblocks(ctx)
	if err != nil {
		return nil, fmt.Errorf("rigblockRead: %w", err)
	}
	definitions := make([]game.PartDefinition, 0, len(rigblocks))
	for _, rigblock := range rigblocks {
		definitions = append(definitions, game.PartDefinition{
			RigblockID: rigblock.ID, ContentFlags: rigblock.ContentFlags,
			SlotType:  rigblock.SlotType,
			ClassType: rigblock.ClassType, ScienceType: rigblock.ScienceType,
			WeaponSlotType: rigblock.WeaponSlotType, MinimumLevel: rigblock.MinimumLevel,
			MaximumLevel: rigblock.MaximumLevel, IsUniqueFamily: rigblock.IsUniqueFamily,
		})
	}
	affixes, err := store.LootAffixes(ctx)
	if err != nil {
		return nil, fmt.Errorf("affixRead: %w", err)
	}
	affixDefinitions := make([]game.PartAffixDefinition, 0, len(affixes))
	for index, affix := range affixes {
		if len(affix.Modifier) != contentdata.LootAttributeCount {
			return nil, fmt.Errorf("affixModifier[%d]: got %d", index, len(affix.Modifier))
		}
		definition := game.PartAffixDefinition{
			Kind: affix.Kind, ID: affix.ID,
			MinimumLevel: affix.MinimumLevel, MaximumLevel: affix.MaximumLevel,
			ClassType: affix.ClassType, ScienceType: affix.ScienceType,
			IsUniqueFamily: affix.IsUniqueFamily, IsBasicEligible: affix.IsBasicEligible,
		}
		copy(definition.Modifiers[:], affix.Modifier)
		affixDefinitions = append(affixDefinitions, definition)
	}
	contentTuning, err := store.LootTuning(ctx)
	if err != nil {
		return nil, fmt.Errorf("tuningRead: %w", err)
	}
	tuning := &game.PartTuning{
		BasePoint: contentTuning.BasePoint, ExtraStatBonusFactor: contentTuning.ExtraStatBonusFactor,
		PointCosts:     contentTuning.PointCosts,
		HandLevelScale: contentTuning.HandLevelScale, HandFlat: contentTuning.HandFlat,
		FootLevelScale: contentTuning.FootLevelScale, FootFlat: contentTuning.FootFlat,
		DefenseFlat: contentTuning.DefenseFlat, OffenseFlat: contentTuning.OffenseFlat,
		UtilityFlat:            contentTuning.UtilityFlat,
		WeaponDamageMultiplier: contentTuning.WeaponDamageMultiplier,
		PriceBase:              contentTuning.PriceBase,
		PriceCurve:             contentTuning.PriceCurve,
		PriceIncrement:         contentTuning.PriceIncrement,
		RarityLevelStep:        contentTuning.RarityLevelStep,
		HandMinimumLevel:       contentTuning.HandMinimumLevel,
		FootMinimumLevel:       contentTuning.FootMinimumLevel,
		WeaponMinimumLevel:     contentTuning.WeaponMinimumLevel,
		UncommonChances:        contentTuning.UncommonChances,
		RareChances:            contentTuning.RareChances,
		EpicChances:            contentTuning.EpicChances,
	}
	for _, band := range contentTuning.LevelBands {
		tuning.LevelBands = append(tuning.LevelBands, game.PartLevelBand{Base: band.Base, MaximumLevel: band.MaximumLevel})
	}
	for index, distribution := range contentTuning.RarityDistributions {
		tuning.RarityDistributions[index] = game.PartRarityDistribution{
			Suffix: distribution.Suffix, Prefix: distribution.Prefix,
			Prefix2: distribution.Prefix2, Standard: distribution.Standard,
		}
	}
	catalog, err := game.NewPartStatCatalog(definitions, affixDefinitions, tuning)
	if err != nil {
		return nil, fmt.Errorf("catalogCreate: %w", err)
	}
	return catalog, nil
}
