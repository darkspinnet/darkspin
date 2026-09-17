package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const crystalTuningAssetType uint32 = 0x20426a63
const crystalTuningAssetGroup uint32 = 0
const crystalTuningAssetInstance uint64 = 0xf1d031ea
const crystalTuningAssetSHA256 = "1b8869a6a260a01847d3a1b3c619744db48ed48adad391ebb7fb8e5fa3b7431d"
const crystalTuningHeaderSize = 28
const crystalTuningOffsetCount = 1
const crystalTuningRecordSize = 16
const crystalTuningDefinitionCount = 192

type crystalDefinitionAsset struct {
	ordinal         int
	minimumLevel    int32
	maximumLevel    int32
	weight          int32
	sourceReference uint32
	nounReference   string
}

type crystalLevelOffsetAsset struct {
	ordinal int
	offset  int32
	weight  float32
}

func writeCrystalTuning(ctx context.Context, transaction *sql.Tx, installPath string) error {
	packagePath := filepath.Join(installPath, "Data", "AssetData_Binary.package")
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	if len(pkg.Entries) == 0 {
		return nil
	}
	var resource *dbpf.Entry
	for index := range pkg.Entries {
		entry := &pkg.Entries[index]
		if entry.Type == crystalTuningAssetType && entry.Group == crystalTuningAssetGroup &&
			entry.Instance == crystalTuningAssetInstance {
			if resource != nil {
				return errors.New("resource duplicate")
			}
			resource = entry
		}
	}
	if resource == nil {
		return errors.New("resource missing")
	}
	payload, err := readDecodedResource(pkg, *resource)
	if err != nil {
		return fmt.Errorf("resourceRead: %w", err)
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != crystalTuningAssetSHA256 {
		return fmt.Errorf("resourceHash: got %x", digest)
	}
	definitions, err := decodeCrystalTuningAsset(payload)
	if err != nil {
		return fmt.Errorf("resourceDecode: %w", err)
	}
	offsets, err := decodeCrystalLevelOffsets(payload)
	if err != nil {
		return fmt.Errorf("levelOffsetDecode: %w", err)
	}
	offsetStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO crystal_level_offset
		(content_source_resource_id, ordinal, level_offset, weight)
		SELECT content_source_resource.id, ?, ?, ?
		FROM content_source_resource
		JOIN content_source_package
		  ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE content_source_package.package_name='AssetData_Binary.package'
		  AND content_source_resource.type_id=?
		  AND content_source_resource.group_id=?
		  AND content_source_resource.instance_id=?`)
	if err != nil {
		return fmt.Errorf("offsetPrepare: %w", err)
	}
	defer offsetStatement.Close()
	for _, offset := range offsets {
		result, insertErr := offsetStatement.ExecContext(ctx, offset.ordinal, offset.offset, offset.weight,
			int64(crystalTuningAssetType), int64(crystalTuningAssetGroup), int64(crystalTuningAssetInstance))
		if insertErr != nil {
			return fmt.Errorf("offsetInsert[%d]: %w", offset.ordinal, insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("offsetCount[%d]: %w", offset.ordinal, countErr)
		}
		if count != 1 {
			return fmt.Errorf("offsetCount[%d]: got %d", offset.ordinal, count)
		}
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO crystal_definition
		(content_source_resource_id, ordinal, minimum_level, maximum_level, weight,
		 source_reference, noun_reference)
		SELECT content_source_resource.id, ?, ?, ?, ?, ?, ?
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
	for _, definition := range definitions {
		result, insertErr := statement.ExecContext(ctx,
			definition.ordinal, definition.minimumLevel, definition.maximumLevel, definition.weight,
			int64(definition.sourceReference), definition.nounReference,
			int64(crystalTuningAssetType), int64(crystalTuningAssetGroup), int64(crystalTuningAssetInstance),
		)
		if insertErr != nil {
			return fmt.Errorf("insert[%d]: %w", definition.ordinal, insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("insertCount[%d]: %w", definition.ordinal, countErr)
		}
		if count != 1 {
			return fmt.Errorf("insertCount[%d]: got %d", definition.ordinal, count)
		}
	}
	return nil
}

func decodeCrystalLevelOffsets(payload []byte) ([]crystalLevelOffsetAsset, error) {
	if len(payload) < crystalTuningHeaderSize {
		return nil, fmt.Errorf("size: got %d, want at least %d", len(payload), crystalTuningHeaderSize)
	}
	count := binary.LittleEndian.Uint32(payload[16:20])
	if count != crystalTuningOffsetCount {
		return nil, fmt.Errorf("offsetCount: got %d", count)
	}
	offsets := make([]crystalLevelOffsetAsset, count)
	var totalWeight float64
	for index := range offsets {
		start := 20 + index*8
		offset := crystalLevelOffsetAsset{
			ordinal: index,
			offset:  int32(binary.LittleEndian.Uint32(payload[start : start+4])),
			weight:  math.Float32frombits(binary.LittleEndian.Uint32(payload[start+4 : start+8])),
		}
		if math.IsNaN(float64(offset.weight)) || math.IsInf(float64(offset.weight), 0) || offset.weight < 0 {
			return nil, fmt.Errorf("offset[%d]: invalid weight", index)
		}
		totalWeight += float64(offset.weight)
		offsets[index] = offset
	}
	if totalWeight <= 0 {
		return nil, errors.New("offsetWeight: empty")
	}
	return offsets, nil
}

func decodeCrystalTuningAsset(payload []byte) ([]crystalDefinitionAsset, error) {
	minimumSize := crystalTuningHeaderSize + crystalTuningDefinitionCount*crystalTuningRecordSize
	if len(payload) < minimumSize {
		return nil, fmt.Errorf("size: got %d, want at least %d", len(payload), minimumSize)
	}
	count := binary.LittleEndian.Uint32(payload[8:12])
	if count != crystalTuningDefinitionCount {
		return nil, fmt.Errorf("definitionCount: got %d", count)
	}
	definitions := make([]crystalDefinitionAsset, crystalTuningDefinitionCount)
	for index := range definitions {
		offset := crystalTuningHeaderSize + index*crystalTuningRecordSize
		definition := crystalDefinitionAsset{
			ordinal:         index,
			minimumLevel:    int32(binary.LittleEndian.Uint32(payload[offset : offset+4])),
			maximumLevel:    int32(binary.LittleEndian.Uint32(payload[offset+4 : offset+8])),
			weight:          int32(binary.LittleEndian.Uint32(payload[offset+8 : offset+12])),
			sourceReference: binary.LittleEndian.Uint32(payload[offset+12 : offset+16]),
		}
		if definition.minimumLevel < 0 || definition.maximumLevel < definition.minimumLevel ||
			definition.weight < 0 || definition.sourceReference == 0 {
			return nil, fmt.Errorf("record[%d]: invalid", index)
		}
		definitions[index] = definition
	}
	stringPayload := payload[minimumSize:]
	for index := range definitions {
		terminator := bytes.IndexByte(stringPayload, 0)
		if terminator < 0 {
			return nil, fmt.Errorf("noun[%d]: unterminated", index)
		}
		nounReference := string(stringPayload[:terminator])
		if nounReference == "" || !strings.HasSuffix(strings.ToLower(nounReference), ".noun") ||
			!isPrintableASCII(nounReference) {
			return nil, fmt.Errorf("noun[%d]: %q", index, nounReference)
		}
		definitions[index].nounReference = nounReference
		stringPayload = stringPayload[terminator+1:]
	}
	if len(stringPayload) != 0 {
		return nil, fmt.Errorf("trailingData: %d", len(stringPayload))
	}
	return definitions, nil
}

func isPrintableASCII(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] < 0x20 || text[index] > 0x7e {
			return false
		}
	}
	return true
}
