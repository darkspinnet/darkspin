package sporenet

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/util"
)

// PartRarity is the inventory rarity tier.
type PartRarity uint8

const (
	PartBasic PartRarity = iota
	PartUncommon
	PartRare
	PartEpic
	PartUnique
	PartRareUnique
	PartEpicUnique
)

const (
	// PartMarketOwned is an inventory item available for equipping or selling.
	PartMarketOwned uint8 = iota
	// PartMarketOffer is reserved for store offers recovered from the client.
	PartMarketOffer
	// PartMarketBuyback is an item sold by the owner and available to buy back.
	PartMarketBuyback
)

// Part is one persistent inventory item.
type Part struct {
	ID                       uint64
	ReferenceID              uint64
	IsFlair                  bool
	Cost                     uint32
	EquippedToCreatureID     uint32
	Level                    uint16
	MarketStatus             uint8
	Rarity                   PartRarity
	Status                   uint8
	Usage                    uint8
	CreationDate             uint64
	RigblockAssetID          uint16
	PrefixAssetID            uint16
	PrefixSecondaryAssetID   uint16
	SuffixAssetID            uint16
	RigblockAssetHash        uint32
	PrefixAssetHash          uint32
	PrefixSecondaryAssetHash uint32
	SuffixAssetHash          uint32
}

// NewPart creates a part and validates its rigblock.
func NewPart(rigblock uint16) Part {
	part := Part{}
	part.SetRigblock(rigblock)
	part.SetPrefix(0, false)
	part.SetPrefix(0, true)
	part.SetSuffix(0)
	return part
}

// Normalize validates IDs and rebuilds API hashes after persistence decoding.
func (p *Part) Normalize() {
	p.SetRigblock(p.RigblockAssetID)
	p.SetPrefix(p.PrefixAssetID, false)
	p.SetPrefix(p.PrefixSecondaryAssetID, true)
	p.SetSuffix(p.SuffixAssetID)
	if p.Cost == 0 {
		p.Cost = FallbackPartCost(p.Level)
	}
}

// FallbackPartCost keeps legacy parts economically usable when no catalog-derived
// authored price was persisted.
func FallbackPartCost(level uint16) uint32 {
	if level == 0 {
		level = 1
	}
	return uint32(level) * 10
}

func (p *Part) SetRigblock(rigblock uint16) {
	if !((rigblock >= 1 && rigblock <= 1573) || (rigblock >= 10001 && rigblock <= 10835)) {
		rigblock = 1
	}
	p.RigblockAssetID = rigblock
	p.RigblockAssetHash = util.HashID(fmt.Sprintf("_Generated/LootRigblock%d.LootRigblock", rigblock))
}

func (p *Part) SetPrefix(prefix uint16, isSecondary bool) {
	if prefix < 1 || prefix > 338 {
		prefix = 0
	}
	hash := uint32(0)
	if prefix > 0 {
		name := fmt.Sprintf("_Generated/LootPrefix%d.LootPrefix", prefix)
		hash = util.HashID(name)
	}
	if isSecondary {
		p.PrefixSecondaryAssetID = prefix
		p.PrefixSecondaryAssetHash = hash
	} else {
		p.PrefixAssetID = prefix
		p.PrefixAssetHash = hash
	}
}

func (p *Part) SetSuffix(suffix uint16) {
	if !((suffix >= 1 && suffix <= 83) || (suffix >= 10001 && suffix <= 10275)) {
		suffix = 0
	}
	hash := uint32(0)
	if suffix > 0 {
		name := fmt.Sprintf("_Generated/LootSuffix%d.LootSuffix", suffix)
		hash = util.HashID(name)
	}
	p.SuffixAssetID = suffix
	p.SuffixAssetHash = hash
}

func (p Part) hasGrantIdentity(other Part) bool {
	return p.IsFlair == other.IsFlair &&
		p.Level == other.Level &&
		p.Rarity == other.Rarity &&
		p.RigblockAssetID == other.RigblockAssetID &&
		p.PrefixAssetID == other.PrefixAssetID &&
		p.PrefixSecondaryAssetID == other.PrefixSecondaryAssetID &&
		p.SuffixAssetID == other.SuffixAssetID
}

// PartOffer is one immutable packaged vendor contract. Its catalog identity,
// price, and account gates are intentionally separate from the owned part template.
type PartOffer struct {
	ID                      uint64
	Price                   uint32
	MinimumAccountLevel     uint32
	MinimumChainProgression uint32
	Part                    Part
}

// Vendor owns the rotating offers and per-user buyback inventory.
type Vendor struct {
	Offers  []PartOffer
	Buyback map[int64][]Part
}

func NewVendor(offers ...PartOffer) *Vendor {
	vendor := &Vendor{Buyback: make(map[int64][]Part)}
	vendor.Refresh(offers...)
	return vendor
}

// Refresh replaces the immutable offer snapshot loaded from packaged content.
func (v *Vendor) Refresh(offers ...PartOffer) {
	v.Offers = append(v.Offers[:0], offers...)
}

// Offer resolves one current authored catalog identity.
func (v *Vendor) Offer(id uint64) (PartOffer, bool) {
	if v == nil || id == 0 {
		return PartOffer{}, false
	}
	for _, offer := range v.Offers {
		if offer.ID == id {
			return offer, true
		}
	}
	return PartOffer{}, false
}
