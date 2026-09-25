package sporenet

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	// ErrVendorOperation indicates an inventory transaction the server cannot safely apply.
	ErrVendorOperation = errors.New("vendor operation invalid")
	// ErrVendorPartStatus protects the recovered owned/sold inventory state graph.
	ErrVendorPartStatus = errors.New("vendor part status invalid")
	// ErrVendorFunds indicates that a charged vendor operation exceeds the account's current DNA.
	ErrVendorFunds = errors.New("vendor funds")
)

// VendorOperation is one recovered api.inventory.vendorParts operation code.
type VendorOperation byte

const (
	VendorBuyback  VendorOperation = 'b'
	VendorPurchase VendorOperation = 'p'
	VendorSell     VendorOperation = 's'
	VendorFlair    VendorOperation = 'f'
)

// VendorTransaction is one typed inventory command decoded by the HTTP adapter.
type VendorTransaction struct {
	Operation VendorOperation
	PartID    uint64
}

// VendorTransactionResult is the durable account state after a transaction batch.
type VendorTransactionResult struct {
	DNA              uint32
	Parts            []Part
	PurchasedPartIDs []uint64
}

// VendorPartPolicy supplies authored content rules consumed by inventory
// transactions without coupling the progression feature to the game catalog.
type VendorPartPolicy interface {
	PartCost(uint16) uint32
	IsFlairEligible(*Part) bool
}

// ApplyVendorTransactions atomically persists an ordered purchase, sell,
// buyback, or flair-conversion batch. Build 103 prices sell and buyback at
// half the authored cost and flair conversion at the full authored cost.
func (m *UserManager) ApplyVendorTransactions(
	ctx context.Context,
	user *User,
	transactions []VendorTransaction,
	vendor *Vendor,
	partPolicy VendorPartPolicy,
) (VendorTransactionResult, error) {
	if user == nil {
		return VendorTransactionResult{}, ErrInvalidUser
	}
	if len(transactions) == 0 {
		return VendorTransactionResult{}, ErrVendorOperation
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previousAccount := user.Account
	previousPart := append([]Part(nil), user.Parts...)
	affectedID := make([]uint64, 0, len(transactions))
	affectedSet := make(map[uint64]struct{}, len(transactions))
	purchasedPartIDs := make([]uint64, 0, len(transactions))
	rollback := func() {
		user.Account = previousAccount
		user.Parts = previousPart
	}
	for _, transaction := range transactions {
		if transaction.Operation == VendorPurchase {
			if vendor == nil {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorOperation
			}
			offer, isFound := vendor.Offer(transaction.PartID)
			if !isFound || offer.Price == 0 || user.Account.Level < offer.MinimumAccountLevel ||
				user.Account.ChainProgression < offer.MinimumChainProgression {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorOperation
			}
			if user.Account.DNA < offer.Price {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorFunds
			}
			ownedCount := uint32(0)
			for index := range user.Parts {
				if user.Parts[index].OccupiesInventorySlot() {
					ownedCount++
				}
			}
			if ownedCount >= user.Account.UnlockInventoryIdentify {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrInventoryFull
			}
			part := offer.Part
			part.ID = nextVendorPartID(user.Parts)
			if part.ID == 0 || user.Account.ID <= 0 ||
				uint64(user.Account.ID) > uint64(^uint32(0)>>1) || part.ID > uint64(^uint32(0)) {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrPartExists
			}
			part.ReferenceID = uint64(user.Account.ID)<<32 | part.ID
			part.EquippedToCreatureID = 0
			part.MarketStatus = PartMarketOwned
			part.CreationDate = uint64(time.Now().Unix())
			part.Normalize()
			user.Account.DNA -= offer.Price
			user.Parts = append(user.Parts, part)
			purchasedPartIDs = append(purchasedPartIDs, part.ID)
			affectedSet[part.ID] = struct{}{}
			affectedID = append(affectedID, part.ID)
			continue
		}
		partIndex := -1
		for index := range user.Parts {
			if user.Parts[index].ID == transaction.PartID {
				partIndex = index
				break
			}
		}
		if partIndex < 0 {
			rollback()
			user.mu.Unlock()
			return VendorTransactionResult{}, ErrPartNotFound
		}
		part := &user.Parts[partIndex]
		price := vendorPartPrice(part)
		switch transaction.Operation {
		case VendorSell:
			if part.MarketStatus != PartMarketOwned {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorPartStatus
			}
			if part.EquippedToCreatureID != 0 {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrPartEquipped
			}
			if price > ^uint32(0)-user.Account.DNA {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, errors.New("vendor DNA overflow")
			}
			part.MarketStatus = PartMarketBuyback
			user.Account.DNA += price
		case VendorBuyback:
			if part.MarketStatus != PartMarketOffer && part.MarketStatus != PartMarketBuyback {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorPartStatus
			}
			if user.Account.DNA <= price {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorFunds
			}
			ownedCount := uint32(0)
			for index := range user.Parts {
				if user.Parts[index].OccupiesInventorySlot() {
					ownedCount++
				}
			}
			if ownedCount >= user.Account.UnlockInventoryIdentify {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrInventoryFull
			}
			part.MarketStatus = PartMarketOwned
			user.Account.DNA -= price
		case VendorFlair:
			if partPolicy == nil || part.IsFlair ||
				!partPolicy.IsFlairEligible(part) {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorOperation
			}
			price = partPolicy.PartCost(part.Level)
			if price == 0 {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorOperation
			}
			if user.Account.DNA < price {
				rollback()
				user.mu.Unlock()
				return VendorTransactionResult{}, ErrVendorFunds
			}
			part.IsFlair = true
			user.Account.DNA -= price
		default:
			rollback()
			user.mu.Unlock()
			return VendorTransactionResult{}, ErrVendorOperation
		}
		if _, exists := affectedSet[part.ID]; !exists {
			affectedSet[part.ID] = struct{}{}
			affectedID = append(affectedID, part.ID)
		}
	}
	result := VendorTransactionResult{
		DNA:              user.Account.DNA,
		Parts:            make([]Part, 0, len(affectedID)),
		PurchasedPartIDs: purchasedPartIDs,
	}
	for _, partID := range affectedID {
		for index := range user.Parts {
			if user.Parts[index].ID == partID {
				result.Parts = append(result.Parts, user.Parts[index])
				break
			}
		}
	}
	user.mu.Unlock()
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		rollback()
		user.mu.Unlock()
		return VendorTransactionResult{}, fmt.Errorf("vendorSave: %w", err)
	}
	return result, nil
}

func nextVendorPartID(parts []Part) uint64 {
	nextID := uint64(1)
	for _, part := range parts {
		if part.ID < nextID {
			continue
		}
		if part.ID == ^uint64(0) {
			return 0
		}
		nextID = part.ID + 1
	}
	return nextID
}

func vendorPartPrice(part *Part) uint32 {
	if part.IsFlair {
		return 5
	}
	halfCost := float32(part.Cost) * 0.5
	return uint32(math.RoundToEven(float64(halfCost)))
}
