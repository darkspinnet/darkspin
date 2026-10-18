package runtime

import (
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// These settings intentionally remain explicit server policy until original
// stock counts, refresh tuning and prices can be recovered from more footage.
type partShopConfiguration struct {
	IsEnabled               bool   `toml:"is_enabled"`
	RefreshSecond           uint32 `toml:"refresh_second"`
	MinimumStockCount       uint32 `toml:"minimum_stock_count"`
	MaximumStockCount       uint32 `toml:"maximum_stock_count"`
	MaximumUncommonCount    uint32 `toml:"maximum_uncommon_count"`
	UncommonChancePercent   uint32 `toml:"uncommon_chance_percent"`
	BasicWeaponPricePercent uint32 `toml:"basic_weapon_price_percent"`
	UncommonPriceMultiplier uint32 `toml:"uncommon_price_multiplier"`
}

func decodePartShopRules(payload []byte) (sporenet.PartShopRules, bool, error) {
	rules := sporenet.DefaultPartShopRules()
	document := struct {
		PartShop partShopConfiguration `toml:"part_shop"`
	}{
		PartShop: partShopConfiguration{IsEnabled: rules.IsEnabled, RefreshSecond: rules.RefreshSecond,
			MinimumStockCount: rules.MinimumStockCount, MaximumStockCount: rules.MaximumStockCount,
			MaximumUncommonCount: rules.MaximumUncommonCount, BasicWeaponPricePercent: rules.BasicWeaponPricePercent,
			UncommonChancePercent:   rules.UncommonChancePercent,
			UncommonPriceMultiplier: rules.UncommonPriceMultiplier},
	}
	metadata, err := toml.Decode(string(payload), &document)
	if err != nil {
		return sporenet.PartShopRules{}, false, fmt.Errorf("shopDecode: %w", err)
	}
	config := document.PartShop
	rules = sporenet.PartShopRules{IsEnabled: config.IsEnabled, RefreshSecond: config.RefreshSecond,
		MinimumStockCount: config.MinimumStockCount, MaximumStockCount: config.MaximumStockCount,
		MaximumUncommonCount: config.MaximumUncommonCount, BasicWeaponPricePercent: config.BasicWeaponPricePercent,
		UncommonChancePercent:   config.UncommonChancePercent,
		UncommonPriceMultiplier: config.UncommonPriceMultiplier}
	err = rules.Validate()
	if err != nil {
		return sporenet.PartShopRules{}, false, fmt.Errorf("shopValidate: %w", err)
	}
	return rules, metadata.IsDefined("part_shop", "uncommon_chance_percent"), nil
}

func partShopSection(rules sporenet.PartShopRules) partShopConfiguration {
	return partShopConfiguration{IsEnabled: rules.IsEnabled, RefreshSecond: rules.RefreshSecond,
		MinimumStockCount: rules.MinimumStockCount, MaximumStockCount: rules.MaximumStockCount,
		MaximumUncommonCount: rules.MaximumUncommonCount, BasicWeaponPricePercent: rules.BasicWeaponPricePercent,
		UncommonChancePercent:   rules.UncommonChancePercent,
		UncommonPriceMultiplier: rules.UncommonPriceMultiplier}
}
