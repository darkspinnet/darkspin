package sporenet

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"time"
)

// PartShopRules are reconstruction policy, not recovered retail tuning.
type PartShopRules struct {
	IsEnabled               bool
	RefreshSecond           uint32
	MinimumStockCount       uint32
	MaximumStockCount       uint32
	MaximumUncommonCount    uint32
	UncommonChancePercent   uint32
	BasicWeaponPricePercent uint32
	UncommonPriceMultiplier uint32
}

func DefaultPartShopRules() PartShopRules {
	return PartShopRules{IsEnabled: true, RefreshSecond: 7200,
		MinimumStockCount: 22, MaximumStockCount: 30, MaximumUncommonCount: 5,
		UncommonChancePercent:   5,
		BasicWeaponPricePercent: 50, UncommonPriceMultiplier: 2}
}

func (e PartShopRules) Validate() error {
	if e.RefreshSecond < 60 || e.RefreshSecond > 7*24*3600 ||
		e.MinimumStockCount < 1 || e.MaximumStockCount < e.MinimumStockCount ||
		e.MaximumStockCount > 30 || e.MaximumUncommonCount > 5 ||
		e.MaximumUncommonCount > e.MinimumStockCount ||
		e.UncommonChancePercent > 100 ||
		e.BasicWeaponPricePercent < 1 || e.BasicWeaponPricePercent > 1000 ||
		e.UncommonPriceMultiplier < 1 || e.UncommonPriceMultiplier > 100 {
		return errors.New("part shop policy outside supported bounds")
	}
	return nil
}

// PartShopState stays private; offers themselves use the existing inventory
// persistence. The high-water mark prevents expired quotes from being reused.
type PartShopState struct {
	ExpiresAt  int64
	NextPartID uint64
	ItemLevel  uint16
}

type PartShopRequest struct {
	ItemLevel     uint16
	Count         uint32
	UncommonCount uint32
	Seed          uint64
}

type PartShopCatalog interface {
	GenerateShopParts(PartShopRequest) ([]Part, error)
}

type PartShopStock struct {
	Parts       []Part
	ExpiresAt   int64
	ItemLevel   uint16
	IsRefreshed bool
}

func (e *User) partShopStock(isRefreshed bool) PartShopStock {
	stock := PartShopStock{ExpiresAt: e.PartShop.ExpiresAt, ItemLevel: e.PartShop.ItemLevel,
		IsRefreshed: isRefreshed}
	for _, part := range e.Parts {
		if part.MarketStatus == PartMarketOffer {
			stock.Parts = append(stock.Parts, part)
		}
	}
	return stock
}

// PreparePartShop persists a complete replacement before returning it. Opening
// the store, buying its last item, or restarting cannot refill a live rotation.
func (e *UserManager) PreparePartShop(ctx context.Context, user *User,
	catalog PartShopCatalog, vendor *Vendor, rules PartShopRules, now time.Time,
) (PartShopStock, error) {
	if user == nil || catalog == nil || vendor == nil || now.Unix() <= 0 {
		return PartShopStock{}, ErrVendorOperation
	}
	err := ctx.Err()
	if err != nil {
		return PartShopStock{}, fmt.Errorf("shopContext: %w", err)
	}
	err = rules.Validate()
	if err != nil {
		return PartShopStock{}, fmt.Errorf("shopRules: %w", err)
	}
	if !rules.IsEnabled {
		return PartShopStock{}, nil
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	if user.PartShop.ExpiresAt > now.Unix() {
		stock := user.partShopStock(false)
		user.mu.Unlock()
		return stock, nil
	}
	level, weaponPrice := vendor.partShopLevel(user.Account)
	if level == 0 {
		user.mu.Unlock()
		return PartShopStock{}, nil
	}
	if user.Account.ID <= 0 || uint64(user.Account.ID) > uint64(^uint32(0)>>1) {
		user.mu.Unlock()
		return PartShopStock{}, ErrInvalidUser
	}
	var entropy [16]byte
	readCount, err := rand.Read(entropy[:])
	if err != nil || readCount != len(entropy) {
		user.mu.Unlock()
		return PartShopStock{}, fmt.Errorf("shopEntropy: %w", errors.Join(err, errors.New("stock seed unavailable")))
	}
	seed := binary.LittleEndian.Uint64(entropy[:8])
	random := mathrand.New(mathrand.NewPCG(seed, binary.LittleEndian.Uint64(entropy[8:])))
	count := rules.MinimumStockCount + random.Uint32N(rules.MaximumStockCount-rules.MinimumStockCount+1)
	uncommonCount := uint32(0)
	for range count {
		if uncommonCount < rules.MaximumUncommonCount && random.Uint32N(100) < rules.UncommonChancePercent {
			uncommonCount++
		}
	}
	parts, err := catalog.GenerateShopParts(PartShopRequest{
		ItemLevel: level, Count: count, UncommonCount: uncommonCount, Seed: seed,
	})
	if err != nil {
		user.mu.Unlock()
		return PartShopStock{}, fmt.Errorf("shopGenerate: %w", err)
	}
	if len(parts) != int(count) {
		user.mu.Unlock()
		return PartShopStock{}, errors.New("part shop generator returned incomplete stock")
	}
	whitePrice := max(uint64(1), uint64(weaponPrice)*uint64(rules.BasicWeaponPricePercent)/100)
	if whitePrice*uint64(rules.UncommonPriceMultiplier) > uint64(PartOfferMaximumPrice) {
		user.mu.Unlock()
		return PartShopStock{}, errors.New("part shop quote exceeds client price limit")
	}
	nextID := nextVendorPartID(user.Parts)
	if nextID == 0 {
		user.mu.Unlock()
		return PartShopStock{}, ErrPartExists
	}
	nextID = max(uint64(1), user.PartShop.NextPartID, nextID)
	if nextID+uint64(len(parts)) > uint64(^uint32(0)) {
		user.mu.Unlock()
		return PartShopStock{}, ErrPartExists
	}
	for index := range parts {
		part := &parts[index]
		if part.Level != level || (part.Rarity != PartBasic && part.Rarity != PartUncommon) ||
			part.IsFlair || part.RigblockAssetID == 0 || part.SuffixAssetID == 0 {
			user.mu.Unlock()
			return PartShopStock{}, errors.New("part shop generator returned an invalid offer")
		}
		part.ID = nextID + uint64(index)
		part.ReferenceID = uint64(user.Account.ID)<<32 | part.ID
		part.CreationDate = uint64(now.Unix())
		part.EquippedToCreatureID = 0
		part.MarketStatus = PartMarketOffer
		part.Cost = uint32(whitePrice)
		if part.Rarity == PartUncommon {
			part.Cost *= rules.UncommonPriceMultiplier
		}
	}
	previousParts := user.Parts
	previousShop := user.PartShop
	retainedParts := make([]Part, 0, len(user.Parts)+len(parts))
	for _, part := range user.Parts {
		if part.MarketStatus != PartMarketOffer {
			retainedParts = append(retainedParts, part)
		}
	}
	user.Parts = append(retainedParts, parts...)
	user.PartShop = PartShopState{ExpiresAt: now.Unix() + int64(rules.RefreshSecond),
		NextPartID: nextID + uint64(len(parts)), ItemLevel: level}
	stock := user.partShopStock(true)
	user.mu.Unlock()
	err = e.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Parts = previousParts
		user.PartShop = previousShop
		user.mu.Unlock()
		return PartShopStock{}, fmt.Errorf("shopSave: %w", err)
	}
	return stock, nil
}

// Use the highest currently purchasable weapon level, with a stable price
// reference when several authored weapon families share that level.
func (e *Vendor) partShopLevel(account Account) (uint16, uint32) {
	var level uint16
	var price uint32
	var firstLevel uint16
	var firstPrice uint32
	for _, offer := range e.Offers {
		if offer.Price == 0 || offer.Part.Level == 0 {
			continue
		}
		if firstLevel == 0 || offer.Part.Level < firstLevel ||
			(offer.Part.Level == firstLevel && offer.Price < firstPrice) {
			firstLevel, firstPrice = offer.Part.Level, offer.Price
		}
		if offer.MinimumAccountLevel > account.Level ||
			offer.MinimumChainProgression > account.ChainProgression || offer.Price == 0 {
			continue
		}
		if offer.Part.Level > level || (offer.Part.Level == level && offer.Price < price) {
			level, price = offer.Part.Level, offer.Price
		}
	}
	// Weapon purchase unlocks do not hide the Parts shop. Before the first
	// weapon unlock, use the first tier displayed by the native Weapons tab.
	if level == 0 {
		return firstLevel, firstPrice
	}
	return level, price
}
