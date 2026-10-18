package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const npcAffixAssetType = uint32(0x7ca6c6c9)

type NPCAffix struct {
	ResourceID        int64
	CatalogOrdinal    int
	AssetName         string
	ModifierHash      uint32
	ModifierName      *string
	ChildName         *string
	ParentName        *string
	Description       *string
	DescriptionLocale *string
	ChildResourceID   *int64
	ParentResourceID  *int64
}

func decodeNPCAffix(payload []byte) (NPCAffix, error) {
	cursor := assetCursor{payload: payload}
	base, err := cursor.reserve(1, 44)
	if err != nil {
		return NPCAffix{}, fmt.Errorf("affixHeader: %w", err)
	}
	_ = base
	affix := NPCAffix{ModifierHash: binary.LittleEndian.Uint32(payload)}
	references := []**string{&affix.ModifierName, &affix.ChildName, &affix.ParentName,
		&affix.Description, &affix.DescriptionLocale}
	for index, reference := range references {
		*reference, err = cursor.reference(12 + index*4)
		if err != nil {
			return NPCAffix{}, fmt.Errorf("affixReference[%d]: %w", index, err)
		}
	}
	err = cursor.finish()
	if err != nil {
		return NPCAffix{}, fmt.Errorf("affixTail: %w", err)
	}
	if affix.ModifierName != nil && hashID(*affix.ModifierName) != affix.ModifierHash {
		return NPCAffix{}, fmt.Errorf("affixModifierHash: mismatch %#x", affix.ModifierHash)
	}
	return affix, nil
}

func writeNPCAffixes(ctx context.Context, transaction *sql.Tx, pkg *dbpf.Reader) error {
	resourcesByKey, err := catalogResourceIDs(ctx, transaction)
	if err != nil {
		return fmt.Errorf("affixResources: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != npcAffixAssetType || entry.Group != 0 {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("affixRead[%d]: %w", ordinal, readErr)
		}
		affix, decodeErr := decodeNPCAffix(payload)
		if decodeErr != nil {
			return fmt.Errorf("affixDecode[%d]: %w", ordinal, decodeErr)
		}
		resourceID, isFound := resourcesByKey[catalogResourceKey{Type: entry.Type, Instance: entry.Instance}]
		if !isFound {
			return fmt.Errorf("affixSource[%d]: missing", ordinal)
		}
		err = transaction.QueryRowContext(ctx, `SELECT ordinal, asset_name FROM asset_catalog
			WHERE content_source_resource_id=? ORDER BY ordinal LIMIT 1`, resourceID).
			Scan(&affix.CatalogOrdinal, &affix.AssetName)
		if err != nil {
			return fmt.Errorf("affixCatalog[%d]: %w", ordinal, err)
		}
		for index, name := range []*string{affix.ChildName, affix.ParentName} {
			if name == nil {
				continue
			}
			if !strings.HasSuffix(*name, ".NPCAffix") {
				return fmt.Errorf("affixLinkName[%d:%d]: %q", ordinal, index, *name)
			}
			linkID, isLinkFound := resourcesByKey[catalogResourceKey{Type: npcAffixAssetType,
				Instance: uint64(hashID(strings.TrimSuffix(*name, ".NPCAffix")))}]
			if !isLinkFound {
				return fmt.Errorf("affixLink[%d:%d]: missing %q", ordinal, index, *name)
			}
			if index == 0 {
				affix.ChildResourceID = &linkID
			} else {
				affix.ParentResourceID = &linkID
			}
		}
		result, insertErr := transaction.ExecContext(ctx, `INSERT INTO npc_affix
			(content_source_resource_id, catalog_ordinal, asset_name, modifier_hash, modifier_name,
			 child_name, parent_name, description, description_locale, child_resource_id, parent_resource_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, resourceID, affix.CatalogOrdinal,
			affix.AssetName, int64(affix.ModifierHash), affix.ModifierName, affix.ChildName,
			affix.ParentName, affix.Description, affix.DescriptionLocale,
			affix.ChildResourceID, affix.ParentResourceID)
		if insertErr != nil {
			return fmt.Errorf("affixInsert[%d]: %w", ordinal, insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("affixCount[%d]: %w", ordinal, countErr)
		}
		if count != 1 {
			return fmt.Errorf("affixCount[%d]: %d", ordinal, count)
		}
	}
	return nil
}

func (e *Store) NPCAffixes(ctx context.Context) (affixes []NPCAffix, resultErr error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("npc affix store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT content_source_resource_id, catalog_ordinal,
		asset_name, modifier_hash, modifier_name, child_name, parent_name, description,
		description_locale, child_resource_id, parent_resource_id FROM npc_affix ORDER BY catalog_ordinal`)
	if err != nil {
		return nil, fmt.Errorf("npcAffixQuery: %w", err)
	}
	defer closeContentRows(rows, &resultErr)
	for rows.Next() {
		var affix NPCAffix
		err = rows.Scan(&affix.ResourceID, &affix.CatalogOrdinal, &affix.AssetName, &affix.ModifierHash,
			&affix.ModifierName, &affix.ChildName, &affix.ParentName, &affix.Description,
			&affix.DescriptionLocale, &affix.ChildResourceID, &affix.ParentResourceID)
		if err != nil {
			return nil, fmt.Errorf("npcAffixScan: %w", err)
		}
		affixes = append(affixes, affix)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("npcAffixRows: %w", err)
	}
	return affixes, nil
}

func verifyNPCAffixes(ctx context.Context, database *sql.DB) error {
	var sourceCount, projectionCount, coveredCount int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(affix.content_source_resource_id),
		(SELECT COUNT(*) FROM npc_affix) FROM content_source_resource AS resource
		JOIN content_source_package AS package ON package.id=resource.content_source_package_id
		LEFT JOIN npc_affix AS affix ON affix.content_source_resource_id=resource.id
		WHERE package.package_name=? AND resource.type_id=? AND resource.group_id=0`,
		lootAssetPackage, npcAffixAssetType).Scan(&sourceCount, &coveredCount, &projectionCount)
	if err != nil {
		return fmt.Errorf("npcAffixCoverage: %w", err)
	}
	if sourceCount != coveredCount || sourceCount != projectionCount {
		return fmt.Errorf("npcAffixCount: source %d covered %d projected %d",
			sourceCount, coveredCount, projectionCount)
	}
	var invalidCount int
	err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM npc_affix AS affix
		LEFT JOIN asset_catalog AS catalog ON catalog.ordinal=affix.catalog_ordinal
		LEFT JOIN npc_affix AS child ON child.content_source_resource_id=affix.child_resource_id
		LEFT JOIN npc_affix AS parent ON parent.content_source_resource_id=affix.parent_resource_id
		WHERE catalog.content_source_resource_id IS NULL
		   OR catalog.content_source_resource_id!=affix.content_source_resource_id
		   OR catalog.asset_name!=affix.asset_name
		   OR (affix.child_name IS NOT NULL AND
		       (child.asset_name IS NULL OR child.asset_name!=affix.child_name))
		   OR (affix.parent_name IS NOT NULL AND
		       (parent.asset_name IS NULL OR parent.asset_name!=affix.parent_name))`).Scan(&invalidCount)
	if err != nil {
		return fmt.Errorf("npcAffixLinkQuery: %w", err)
	}
	if invalidCount != 0 {
		return fmt.Errorf("npcAffixLink: invalid %d", invalidCount)
	}
	err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM non_player_class_affix AS entry
		LEFT JOIN npc_affix AS affix ON affix.asset_name=entry.asset_name
		WHERE affix.content_source_resource_id IS NULL`).Scan(&invalidCount)
	if err != nil {
		return fmt.Errorf("classAffixLinkQuery: %w", err)
	}
	if invalidCount != 0 {
		return fmt.Errorf("classAffixLink: unresolved %d", invalidCount)
	}
	return nil
}
