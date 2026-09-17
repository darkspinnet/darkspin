package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const (
	chainLevelAssetType     = uint32(0xa8a25294)
	chainLevelAssetGroup    = uint32(0)
	chainLevelAssetInstance = uint64(0x304f6f19)
	chainLevelHeaderSize    = 12
	chainLevelRecordSize    = 72
)

type chainLevelAsset struct {
	ordinal        int
	referenceToken uint32
	levelReference string
}

// build103ChainLevelReference is the complete authored order independently
// recovered for build 103. The fixed records contain runtime reference/pointer
// structures whose direct relocation has not been reconstructed; their
// trailing strings are serialized in record visitation order rather than
// carrying record ordinals. Validating the complete known sequence prevents a
// reordered string pool from being accepted as a different campaign.
var build103ChainLevelReference = []string{
	"zelems_1.Level", "zelems_3.Level", "nocturna_4.Level", "nocturna_1.Level",
	"verdanth_1.Level", "verdanth_3.Level", "zelems_2.Level", "zelems_4.Level",
	"cryos_4.Level", "cryos_3.Level", "verdanth_2.Level", "verdanth_4.Level",
	"infinity_2.Level", "infinity_3.Level", "cryos_1.Level", "cryos_2.Level",
	"nocturna_3.Level", "nocturna_2.Level", "infinity_1.Level", "infinity_4.Level",
	"scaldron_2.Level", "scaldron_1.Level", "scaldron_3.Level", "scaldron_4.Level",
	"cryos_1.Level", "cryos_2.Level", "zelems_2.Level", "zelems_3.Level",
	"infinity_1.Level", "infinity_3.Level", "verdanth_2.Level", "verdanth_3.Level",
	"scaldron_2.Level", "scaldron_4.Level", "cryos_4.Level", "cryos_3.Level",
	"nocturna_1.Level", "nocturna_2.Level", "infinity_4.Level", "infinity_2.Level",
	"zelems_1.Level", "zelems_4.Level", "nocturna_4.Level", "nocturna_3.Level",
	"verdanth_1.Level", "verdanth_4.Level", "scaldron_3.Level", "scaldron_1.Level",
	"nocturna_3.Level", "infinity_4.Level", "cryos_2.Level", "verdanth_1.Level",
	"scaldron_1.Level", "zelems_3.Level", "nocturna_4.Level", "infinity_3.Level",
	"zelems_2.Level", "verdanth_4.Level", "scaldron_4.Level", "cryos_1.Level",
	"verdanth_3.Level", "cryos_3.Level", "zelems_4.Level", "nocturna_2.Level",
	"infinity_2.Level", "scaldron_2.Level", "verdanth_2.Level", "zelems_1.Level",
	"cryos_4.Level", "nocturna_1.Level", "infinity_1.Level", "scaldron_3.Level",
}

func writeChainLevels(ctx context.Context, transaction *sql.Tx, installPath string) error {
	packagePath := filepath.Join(installPath, "Data", "AssetData_Binary.package")
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("assetOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("assetStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("assetPackage: %w", err)
	}
	if len(pkg.Entries) == 0 {
		return nil
	}
	entry, err := findChainLevelEntry(pkg)
	if err != nil {
		return fmt.Errorf("resourceFind: %w", err)
	}
	payload, err := readDecodedResource(pkg, entry)
	if err != nil {
		return fmt.Errorf("resourceRead: %w", err)
	}
	chainLevels, err := decodeBuild103ChainLevelAsset(payload)
	if err != nil {
		return fmt.Errorf("resourceDecode: %w", err)
	}
	var sourceResourceID int64
	err = transaction.QueryRowContext(ctx, `
		SELECT content_source_resource.id
		FROM content_source_resource
		JOIN content_source_package
		  ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE content_source_package.package_name='AssetData_Binary.package'
		  AND content_source_resource.type_id=?
		  AND content_source_resource.group_id=?
		  AND content_source_resource.instance_id=?`,
		int64(chainLevelAssetType), int64(chainLevelAssetGroup), int64(chainLevelAssetInstance),
	).Scan(&sourceResourceID)
	if err != nil {
		return fmt.Errorf("sourceLookup: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO chain_level
		(id, content_source_resource_id, ordinal, level_reference, level_id)
		VALUES (?, ?, ?, ?, (
			SELECT level_alias.level_id FROM level_alias
			WHERE level_alias.alias=? COLLATE NOCASE
		))`)
	if err != nil {
		return fmt.Errorf("insertPrepare: %w", err)
	}
	defer statement.Close()
	for _, chainLevel := range chainLevels {
		result, insertErr := statement.ExecContext(ctx,
			chainLevel.ordinal+1, sourceResourceID, chainLevel.ordinal,
			chainLevel.levelReference, chainLevel.levelReference,
		)
		if insertErr != nil {
			return fmt.Errorf("insert[%d]: %w", chainLevel.ordinal, insertErr)
		}
		count, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("insertCount[%d]: %w", chainLevel.ordinal, rowsErr)
		}
		if count != 1 {
			return fmt.Errorf("insertCount[%d]: got %d", chainLevel.ordinal, count)
		}
	}
	return nil
}

func findChainLevelEntry(pkg *dbpf.Reader) (dbpf.Entry, error) {
	if pkg == nil {
		return dbpf.Entry{}, errors.New("nil package")
	}
	var matched dbpf.Entry
	isFound := false
	for _, entry := range pkg.Entries {
		if entry.Type != chainLevelAssetType || entry.Group != chainLevelAssetGroup ||
			entry.Instance != chainLevelAssetInstance {
			continue
		}
		if isFound {
			return dbpf.Entry{}, errors.New("resource ambiguous")
		}
		matched = entry
		isFound = true
	}
	if !isFound {
		return dbpf.Entry{}, errors.New("resource missing")
	}
	return matched, nil
}

func decodeChainLevelAsset(payload []byte) ([]chainLevelAsset, error) {
	if len(payload) < chainLevelHeaderSize {
		return nil, fmt.Errorf("payloadSize: %d", len(payload))
	}
	recordCount := binary.LittleEndian.Uint32(payload[4:8])
	if recordCount == 0 {
		return nil, fmt.Errorf("recordCount: %d", recordCount)
	}
	recordEnd64 := uint64(chainLevelHeaderSize) + uint64(recordCount)*uint64(chainLevelRecordSize)
	if recordEnd64 > uint64(len(payload)) {
		return nil, fmt.Errorf("recordBounds: %d records in %d bytes", recordCount, len(payload))
	}
	recordEnd := int(recordEnd64)
	referenceTokens := make([]uint32, recordCount)
	for ordinal := range int(recordCount) {
		recordOffset := chainLevelHeaderSize + ordinal*chainLevelRecordSize
		referenceToken := binary.LittleEndian.Uint32(payload[recordOffset : recordOffset+4])
		referenceTag := binary.LittleEndian.Uint32(payload[recordOffset+4 : recordOffset+8])
		if referenceToken == 0 || referenceTag != 1 {
			return nil, fmt.Errorf("referenceRecord[%d]: token=%#x tag=%d", ordinal, referenceToken, referenceTag)
		}
		referenceTokens[ordinal] = referenceToken
	}
	fields := scanCStringFields(payload[recordEnd:])
	chainLevels := make([]chainLevelAsset, 0, int(recordCount))
	for _, field := range fields {
		if !strings.HasSuffix(strings.ToLower(field.text), ".level") {
			continue
		}
		chainLevels = append(chainLevels, chainLevelAsset{
			ordinal: len(chainLevels), referenceToken: referenceTokens[len(chainLevels)],
			levelReference: field.text,
		})
	}
	if len(chainLevels) != int(recordCount) {
		return nil, fmt.Errorf("referenceCount: got %d, want %d", len(chainLevels), recordCount)
	}
	tokenReference := make(map[uint32]string)
	referenceToken := make(map[string]uint32)
	for _, chainLevel := range chainLevels {
		if existingReference, isFound := tokenReference[chainLevel.referenceToken]; isFound && !strings.EqualFold(existingReference, chainLevel.levelReference) {
			return nil, fmt.Errorf("tokenAlias[%#x]: %q/%q", chainLevel.referenceToken,
				existingReference, chainLevel.levelReference)
		}
		lowerReference := strings.ToLower(chainLevel.levelReference)
		if existingToken, isFound := referenceToken[lowerReference]; isFound && existingToken != chainLevel.referenceToken {
			return nil, fmt.Errorf("referenceAlias[%s]: %#x/%#x", chainLevel.levelReference,
				existingToken, chainLevel.referenceToken)
		}
		tokenReference[chainLevel.referenceToken] = chainLevel.levelReference
		referenceToken[lowerReference] = chainLevel.referenceToken
	}
	return chainLevels, nil
}

func decodeBuild103ChainLevelAsset(payload []byte) ([]chainLevelAsset, error) {
	chainLevels, err := decodeChainLevelAsset(payload)
	if err != nil {
		return nil, fmt.Errorf("assetDecode: %w", err)
	}
	err = validateChainLevelSequence(chainLevels, build103ChainLevelReference)
	if err != nil {
		return nil, fmt.Errorf("sequenceValidate: %w", err)
	}
	return chainLevels, nil
}

func validateChainLevelSequence(chainLevels []chainLevelAsset, expected []string) error {
	if len(chainLevels) != len(expected) {
		return fmt.Errorf("sequenceCount: got %d, want %d", len(chainLevels), len(expected))
	}
	for ordinal, levelReference := range expected {
		if chainLevels[ordinal].ordinal != ordinal || chainLevels[ordinal].levelReference != levelReference {
			return fmt.Errorf("sequence[%d]: got %d/%q, want %q", ordinal,
				chainLevels[ordinal].ordinal, chainLevels[ordinal].levelReference, levelReference)
		}
	}
	return nil
}
