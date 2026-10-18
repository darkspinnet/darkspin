package sqlite

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

type NonPlayerClassAffix struct {
	ClassResourceID   int64
	Ordinal           int
	AssetName         string
	MinimumDifficulty int32
	MaximumDifficulty int32
}

// Arrays reserve all fixed records before consuming descendant strings.
// This follows sub_F5C100, including fields between drops and elite affixes.
func decodeNonPlayerClassMetadata(payload []byte) (nonPlayerClassAsset, error) {
	cursor := assetCursor{payload: payload}
	base, err := cursor.reserve(1, nonPlayerClassPrefixSize)
	if err != nil {
		return nonPlayerClassAsset{}, fmt.Errorf("classHeader: %w", err)
	}
	_ = base
	references := make([]*string, 0, 6)
	for _, offset := range []int{16, 20, 12, 8, 80, 84} {
		reference, referenceErr := cursor.reference(offset)
		if referenceErr != nil {
			return nonPlayerClassAsset{}, fmt.Errorf("classReference[%d]: %w", offset, referenceErr)
		}
		references = append(references, reference)
	}
	class := nonPlayerClassAsset{}
	if references[2] == nil || !strings.HasSuffix(*references[2], ".ClassAttributes") {
		return class, fmt.Errorf("classAttribute: missing or invalid")
	}
	class.attributeName = *references[2]
	if references[0] != nil {
		class.displayName = *references[0]
	}
	if references[4] != nil {
		class.description = *references[4]
	}
	for index, key := range []*string{&class.displayNameLocaleKey, &class.descriptionLocaleKey} {
		reference := references[1+index*4]
		if reference == nil {
			continue
		}
		*key, err = nonPlayerLocaleKey(*reference)
		if err != nil {
			return class, fmt.Errorf("classLocale[%d]: %w", index, err)
		}
	}
	dropCount := binary.LittleEndian.Uint32(payload[44:])
	dropStart, err := cursor.reserve(dropCount, 4)
	if err != nil {
		return class, fmt.Errorf("classDrops: %w", err)
	}
	for index := range int(dropCount) {
		class.dropTypes = append(class.dropTypes, binary.LittleEndian.Uint32(payload[dropStart+index*4:]))
	}
	descriptionCount := binary.LittleEndian.Uint32(payload[108:])
	descriptionStart, err := cursor.reserve(descriptionCount, 20)
	if err != nil {
		return class, fmt.Errorf("classDescriptions: %w", err)
	}
	for index := range int(descriptionCount) {
		err = cursor.references(descriptionStart+index*20, descriptionStart+index*20+4)
		if err != nil {
			return class, fmt.Errorf("classDescription[%d]: %w", index, err)
		}
	}
	affixCount := binary.LittleEndian.Uint32(payload[116:])
	if affixCount > NonPlayerAffixLimit {
		return class, fmt.Errorf("classAffixCount: %d", affixCount)
	}
	affixStart, err := cursor.reserve(affixCount, 12)
	if err != nil {
		return class, fmt.Errorf("classAffixes: %w", err)
	}
	for index := range int(affixCount) {
		record := affixStart + index*12
		name, referenceErr := cursor.reference(record)
		if referenceErr != nil {
			return class, fmt.Errorf("classAffixReference[%d]: %w", index, referenceErr)
		}
		if name == nil || !strings.HasSuffix(*name, ".NPCAffix") || !isNonPlayerAffixName(*name) {
			return class, fmt.Errorf("classAffixName[%d]: missing or invalid", index)
		}
		class.affixes = append(class.affixes, NonPlayerClassAffix{Ordinal: index, AssetName: *name,
			MinimumDifficulty: int32(binary.LittleEndian.Uint32(payload[record+4:])),
			MaximumDifficulty: int32(binary.LittleEndian.Uint32(payload[record+8:]))})
	}
	err = cursor.finish()
	if err != nil {
		return class, fmt.Errorf("classTail: %w", err)
	}
	return class, nil
}

func (e *Store) NonPlayerClassAffixes(ctx context.Context) (affixes []NonPlayerClassAffix, resultErr error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("class affix store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT non_player_class_resource_id, ordinal,
		asset_name, minimum_difficulty, maximum_difficulty FROM non_player_class_affix
		ORDER BY non_player_class_resource_id, ordinal`)
	if err != nil {
		return nil, fmt.Errorf("classAffixQuery: %w", err)
	}
	defer closeContentRows(rows, &resultErr)
	for rows.Next() {
		var affix NonPlayerClassAffix
		err = rows.Scan(&affix.ClassResourceID, &affix.Ordinal, &affix.AssetName,
			&affix.MinimumDifficulty, &affix.MaximumDifficulty)
		if err != nil {
			return nil, fmt.Errorf("classAffixScan: %w", err)
		}
		affixes = append(affixes, affix)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("classAffixRows: %w", err)
	}
	return affixes, nil
}
