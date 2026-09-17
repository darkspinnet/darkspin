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
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const nounAssetType = uint32(0x76a8f7d8)
const nounAssetGroup = uint32(0)
const classAttributeAssetType = uint32(0xd117afca)
const classAttributeCreatureTypeOffset = 4
const characterAnimationAssetType = uint32(0x17bbce29)
const nounLifetimeOffset = 0x0c
const playableNounPhysicsCount = 100

type nounPhysicsSpec struct {
	assetName                    string
	instanceID                   uint32
	decodedSize                  int
	decodedSHA256                string
	footprintRadius              float32
	geometry                     string
	classAttributeSize           int
	classAttributeSHA256         string
	characterAnimationInstanceID uint32
	characterAnimationSize       int
	characterAnimationSHA256     string
	ordinaryDeathAnimation       string
	danceAnimation               string
	isPlayable                   bool
	lifetimeSeconds              float32
	pickupTriggerDimension       [3]float32
	pickupTriggerOffset          int
}

type nounClassAttribute struct {
	creatureType uint32
}

type nounCharacterAnimation struct {
	ordinaryDeathAnimation string
	danceAnimation         string
}

type nounPhysicsAsset struct {
	spec          nounPhysicsSpec
	graphicsScale float32
	lifetime      float32
	minimum       [3]float32
	maximum       [3]float32
	sourcePayload []byte
}

var tutorialNounPhysicsSpec = map[uint32]nounPhysicsSpec{
	0x09a3ac7f: {assetName: "TutorialBasicPoison.Noun", instanceID: 0x09a3ac7f, decodedSize: 703, decodedSHA256: "ab0ed9b141763a95afae20e6699f6e0f07e7a8ad81ffdc0f5c9b0be674336d14", footprintRadius: 1.3, geometry: "builtins!sphere", classAttributeSize: 261, classAttributeSHA256: "f4cb7e94d90a6de3ed1f3db253501488159cfd7062b7d9251cb81fcdb43e1c3e", characterAnimationInstanceID: 0x1c3ce813, characterAnimationSize: 1207, characterAnimationSHA256: "12177aa70f47a28eb949a93f0a9a1020dcdc7ab999fa9db53308d9ee3e6e97fd", ordinaryDeathAnimation: "zlm_minn_sp_3_death_melee"},
	0x52c73d4d: {assetName: "TutorialBasicDiseased.Noun", instanceID: 0x52c73d4d, decodedSize: 729, decodedSHA256: "7fa30e9983ea108ef0462c91c4b7da8870c0982439dd37c94c58ae9c7d19211c", footprintRadius: 1.4, geometry: "builtins!sphere", classAttributeSize: 268, classAttributeSHA256: "9568e825e66b0207558cef2ba6e7317d2f77fceb24ca90f004ce923aa6d890c7", characterAnimationInstanceID: 0xdd914f51, characterAnimationSize: 1207, characterAnimationSHA256: "c14922d1d5d9dec6658941c4dcba96e815126a0d61927ce4a9230fb435cac085", ordinaryDeathAnimation: "zlm_minn_sp_3_death_melee"},
	0x3aec5fe2: {assetName: "TutorialBasicRanged.Noun", instanceID: 0x3aec5fe2, decodedSize: 690, decodedSHA256: "f3530e2d3f0d7f346b906ace41dac76457fc4c1cab44d5f175aaf69e3362c82c", footprintRadius: 1.45, geometry: "builtins!sphere", classAttributeSize: 266, classAttributeSHA256: "642ba57f0025e7c43b767592153884bba4f8370830140a685512ad7aae0c08cf", characterAnimationInstanceID: 0x957c88a3, characterAnimationSize: 1197, characterAnimationSHA256: "12c875ab6b728182f054799ac4ca7b34d7e592a68f262082eee0c17134b51046", ordinaryDeathAnimation: "gen_death_melee"},
	0xf5a88155: {assetName: "TutorialSloth.Noun", instanceID: 0xf5a88155, decodedSize: 690, decodedSHA256: "a2bb19651e33a6536275472e5dd3a5b0f82b15803fc49163d867db03e2cc71a1", footprintRadius: 1.3, geometry: "builtins!sphere", classAttributeSize: 253, classAttributeSHA256: "38f59efe39a1332e2d42c3014a9f2daee651858a4f74a33444fc9154d8ed4636", characterAnimationInstanceID: 0x3e0af2fc, characterAnimationSize: 1202, characterAnimationSHA256: "0a24a173cb1213e4af0c5e6372d8050414f353ae7338507d70d6c2dff6f2eeae", ordinaryDeathAnimation: "gen_death_melee_quad"},
	0xcc7ecbe0: {assetName: "TutorialSpecialOne.Noun", instanceID: 0xcc7ecbe0, decodedSize: 707, decodedSHA256: "7f701d8df5762af189f9d5f6c325fee149d2009dd5a21fde3ff4149fa52d739a", footprintRadius: 2.3, geometry: "builtins!sphere", classAttributeSize: 284, classAttributeSHA256: "26f1acb2bddff0801801b14049e018741dca1db3c21d8ce1be38699ea5446366", characterAnimationInstanceID: 0x957c88a3, characterAnimationSize: 1197, characterAnimationSHA256: "12c875ab6b728182f054799ac4ca7b34d7e592a68f262082eee0c17134b51046", ordinaryDeathAnimation: "gen_death_melee"},
	0xa823e2e9: {assetName: "PC_EL_Rogue.Noun", instanceID: 0xa823e2e9, decodedSize: 771, decodedSHA256: "7aa6ab3a11d78d8ee830d4a2a7bd85e1ea49bf1d3c926d1710c68b67ff0bd4fe", footprintRadius: 0.8, geometry: "builtins!sphere", characterAnimationInstanceID: 0x070ef53e, characterAnimationSize: 1198, characterAnimationSHA256: "242ab598b9182f4a77a325604aafaf3dcdcd6cafccf5101f192b18e39cc73573", ordinaryDeathAnimation: "gen_player_death", danceAnimation: "emote_dance_carlton"},
	0x1d521f70: {assetName: "PC_LF_Mage.Noun", instanceID: 0x1d521f70, decodedSize: 777, decodedSHA256: "0232b342f3c1622b2eabcfefb0b1205abd6c423b9c8ca5888dac7d3467a8a1b1", footprintRadius: 0.825, geometry: "builtins!sphere", characterAnimationInstanceID: 0xdef804c7, characterAnimationSize: 1177, characterAnimationSHA256: "5081aba4541b8d3a3a8fb447f80e486879f2824b6394f3ab69284fd7d93edf1f", ordinaryDeathAnimation: "gen_player_death", danceAnimation: "emote_dance_runman_gun_r"},
	0x75bbfd0f: {assetName: "HelperMelee.Noun", instanceID: 0x75bbfd0f, decodedSize: 672, decodedSHA256: "4809ae68d0ce82ea331af1d9f0cced95c9ed3e1ef631cf41975bf4bc368a299c", footprintRadius: 0.5, geometry: "builtins!sphere"},
	0x5a5aafaf: {assetName: "Ability_Fireball.Noun", instanceID: 0x5a5aafaf, decodedSize: 629, decodedSHA256: "9011e75175f8624f04c666e2a6cb113fb94bfcd7775acdbcac0afd1f7ff85058", footprintRadius: 0, geometry: "inline!box"},
	0x75b43ff2: {assetName: "HealthOrb.Noun", instanceID: 0x75b43ff2, decodedSize: 736, decodedSHA256: "9a56b99636cc0f0b88bc9509d7e7295bff27d9841c52150229564462388eea87", geometry: "inline!box", lifetimeSeconds: 30, pickupTriggerDimension: [3]float32{2, 2, 4}, pickupTriggerOffset: 0x265},
	0xf402465f: {assetName: "manaorb.Noun", instanceID: 0xf402465f, decodedSize: 734, decodedSHA256: "70def252863f937abc90a810a008ca2a4bbbc7df9eb650b0a1444114f502f181", geometry: "inline!box", lifetimeSeconds: 30, pickupTriggerDimension: [3]float32{2, 2, 4}, pickupTriggerOffset: 0x263},
	0x801b75bd: {assetName: "HealthOrbPlaced.Noun", instanceID: 0x801b75bd, decodedSize: 736, decodedSHA256: "e37d84f0ce09489268c9b26713eda6d0a37b4289ffa09932bd135b35e40b2487", geometry: "inline!box", pickupTriggerDimension: [3]float32{1, 1, 1}, pickupTriggerOffset: 0x265},
	0x00084600: {assetName: "ManaOrbPlaced.Noun", instanceID: 0x00084600, decodedSize: 734, decodedSHA256: "6a3db09f76eda2f0015306cea7fe3b2c01e1e6f4425829f3d2ccc2b2bd99954a", geometry: "inline!box", pickupTriggerDimension: [3]float32{1, 1, 1}, pickupTriggerOffset: 0x263},
}

func curatedPlayableNounPhysicsCount() int {
	curatedPlayableCount := 0
	for _, spec := range tutorialNounPhysicsSpec {
		if strings.HasPrefix(strings.ToLower(spec.assetName), "pc_") {
			curatedPlayableCount++
		}
	}
	return curatedPlayableCount
}

func expectedNounPhysicsRowCount() int {
	return len(tutorialNounPhysicsSpec) + playableNounPhysicsCount -
		curatedPlayableNounPhysicsCount()
}

func writeNounPhysics(ctx context.Context, transaction *sql.Tx, installPath string) error {
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
	specByInstance := make(map[uint32]nounPhysicsSpec, len(tutorialNounPhysicsSpec)+100)
	for instanceID, spec := range tutorialNounPhysicsSpec {
		specByInstance[instanceID] = spec
	}
	playableCount := 0
	for ordinal, entry := range pkg.Entries {
		if entry.Type != nounAssetType || entry.Group != nounAssetGroup {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("heroNounOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("heroNounRead[%d]: %w", ordinal, readErr)
		}
		noun, isPlayable := decodePlayerNoun(payload)
		if !isPlayable {
			continue
		}
		playableCount++
		animationName, isAnimationFound := readStringWithSuffix(
			payload, nounFixedSize, 8, ".CharacterAnimation",
		)
		if !isAnimationFound {
			return fmt.Errorf("heroAnimationName[%d]: %s", ordinal, noun.BaseName)
		}
		instanceID := uint32(entry.Instance)
		animationBaseName := strings.TrimSuffix(animationName, ".CharacterAnimation")
		digest := sha256.Sum256(payload)
		spec, isCurated := specByInstance[instanceID]
		if !isCurated {
			spec = nounPhysicsSpec{
				assetName:                    noun.BaseName + ".Noun",
				instanceID:                   instanceID,
				decodedSize:                  len(payload),
				decodedSHA256:                hex.EncodeToString(digest[:]),
				footprintRadius:              readNounFloat(payload, 0x14) / 2,
				geometry:                     "builtins!sphere",
				characterAnimationInstanceID: hashID(animationBaseName),
			}
		} else if spec.characterAnimationInstanceID != hashID(animationBaseName) {
			return fmt.Errorf(
				"heroAnimationIdentity[%d]: got %#08x, want %#08x",
				ordinal, hashID(animationBaseName), spec.characterAnimationInstanceID,
			)
		}
		spec.isPlayable = true
		specByInstance[instanceID] = spec
	}
	if playableCount != 0 && playableCount != playableNounPhysicsCount {
		return fmt.Errorf(
			"heroNounCount: got %d playable nouns, want %d",
			playableCount, playableNounPhysicsCount,
		)
	}
	if playableCount != 0 && len(specByInstance) != expectedNounPhysicsRowCount() {
		return fmt.Errorf(
			"heroNounCount: got %d noun physics, want %d",
			len(specByInstance), expectedNounPhysicsRowCount(),
		)
	}
	classAttribute := make(map[uint32]nounClassAttribute, 5)
	characterAnimationSpec := make(map[uint32]nounPhysicsSpec, 32)
	for _, spec := range specByInstance {
		if spec.characterAnimationInstanceID == 0 {
			continue
		}
		current, isFound := characterAnimationSpec[spec.characterAnimationInstanceID]
		if !isFound || current.characterAnimationSHA256 == "" &&
			spec.characterAnimationSHA256 != "" {
			characterAnimationSpec[spec.characterAnimationInstanceID] = spec
		}
	}
	characterAnimation := make(map[uint32]nounCharacterAnimation, len(characterAnimationSpec))
	for ordinal, entry := range pkg.Entries {
		animationSpec, isAnimationExpected := characterAnimationSpec[uint32(entry.Instance)]
		if isAnimationExpected && entry.Type == characterAnimationAssetType &&
			entry.Group == nounAssetGroup {
			decoded, openErr := pkg.Open(entry)
			if openErr != nil {
				return fmt.Errorf("animationOpen[%d]: %w", ordinal, openErr)
			}
			payload, readErr := io.ReadAll(decoded)
			if readErr != nil {
				return fmt.Errorf("animationRead[%d]: %w", ordinal, readErr)
			}
			animation, decodeErr := decodeNounCharacterAnimation(animationSpec, payload)
			if decodeErr != nil {
				return fmt.Errorf("animationDecode[%d]: %w", ordinal, decodeErr)
			}
			characterAnimation[uint32(entry.Instance)] = animation
		}
		spec, isKnown := specByInstance[uint32(entry.Instance)]
		if !isKnown || spec.classAttributeSize == 0 || entry.Type != classAttributeAssetType ||
			entry.Group != nounAssetGroup {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("attributeOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("attributeRead[%d]: %w", ordinal, readErr)
		}
		attribute, decodeErr := decodeNounClassAttribute(spec, payload)
		if decodeErr != nil {
			return fmt.Errorf("attributeDecode[%d]: %w", ordinal, decodeErr)
		}
		classAttribute[uint32(entry.Instance)] = attribute
	}
	for ordinal, entry := range pkg.Entries {
		spec, isKnown := specByInstance[uint32(entry.Instance)]
		if !isKnown || entry.Type != nounAssetType || entry.Group != nounAssetGroup {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("payloadOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("payloadRead[%d]: %w", ordinal, readErr)
		}
		asset, decodeErr := decodeNounPhysics(spec, payload)
		if decodeErr != nil {
			return fmt.Errorf("payloadDecode[%d]: %w", ordinal, decodeErr)
		}
		attribute, isAttributeFound := classAttribute[asset.spec.instanceID]
		if asset.spec.classAttributeSize != 0 && !isAttributeFound {
			return fmt.Errorf("attributeMissing[%d]: %s", ordinal, asset.spec.assetName)
		}
		animation, isAnimationFound := characterAnimation[asset.spec.characterAnimationInstanceID]
		if asset.spec.characterAnimationInstanceID != 0 && !isAnimationFound {
			return fmt.Errorf("animationMissing[%d]: %s", ordinal, asset.spec.assetName)
		}
		err = insertNounPhysics(ctx, transaction, asset, attribute, isAttributeFound, animation, isAnimationFound)
		if err != nil {
			return fmt.Errorf("insert[%d]: %w", ordinal, err)
		}
	}
	return nil
}

func decodeNounCharacterAnimation(
	spec nounPhysicsSpec, payload []byte,
) (nounCharacterAnimation, error) {
	if spec.characterAnimationSize != 0 && len(payload) != spec.characterAnimationSize {
		return nounCharacterAnimation{}, fmt.Errorf("size: got %d, want %d", len(payload), spec.characterAnimationSize)
	}
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	if spec.characterAnimationSHA256 != "" && digestText != spec.characterAnimationSHA256 {
		return nounCharacterAnimation{}, fmt.Errorf("sha256: got %s", digestText)
	}
	if spec.ordinaryDeathAnimation != "" &&
		!containsNounAnimationState(payload, spec.ordinaryDeathAnimation) {
		return nounCharacterAnimation{}, fmt.Errorf(
			"ordinaryDeath: missing %q", spec.ordinaryDeathAnimation,
		)
	}
	danceAnimation := spec.danceAnimation
	if spec.isPlayable && danceAnimation == "" {
		resolvedAnimation, err := decodeNounDanceAnimation(payload)
		if err != nil {
			return nounCharacterAnimation{}, fmt.Errorf("danceResolve: %w", err)
		}
		danceAnimation = resolvedAnimation
	}
	if danceAnimation != "" && !containsNounAnimationState(payload, danceAnimation) {
		return nounCharacterAnimation{}, fmt.Errorf("dance: missing %q", spec.danceAnimation)
	}
	return nounCharacterAnimation{
		ordinaryDeathAnimation: spec.ordinaryDeathAnimation,
		danceAnimation:         danceAnimation,
	}, nil
}

func decodeNounDanceAnimation(payload []byte) (string, error) {
	const danceStateOffset = 324
	if len(payload) < danceStateOffset+4 {
		return "", errors.New("payload too short")
	}
	danceState := binary.LittleEndian.Uint32(payload[danceStateOffset : danceStateOffset+4])
	if danceState == 0 {
		return "", errors.New("state missing")
	}
	for _, field := range bytes.Split(payload, []byte{0}) {
		animationName := string(field)
		if strings.HasPrefix(animationName, "emote_dance_") &&
			hashID(animationName) == danceState {
			return animationName, nil
		}
	}
	return "", fmt.Errorf("state %#08x unresolved", danceState)
}

func containsNounAnimationState(payload []byte, animationName string) bool {
	for _, field := range bytes.Split(payload, []byte{0}) {
		if string(field) == animationName {
			return true
		}
	}
	return false
}

func decodeNounClassAttribute(spec nounPhysicsSpec, payload []byte) (nounClassAttribute, error) {
	if len(payload) != spec.classAttributeSize {
		return nounClassAttribute{}, fmt.Errorf("size: got %d, want %d", len(payload), spec.classAttributeSize)
	}
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	if digestText != spec.classAttributeSHA256 {
		return nounClassAttribute{}, fmt.Errorf("sha256: got %s", digestText)
	}
	creatureType := binary.LittleEndian.Uint32(
		payload[classAttributeCreatureTypeOffset : classAttributeCreatureTypeOffset+4],
	)
	if creatureType > 4 {
		return nounClassAttribute{}, fmt.Errorf("creatureType: %d", creatureType)
	}
	return nounClassAttribute{creatureType: creatureType}, nil
}

func decodeNounPhysics(spec nounPhysicsSpec, payload []byte) (nounPhysicsAsset, error) {
	if len(payload) != spec.decodedSize {
		return nounPhysicsAsset{}, fmt.Errorf("size: got %d, want %d", len(payload), spec.decodedSize)
	}
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	if digestText != spec.decodedSHA256 {
		return nounPhysicsAsset{}, fmt.Errorf("sha256: got %s", digestText)
	}
	asset := nounPhysicsAsset{spec: spec, sourcePayload: payload}
	asset.lifetime = readNounFloat(payload, nounLifetimeOffset)
	asset.graphicsScale = readNounFloat(payload, 0x14)
	for index := range 3 {
		asset.minimum[index] = readNounFloat(payload, 0x38+index*4)
		asset.maximum[index] = readNounFloat(payload, 0x44+index*4)
	}
	if !isFinitePositive(asset.graphicsScale) {
		return nounPhysicsAsset{}, errors.New("graphicsScale: invalid")
	}
	if !isFinite(asset.lifetime) || asset.lifetime < 0 || asset.lifetime != spec.lifetimeSeconds {
		return nounPhysicsAsset{}, fmt.Errorf("lifetime: got %g, want %g", asset.lifetime, spec.lifetimeSeconds)
	}
	for index := range 3 {
		if !isFinite(asset.minimum[index]) || !isFinite(asset.maximum[index]) ||
			asset.minimum[index] > asset.maximum[index] {
			return nounPhysicsAsset{}, fmt.Errorf("bound[%d]: invalid", index)
		}
	}
	if spec.assetName == "Ability_Fireball.Noun" {
		if payload[0x205] != 1 || readNounFloat(payload, 0x209) != 1 ||
			readNounFloat(payload, 0x20d) != 1 || readNounFloat(payload, 0x211) != 3 {
			return nounPhysicsAsset{}, errors.New("projectileShape: invalid")
		}
	}
	if spec.pickupTriggerOffset != 0 {
		for index, dimension := range spec.pickupTriggerDimension {
			if readNounFloat(payload, spec.pickupTriggerOffset+index*4) != dimension {
				return nounPhysicsAsset{}, fmt.Errorf("pickupDimension[%d]: invalid", index)
			}
		}
	}
	return asset, nil
}

func insertNounPhysics(
	ctx context.Context, transaction *sql.Tx, asset nounPhysicsAsset,
	attribute nounClassAttribute, isAttributeFound bool,
	animation nounCharacterAnimation, isAnimationFound bool,
) error {
	compressedPayload, err := compressContent(asset.sourcePayload)
	if err != nil {
		return fmt.Errorf("payloadCompress: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO noun_physics
		(content_source_resource_id, asset_name, lifetime_seconds, graphics_scale, footprint_radius,
		 bound_min_x, bound_min_y, bound_min_z, bound_max_x, bound_max_y, bound_max_z,
		 geometry_reference, property_reference, source_size, source_sha256, source_payload)
		SELECT id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'DefaultPhysics.prop', ?, ?, ?
		FROM content_source_resource
		WHERE content_source_package_id=(
			SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package'
		) AND type_id=? AND group_id=? AND instance_id=?`,
		asset.spec.assetName, asset.lifetime, asset.graphicsScale, asset.spec.footprintRadius,
		asset.minimum[0], asset.minimum[1], asset.minimum[2],
		asset.maximum[0], asset.maximum[1], asset.maximum[2], asset.spec.geometry,
		len(asset.sourcePayload), asset.spec.decodedSHA256, compressedPayload,
		int64(nounAssetType), int64(nounAssetGroup), int64(asset.spec.instanceID),
	)
	if err != nil {
		return fmt.Errorf("parentInsert: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("parentCount: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("parentCount: got %d", count)
	}
	if isAttributeFound {
		result, err = transaction.ExecContext(ctx, `
			UPDATE noun_physics
			SET class_attribute_resource_id=(
				SELECT id FROM content_source_resource
				WHERE content_source_package_id=(
					SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package'
				) AND type_id=? AND group_id=? AND instance_id=?
			), creature_type=?
			WHERE asset_name=?`,
			int64(classAttributeAssetType), int64(nounAssetGroup), int64(asset.spec.instanceID),
			attribute.creatureType, asset.spec.assetName,
		)
		if err != nil {
			return fmt.Errorf("attributeUpdate: %w", err)
		}
		count, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("attributeCount: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("attributeCount: got %d", count)
		}
	}
	if isAnimationFound {
		result, err = transaction.ExecContext(ctx, `
			UPDATE noun_physics
			SET character_animation_resource_id=(
				SELECT id FROM content_source_resource
				WHERE content_source_package_id=(
					SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package'
				) AND type_id=? AND group_id=? AND instance_id=?
			), ordinary_death_animation=NULLIF(?, ''), dance_animation=NULLIF(?, '')
			WHERE asset_name=?`,
			int64(characterAnimationAssetType), int64(nounAssetGroup),
			int64(asset.spec.characterAnimationInstanceID), animation.ordinaryDeathAnimation,
			animation.danceAnimation,
			asset.spec.assetName,
		)
		if err != nil {
			return fmt.Errorf("animationUpdate: %w", err)
		}
		count, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("animationCount: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("animationCount: got %d", count)
		}
	}
	if asset.spec.assetName == "Ability_Fireball.Noun" {
		err = insertNounPhysicsBoxShape(ctx, transaction, asset.spec.assetName,
			"projectile_query", [3]float32{1, 1, 3}, 517)
		if err != nil {
			return fmt.Errorf("projectileShape: %w", err)
		}
	}
	if asset.spec.pickupTriggerOffset != 0 {
		err = insertNounPhysicsBoxShape(ctx, transaction, asset.spec.assetName,
			"pickup_trigger", asset.spec.pickupTriggerDimension, asset.spec.pickupTriggerOffset)
		if err != nil {
			return fmt.Errorf("pickupShape: %w", err)
		}
	}
	return nil
}

func insertNounPhysicsBoxShape(
	ctx context.Context, transaction *sql.Tx, assetName string, shapeRole string,
	dimension [3]float32, sourceOffset int,
) error {
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO noun_physics_shape
		(noun_physics_id, content_source_resource_id, ordinal, shape_role, shape_kind,
		 dimension_x, dimension_y, dimension_z, radius, source_offset)
		SELECT id, content_source_resource_id, 0, ?, 'box', ?, ?, ?, NULL, ?
		FROM noun_physics WHERE asset_name=?`, shapeRole, dimension[0], dimension[1], dimension[2],
		sourceOffset, assetName)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	return nil
}

func readNounFloat(payload []byte, offset int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(payload[offset : offset+4]))
}

func isFinite(number float32) bool {
	return !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}

func isFinitePositive(number float32) bool {
	return isFinite(number) && number > 0
}
