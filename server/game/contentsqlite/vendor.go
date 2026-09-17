package contentsqlite

import (
	"context"
	"errors"
	"fmt"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type vendorOfferStore interface {
	VendorOffers(context.Context) ([]contentsqlite.VendorOffer, error)
}

// LoadVendor projects the authored WeaponTuning rows into immutable purchase offers.
func LoadVendor(ctx context.Context, store vendorOfferStore) (*sporenet.Vendor, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if store == nil {
		return nil, errors.New("nil vendor store")
	}
	contentOffers, err := store.VendorOffers(ctx)
	if err != nil {
		return nil, fmt.Errorf("offerRead: %w", err)
	}
	offers := make([]sporenet.PartOffer, 0, len(contentOffers))
	for index, contentOffer := range contentOffers {
		part := sporenet.NewPart(uint16(contentOffer.RigblockID))
		part.Level = uint16(contentOffer.ItemLevel)
		part.Cost = contentOffer.Price
		part.MarketStatus = sporenet.PartMarketOffer
		part.SetSuffix(uint16(contentOffer.SuffixID))
		if part.RigblockAssetID != uint16(contentOffer.RigblockID) ||
			part.SuffixAssetID != uint16(contentOffer.SuffixID) {
			return nil, fmt.Errorf("offerAsset[%d]: invalid rigblock or suffix", index)
		}
		offers = append(offers, sporenet.PartOffer{
			ID: contentOffer.ID, Price: contentOffer.Price,
			MinimumAccountLevel:     contentOffer.MinimumAccountLevel,
			MinimumChainProgression: contentOffer.MinimumChainProgression,
			Part:                    part,
		})
	}
	return sporenet.NewVendor(offers...), nil
}
