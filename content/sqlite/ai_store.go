package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AIAsset struct {
	ResourceID int64
	AssetType  uint32
	InstanceID uint32
	Name       string
}

const aiPayloadCacheLimit = 16 << 20

func writeAIAssets(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("aiOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("aiStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("aiPackage: %w", err)
	}
	namesByType := map[uint32]map[uint32]string{aiDefinitionAssetType: {}, aiPhaseAssetType: {}, aiConditionAssetType: {}}
	referencesByType := map[uint32]map[uint32]string{aiPhaseAssetType: {}, aiConditionAssetType: {}}
	decodedAssets := make(map[int][]byte)
	cacheBytes := 0
	for ordinal, entry := range pkg.Entries {
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("aiNameRead[%d]: %w", ordinal, readErr)
		}
		if entry.Group == 0 {
			_, isDefinition := namesByType[entry.Type]
			if isDefinition && len(payload) <= aiPayloadCacheLimit-cacheBytes {
				decodedAssets[ordinal] = payload
				cacheBytes += len(payload)
			}
		}
		for _, field := range scanCStringFields(payload) {
			for assetType, namesByInstance := range namesByType {
				suffix := ".AIDefinition"
				if assetType == aiPhaseAssetType {
					suffix = ".Phase"
				}
				if assetType == aiConditionAssetType {
					suffix = ".Condition"
				}
				if strings.HasSuffix(field.text, suffix) {
					stem := strings.TrimSuffix(field.text, suffix)
					namesByInstance[hashID(stem)] = field.text
				}
			}
		}
		if entry.Group != 0 || entry.Type != aiDefinitionAssetType {
			continue
		}
		definition, decodeErr := decodeAIDefinition(payload)
		if decodeErr != nil {
			return fmt.Errorf("aiNameDecode[%d]: %w", ordinal, decodeErr)
		}
		for _, node := range definition.Nodes {
			collectAIReferenceName(node.Phase, ".Phase", referencesByType[aiPhaseAssetType])
			collectAIReferenceName(node.Condition, ".Condition", referencesByType[aiConditionAssetType])
		}
	}
	// Typed references take precedence over the generic identifier scan. Hash
	// the entire authored stem, retaining spaces and punctuation such as '%'.
	for assetType, referencesByInstance := range referencesByType {
		for instanceID, name := range referencesByInstance {
			namesByType[assetType][instanceID] = name
		}
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Group != 0 {
			continue
		}
		namesByInstance, isAIAsset := namesByType[entry.Type]
		if !isAIAsset {
			continue
		}
		payload, isCached := decodedAssets[ordinal]
		if !isCached {
			var readErr error
			payload, readErr = readDecodedResource(ctx, pkg, entry)
			if readErr != nil {
				return fmt.Errorf("aiRead[%d]: %w", ordinal, readErr)
			}
		}
		decoded, decodeErr := decodeAIAsset(entry.Type, payload)
		if decodeErr != nil {
			return fmt.Errorf("aiDecode[%d]: %w", ordinal, decodeErr)
		}
		encoded, marshalErr := json.Marshal(decoded)
		if marshalErr != nil {
			return fmt.Errorf("aiMarshal[%d]: %w", ordinal, marshalErr)
		}
		result, insertErr := transaction.ExecContext(ctx, `
			INSERT INTO ai_asset (content_source_resource_id, asset_type, instance_id, asset_name, decoded_asset)
			SELECT resource.id, ?, ?, ?, ? FROM content_source_resource AS resource
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE package.package_name=? AND resource.ordinal=?`,
			int64(entry.Type), int64(uint32(entry.Instance)), namesByInstance[uint32(entry.Instance)],
			string(encoded), lootAssetPackage, ordinal)
		if insertErr != nil {
			return fmt.Errorf("aiInsert[%d]: %w", ordinal, insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("aiCount[%d]: %w", ordinal, countErr)
		}
		if count != 1 {
			return fmt.Errorf("aiCount[%d]: got %d", ordinal, count)
		}
	}
	return nil
}

func collectAIReferenceName(reference *string, suffix string, namesByInstance map[uint32]string) {
	if reference == nil || !strings.HasSuffix(*reference, suffix) {
		return
	}
	stem := strings.TrimSuffix(*reference, suffix)
	if stem == "" {
		return
	}
	namesByInstance[hashID(stem)] = *reference
}

func decodeAIAsset(assetType uint32, payload []byte) (any, error) {
	switch assetType {
	case aiDefinitionAssetType:
		definition, err := decodeAIDefinition(payload)
		if err != nil {
			return nil, fmt.Errorf("definitionDecode: %w", err)
		}
		return definition, nil
	case aiPhaseAssetType:
		phase, err := decodeAIPhase(payload)
		if err != nil {
			return nil, fmt.Errorf("phaseDecode: %w", err)
		}
		return phase, nil
	case aiConditionAssetType:
		condition, err := decodeAICondition(payload)
		if err != nil {
			return nil, fmt.Errorf("conditionDecode: %w", err)
		}
		return condition, nil
	default:
		return nil, fmt.Errorf("unsupported AI type %#x", assetType)
	}
}

func (e *Store) AIAssets(ctx context.Context) ([]AIAsset, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("AI store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT content_source_resource_id, asset_type, instance_id, asset_name
		FROM ai_asset ORDER BY content_source_resource_id`)
	if err != nil {
		return nil, fmt.Errorf("aiQuery: %w", err)
	}
	assets := make([]AIAsset, 0)
	for rows.Next() {
		var asset AIAsset
		err = rows.Scan(&asset.ResourceID, &asset.AssetType, &asset.InstanceID, &asset.Name)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("aiScan: %w", errors.Join(err, closeErr))
		}
		assets = append(assets, asset)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("aiRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("aiClose: %w", closeErr)
	}
	return assets, nil
}

func (e *Store) readAIAsset(ctx context.Context, assetType, instanceID uint32, decoded any) error {
	if e == nil || e.database == nil || ctx == nil {
		return errors.New("AI store or context unavailable")
	}
	var encoded string
	err := e.database.QueryRowContext(ctx, `SELECT decoded_asset FROM ai_asset WHERE asset_type=? AND instance_id=?`,
		int64(assetType), int64(instanceID)).Scan(&encoded)
	if err != nil {
		return fmt.Errorf("aiAssetQuery: %w", err)
	}
	err = json.Unmarshal([]byte(encoded), decoded)
	if err != nil {
		return fmt.Errorf("aiAssetDecode: %w", err)
	}
	return nil
}

func (e *Store) AIDefinition(ctx context.Context, instanceID uint32) (AIDefinition, error) {
	var definition AIDefinition
	err := e.readAIAsset(ctx, aiDefinitionAssetType, instanceID, &definition)
	if err != nil {
		return AIDefinition{}, fmt.Errorf("definitionLoad: %w", err)
	}
	return definition, nil
}

func (e *Store) AIPhase(ctx context.Context, instanceID uint32) (AIPhase, error) {
	var phase AIPhase
	err := e.readAIAsset(ctx, aiPhaseAssetType, instanceID, &phase)
	if err != nil {
		return AIPhase{}, fmt.Errorf("phaseLoad: %w", err)
	}
	return phase, nil
}

func (e *Store) AICondition(ctx context.Context, instanceID uint32) (AICondition, error) {
	var condition AICondition
	err := e.readAIAsset(ctx, aiConditionAssetType, instanceID, &condition)
	if err != nil {
		return AICondition{}, fmt.Errorf("conditionLoad: %w", err)
	}
	return condition, nil
}

func (e *Store) AIDefinitionForNoun(ctx context.Context, nounName string) (*AIDefinition, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("AI noun store or context unavailable")
	}
	var encoded sql.NullString
	err := e.database.QueryRowContext(ctx, `SELECT ai_asset.decoded_asset FROM non_player_noun
		LEFT JOIN ai_asset ON ai_asset.content_source_resource_id=non_player_noun.ai_definition_resource_id
		WHERE non_player_noun.noun_name=? COLLATE NOCASE`, nounName).Scan(&encoded)
	if err != nil {
		return nil, fmt.Errorf("nounAIQuery: %w", err)
	}
	if !encoded.Valid {
		return nil, nil
	}
	var definition AIDefinition
	err = json.Unmarshal([]byte(encoded.String), &definition)
	if err != nil {
		return nil, fmt.Errorf("nounAIDecode: %w", err)
	}
	return &definition, nil
}
