package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const assetCatalogType = uint32(0x2699c284)
const assetCatalogInstance = uint64(0xdf1fd552)

// Catalog metadata type CRCs are independent of the linked DBPF resource type.
// Entries retain their authored order even when several share one source file.
type AssetCatalogEntry struct {
	Ordinal                 uint32
	CatalogSourceResourceID int64
	AssetName               *string
	AssetNameHash           *uint32
	SourceFileName          *string
	CompileTime             uint64
	Version                 uint32
	TypeCRC                 uint32
	DataCRC                 uint32
	Tags                    []*string
	ContentSourceResourceID *int64
}

type catalogResourceKey struct {
	Type     uint32
	Instance uint64
}

var catalogResourceTypes = map[string]uint32{
	"lootrigblock":   0x1bced3d7,
	"lootprefix":     0x6a1812c6,
	"lootsuffix":     0x447dc2e5,
	"markerset":      0xa11d3144,
	"level":          0xb9193960,
	"nonplayerclass": 0xd117afca,
	"npcaffix":       npcAffixAssetType,
	"levelconfig":    0x52d095f6,
}

func decodeAssetCatalog(payload []byte) ([]AssetCatalogEntry, error) {
	if len(payload) < 8 {
		return nil, errors.New("catalog header truncated")
	}
	count := binary.LittleEndian.Uint32(payload[4:])
	if binary.LittleEndian.Uint32(payload) == 0 && count != 0 {
		return nil, errors.New("catalog array absent with nonzero count")
	}
	cursor := assetCursor{payload: payload, offset: 8}
	start, err := cursor.reserve(count, 40)
	if err != nil {
		return nil, fmt.Errorf("catalogRecords: %w", err)
	}
	entries := make([]AssetCatalogEntry, 0, count)
	for index := uint32(0); index < count; index++ {
		base := start + int(index)*40
		entry := AssetCatalogEntry{Ordinal: index, CompileTime: binary.LittleEndian.Uint64(payload[base+8:]),
			Version: binary.LittleEndian.Uint32(payload[base+16:]), TypeCRC: binary.LittleEndian.Uint32(payload[base+20:]),
			DataCRC: binary.LittleEndian.Uint32(payload[base+24:])}
		entry.AssetName, err = cursor.reference(base)
		if err != nil {
			return nil, fmt.Errorf("catalogName[%d]: %w", index, err)
		}
		if entry.AssetName != nil {
			nameHash := hashID(*entry.AssetName)
			entry.AssetNameHash = &nameHash
		}
		entry.SourceFileName, err = cursor.reference(base + 28)
		if err != nil {
			return nil, fmt.Errorf("catalogSourceName[%d]: %w", index, err)
		}
		entry.Tags, err = cursor.catalogTags(base + 32)
		if err != nil {
			return nil, fmt.Errorf("catalogTags[%d]: %w", index, err)
		}
		entries = append(entries, entry)
	}
	err = cursor.finish()
	if err != nil {
		return nil, fmt.Errorf("catalogTail: %w", err)
	}
	return entries, nil
}

func (e *assetCursor) catalogTags(fieldOffset int) ([]*string, error) {
	count := binary.LittleEndian.Uint32(e.payload[fieldOffset+4:])
	if binary.LittleEndian.Uint32(e.payload[fieldOffset:]) == 0 {
		if count != 0 {
			return nil, fmt.Errorf("tagPresence: count %d", count)
		}
		return nil, nil
	}
	start, err := e.reserve(count, 4)
	if err != nil {
		return nil, fmt.Errorf("tagRecords: %w", err)
	}
	tags := make([]*string, 0, count)
	for index := uint32(0); index < count; index++ {
		tag, readErr := e.reference(start + int(index)*4)
		if readErr != nil {
			return nil, fmt.Errorf("tagString[%d]: %w", index, readErr)
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func writeAssetCatalog(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("catalogOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("catalogStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("catalogPackage: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != assetCatalogType || entry.Group != 0 || entry.Instance != assetCatalogInstance {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("catalogRead: %w", readErr)
		}
		entries, decodeErr := decodeAssetCatalog(payload)
		if decodeErr != nil {
			return fmt.Errorf("catalogDecode: %w", decodeErr)
		}
		var resourceID int64
		err = transaction.QueryRowContext(ctx, `SELECT resource.id FROM content_source_resource AS resource
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE package.package_name=? AND resource.ordinal=?`, lootAssetPackage, ordinal).Scan(&resourceID)
		if err != nil {
			return fmt.Errorf("catalogSource: %w", err)
		}
		resourcesByKey, linkErr := catalogResourceIDs(ctx, transaction)
		if linkErr != nil {
			return fmt.Errorf("catalogResources: %w", linkErr)
		}
		for _, catalogEntry := range entries {
			if catalogEntry.AssetName != nil {
				name := path.Base(strings.ReplaceAll(*catalogEntry.AssetName, "\\", "/"))
				extension := path.Ext(name)
				resourceType, isKnown := catalogResourceTypes[strings.ToLower(strings.TrimPrefix(extension, "."))]
				if isKnown {
					key := catalogResourceKey{Type: resourceType, Instance: uint64(hashID(strings.TrimSuffix(name, extension)))}
					linkedID, isFound := resourcesByKey[key]
					if isFound {
						catalogEntry.ContentSourceResourceID = &linkedID
					}
				}
			}
			tags, marshalErr := json.Marshal(catalogEntry.Tags)
			if marshalErr != nil {
				return fmt.Errorf("catalogTagEncode: %w", marshalErr)
			}
			compileTime := make([]byte, 8)
			binary.LittleEndian.PutUint64(compileTime, catalogEntry.CompileTime)
			result, insertErr := transaction.ExecContext(ctx, `INSERT INTO asset_catalog
				(ordinal, catalog_source_resource_id, asset_name, asset_name_hash, source_file_name,
				 compile_time, version, type_crc, data_crc, tag, content_source_resource_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, catalogEntry.Ordinal, resourceID,
				catalogEntry.AssetName, catalogEntry.AssetNameHash, catalogEntry.SourceFileName,
				compileTime, catalogEntry.Version, catalogEntry.TypeCRC, catalogEntry.DataCRC,
				string(tags), catalogEntry.ContentSourceResourceID)
			if insertErr != nil {
				return fmt.Errorf("catalogInsert[%d]: %w", catalogEntry.Ordinal, insertErr)
			}
			count, countErr := result.RowsAffected()
			if countErr != nil {
				return fmt.Errorf("catalogRows: %w", countErr)
			}
			if count != 1 {
				return fmt.Errorf("catalogCount: %d", count)
			}
		}
		return nil
	}
	return errors.New("asset catalog missing")
}

func catalogResourceIDs(ctx context.Context, transaction *sql.Tx) (resourcesByKey map[catalogResourceKey]int64, resultErr error) {
	rows, err := transaction.QueryContext(ctx, `SELECT resource.id, resource.type_id, resource.instance_id
		FROM content_source_resource AS resource
		JOIN content_source_package AS package ON package.id=resource.content_source_package_id
		WHERE package.package_name=? AND resource.group_id=0`, lootAssetPackage)
	if err != nil {
		return nil, fmt.Errorf("catalogResourceQuery: %w", err)
	}
	defer closeContentRows(rows, &resultErr)
	resourcesByKey = make(map[catalogResourceKey]int64)
	for rows.Next() {
		var key catalogResourceKey
		var resourceID int64
		err = rows.Scan(&resourceID, &key.Type, &key.Instance)
		if err != nil {
			return nil, fmt.Errorf("catalogResourceScan: %w", err)
		}
		isKnown := false
		for _, resourceType := range catalogResourceTypes {
			if resourceType == key.Type {
				isKnown = true
				break
			}
		}
		if !isKnown {
			continue
		}
		if _, isFound := resourcesByKey[key]; isFound {
			return nil, fmt.Errorf("catalogResourceDuplicate: %#x/%#x", key.Type, key.Instance)
		}
		resourcesByKey[key] = resourceID
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("catalogResourceRows: %w", err)
	}
	return resourcesByKey, nil
}

func (e *Store) AssetCatalogEntries(ctx context.Context) (entries []AssetCatalogEntry, resultErr error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("catalog store unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT ordinal, catalog_source_resource_id, asset_name, asset_name_hash,
		source_file_name, compile_time, version, type_crc, data_crc, tag, content_source_resource_id
		FROM asset_catalog ORDER BY ordinal`)
	if err != nil {
		return nil, fmt.Errorf("catalogQuery: %w", err)
	}
	defer closeContentRows(rows, &resultErr)
	entries = make([]AssetCatalogEntry, 0)
	for rows.Next() {
		var entry AssetCatalogEntry
		var compileTime []byte
		var encodedTag string
		err = rows.Scan(&entry.Ordinal, &entry.CatalogSourceResourceID, &entry.AssetName, &entry.AssetNameHash,
			&entry.SourceFileName, &compileTime, &entry.Version, &entry.TypeCRC, &entry.DataCRC, &encodedTag, &entry.ContentSourceResourceID)
		if err != nil {
			return nil, fmt.Errorf("catalogScan: %w", err)
		}
		if len(compileTime) != 8 {
			return nil, fmt.Errorf("catalogCompileTime[%d]: %d", entry.Ordinal, len(compileTime))
		}
		entry.CompileTime = binary.LittleEndian.Uint64(compileTime)
		err = json.Unmarshal([]byte(encodedTag), &entry.Tags)
		if err != nil {
			return nil, fmt.Errorf("catalogTagDecode: %w", err)
		}
		entries = append(entries, entry)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("catalogReadRows: %w", err)
	}
	return entries, nil
}
