package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"regexp"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

type nonPlayerNounReference struct {
	instanceID      uint32
	classInstanceID uint32
	nounName        string
	aiInstanceID    uint32
	aggroType       *uint32
}

// Resource names are carried by authored references, not by the DBPF index.
// Collect noun names independently because a noun can reuse another noun's
// NonPlayerClass, and a NonPlayerClass can reuse another class's attributes.
func readNonPlayerNounReferences(
	pkg *dbpf.Reader,
) (map[uint32]string, []nonPlayerNounReference, error) {
	nounPattern := regexp.MustCompile(`([A-Za-z0-9_]+)\.Noun\x00`)
	classPattern := regexp.MustCompile(`([A-Za-z0-9_]+)\.NonPlayerClass\x00`)
	aiPattern := regexp.MustCompile(`([A-Za-z0-9_]+)\.AIDefinition\x00`)
	aggroTypesByInstance := make(map[uint32]uint32)
	nounNamesByInstance := make(map[uint32]string)
	nounReferences := make([]nonPlayerNounReference, 0, 600)
	for ordinal, entry := range pkg.Entries {
		decoded, err := pkg.Open(entry)
		if err != nil {
			return nil, nil, fmt.Errorf("referenceOpen[%d]: %w", ordinal, err)
		}
		payload, err := io.ReadAll(decoded)
		if err != nil {
			return nil, nil, fmt.Errorf("referenceRead[%d]: %w", ordinal, err)
		}
		for _, parts := range nounPattern.FindAllSubmatch(payload, -1) {
			stem := string(parts[1])
			nounNamesByInstance[hashID(stem)] = stem + ".Noun"
		}
		// Binary AIDefinition preserves the reflected uint32 at +120.
		if entry.Type == aiDefinitionAssetType && entry.Group == 0 {
			if len(payload) < 124 {
				return nil, nil, fmt.Errorf("aiSize[%d]: %d", ordinal, len(payload))
			}
			aggroTypesByInstance[uint32(entry.Instance)] = binary.LittleEndian.Uint32(payload[120:124])
		}
		if entry.Type != nounAssetType || entry.Group != nounAssetGroup {
			continue
		}
		parts := classPattern.FindSubmatch(payload)
		if len(parts) == 0 {
			continue
		}
		classStem := string(parts[1])
		classInstanceID := hashID(classStem)
		// Matching stems identify the ordinary noun/class pair. A shared
		// class reference never supplies the identity of its referring noun.
		if uint32(entry.Instance) == classInstanceID {
			nounNamesByInstance[classInstanceID] = classStem + ".Noun"
		}
		reference := nonPlayerNounReference{
			instanceID: uint32(entry.Instance), classInstanceID: classInstanceID,
		}
		aiParts := aiPattern.FindSubmatch(payload)
		if len(aiParts) != 0 {
			reference.aiInstanceID = hashID(string(aiParts[1]))
		}
		nounReferences = append(nounReferences, reference)
	}
	for index := range nounReferences {
		reference := &nounReferences[index]
		reference.nounName = nounNamesByInstance[reference.instanceID]
		if reference.nounName == "" {
			return nil, nil, fmt.Errorf("nounName[%#x]: missing", reference.instanceID)
		}
		if reference.aiInstanceID != 0 {
			aggroType, isFound := aggroTypesByInstance[reference.aiInstanceID]
			if !isFound {
				// Shipped nouns can reference AI assets absent from the package.
				// Retain the hash without fabricating an authored aggro mode.
				continue
			}
			reference.aggroType = &aggroType
		}
	}
	return nounNamesByInstance, nounReferences, nil
}

func writeNonPlayerNounReferences(
	ctx context.Context, transaction *sql.Tx, nounReferences []nonPlayerNounReference,
) error {
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO non_player_noun (
			content_source_resource_id, noun_name, non_player_class_resource_id, aggro_type,
			ai_definition_instance_id, ai_definition_resource_id
		)
		SELECT noun.id, ?, class.content_source_resource_id, ?, ?,
		  (SELECT content_source_resource_id FROM ai_asset WHERE asset_type=? AND instance_id=?)
		FROM content_source_resource AS noun
		JOIN content_source_package AS package ON package.id=noun.content_source_package_id
		JOIN non_player_class AS class ON class.instance_id=?
		WHERE package.package_name='AssetData_Binary.package'
		  AND noun.type_id=? AND noun.group_id=? AND noun.instance_id=?`)
	if err != nil {
		return fmt.Errorf("nounPrepare: %w", err)
	}
	defer statement.Close()
	for _, reference := range nounReferences {
		var aiInstanceID *uint32
		if reference.aiInstanceID != 0 {
			aiInstanceID = &reference.aiInstanceID
		}
		result, err := statement.ExecContext(ctx, reference.nounName, reference.aggroType, aiInstanceID,
			int64(aiDefinitionAssetType), aiInstanceID,
			int64(reference.classInstanceID), int64(nounAssetType),
			int64(nounAssetGroup), int64(reference.instanceID))
		if err != nil {
			return fmt.Errorf("nounWrite[%s]: %w", reference.nounName, err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("nounCount[%s]: %w", reference.nounName, err)
		}
		if count != 1 {
			return fmt.Errorf("nounCount[%s]: got %d", reference.nounName, count)
		}
	}
	return nil
}
