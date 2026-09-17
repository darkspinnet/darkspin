package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const weaponTuningAssetType uint32 = 0x58e9a177
const weaponTuningAssetGroup uint32 = 0
const weaponTuningAssetInstance uint64 = 0x8324b40e
const weaponTuningAssetSHA256 = "209805468d39d97df656e64c753afa6a0a4a3a7dd5471d82d515b7bb69549ffb"
const weaponTuningHeaderSize = 8
const weaponTuningRecordSize = 32
const weaponTuningOfferCount = 381

// VendorOffer is one immutable row from WeaponTuning.WeaponTuning.
type VendorOffer struct {
	ID                      uint64
	ItemLevel               uint32
	RigblockID              uint32
	SuffixID                uint32
	Price                   uint32
	MinimumAccountLevel     uint32
	MinimumChainProgression uint32
}

func writeWeaponTuning(ctx context.Context, transaction *sql.Tx, installPath string) error {
	var resourceCount int
	err := transaction.QueryRowContext(ctx,
		"SELECT resource_count FROM content_source_package WHERE package_name='AssetData_Binary.package'",
	).Scan(&resourceCount)
	if err != nil {
		return fmt.Errorf("packageCount: %w", err)
	}
	if resourceCount == 0 {
		return nil
	}
	offers, err := loadWeaponTuning(filepath.Join(installPath, "Data", "AssetData_Binary.package"))
	if err != nil {
		return fmt.Errorf("resourceLoad: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO weapon_tuning
		(content_source_resource_id, offer_id, item_level, rigblock_id, suffix_id, price,
		 minimum_account_level, minimum_chain_progression)
		SELECT content_source_resource.id, ?, ?, ?, ?, ?, ?, ?
		FROM content_source_resource
		JOIN content_source_package
		  ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE content_source_package.package_name='AssetData_Binary.package'
		  AND content_source_resource.type_id=?
		  AND content_source_resource.group_id=?
		  AND content_source_resource.instance_id=?`)
	if err != nil {
		return fmt.Errorf("insertPrepare: %w", err)
	}
	defer statement.Close()
	for index, offer := range offers {
		result, insertErr := statement.ExecContext(ctx,
			offer.ID, offer.ItemLevel, offer.RigblockID, offer.SuffixID, offer.Price,
			offer.MinimumAccountLevel, offer.MinimumChainProgression,
			int64(weaponTuningAssetType), int64(weaponTuningAssetGroup), int64(weaponTuningAssetInstance),
		)
		if insertErr != nil {
			return fmt.Errorf("insert[%d]: %w", index, insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("insertCount[%d]: %w", index, countErr)
		}
		if count != 1 {
			return fmt.Errorf("insertCount[%d]: got %d", index, count)
		}
	}
	return nil
}

func loadWeaponTuning(packagePath string) ([]VendorOffer, error) {
	r, err := os.Open(packagePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return nil, fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("packageRead: %w", err)
	}
	var resource *dbpf.Entry
	for index := range pkg.Entries {
		entry := &pkg.Entries[index]
		if entry.Type != weaponTuningAssetType || entry.Group != weaponTuningAssetGroup ||
			entry.Instance != weaponTuningAssetInstance {
			continue
		}
		if resource != nil {
			return nil, errors.New("resource duplicate")
		}
		resource = entry
	}
	if resource == nil {
		return nil, errors.New("resource missing")
	}
	payload, err := readDecodedResource(pkg, *resource)
	if err != nil {
		return nil, fmt.Errorf("resourceRead: %w", err)
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != weaponTuningAssetSHA256 {
		return nil, fmt.Errorf("resourceHash: got %x", digest)
	}
	return decodeWeaponTuning(payload)
}

func decodeWeaponTuning(payload []byte) ([]VendorOffer, error) {
	if len(payload) < weaponTuningHeaderSize {
		return nil, fmt.Errorf("size: got %d", len(payload))
	}
	count := binary.LittleEndian.Uint32(payload[4:8])
	if count != weaponTuningOfferCount {
		return nil, fmt.Errorf("offerCount: got %d", count)
	}
	expectedSize := weaponTuningHeaderSize + int(count)*weaponTuningRecordSize
	if len(payload) != expectedSize {
		return nil, fmt.Errorf("size: got %d, want %d", len(payload), expectedSize)
	}
	offers := make([]VendorOffer, count)
	offerIDs := make(map[uint64]bool, count)
	for index := range offers {
		offset := weaponTuningHeaderSize + index*weaponTuningRecordSize
		offer := VendorOffer{
			ID:                      binary.LittleEndian.Uint64(payload[offset : offset+8]),
			ItemLevel:               binary.LittleEndian.Uint32(payload[offset+8 : offset+12]),
			RigblockID:              binary.LittleEndian.Uint32(payload[offset+12 : offset+16]),
			SuffixID:                binary.LittleEndian.Uint32(payload[offset+16 : offset+20]),
			Price:                   binary.LittleEndian.Uint32(payload[offset+20 : offset+24]),
			MinimumAccountLevel:     binary.LittleEndian.Uint32(payload[offset+24 : offset+28]),
			MinimumChainProgression: binary.LittleEndian.Uint32(payload[offset+28 : offset+32]),
		}
		if offer.ID == 0 || offer.ItemLevel == 0 || offer.ItemLevel > uint32(^uint16(0)) ||
			offer.RigblockID == 0 || offer.RigblockID > uint32(^uint16(0)) ||
			offer.SuffixID == 0 || offer.SuffixID > uint32(^uint16(0)) ||
			offer.Price == 0 || offer.MinimumAccountLevel == 0 {
			return nil, fmt.Errorf("offer[%d]: invalid", index)
		}
		if offerIDs[offer.ID] {
			return nil, fmt.Errorf("offer[%d]: duplicate ID %d", index, offer.ID)
		}
		offerIDs[offer.ID] = true
		offers[index] = offer
	}
	return offers, nil
}

// VendorOffers reads the complete authored build-103 vendor catalog.
func (s *Store) VendorOffers(ctx context.Context) ([]VendorOffer, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT offer_id, item_level, rigblock_id, suffix_id, price,
		       minimum_account_level, minimum_chain_progression
		FROM weapon_tuning ORDER BY offer_id`)
	if err != nil {
		return nil, fmt.Errorf("offerQuery: %w", err)
	}
	defer rows.Close()
	offers := make([]VendorOffer, 0, weaponTuningOfferCount)
	for rows.Next() {
		var offer VendorOffer
		err = rows.Scan(&offer.ID, &offer.ItemLevel, &offer.RigblockID, &offer.SuffixID,
			&offer.Price, &offer.MinimumAccountLevel, &offer.MinimumChainProgression)
		if err != nil {
			return nil, fmt.Errorf("offerScan: %w", err)
		}
		offers = append(offers, offer)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("offerRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("offerClose: %w", err)
	}
	return offers, nil
}
