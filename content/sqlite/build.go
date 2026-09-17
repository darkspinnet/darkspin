package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const (
	SourceVersion  = "5.3.0.103"
	SourceBuild    = 103
	RecipeVersion  = 55
	ContentRelease = "build-103-content"
	RuntimeRole    = "runtime-content"
)

// BuildOptions identifies the installed client and destination database.
type BuildOptions struct {
	GamePath     string
	DatabasePath string
	OnProgress   func(BuildProgress)
}

// BuildProgress reports one coarse, stable content preparation phase.
type BuildProgress struct {
	Phase     string
	Completed int
	Total     int
}

// Verification summarizes a validated runtime content database.
type Verification struct {
	ContentRelease            string
	SourceBuild               int
	RecipeVersion             int
	IsSourceRecorded          bool
	IsResourceStored          bool
	IsServerDataStored        bool
	IsCreatureStored          bool
	IsNonPlayerClassStored    bool
	IsNounPhysicsStored       bool
	IsLocalizationStored      bool
	IsLevelStored             bool
	IsLevelNavigationStored   bool
	IsChainLevelStored        bool
	IsCrystalDefinitionStored bool
	IsLuaChunkStored          bool
	IsLuaStringStored         bool
	IsLuaStaticPropertyStored bool
	IsLuaTokenBindingStored   bool
	IsLootRigblockStored      bool
	IsLootAffixStored         bool
	IsLootTuningStored        bool
	IsWeaponTuningStored      bool
	IsCombatTuningStored      bool
}

type packageSpec struct {
	name             string
	relativePath     string
	isResourceStored bool
}

type inspectedPackage struct {
	spec     packageSpec
	pkg      *dbpf.Reader
	sha256   string
	fileSize int64
}

type packageInspectionResult struct {
	inspection inspectedPackage
	err        error
}

var buildPackages = []packageSpec{
	{name: "AssetData_Binary.package", relativePath: filepath.Join("Data", "AssetData_Binary.package"), isResourceStored: true},
	{name: "Levels.package", relativePath: filepath.Join("Data", "Levels.package")},
	{name: "ServerData.package", relativePath: filepath.Join("Data", "ServerData.package"), isResourceStored: true},
	{name: "Text.de-de.package", relativePath: filepath.Join("Data", "Locale", "de-de", "Text.package"), isResourceStored: true},
	{name: "Text.en-us.package", relativePath: filepath.Join("Data", "Locale", "en-us", "Text.package"), isResourceStored: true},
	{name: "Text.fr-fr.package", relativePath: filepath.Join("Data", "Locale", "fr-fr", "Text.package"), isResourceStored: true},
	{name: "Text.pl-pl.package", relativePath: filepath.Join("Data", "Locale", "pl-pl", "Text.package"), isResourceStored: true},
	{name: "Text.ru-ru.package", relativePath: filepath.Join("Data", "Locale", "ru-ru", "Text.package"), isResourceStored: true},
}

// Build creates the first recipe/version manifest and package inventory for a
// future full content projection. The target must not already exist.
func Build(ctx context.Context, options BuildOptions) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if options.DatabasePath == "" {
		return errors.New("empty database path")
	}
	_, err := os.Stat(options.DatabasePath)
	if err == nil {
		return fmt.Errorf("databaseExists: %q", options.DatabasePath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("databaseStat: %w", err)
	}

	reportBuildProgress(options.OnProgress, "Validating installed content", 0, 20)
	installPath, err := resolveInstallPath(options.GamePath)
	if err != nil {
		return fmt.Errorf("installResolve: %w", err)
	}
	err = validateSourceVersion(installPath)
	if err != nil {
		return fmt.Errorf("sourceVersion: %w", err)
	}
	reportBuildProgress(options.OnProgress, "Indexing content packages", 1, 20)
	packages, err := inspectPackages(ctx, installPath)
	if err != nil {
		return fmt.Errorf("packageInspect: %w", err)
	}

	databasePath, err := filepath.Abs(options.DatabasePath)
	if err != nil {
		return fmt.Errorf("databasePath: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(databasePath), 0o755)
	if err != nil {
		return fmt.Errorf("databaseMkdir: %w", err)
	}
	temporaryFile, err := os.CreateTemp(filepath.Dir(databasePath), ".content-*.db")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	err = temporaryFile.Close()
	if err != nil {
		return fmt.Errorf("temporaryClose: %w", err)
	}
	defer os.Remove(temporaryPath)

	err = writeContentDatabase(ctx, temporaryPath, installPath, packages, options.OnProgress)
	if err != nil {
		return fmt.Errorf("databaseWrite: %w", err)
	}
	reportBuildProgress(options.OnProgress, "Verifying prepared content", 19, 20)
	_, err = Verify(ctx, temporaryPath)
	if err != nil {
		return fmt.Errorf("databaseVerify: %w", err)
	}
	err = os.Rename(temporaryPath, databasePath)
	if err != nil {
		return fmt.Errorf("databaseInstall: %w", err)
	}
	reportBuildProgress(options.OnProgress, "Prepared content ready", 20, 20)
	return nil
}

func reportBuildProgress(report func(BuildProgress), phase string, completed int, total int) {
	if report == nil {
		return
	}
	report(BuildProgress{Phase: phase, Completed: completed, Total: total})
}

// Verify checks the stable manifest and SQLite structural integrity.
func Verify(ctx context.Context, path string) (*Verification, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if path == "" {
		return nil, errors.New("empty path")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(absolutePath)+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	defer database.Close()

	var role string
	var sourceBuild int
	var recipeVersion int
	var contentRelease string
	var isRuntimeRequired int
	err = database.QueryRowContext(ctx,
		"SELECT database_role, source_build, content_release, recipe_version, is_runtime_required FROM database_manifest WHERE id=1",
	).Scan(&role, &sourceBuild, &contentRelease, &recipeVersion, &isRuntimeRequired)
	if err != nil {
		return nil, fmt.Errorf("manifestRead: %w", err)
	}
	if role != RuntimeRole {
		return nil, fmt.Errorf("manifestRole: got %q", role)
	}
	if sourceBuild != SourceBuild {
		return nil, fmt.Errorf("manifestBuild: got %d", sourceBuild)
	}
	if recipeVersion != RecipeVersion {
		return nil, fmt.Errorf("manifestRecipe: got %d, want %d", recipeVersion, RecipeVersion)
	}
	if isRuntimeRequired != 1 {
		return nil, fmt.Errorf("manifestRuntime: got %d", isRuntimeRequired)
	}

	var integrity string
	err = database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity)
	if err != nil {
		return nil, fmt.Errorf("integrityRead: %w", err)
	}
	if integrity != "ok" {
		return nil, fmt.Errorf("integrityCheck: %s", integrity)
	}
	foreignKeyRows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return nil, fmt.Errorf("foreignKeyRead: %w", err)
	}
	if foreignKeyRows.Next() {
		_ = foreignKeyRows.Close()
		return nil, errors.New("foreignKeyCheck: violation")
	}
	err = foreignKeyRows.Err()
	if err != nil {
		_ = foreignKeyRows.Close()
		return nil, fmt.Errorf("foreignKeyRows: %w", err)
	}
	err = foreignKeyRows.Close()
	if err != nil {
		return nil, fmt.Errorf("foreignKeyClose: %w", err)
	}

	isSourceRecorded, err := hasTable(ctx, database, "content_source_package")
	if err != nil {
		return nil, fmt.Errorf("sourceTable: %w", err)
	}
	if isSourceRecorded {
		var packageCount int
		err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM content_source_package").Scan(&packageCount)
		if err != nil {
			return nil, fmt.Errorf("sourceCount: %w", err)
		}
		if packageCount != len(buildPackages) {
			return nil, fmt.Errorf("sourceCount: got %d, want %d", packageCount, len(buildPackages))
		}
	}
	isResourceStored, err := hasTable(ctx, database, "content_source_resource")
	if err != nil {
		return nil, fmt.Errorf("resourceTable: %w", err)
	}
	isServerDataStored, err := hasTable(ctx, database, "server_data")
	if err != nil {
		return nil, fmt.Errorf("serverDataTable: %w", err)
	}
	if isResourceStored {
		var resourceCount int
		err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM content_source_resource").Scan(&resourceCount)
		if err != nil {
			return nil, fmt.Errorf("resourceCount: %w", err)
		}
		var expectedResourceCount int
		err = database.QueryRowContext(ctx, "SELECT COALESCE(SUM(resource_count), 0) FROM content_source_package WHERE is_resource_stored=1").Scan(&expectedResourceCount)
		if err != nil {
			return nil, fmt.Errorf("resourceExpected: %w", err)
		}
		if resourceCount != expectedResourceCount {
			return nil, fmt.Errorf("resourceCount: got %d, want %d", resourceCount, expectedResourceCount)
		}
		if !isServerDataStored && contentRelease == ContentRelease {
			return nil, errors.New("serverDataTable: missing")
		}
	} else {
		isContentAssetStored, assetErr := hasTable(ctx, database, "content_asset")
		if assetErr != nil {
			return nil, fmt.Errorf("contentAssetTable: %w", assetErr)
		}
		if !isContentAssetStored || !isServerDataStored {
			return nil, errors.New("resourceTable: missing")
		}
		isResourceStored = true
	}
	if isServerDataStored {
		var serverDataCount int
		var compiledLuaCount int
		err = database.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(is_compiled_lua), 0) FROM server_data").Scan(&serverDataCount, &compiledLuaCount)
		if err != nil {
			return nil, fmt.Errorf("serverDataCount: %w", err)
		}
		expectedServerDataCount := 1101
		if isSourceRecorded {
			err = database.QueryRowContext(ctx,
				"SELECT resource_count FROM content_source_package WHERE package_name='ServerData.package'",
			).Scan(&expectedServerDataCount)
			if err != nil {
				return nil, fmt.Errorf("serverDataExpected: %w", err)
			}
		}
		if serverDataCount != expectedServerDataCount {
			return nil, fmt.Errorf("serverDataCount: got %d, want %d", serverDataCount, expectedServerDataCount)
		}
		if expectedServerDataCount == 1101 && compiledLuaCount != 1029 {
			return nil, fmt.Errorf("compiledLuaCount: got %d, want 1029", compiledLuaCount)
		}
	}
	isLootRigblockStored, err := hasTable(ctx, database, "loot_rigblock")
	if err != nil {
		return nil, fmt.Errorf("lootRigblockTable: %w", err)
	}
	if !isLootRigblockStored {
		return nil, errors.New("lootRigblockTable: missing")
	}
	var lootRigblockCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM loot_rigblock").Scan(&lootRigblockCount)
	if err != nil {
		return nil, fmt.Errorf("lootRigblockCount: %w", err)
	}
	var lootSourceCount int
	err = database.QueryRowContext(ctx,
		"SELECT resource_count FROM content_source_package WHERE package_name='AssetData_Binary.package'",
	).Scan(&lootSourceCount)
	if err != nil {
		return nil, fmt.Errorf("lootRigblockSource: %w", err)
	}
	expectedLootRigblockCount := 0
	if lootSourceCount != 0 {
		expectedLootRigblockCount = 2288
	}
	if lootRigblockCount != expectedLootRigblockCount {
		return nil, fmt.Errorf("lootRigblockCount: got %d, want %d", lootRigblockCount, expectedLootRigblockCount)
	}
	isLootAffixStored, err := hasTable(ctx, database, "loot_affix")
	if err != nil {
		return nil, fmt.Errorf("lootAffixTable: %w", err)
	}
	if !isLootAffixStored {
		return nil, errors.New("lootAffixTable: missing")
	}
	var lootAffixCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM loot_affix").Scan(&lootAffixCount)
	if err != nil {
		return nil, fmt.Errorf("lootAffixCount: %w", err)
	}
	expectedLootAffixCount := 0
	if lootSourceCount != 0 {
		expectedLootAffixCount = 666
	}
	if lootAffixCount != expectedLootAffixCount {
		return nil, fmt.Errorf("lootAffixCount: got %d, want %d", lootAffixCount, expectedLootAffixCount)
	}
	isLootTuningStored, err := hasTable(ctx, database, "loot_tuning")
	if err != nil {
		return nil, fmt.Errorf("lootTuningTable: %w", err)
	}
	if !isLootTuningStored {
		return nil, errors.New("lootTuningTable: missing")
	}
	var lootTuningCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM loot_tuning").Scan(&lootTuningCount)
	if err != nil {
		return nil, fmt.Errorf("lootTuningCount: %w", err)
	}
	expectedLootTuningCount := 0
	if lootSourceCount != 0 {
		expectedLootTuningCount = 1
	}
	if lootTuningCount != expectedLootTuningCount {
		return nil, fmt.Errorf("lootTuningCount: got %d, want %d", lootTuningCount, expectedLootTuningCount)
	}
	isWeaponTuningStored, err := hasTable(ctx, database, "weapon_tuning")
	if err != nil {
		return nil, fmt.Errorf("weaponTuningTable: %w", err)
	}
	if !isWeaponTuningStored {
		return nil, errors.New("weaponTuningTable: missing")
	}
	var weaponTuningCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM weapon_tuning").Scan(&weaponTuningCount)
	if err != nil {
		return nil, fmt.Errorf("weaponTuningCount: %w", err)
	}
	expectedWeaponTuningCount := 0
	if lootSourceCount != 0 {
		expectedWeaponTuningCount = weaponTuningOfferCount
	}
	if weaponTuningCount != expectedWeaponTuningCount {
		return nil, fmt.Errorf("weaponTuningCount: got %d, want %d", weaponTuningCount, expectedWeaponTuningCount)
	}
	isCombatTuningStored, err := hasTable(ctx, database, "combat_tuning")
	if err != nil {
		return nil, fmt.Errorf("combatTuningTable: %w", err)
	}
	if !isCombatTuningStored {
		return nil, errors.New("combatTuningTable: missing")
	}
	var combatTuningCount, ratingConversionCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM combat_tuning").Scan(&combatTuningCount)
	if err != nil {
		return nil, fmt.Errorf("combatTuningCount: %w", err)
	}
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM difficulty_tuning").Scan(&ratingConversionCount)
	if err != nil {
		return nil, fmt.Errorf("ratingConversionCount: %w", err)
	}
	expectedCombatTuningCount := 0
	expectedRatingConversionCount := 0
	if lootSourceCount != 0 {
		expectedCombatTuningCount = 1
		expectedRatingConversionCount = difficultyTuningCount
	}
	if combatTuningCount != expectedCombatTuningCount || ratingConversionCount != expectedRatingConversionCount {
		return nil, fmt.Errorf("combatTuningCount: got %d/%d, want %d/%d",
			combatTuningCount, ratingConversionCount, expectedCombatTuningCount, expectedRatingConversionCount)
	}
	isCreatureStored, err := hasTable(ctx, database, "creature_template")
	if err != nil {
		return nil, fmt.Errorf("creatureTable: %w", err)
	}
	if !isCreatureStored {
		return nil, errors.New("creatureTable: missing")
	}
	var creatureCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM creature_template").Scan(&creatureCount)
	if err != nil {
		return nil, fmt.Errorf("creatureCount: %w", err)
	}
	expectedCreatureCount := 100
	if isSourceRecorded {
		var assetResourceCount int
		err = database.QueryRowContext(ctx,
			"SELECT resource_count FROM content_source_package WHERE package_name='AssetData_Binary.package'",
		).Scan(&assetResourceCount)
		if err != nil {
			return nil, fmt.Errorf("creatureSource: %w", err)
		}
		if assetResourceCount == 0 {
			expectedCreatureCount = 0
		}
	}
	if creatureCount != expectedCreatureCount {
		return nil, fmt.Errorf("creatureCount: got %d, want %d", creatureCount, expectedCreatureCount)
	}
	var creatureAbilityCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM creature_template_ability").Scan(&creatureAbilityCount)
	if err != nil {
		return nil, fmt.Errorf("creatureAbilityCount: %w", err)
	}
	expectedCreatureAbilityCount := creatureCount * len(creatureAbilitySlots)
	if creatureAbilityCount != expectedCreatureAbilityCount {
		return nil, fmt.Errorf("creatureAbilityCount: got %d, want %d", creatureAbilityCount, expectedCreatureAbilityCount)
	}
	var incompleteCreatureAbilityCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT creature_template_id
			FROM creature_template_ability
			GROUP BY creature_template_id
			HAVING COUNT(*) != ? OR COUNT(DISTINCT slot) != ?
		)`, len(creatureAbilitySlots), len(creatureAbilitySlots)).Scan(&incompleteCreatureAbilityCount)
	if err != nil {
		return nil, fmt.Errorf("creatureAbilityShape: %w", err)
	}
	if incompleteCreatureAbilityCount != 0 {
		return nil, fmt.Errorf("creatureAbilityShape: got %d incomplete templates", incompleteCreatureAbilityCount)
	}
	isNonPlayerClassStored, err := hasTable(ctx, database, "non_player_class")
	if err != nil {
		return nil, fmt.Errorf("nonPlayerClassTable: %w", err)
	}
	if !isNonPlayerClassStored {
		return nil, errors.New("nonPlayerClassTable: missing")
	}
	var nonPlayerClassCount int
	var expectedNonPlayerClassCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM non_player_class").Scan(&nonPlayerClassCount)
	if err != nil {
		return nil, fmt.Errorf("nonPlayerClassCount: %w", err)
	}
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM content_source_resource
		JOIN content_source_package
		  ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE content_source_package.package_name='AssetData_Binary.package'
		  AND content_source_resource.type_id=? AND content_source_resource.group_id=?`,
		int64(nonPlayerClassAssetType), int64(nonPlayerClassAssetGroup),
	).Scan(&expectedNonPlayerClassCount)
	if err != nil {
		return nil, fmt.Errorf("nonPlayerClassExpected: %w", err)
	}
	if nonPlayerClassCount != expectedNonPlayerClassCount {
		return nil, fmt.Errorf("nonPlayerClassCount: got %d, want %d", nonPlayerClassCount, expectedNonPlayerClassCount)
	}
	var invalidNonPlayerIdentityCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM non_player_class
		WHERE noun_name=''
		   OR (display_name_locale_key<>'' AND display_name_locale_key NOT GLOB '0x????????')
		   OR (description_locale_key<>'' AND description_locale_key NOT GLOB '0x????????')`,
	).Scan(&invalidNonPlayerIdentityCount)
	if err != nil {
		return nil, fmt.Errorf("nonPlayerIdentityValidate: %w", err)
	}
	if invalidNonPlayerIdentityCount != 0 {
		return nil, fmt.Errorf("nonPlayerIdentityValidate: invalid=%d", invalidNonPlayerIdentityCount)
	}
	var invalidNonPlayerAffixCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM non_player_class_affix
		LEFT JOIN non_player_class
		  ON non_player_class.content_source_resource_id=
		     non_player_class_affix.non_player_class_resource_id
		WHERE non_player_class.content_source_resource_id IS NULL
		   OR non_player_class_affix.ordinal < 0
		   OR non_player_class_affix.ordinal >= ?
		   OR non_player_class_affix.asset_name NOT LIKE '%.NPCAffix'`,
		NonPlayerAffixLimit,
	).Scan(&invalidNonPlayerAffixCount)
	if err != nil {
		return nil, fmt.Errorf("nonPlayerAffixValidate: %w", err)
	}
	if invalidNonPlayerAffixCount != 0 {
		return nil, fmt.Errorf("nonPlayerAffixValidate: invalid=%d", invalidNonPlayerAffixCount)
	}
	isNounPhysicsStored, err := hasTable(ctx, database, "noun_physics")
	if err != nil {
		return nil, fmt.Errorf("nounPhysicsTable: %w", err)
	}
	if !isNounPhysicsStored {
		return nil, errors.New("nounPhysicsTable: missing")
	}
	var nounPhysicsCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM noun_physics").Scan(&nounPhysicsCount)
	if err != nil {
		return nil, fmt.Errorf("nounPhysicsCount: %w", err)
	}
	var assetResourceCount int
	err = database.QueryRowContext(ctx,
		"SELECT resource_count FROM content_source_package WHERE package_name='AssetData_Binary.package'",
	).Scan(&assetResourceCount)
	if err != nil {
		return nil, fmt.Errorf("nounPhysicsSource: %w", err)
	}
	expectedNounPhysicsCount := 0
	if assetResourceCount != 0 {
		expectedNounPhysicsCount = len(tutorialNounPhysicsSpec)
		if creatureCount != 0 {
			expectedNounPhysicsCount += creatureCount - curatedPlayableNounPhysicsCount()
		}
	}
	if nounPhysicsCount != expectedNounPhysicsCount {
		return nil, fmt.Errorf("nounPhysicsCount: got %d, want %d", nounPhysicsCount, expectedNounPhysicsCount)
	}
	var creatureTypeCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM noun_physics WHERE creature_type IS NOT NULL`,
	).Scan(&creatureTypeCount)
	if err != nil {
		return nil, fmt.Errorf("nounCreatureTypeCount: %w", err)
	}
	expectedCreatureTypeCount := 0
	if assetResourceCount != 0 {
		for _, spec := range tutorialNounPhysicsSpec {
			if spec.classAttributeSize != 0 {
				expectedCreatureTypeCount++
			}
		}
	}
	if creatureTypeCount != expectedCreatureTypeCount {
		return nil, fmt.Errorf("nounCreatureTypeCount: got %d, want %d", creatureTypeCount, expectedCreatureTypeCount)
	}
	var deathAnimationCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM noun_physics WHERE ordinary_death_animation IS NOT NULL`,
	).Scan(&deathAnimationCount)
	if err != nil {
		return nil, fmt.Errorf("nounDeathAnimationCount: %w", err)
	}
	expectedDeathAnimationCount := 0
	expectedDanceAnimationCount := 0
	if assetResourceCount != 0 {
		for _, spec := range tutorialNounPhysicsSpec {
			if spec.ordinaryDeathAnimation != "" {
				expectedDeathAnimationCount++
			}
			if spec.danceAnimation != "" {
				expectedDanceAnimationCount++
			}
		}
		if creatureCount != 0 {
			expectedDanceAnimationCount = creatureCount
		}
	}
	if deathAnimationCount != expectedDeathAnimationCount {
		return nil, fmt.Errorf(
			"nounDeathAnimationCount: got %d, want %d", deathAnimationCount, expectedDeathAnimationCount,
		)
	}
	var danceAnimationCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM noun_physics WHERE dance_animation IS NOT NULL`,
	).Scan(&danceAnimationCount)
	if err != nil {
		return nil, fmt.Errorf("nounDanceAnimationCount: %w", err)
	}
	if danceAnimationCount != expectedDanceAnimationCount {
		return nil, fmt.Errorf(
			"nounDanceAnimationCount: got %d, want %d", danceAnimationCount, expectedDanceAnimationCount,
		)
	}
	var nounShapeCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM noun_physics_shape").Scan(&nounShapeCount)
	if err != nil {
		return nil, fmt.Errorf("nounShapeCount: %w", err)
	}
	expectedNounShapeCount := 0
	if assetResourceCount != 0 {
		expectedNounShapeCount = 1
		for _, spec := range tutorialNounPhysicsSpec {
			if spec.pickupTriggerOffset != 0 {
				expectedNounShapeCount++
			}
		}
	}
	if nounShapeCount != expectedNounShapeCount {
		return nil, fmt.Errorf("nounShapeCount: got %d, want %d", nounShapeCount, expectedNounShapeCount)
	}
	var invalidOrbLifetimeCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM noun_physics
		WHERE (asset_name IN ('HealthOrb.Noun', 'manaorb.Noun') AND lifetime_seconds != 30)
		   OR (asset_name IN ('HealthOrbPlaced.Noun', 'ManaOrbPlaced.Noun') AND lifetime_seconds != 0)`,
	).Scan(&invalidOrbLifetimeCount)
	if err != nil {
		return nil, fmt.Errorf("orbLifetime: %w", err)
	}
	if invalidOrbLifetimeCount != 0 {
		return nil, fmt.Errorf("orbLifetime: %d invalid rows", invalidOrbLifetimeCount)
	}
	var invalidNounShapeCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM noun_physics_shape
		JOIN noun_physics ON noun_physics.id=noun_physics_shape.noun_physics_id
		WHERE noun_physics.content_source_resource_id != noun_physics_shape.content_source_resource_id`,
	).Scan(&invalidNounShapeCount)
	if err != nil {
		return nil, fmt.Errorf("nounPhysicsShape: %w", err)
	}
	if invalidNounShapeCount != 0 {
		return nil, fmt.Errorf("nounPhysicsShape: %d mismatched sources", invalidNounShapeCount)
	}
	isLocalizationStored, err := hasTable(ctx, database, "localization_text")
	if err != nil {
		return nil, fmt.Errorf("localeTable: %w", err)
	}
	if !isLocalizationStored {
		return nil, errors.New("localeTable: missing")
	}
	var localizationCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM localization_text").Scan(&localizationCount)
	if err != nil {
		return nil, fmt.Errorf("localeCount: %w", err)
	}
	expectedLocalizationCount := 50661
	if isSourceRecorded {
		var localeResourceCount int
		err = database.QueryRowContext(ctx,
			"SELECT COALESCE(SUM(resource_count), 0) FROM content_source_package WHERE package_name LIKE 'Text.%'",
		).Scan(&localeResourceCount)
		if err != nil {
			return nil, fmt.Errorf("localeSource: %w", err)
		}
		if localeResourceCount == 0 {
			expectedLocalizationCount = 0
		}
	}
	if localizationCount != expectedLocalizationCount {
		return nil, fmt.Errorf("localeCount: got %d, want %d", localizationCount, expectedLocalizationCount)
	}
	isLevelStored, err := hasTable(ctx, database, "level")
	if err != nil {
		return nil, fmt.Errorf("levelTable: %w", err)
	}
	if !isLevelStored {
		return nil, errors.New("levelTable: missing")
	}
	var levelCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM level").Scan(&levelCount)
	if err != nil {
		return nil, fmt.Errorf("levelCount: %w", err)
	}
	expectedLevelCount := 61
	if isSourceRecorded {
		var assetResourceCount int
		err = database.QueryRowContext(ctx,
			"SELECT resource_count FROM content_source_package WHERE package_name='AssetData_Binary.package'",
		).Scan(&assetResourceCount)
		if err != nil {
			return nil, fmt.Errorf("levelSource: %w", err)
		}
		if assetResourceCount == 0 {
			expectedLevelCount = 0
		}
	}
	if levelCount != expectedLevelCount {
		return nil, fmt.Errorf("levelCount: got %d, want %d", levelCount, expectedLevelCount)
	}
	isLevelNavigationStored, err := hasTable(ctx, database, "level_navigation")
	if err != nil {
		return nil, fmt.Errorf("levelNavigationTable: %w", err)
	}
	if !isLevelNavigationStored {
		return nil, errors.New("levelNavigationTable: missing")
	}
	var levelNavigationCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM level_navigation
		WHERE type_id=? AND decoded_size>48 AND decoded_compression='zlib'
		      AND length(decoded_sha256)=64 AND length(decoded_payload)>0`,
		bfxNavigationType,
	).Scan(&levelNavigationCount)
	if err != nil {
		return nil, fmt.Errorf("levelNavigationCount: %w", err)
	}
	var totalLevelNavigationCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM level_navigation").Scan(
		&totalLevelNavigationCount,
	)
	if err != nil {
		return nil, fmt.Errorf("levelNavigationTotal: %w", err)
	}
	expectedLevelNavigationCount := 0
	expectedMissingNavigationCount := 0
	if levelCount > 0 {
		expectedLevelNavigationCount = 59
		expectedMissingNavigationCount = 2
	}
	var missingNavigationCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM level
		LEFT JOIN level_navigation ON level_navigation.level_id=level.id
		WHERE level_navigation.level_id IS NULL
		      AND level.name IN ('test_AI_zoo_ugc', 'test_holodeck')`,
	).Scan(&missingNavigationCount)
	if err != nil {
		return nil, fmt.Errorf("levelNavigationMissingCount: %w", err)
	}
	if levelNavigationCount != expectedLevelNavigationCount ||
		totalLevelNavigationCount != expectedLevelNavigationCount {
		var missingLevelNames string
		err = database.QueryRowContext(ctx, `
			SELECT COALESCE(GROUP_CONCAT(level.name, ','), '')
			FROM level
			LEFT JOIN level_navigation ON level_navigation.level_id=level.id
			WHERE level.nav_mesh!='' AND level_navigation.level_id IS NULL`,
		).Scan(&missingLevelNames)
		if err != nil {
			return nil, fmt.Errorf("levelNavigationMissing: %w", err)
		}
		return nil, fmt.Errorf(
			"levelNavigationCount: got %d/%d valid/total, want %d; missing %q",
			levelNavigationCount, totalLevelNavigationCount,
			expectedLevelNavigationCount, missingLevelNames,
		)
	}
	if missingNavigationCount != expectedMissingNavigationCount {
		return nil, fmt.Errorf(
			"levelNavigationMissingCount: got %d, want %d",
			missingNavigationCount, expectedMissingNavigationCount,
		)
	}
	if levelCount > 0 {
		var tutorialAliasCount int
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM level_alias
			JOIN level ON level.id=level_alias.level_id
			WHERE level_alias.alias=? AND level.name=?`,
			tutorialLevelAlias, tutorialLevelName,
		).Scan(&tutorialAliasCount)
		if err != nil {
			return nil, fmt.Errorf("tutorialAlias: %w", err)
		}
		if tutorialAliasCount != 1 {
			return nil, fmt.Errorf("tutorialAlias: got %d, want 1", tutorialAliasCount)
		}
		var markerCount int
		err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM marker").Scan(&markerCount)
		if err != nil {
			return nil, fmt.Errorf("markerCount: %w", err)
		}
		if markerCount == 0 {
			return nil, errors.New("markerCount: empty")
		}
		var invalidTeleporterDestinationCount int
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM marker AS teleporter
			LEFT JOIN marker AS destination
			  ON destination.level_marker_set_id=teleporter.level_marker_set_id
			 AND destination.marker_id=teleporter.target_marker_id
			WHERE teleporter.noun_name='Teleporter.Noun' COLLATE NOCASE
			  AND (teleporter.target_marker_id=0 OR destination.id IS NULL)`,
		).Scan(&invalidTeleporterDestinationCount)
		if err != nil {
			return nil, fmt.Errorf("teleporterDestination: %w", err)
		}
		if invalidTeleporterDestinationCount != 0 {
			return nil, fmt.Errorf(
				"teleporterDestination: %d invalid routes",
				invalidTeleporterDestinationCount,
			)
		}
	}
	isChainLevelStored, err := hasTable(ctx, database, "chain_level")
	if err != nil {
		return nil, fmt.Errorf("chainLevelTable: %w", err)
	}
	if !isChainLevelStored {
		return nil, errors.New("chainLevelTable: missing")
	}
	var chainLevelCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM chain_level").Scan(&chainLevelCount)
	if err != nil {
		return nil, fmt.Errorf("chainLevelCount: %w", err)
	}
	expectedChainLevelCount := 72
	if levelCount == 0 {
		expectedChainLevelCount = 0
	}
	if chainLevelCount != expectedChainLevelCount {
		return nil, fmt.Errorf("chainLevelCount: got %d, want %d", chainLevelCount, expectedChainLevelCount)
	}
	if chainLevelCount > 0 {
		var minimumOrdinal, maximumOrdinal, distinctOrdinal int
		err = database.QueryRowContext(ctx,
			"SELECT MIN(ordinal), MAX(ordinal), COUNT(DISTINCT ordinal) FROM chain_level",
		).Scan(&minimumOrdinal, &maximumOrdinal, &distinctOrdinal)
		if err != nil {
			return nil, fmt.Errorf("chainLevelOrdinal: %w", err)
		}
		if minimumOrdinal != 0 || maximumOrdinal != 71 || distinctOrdinal != 72 {
			return nil, fmt.Errorf("chainLevelOrdinal: got %d..%d/%d", minimumOrdinal, maximumOrdinal, distinctOrdinal)
		}
		var invalidReferenceCount, unresolvedCount, distinctLevelCount int
		err = database.QueryRowContext(ctx, `
			SELECT
				SUM(CASE WHEN LOWER(level_reference) NOT LIKE '%.level' THEN 1 ELSE 0 END),
				SUM(CASE WHEN level_id IS NULL THEN 1 ELSE 0 END),
				COUNT(DISTINCT level_id)
			FROM chain_level`,
		).Scan(&invalidReferenceCount, &unresolvedCount, &distinctLevelCount)
		if err != nil {
			return nil, fmt.Errorf("chainLevelReference: %w", err)
		}
		if invalidReferenceCount != 0 || unresolvedCount != 0 || distinctLevelCount != 24 {
			return nil, fmt.Errorf("chainLevelReference: invalid=%d unresolved=%d distinct=%d",
				invalidReferenceCount, unresolvedCount, distinctLevelCount)
		}
		var repetitionErrorCount int
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM (
				SELECT level_id FROM chain_level GROUP BY level_id HAVING COUNT(*) != 3
			)`,
		).Scan(&repetitionErrorCount)
		if err != nil {
			return nil, fmt.Errorf("chainLevelRepetition: %w", err)
		}
		if repetitionErrorCount != 0 {
			return nil, fmt.Errorf("chainLevelRepetition: %d invalid levels", repetitionErrorCount)
		}
		var entryPositionErrorCount int
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM (
				SELECT chain_level_id.level_id
				FROM (SELECT DISTINCT level_id FROM chain_level) AS chain_level_id
				LEFT JOIN level_marker_set
				  ON level_marker_set.level_id=chain_level_id.level_id
				LEFT JOIN marker
				  ON marker.level_marker_set_id=level_marker_set.id
				 AND marker.noun_name='CameraSpawnPoint.Noun' COLLATE NOCASE
				GROUP BY chain_level_id.level_id
				HAVING COUNT(marker.id) != 4
			)`,
		).Scan(&entryPositionErrorCount)
		if err != nil {
			return nil, fmt.Errorf("chainLevelEntryPosition: %w", err)
		}
		if entryPositionErrorCount != 0 {
			return nil, fmt.Errorf(
				"chainLevelEntryPosition: %d invalid levels", entryPositionErrorCount,
			)
		}
		err = verifyChainLevelSequence(ctx, database)
		if err != nil {
			return nil, fmt.Errorf("chainLevelSequence: %w", err)
		}
	}
	isCrystalDefinitionStored, err := hasTable(ctx, database, "crystal_definition")
	if err != nil {
		return nil, fmt.Errorf("crystalDefinitionTable: %w", err)
	}
	if !isCrystalDefinitionStored {
		return nil, errors.New("crystalDefinitionTable: missing")
	}
	var crystalDefinitionCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM crystal_definition").Scan(&crystalDefinitionCount)
	if err != nil {
		return nil, fmt.Errorf("crystalDefinitionCount: %w", err)
	}
	expectedCrystalDefinitionCount := crystalTuningDefinitionCount
	if assetResourceCount == 0 {
		expectedCrystalDefinitionCount = 0
	}
	if crystalDefinitionCount != expectedCrystalDefinitionCount {
		return nil, fmt.Errorf("crystalDefinitionCount: got %d, want %d",
			crystalDefinitionCount, expectedCrystalDefinitionCount)
	}
	if crystalDefinitionCount > 0 {
		var minimumOrdinal, maximumOrdinal, distinctOrdinal int
		err = database.QueryRowContext(ctx, `
			SELECT MIN(ordinal), MAX(ordinal), COUNT(DISTINCT ordinal)
			FROM crystal_definition`,
		).Scan(&minimumOrdinal, &maximumOrdinal, &distinctOrdinal)
		if err != nil {
			return nil, fmt.Errorf("crystalDefinitionOrdinal: %w", err)
		}
		if minimumOrdinal != 0 || maximumOrdinal != crystalTuningDefinitionCount-1 ||
			distinctOrdinal != crystalTuningDefinitionCount {
			return nil, fmt.Errorf("crystalDefinitionOrdinal: got %d..%d/%d",
				minimumOrdinal, maximumOrdinal, distinctOrdinal)
		}
		var invalidDefinitionCount int
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM crystal_definition
			WHERE minimum_level < 0 OR maximum_level < minimum_level OR weight < 0
			   OR source_reference=0 OR LOWER(noun_reference) NOT LIKE '%.noun'`,
		).Scan(&invalidDefinitionCount)
		if err != nil {
			return nil, fmt.Errorf("crystalDefinitionValidate: %w", err)
		}
		if invalidDefinitionCount != 0 {
			return nil, fmt.Errorf("crystalDefinitionValidate: invalid=%d", invalidDefinitionCount)
		}
	}
	isCrystalLevelOffsetStored, err := hasTable(ctx, database, "crystal_level_offset")
	if err != nil {
		return nil, fmt.Errorf("crystalLevelOffsetTable: %w", err)
	}
	if !isCrystalLevelOffsetStored {
		return nil, errors.New("crystalLevelOffsetTable: missing")
	}
	var crystalLevelOffsetCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM crystal_level_offset").Scan(&crystalLevelOffsetCount)
	if err != nil {
		return nil, fmt.Errorf("crystalLevelOffsetCount: %w", err)
	}
	expectedCrystalLevelOffsetCount := crystalTuningOffsetCount
	if assetResourceCount == 0 {
		expectedCrystalLevelOffsetCount = 0
	}
	if crystalLevelOffsetCount != expectedCrystalLevelOffsetCount {
		return nil, fmt.Errorf("crystalLevelOffsetCount: got %d, want %d",
			crystalLevelOffsetCount, expectedCrystalLevelOffsetCount)
	}
	if crystalLevelOffsetCount > 0 {
		var invalidCrystalLevelOffsetCount int
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM crystal_level_offset
			WHERE ordinal != 0 OR level_offset != 0 OR weight != 1`,
		).Scan(&invalidCrystalLevelOffsetCount)
		if err != nil {
			return nil, fmt.Errorf("crystalLevelOffsetValidate: %w", err)
		}
		if invalidCrystalLevelOffsetCount != 0 {
			return nil, fmt.Errorf("crystalLevelOffsetValidate: invalid=%d", invalidCrystalLevelOffsetCount)
		}
	}
	isLuaChunkStored, err := hasTable(ctx, database, "lua_chunk")
	if err != nil {
		return nil, fmt.Errorf("luaChunkTable: %w", err)
	}
	if !isLuaChunkStored {
		return nil, errors.New("luaChunkTable: missing")
	}
	var luaChunkCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM lua_chunk").Scan(&luaChunkCount)
	if err != nil {
		return nil, fmt.Errorf("luaChunkCount: %w", err)
	}
	if isServerDataStored {
		var compiledLuaCount, missingLuaChunkCount, invalidLuaChunkCount int
		err = database.QueryRowContext(ctx, "SELECT COALESCE(SUM(is_compiled_lua), 0) FROM server_data").Scan(&compiledLuaCount)
		if err != nil {
			return nil, fmt.Errorf("luaChunkExpected: %w", err)
		}
		if luaChunkCount != compiledLuaCount {
			return nil, fmt.Errorf("luaChunkCount: got %d, want %d", luaChunkCount, compiledLuaCount)
		}
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM server_data
			LEFT JOIN lua_chunk
			  ON lua_chunk.server_data_resource_id=server_data.content_source_resource_id
			WHERE server_data.is_compiled_lua=1 AND lua_chunk.id IS NULL`).Scan(&missingLuaChunkCount)
		if err != nil {
			return nil, fmt.Errorf("luaChunkCoverage: %w", err)
		}
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM lua_chunk
			JOIN server_data
			  ON server_data.content_source_resource_id=lua_chunk.server_data_resource_id
			WHERE server_data.is_compiled_lua!=1
			   OR lua_chunk.bytecode_size!=server_data.decoded_size`).Scan(&invalidLuaChunkCount)
		if err != nil {
			return nil, fmt.Errorf("luaChunkMapping: %w", err)
		}
		if missingLuaChunkCount != 0 || invalidLuaChunkCount != 0 {
			return nil, fmt.Errorf("luaChunkCoverage: missing=%d invalid=%d",
				missingLuaChunkCount, invalidLuaChunkCount)
		}
	}
	isLuaStringStored, err := hasTable(ctx, database, "lua_string_constant")
	if err != nil {
		return nil, fmt.Errorf("luaStringTable: %w", err)
	}
	if !isLuaStringStored {
		return nil, errors.New("luaStringTable: missing")
	}
	var luaStringCount, orphanLuaStringCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM lua_string_constant").Scan(&luaStringCount)
	if err != nil {
		return nil, fmt.Errorf("luaStringCount: %w", err)
	}
	if luaChunkCount > 0 && luaStringCount == 0 {
		return nil, errors.New("luaStringCount: empty")
	}
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM lua_string_constant
		LEFT JOIN lua_chunk ON lua_chunk.id=lua_string_constant.lua_chunk_id
		WHERE lua_chunk.id IS NULL`).Scan(&orphanLuaStringCount)
	if err != nil {
		return nil, fmt.Errorf("luaStringOrphan: %w", err)
	}
	if orphanLuaStringCount != 0 {
		return nil, fmt.Errorf("luaStringOrphan: %d", orphanLuaStringCount)
	}
	isLuaStaticPropertyStored, err := hasTable(ctx, database, "lua_static_property")
	if err != nil {
		return nil, fmt.Errorf("luaStaticPropertyTable: %w", err)
	}
	if !isLuaStaticPropertyStored {
		return nil, errors.New("luaStaticPropertyTable: missing")
	}
	var orphanLuaStaticPropertyCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM lua_static_property
		LEFT JOIN lua_chunk ON lua_chunk.id=lua_static_property.lua_chunk_id
		WHERE lua_chunk.id IS NULL`).Scan(&orphanLuaStaticPropertyCount)
	if err != nil {
		return nil, fmt.Errorf("luaStaticPropertyOrphan: %w", err)
	}
	if orphanLuaStaticPropertyCount != 0 {
		return nil, fmt.Errorf("luaStaticPropertyOrphan: %d", orphanLuaStaticPropertyCount)
	}
	isLuaTokenBindingStored, err := hasTable(ctx, database, "lua_token_binding")
	if err != nil {
		return nil, fmt.Errorf("luaTokenBindingTable: %w", err)
	}
	if !isLuaTokenBindingStored {
		return nil, errors.New("luaTokenBindingTable: missing")
	}
	var orphanLuaTokenBindingCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM lua_token_binding
		LEFT JOIN lua_chunk ON lua_chunk.id=lua_token_binding.lua_chunk_id
		WHERE lua_chunk.id IS NULL`).Scan(&orphanLuaTokenBindingCount)
	if err != nil {
		return nil, fmt.Errorf("luaTokenBindingOrphan: %w", err)
	}
	if orphanLuaTokenBindingCount != 0 {
		return nil, fmt.Errorf("luaTokenBindingOrphan: %d", orphanLuaTokenBindingCount)
	}

	return &Verification{
		ContentRelease:            contentRelease,
		SourceBuild:               sourceBuild,
		RecipeVersion:             recipeVersion,
		IsSourceRecorded:          isSourceRecorded,
		IsResourceStored:          isResourceStored,
		IsServerDataStored:        isServerDataStored,
		IsCreatureStored:          isCreatureStored,
		IsNonPlayerClassStored:    isNonPlayerClassStored,
		IsNounPhysicsStored:       isNounPhysicsStored,
		IsLocalizationStored:      isLocalizationStored,
		IsLevelStored:             isLevelStored,
		IsLevelNavigationStored:   isLevelNavigationStored,
		IsChainLevelStored:        isChainLevelStored,
		IsCrystalDefinitionStored: isCrystalDefinitionStored,
		IsLuaChunkStored:          isLuaChunkStored,
		IsLuaStringStored:         isLuaStringStored,
		IsLuaStaticPropertyStored: isLuaStaticPropertyStored,
		IsLuaTokenBindingStored:   isLuaTokenBindingStored,
		IsLootRigblockStored:      isLootRigblockStored,
		IsLootAffixStored:         isLootAffixStored,
		IsLootTuningStored:        isLootTuningStored,
		IsWeaponTuningStored:      isWeaponTuningStored,
		IsCombatTuningStored:      isCombatTuningStored,
	}, nil
}

func verifyChainLevelSequence(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, `
		SELECT ordinal, level_reference FROM chain_level ORDER BY ordinal`)
	if err != nil {
		return fmt.Errorf("sequenceQuery: %w", err)
	}
	defer rows.Close()
	chainLevels := make([]chainLevelAsset, 0, len(build103ChainLevelReference))
	for rows.Next() {
		var chainLevel chainLevelAsset
		err = rows.Scan(&chainLevel.ordinal, &chainLevel.levelReference)
		if err != nil {
			return fmt.Errorf("sequenceScan[%d]: %w", len(chainLevels), err)
		}
		chainLevels = append(chainLevels, chainLevel)
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("sequenceRows: %w", err)
	}
	err = validateChainLevelSequence(chainLevels, build103ChainLevelReference)
	if err != nil {
		return fmt.Errorf("sequenceValidate: %w", err)
	}
	return nil
}

func resolveInstallPath(gamePath string) (string, error) {
	if gamePath == "" {
		return "", errors.New("empty game path")
	}
	absolutePath, err := filepath.Abs(gamePath)
	if err != nil {
		return "", fmt.Errorf("gamePath: %w", err)
	}
	if strings.EqualFold(filepath.Base(absolutePath), "DarksporeBin") {
		absolutePath = filepath.Dir(absolutePath)
	}
	binPath := filepath.Join(absolutePath, "DarksporeBin")
	fi, err := os.Stat(binPath)
	if err != nil {
		return "", fmt.Errorf("binStat: %w", err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("binType: expected directory %q", binPath)
	}
	return absolutePath, nil
}

func validateSourceVersion(installPath string) error {
	versionPath := filepath.Join(installPath, "DarksporeBin", "version_bin.txt")
	contents, err := os.ReadFile(versionPath)
	if err != nil {
		return fmt.Errorf("versionRead: %w", err)
	}
	version := strings.TrimSpace(string(contents))
	if version != SourceVersion {
		return fmt.Errorf("versionMismatch: got %q, want %q", version, SourceVersion)
	}
	return nil
}

func inspectPackages(ctx context.Context, installPath string) ([]inspectedPackage, error) {
	return inspectPackageSet(ctx, installPath, buildPackages)
}

func inspectPackageSet(ctx context.Context, installPath string, specs []packageSpec) ([]inspectedPackage, error) {
	resultChannel := make(chan packageInspectionResult, len(specs))
	for _, spec := range specs {
		go inspectPackage(ctx, installPath, spec, resultChannel)
	}

	packages := make([]inspectedPackage, 0, len(specs))
	for range specs {
		packageResult := <-resultChannel
		if packageResult.err != nil {
			return nil, fmt.Errorf("packageResult: %w", packageResult.err)
		}
		packages = append(packages, packageResult.inspection)
	}
	sort.Slice(packages, func(left, right int) bool {
		return packages[left].spec.name < packages[right].spec.name
	})
	return packages, nil
}

func inspectPackage(
	ctx context.Context, installPath string, spec packageSpec,
	resultChannel chan<- packageInspectionResult,
) {
	path := filepath.Join(installPath, spec.relativePath)
	r, err := os.Open(path)
	if err != nil {
		resultChannel <- packageInspectionResult{
			err: fmt.Errorf("packageOpen[%s]: %w", spec.name, err),
		}
		return
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		resultChannel <- packageInspectionResult{
			err: fmt.Errorf("packageStat[%s]: %w", spec.name, err),
		}
		return
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		resultChannel <- packageInspectionResult{
			err: fmt.Errorf("packageRead[%s]: %w", spec.name, err),
		}
		return
	}
	sha256, err := dbpf.Fingerprint(ctx, r, fi.Size())
	if err != nil {
		resultChannel <- packageInspectionResult{
			err: fmt.Errorf("packageHash[%s]: %w", spec.name, err),
		}
		return
	}
	resultChannel <- packageInspectionResult{inspection: inspectedPackage{
		spec: spec, pkg: pkg, sha256: sha256, fileSize: fi.Size(),
	}}
}

func writeContentDatabase(
	ctx context.Context, path, installPath string, packages []inspectedPackage,
	report func(BuildProgress),
) error {
	reportBuildProgress(report, "Creating content database", 2, 20)
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("databaseOpen: %w", err)
	}
	defer database.Close()
	_, err = database.ExecContext(ctx, `
		PRAGMA journal_mode=OFF;
		PRAGMA synchronous=OFF;
		PRAGMA foreign_keys=ON;
		CREATE TABLE database_manifest (
			id INTEGER PRIMARY KEY CHECK (id=1),
			database_role TEXT NOT NULL,
			source_build INTEGER NOT NULL,
			content_release TEXT NOT NULL,
			recipe_version INTEGER NOT NULL,
			input_fingerprint TEXT NOT NULL,
			is_runtime_required INTEGER NOT NULL CHECK (is_runtime_required IN (0, 1))
		);
		CREATE TABLE content_source_package (
			id INTEGER PRIMARY KEY,
			package_name TEXT NOT NULL UNIQUE,
			source_file TEXT NOT NULL,
			sha256 TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			dbpf_major_version INTEGER NOT NULL,
			dbpf_minor_version INTEGER NOT NULL,
			index_version INTEGER NOT NULL,
			resource_count INTEGER NOT NULL,
			index_size INTEGER NOT NULL,
			index_offset INTEGER NOT NULL,
			is_resource_stored INTEGER NOT NULL CHECK (is_resource_stored IN (0, 1))
		);
		CREATE TABLE content_source_resource (
			id INTEGER PRIMARY KEY,
			content_source_package_id INTEGER NOT NULL REFERENCES content_source_package(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			type_id INTEGER NOT NULL,
			group_id INTEGER NOT NULL,
			instance_id INTEGER NOT NULL,
			stored_size INTEGER NOT NULL,
			decoded_size INTEGER NOT NULL,
			compression INTEGER NOT NULL,
			entry_flag INTEGER NOT NULL,
			raw_sha256 TEXT NOT NULL,
			decoded_sha256 TEXT NOT NULL,
			raw_payload BLOB NOT NULL
		);
		CREATE TABLE server_data (
			content_source_resource_id INTEGER PRIMARY KEY REFERENCES content_source_resource(id) ON DELETE CASCADE,
			resource_group TEXT NOT NULL,
			resource_name TEXT NOT NULL,
			format TEXT NOT NULL,
			decoded_size INTEGER NOT NULL,
			decoded_compression TEXT NOT NULL,
			decoded_payload BLOB NOT NULL,
			is_compiled_lua INTEGER NOT NULL CHECK (is_compiled_lua IN (0, 1))
		);
		CREATE TABLE creature_template (
			id INTEGER PRIMARY KEY,
			name_locale_key TEXT NOT NULL,
			description_locale_key TEXT NOT NULL,
			name TEXT NOT NULL UNIQUE,
			element_type TEXT NOT NULL,
			class_type TEXT NOT NULL,
			weapon_min_damage REAL NOT NULL,
			weapon_max_damage REAL NOT NULL,
			gear_score REAL NOT NULL,
			stat_template TEXT NOT NULL,
			ability_passive INTEGER NOT NULL,
			ability_basic INTEGER NOT NULL,
			ability_random INTEGER NOT NULL,
			ability_special_1 INTEGER NOT NULL,
			ability_special_2 INTEGER NOT NULL,
			is_hand_present INTEGER NOT NULL CHECK (is_hand_present IN (0, 1)),
			is_foot_present INTEGER NOT NULL CHECK (is_foot_present IN (0, 1))
		);
		CREATE TABLE loot_rigblock (
			id INTEGER PRIMARY KEY,
			content_source_resource_id INTEGER NOT NULL UNIQUE
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			slot_type TEXT NOT NULL CHECK (slot_type IN ('weapon', 'grasper', 'foot', 'defense', 'offense', 'utility')),
			class_type TEXT NOT NULL,
			science_type TEXT NOT NULL,
			image_group_id INTEGER NOT NULL,
			image_instance_id INTEGER NOT NULL,
			image_name TEXT NOT NULL,
			weapon_noun_id INTEGER NOT NULL,
			content_flags INTEGER NOT NULL CHECK (content_flags >= 0 AND content_flags <= 255),
			minimum_level INTEGER NOT NULL CHECK (minimum_level >= 0),
			maximum_level INTEGER NOT NULL CHECK (maximum_level >= minimum_level),
			is_unique_family INTEGER NOT NULL CHECK (is_unique_family IN (0, 1))
		);
		CREATE TABLE loot_affix (
			kind TEXT NOT NULL CHECK (kind IN ('prefix', 'suffix')),
			id INTEGER NOT NULL,
			content_source_resource_id INTEGER NOT NULL UNIQUE
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			minimum_level INTEGER NOT NULL CHECK (minimum_level > 0),
			maximum_level INTEGER NOT NULL CHECK (maximum_level >= minimum_level),
			class_type TEXT NOT NULL,
			science_type TEXT NOT NULL,
			modifier BLOB NOT NULL CHECK (length(modifier) = 460),
			is_unique_family INTEGER NOT NULL CHECK (is_unique_family IN (0, 1)),
			is_basic_eligible INTEGER NOT NULL CHECK (is_basic_eligible IN (0, 1)),
			PRIMARY KEY (kind, id)
		);
		CREATE TABLE loot_tuning (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			content_source_resource_id INTEGER NOT NULL UNIQUE
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			rarity_level_step INTEGER NOT NULL CHECK (rarity_level_step > 0),
			base_point REAL NOT NULL CHECK (base_point > 0),
			extra_stat_bonus_factor REAL NOT NULL CHECK (extra_stat_bonus_factor >= 0),
			level_band BLOB NOT NULL CHECK (length(level_band) > 0 AND length(level_band) % 8 = 0),
			point_cost BLOB NOT NULL CHECK (length(point_cost) = 52),
			rarity_distribution BLOB NOT NULL CHECK (length(rarity_distribution) = 64),
			hand_level_scale REAL NOT NULL,
			hand_flat REAL NOT NULL,
			foot_level_scale REAL NOT NULL,
			foot_flat REAL NOT NULL,
			defense_flat REAL NOT NULL,
			offense_flat REAL NOT NULL,
			utility_flat REAL NOT NULL,
			weapon_damage_multiplier REAL NOT NULL CHECK (weapon_damage_multiplier > 0),
			price_base REAL NOT NULL CHECK (price_base > 0),
			price_curve REAL NOT NULL CHECK (price_curve > 0),
			price_increment INTEGER NOT NULL CHECK (price_increment > 0),
			hand_minimum_level INTEGER NOT NULL,
			foot_minimum_level INTEGER NOT NULL,
			weapon_minimum_level INTEGER NOT NULL,
			uncommon_chance BLOB NOT NULL CHECK (length(uncommon_chance) = 76),
			rare_chance BLOB NOT NULL CHECK (length(rare_chance) = 76),
			epic_chance BLOB NOT NULL CHECK (length(epic_chance) = 76)
		);
		CREATE TABLE weapon_tuning (
			content_source_resource_id INTEGER NOT NULL
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			offer_id INTEGER PRIMARY KEY,
			item_level INTEGER NOT NULL CHECK (item_level > 0),
			rigblock_id INTEGER NOT NULL CHECK (rigblock_id > 0),
			suffix_id INTEGER NOT NULL CHECK (suffix_id > 0),
			price INTEGER NOT NULL CHECK (price > 0),
			minimum_account_level INTEGER NOT NULL CHECK (minimum_account_level > 0),
			minimum_chain_progression INTEGER NOT NULL CHECK (minimum_chain_progression >= 0)
		);
		CREATE TABLE combat_tuning (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			magic_content_source_resource_id INTEGER NOT NULL UNIQUE
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			difficulty_content_source_resource_id INTEGER NOT NULL UNIQUE
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			critical_damage_bonus REAL NOT NULL CHECK (critical_damage_bonus > 0),
			health_party_base REAL NOT NULL CHECK (health_party_base > 0),
			damage_party_base REAL NOT NULL CHECK (damage_party_base > 0)
		);
		CREATE TABLE difficulty_tuning (
			difficulty INTEGER PRIMARY KEY CHECK (difficulty BETWEEN 1 AND 72),
			content_source_resource_id INTEGER NOT NULL
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			health_multiplier REAL NOT NULL CHECK (health_multiplier > 0),
			damage_multiplier REAL NOT NULL CHECK (damage_multiplier > 0),
			expected_avatar_level INTEGER NOT NULL CHECK (expected_avatar_level >= 0),
			rating_conversion REAL NOT NULL CHECK (rating_conversion > 0)
		);
		CREATE TABLE non_player_class (
			content_source_resource_id INTEGER PRIMARY KEY REFERENCES content_source_resource(id) ON DELETE CASCADE,
			instance_id INTEGER NOT NULL UNIQUE,
			noun_name TEXT NOT NULL COLLATE NOCASE,
			display_name TEXT NOT NULL,
			display_name_locale_key TEXT NOT NULL,
			description TEXT NOT NULL,
			description_locale_key TEXT NOT NULL,
			challenge_value INTEGER NOT NULL CHECK (challenge_value >= 0),
			npc_rank INTEGER NOT NULL CHECK (npc_rank >= 0),
			is_targetable INTEGER NOT NULL CHECK (is_targetable IN (0, 1)),
			is_player_pet INTEGER NOT NULL CHECK (is_player_pet IN (0, 1)),
			player_count_health_scale REAL NOT NULL CHECK (player_count_health_scale >= 0),
			hit_point REAL NOT NULL CHECK (hit_point >= 0),
			power_point REAL NOT NULL CHECK (power_point >= 0),
			strength REAL NOT NULL CHECK (strength >= 0),
			dexterity REAL NOT NULL CHECK (dexterity >= 0),
			mind REAL NOT NULL CHECK (mind >= 0),
			dodge_rating REAL NOT NULL CHECK (dodge_rating >= 0),
			resist_rating REAL NOT NULL CHECK (resist_rating >= 0),
			critical_rating REAL NOT NULL CHECK (critical_rating >= 0)
		);
		CREATE TABLE non_player_class_affix (
			id INTEGER PRIMARY KEY,
			non_player_class_resource_id INTEGER NOT NULL
				REFERENCES non_player_class(content_source_resource_id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 5),
			asset_name TEXT NOT NULL,
			UNIQUE (non_player_class_resource_id, ordinal),
			UNIQUE (non_player_class_resource_id, asset_name)
		);
		CREATE TABLE npc_death_animation (
			noun_name TEXT PRIMARY KEY COLLATE NOCASE,
			animation_name TEXT NOT NULL
		);
		CREATE TABLE noun_physics (
			id INTEGER PRIMARY KEY,
			content_source_resource_id INTEGER NOT NULL UNIQUE REFERENCES content_source_resource(id) ON DELETE CASCADE,
			class_attribute_resource_id INTEGER UNIQUE REFERENCES content_source_resource(id) ON DELETE CASCADE,
			character_animation_resource_id INTEGER REFERENCES content_source_resource(id) ON DELETE CASCADE,
			asset_name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			creature_type INTEGER CHECK (creature_type >= 0),
			ordinary_death_animation TEXT,
			dance_animation TEXT,
			lifetime_seconds REAL NOT NULL CHECK (lifetime_seconds >= 0),
			graphics_scale REAL NOT NULL CHECK (graphics_scale > 0),
			footprint_radius REAL NOT NULL CHECK (footprint_radius >= 0),
			bound_min_x REAL NOT NULL,
			bound_min_y REAL NOT NULL,
			bound_min_z REAL NOT NULL,
			bound_max_x REAL NOT NULL,
			bound_max_y REAL NOT NULL,
			bound_max_z REAL NOT NULL,
			geometry_reference TEXT NOT NULL,
			property_reference TEXT NOT NULL,
			source_size INTEGER NOT NULL CHECK (source_size > 0),
			source_sha256 TEXT NOT NULL,
			source_payload BLOB NOT NULL,
			CHECK (bound_min_x <= bound_max_x),
			CHECK (bound_min_y <= bound_max_y),
			CHECK (bound_min_z <= bound_max_z),
			CHECK ((class_attribute_resource_id IS NULL) = (creature_type IS NULL)),
			CHECK ((character_animation_resource_id IS NULL) =
			       (ordinary_death_animation IS NULL AND dance_animation IS NULL))
		);
		CREATE TABLE noun_physics_shape (
			id INTEGER PRIMARY KEY,
			noun_physics_id INTEGER NOT NULL REFERENCES noun_physics(id) ON DELETE CASCADE,
			content_source_resource_id INTEGER NOT NULL REFERENCES content_source_resource(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			shape_role TEXT NOT NULL,
			shape_kind TEXT NOT NULL,
			dimension_x REAL,
			dimension_y REAL,
			dimension_z REAL,
			radius REAL,
			source_offset INTEGER NOT NULL,
			UNIQUE (noun_physics_id, ordinal),
			UNIQUE (noun_physics_id, shape_role),
			CHECK (
				(shape_kind='box' AND dimension_x > 0 AND dimension_y > 0
				 AND dimension_z > 0 AND radius IS NULL)
				OR
				(shape_kind='sphere' AND radius > 0 AND dimension_x IS NULL
				 AND dimension_y IS NULL AND dimension_z IS NULL)
			)
		);
		CREATE TABLE creature_template_ability (
			id INTEGER PRIMARY KEY,
			creature_template_id INTEGER NOT NULL REFERENCES creature_template(id) ON DELETE CASCADE,
			slot TEXT NOT NULL,
			asset_name TEXT NOT NULL,
			UNIQUE (creature_template_id, slot)
		);
		CREATE TABLE localization_text (
			id INTEGER PRIMARY KEY,
			locale TEXT NOT NULL,
			table_id INTEGER NOT NULL,
			locale_key TEXT NOT NULL,
			localized_text TEXT NOT NULL
		);
		CREATE TABLE level (
			id INTEGER PRIMARY KEY,
			content_source_resource_id INTEGER NOT NULL UNIQUE REFERENCES content_source_resource(id) ON DELETE CASCADE,
			name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			package_group_id INTEGER NOT NULL,
			music TEXT NOT NULL,
			nav_mesh TEXT NOT NULL,
			physics_mesh TEXT NOT NULL,
			rendering_config TEXT NOT NULL,
			planet_config TEXT NOT NULL,
			primary_type INTEGER NOT NULL,
			secondary_type INTEGER NOT NULL,
			camera_pitch REAL NOT NULL,
			camera_yaw REAL NOT NULL,
			camera_distance REAL NOT NULL,
			source_sha256 TEXT NOT NULL,
			source_size INTEGER NOT NULL,
			source_compression TEXT NOT NULL,
			source_payload BLOB NOT NULL
		);
		CREATE TABLE level_navigation (
			level_id INTEGER PRIMARY KEY REFERENCES level(id) ON DELETE CASCADE,
			content_ordinal INTEGER NOT NULL,
			type_id INTEGER NOT NULL,
			group_id INTEGER NOT NULL,
			instance_id INTEGER NOT NULL,
			decoded_size INTEGER NOT NULL,
			decoded_sha256 TEXT NOT NULL,
			decoded_compression TEXT NOT NULL,
			decoded_payload BLOB NOT NULL
		);
		CREATE TABLE level_alias (
			id INTEGER PRIMARY KEY,
			level_id INTEGER NOT NULL REFERENCES level(id) ON DELETE CASCADE,
			alias TEXT NOT NULL UNIQUE COLLATE NOCASE,
			alias_kind TEXT NOT NULL
		);
		CREATE TABLE chain_level (
			id INTEGER PRIMARY KEY,
			content_source_resource_id INTEGER NOT NULL REFERENCES content_source_resource(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			level_reference TEXT NOT NULL COLLATE NOCASE,
			level_id INTEGER REFERENCES level(id) ON DELETE SET NULL,
			UNIQUE (content_source_resource_id, ordinal)
		);
		CREATE TABLE crystal_definition (
			id INTEGER PRIMARY KEY,
			content_source_resource_id INTEGER NOT NULL
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			minimum_level INTEGER NOT NULL CHECK (minimum_level >= 0),
			maximum_level INTEGER NOT NULL CHECK (maximum_level >= minimum_level),
			weight INTEGER NOT NULL CHECK (weight >= 0),
			source_reference INTEGER NOT NULL CHECK (source_reference > 0),
			noun_reference TEXT NOT NULL COLLATE NOCASE,
			UNIQUE (content_source_resource_id, ordinal)
		);
		CREATE TABLE crystal_level_offset (
			id INTEGER PRIMARY KEY,
			content_source_resource_id INTEGER NOT NULL
				REFERENCES content_source_resource(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			level_offset INTEGER NOT NULL,
			weight REAL NOT NULL CHECK (weight >= 0),
			UNIQUE (content_source_resource_id, ordinal)
		);
		CREATE TABLE level_marker_set (
			id INTEGER PRIMARY KEY,
			level_id INTEGER NOT NULL REFERENCES level(id) ON DELETE CASCADE,
			content_source_resource_id INTEGER UNIQUE REFERENCES content_source_resource(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			asset_name TEXT NOT NULL COLLATE NOCASE,
			group_name TEXT NOT NULL,
			weight REAL NOT NULL,
			source_sha256 TEXT,
			source_size INTEGER,
			source_compression TEXT,
			source_payload BLOB,
			UNIQUE (level_id, ordinal),
			UNIQUE (level_id, asset_name)
		);
		CREATE TABLE marker (
			id INTEGER PRIMARY KEY,
			level_marker_set_id INTEGER NOT NULL REFERENCES level_marker_set(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			marker_id INTEGER NOT NULL,
			marker_name TEXT NOT NULL,
			noun_name TEXT NOT NULL,
			position_x REAL NOT NULL,
			position_y REAL NOT NULL,
			position_z REAL NOT NULL,
			rotation_x REAL NOT NULL,
			rotation_y REAL NOT NULL,
			rotation_z REAL NOT NULL,
			scale REAL NOT NULL,
			dimension_x REAL NOT NULL,
			dimension_y REAL NOT NULL,
			dimension_z REAL NOT NULL,
			is_visible INTEGER NOT NULL CHECK (is_visible IN (0, 1)),
			is_collision_enabled INTEGER NOT NULL CHECK (is_collision_enabled IN (0, 1)),
			asset_override_id TEXT NOT NULL,
			target_marker_id INTEGER NOT NULL,
			teleporter_trigger_radius REAL NOT NULL,
			interactable_ability TEXT,
			interactable_use_limit INTEGER,
			interactable_challenge INTEGER,
			UNIQUE (level_marker_set_id, ordinal)
		);
		CREATE TABLE level_event (
			id INTEGER PRIMARY KEY,
			marker_id INTEGER NOT NULL REFERENCES marker(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			component_name TEXT NOT NULL,
			event_kind TEXT NOT NULL,
			 event_slot TEXT NOT NULL,
			 event_name TEXT NOT NULL,
			 callback_name TEXT NOT NULL,
			 trigger_radius REAL NOT NULL,
			 is_trigger_once_only INTEGER NOT NULL CHECK (is_trigger_once_only IN (0, 1)),
			 is_server_only INTEGER NOT NULL CHECK (is_server_only IN (0, 1)),
			 UNIQUE (marker_id, ordinal)
		);
		CREATE TABLE level_director_entry (
			id INTEGER PRIMARY KEY,
			level_id INTEGER NOT NULL REFERENCES level(id) ON DELETE CASCADE,
			config_kind TEXT NOT NULL,
			spawn_kind TEXT NOT NULL,
			configuration_ordinal INTEGER NOT NULL CHECK (configuration_ordinal >= 0),
			configuration_entry_ordinal INTEGER NOT NULL CHECK (configuration_entry_ordinal >= 0),
			ordinal INTEGER NOT NULL,
			noun_name TEXT NOT NULL,
			minimum_difficulty INTEGER NOT NULL,
			maximum_difficulty INTEGER NOT NULL,
			is_horde_legal INTEGER NOT NULL CHECK (is_horde_legal IN (0, 1)),
			UNIQUE (level_id, configuration_ordinal, configuration_entry_ordinal),
			UNIQUE (level_id, ordinal)
		);
		CREATE TABLE lua_chunk (
			id INTEGER PRIMARY KEY,
			server_data_resource_id INTEGER NOT NULL UNIQUE REFERENCES server_data(content_source_resource_id) ON DELETE CASCADE,
			source_name TEXT NOT NULL,
			bytecode_sha256 TEXT NOT NULL,
			bytecode_size INTEGER NOT NULL
		);
		CREATE TABLE lua_string_constant (
			id INTEGER PRIMARY KEY,
			lua_chunk_id INTEGER NOT NULL REFERENCES lua_chunk(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			string_constant TEXT NOT NULL,
			UNIQUE (lua_chunk_id, ordinal)
		);
		CREATE TABLE lua_static_property (
			id INTEGER PRIMARY KEY,
			lua_chunk_id INTEGER NOT NULL REFERENCES lua_chunk(id) ON DELETE CASCADE,
			table_name TEXT NOT NULL,
			property_name TEXT NOT NULL,
			minimum REAL NOT NULL,
			maximum REAL NOT NULL,
			evidence TEXT NOT NULL,
			UNIQUE (lua_chunk_id, table_name, property_name)
		);
		CREATE TABLE lua_token_binding (
			id INTEGER PRIMARY KEY,
			lua_chunk_id INTEGER NOT NULL REFERENCES lua_chunk(id) ON DELETE CASCADE,
			ability_table_name TEXT NOT NULL,
			token_name TEXT NOT NULL,
			source_table_name TEXT NOT NULL,
			property_name TEXT NOT NULL,
			element_index INTEGER NOT NULL CHECK (element_index BETWEEN 0 AND 2),
			multiplier REAL NOT NULL,
			evidence TEXT NOT NULL,
			UNIQUE (lua_chunk_id, ability_table_name, token_name)
		);
		CREATE TABLE lua_module_alias (
			id INTEGER PRIMARY KEY,
			lua_chunk_id INTEGER NOT NULL REFERENCES lua_chunk(id) ON DELETE CASCADE,
			module_name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			evidence TEXT NOT NULL
		);
		CREATE TABLE lua_dependency (
			id INTEGER PRIMARY KEY,
			lua_chunk_id INTEGER NOT NULL REFERENCES lua_chunk(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			dependency_name TEXT NOT NULL,
			target_lua_chunk_id INTEGER REFERENCES lua_chunk(id) ON DELETE SET NULL,
			UNIQUE (lua_chunk_id, ordinal)
		);
		CREATE TABLE level_script (
			id INTEGER PRIMARY KEY,
			level_id INTEGER NOT NULL REFERENCES level(id) ON DELETE CASCADE,
			level_event_id INTEGER NOT NULL REFERENCES level_event(id) ON DELETE CASCADE,
			lua_chunk_id INTEGER NOT NULL REFERENCES lua_chunk(id) ON DELETE CASCADE,
			callback_name TEXT NOT NULL,
			UNIQUE (level_event_id, lua_chunk_id, callback_name)
		);
	`)
	if err != nil {
		return fmt.Errorf("schemaCreate: %w", err)
	}

	fingerprint := sourceFingerprint(packages)
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("transactionBegin: %w", err)
	}
	isCommitted := false
	defer func() {
		if !isCommitted {
			_ = transaction.Rollback()
		}
	}()
	_, err = transaction.ExecContext(ctx,
		"INSERT INTO database_manifest VALUES (1, ?, ?, ?, ?, ?, 1)",
		RuntimeRole, SourceBuild, ContentRelease, RecipeVersion, fingerprint,
	)
	if err != nil {
		return fmt.Errorf("manifestInsert: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO content_source_package
		(package_name, source_file, sha256, file_size, dbpf_major_version, dbpf_minor_version,
		 index_version, resource_count, index_size, index_offset, is_resource_stored)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("packagePrepare: %w", err)
	}
	reportBuildProgress(report, "Recording package inventory", 3, 20)
	for _, packageInst := range packages {
		_, err = statement.ExecContext(ctx,
			packageInst.spec.name,
			filepath.ToSlash(packageInst.spec.relativePath),
			packageInst.sha256,
			packageInst.fileSize,
			packageInst.pkg.Header.MajorVersion,
			packageInst.pkg.Header.MinorVersion,
			packageInst.pkg.Header.IndexVersion,
			packageInst.pkg.Header.ResourceCount,
			packageInst.pkg.Header.IndexSize,
			packageInst.pkg.Header.IndexOffset,
			packageInst.spec.isResourceStored,
		)
		if err != nil {
			_ = statement.Close()
			return fmt.Errorf("packageInsert[%s]: %w", packageInst.spec.name, err)
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("packageClose: %w", err)
	}
	reportBuildProgress(report, "Importing runtime resources", 4, 20)
	err = insertResources(ctx, transaction, installPath, packages)
	if err != nil {
		return fmt.Errorf("resourceInsert: %w", err)
	}
	reportBuildProgress(report, "Indexing Lua content", 5, 20)
	err = writeLuaChunks(ctx, transaction)
	if err != nil {
		return fmt.Errorf("luaInsert: %w", err)
	}
	reportBuildProgress(report, "Importing equipment models", 6, 20)
	err = writeLootRigblocks(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("lootRigblockInsert: %w", err)
	}
	reportBuildProgress(report, "Importing equipment affixes", 7, 20)
	err = writeLootAffixes(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("lootAffixInsert: %w", err)
	}
	reportBuildProgress(report, "Importing loot tuning", 8, 20)
	err = writeLootTuning(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("lootTuningInsert: %w", err)
	}
	err = writeWeaponTuning(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("weaponTuningInsert: %w", err)
	}
	reportBuildProgress(report, "Importing combat tuning", 9, 20)
	err = writeCombatTuning(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("combatTuningInsert: %w", err)
	}
	reportBuildProgress(report, "Importing levels and navigation", 10, 20)
	err = writeLevels(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("levelInsert: %w", err)
	}
	reportBuildProgress(report, "Importing campaign chain", 11, 20)
	err = writeChainLevels(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("chainLevelInsert: %w", err)
	}
	reportBuildProgress(report, "Importing crystal tuning", 12, 20)
	err = writeCrystalTuning(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("crystalTuningInsert: %w", err)
	}
	reportBuildProgress(report, "Linking level scripts", 13, 20)
	err = writeLevelScripts(ctx, transaction)
	if err != nil {
		return fmt.Errorf("levelScriptInsert: %w", err)
	}
	reportBuildProgress(report, "Importing hero templates", 14, 20)
	err = writeCreatureTemplates(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("creatureInsert: %w", err)
	}
	reportBuildProgress(report, "Importing enemy attributes", 15, 20)
	err = writeNonPlayerClasses(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("nonPlayerClassInsert: %w", err)
	}
	reportBuildProgress(report, "Importing object physics", 16, 20)
	err = writeNounPhysics(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("nounPhysicsInsert: %w", err)
	}
	err = writeNPCDeathAnimations(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("npcDeathInsert: %w", err)
	}
	reportBuildProgress(report, "Importing localized text", 17, 20)
	err = writeLocalizationText(ctx, transaction, installPath)
	if err != nil {
		return fmt.Errorf("localeInsert: %w", err)
	}
	reportBuildProgress(report, "Building content indexes", 18, 20)
	err = createContentIndexes(ctx, transaction)
	if err != nil {
		return fmt.Errorf("indexCreate: %w", err)
	}
	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("transactionCommit: %w", err)
	}
	isCommitted = true
	return nil
}

func createContentIndexes(ctx context.Context, transaction *sql.Tx) error {
	_, err := transaction.ExecContext(ctx, `
		CREATE UNIQUE INDEX content_source_resource_package_ordinal_uidx
		ON content_source_resource (content_source_package_id, ordinal);
		CREATE UNIQUE INDEX content_source_resource_package_identity_uidx
		ON content_source_resource (content_source_package_id, type_id, group_id, instance_id);
		CREATE INDEX content_source_resource_identity_idx
		ON content_source_resource (type_id, group_id, instance_id);
		CREATE UNIQUE INDEX server_data_identity_uidx
		ON server_data (resource_group, resource_name);
		CREATE UNIQUE INDEX localization_text_identity_uidx
		ON localization_text (locale, table_id, locale_key);
		CREATE INDEX localization_text_key_idx
		ON localization_text (locale, locale_key);
		CREATE INDEX level_marker_set_level_idx
		ON level_marker_set (level_id, ordinal);
		CREATE INDEX chain_level_level_idx
		ON chain_level (level_id);
		CREATE INDEX crystal_definition_level_idx
		ON crystal_definition (minimum_level, maximum_level, ordinal);
		CREATE INDEX crystal_definition_noun_idx
		ON crystal_definition (noun_reference);
		CREATE INDEX marker_set_marker_id_idx
		ON marker (level_marker_set_id, marker_id);
		CREATE INDEX marker_noun_idx
		ON marker (noun_name);
		CREATE INDEX level_event_name_idx
		ON level_event (event_name);
		CREATE INDEX level_event_callback_idx
		ON level_event (callback_name);
		CREATE INDEX level_director_level_idx
		ON level_director_entry (level_id, config_kind, spawn_kind);
		CREATE INDEX lua_dependency_target_idx
		ON lua_dependency (target_lua_chunk_id);
		CREATE INDEX lua_string_constant_lookup_idx
		ON lua_string_constant (string_constant COLLATE NOCASE, lua_chunk_id);
		CREATE INDEX lua_static_property_chunk_idx
		ON lua_static_property (lua_chunk_id, property_name);
		CREATE INDEX lua_token_binding_chunk_idx
		ON lua_token_binding (lua_chunk_id, token_name);
		CREATE INDEX lua_module_alias_chunk_idx
		ON lua_module_alias (lua_chunk_id);
		CREATE INDEX level_script_level_idx
		ON level_script (level_id, callback_name);
		CREATE INDEX loot_rigblock_slot_idx
		ON loot_rigblock (slot_type);
		CREATE INDEX loot_affix_level_idx
		ON loot_affix (kind, minimum_level, maximum_level);
	`)
	if err != nil {
		return fmt.Errorf("indexExec: %w", err)
	}
	return nil
}

func insertResources(ctx context.Context, transaction *sql.Tx, installPath string, packages []inspectedPackage) error {
	packageID, err := loadContentPackageIDs(ctx, transaction)
	if err != nil {
		return fmt.Errorf("packageID: %w", err)
	}
	resourceRows := make([]contentResourceRow, 0, resourceInsertBatchSize)
	serverDataRows := make([]serverDataResourceRow, 0, resourceInsertBatchSize)
	resourceID := int64(1)
	for _, packageInst := range packages {
		if !packageInst.spec.isResourceStored {
			continue
		}
		contentPackageID, isFound := packageID[packageInst.spec.name]
		if !isFound {
			return fmt.Errorf("packageIDMissing: %s", packageInst.spec.name)
		}
		path := filepath.Join(installPath, packageInst.spec.relativePath)
		r, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("resourceOpen[%s]: %w", packageInst.spec.name, err)
		}
		fi, err := r.Stat()
		if err != nil {
			_ = r.Close()
			return fmt.Errorf("resourceStat[%s]: %w", packageInst.spec.name, err)
		}
		pkg, err := dbpf.NewReader(r, fi.Size())
		if err != nil {
			_ = r.Close()
			return fmt.Errorf("resourcePackage[%s]: %w", packageInst.spec.name, err)
		}
		for ordinal, entry := range pkg.Entries {
			select {
			case <-ctx.Done():
				_ = r.Close()
				return fmt.Errorf("resourceContext: %w", ctx.Err())
			default:
			}
			raw, openErr := pkg.OpenRaw(entry)
			if openErr != nil {
				_ = r.Close()
				return fmt.Errorf("rawOpen[%s:%d]: %w", packageInst.spec.name, ordinal, openErr)
			}
			rawPayload, readErr := io.ReadAll(raw)
			if readErr != nil {
				_ = r.Close()
				return fmt.Errorf("rawRead[%s:%d]: %w", packageInst.spec.name, ordinal, readErr)
			}
			decodedPayload, decodeErr := dbpf.Decode(entry, rawPayload)
			if decodeErr != nil {
				_ = r.Close()
				return fmt.Errorf("decodedRead[%s:%d]: %w", packageInst.spec.name, ordinal, decodeErr)
			}
			rawDigest := sha256.Sum256(rawPayload)
			decodedDigest := sha256.Sum256(decodedPayload)
			resourceRows = append(resourceRows, contentResourceRow{
				ID:                     resourceID,
				ContentSourcePackageID: contentPackageID,
				Ordinal:                ordinal,
				Entry:                  entry,
				RawSHA256:              hex.EncodeToString(rawDigest[:]),
				DecodedSHA256:          hex.EncodeToString(decodedDigest[:]),
				RawPayload:             rawPayload,
			})
			if packageInst.spec.name == "ServerData.package" {
				resourceGroup, resourceName, resourceFormat, isCompiledLua := serverDataIdentity(entry)
				compressedPayload, compressErr := compressContent(decodedPayload)
				if compressErr != nil {
					_ = r.Close()
					return fmt.Errorf("serverDataCompress[%d]: %w", ordinal, compressErr)
				}
				serverDataRows = append(serverDataRows, serverDataResourceRow{
					ContentSourceResourceID: resourceID,
					ResourceGroup:           resourceGroup,
					ResourceName:            resourceName,
					Format:                  resourceFormat,
					DecodedSize:             len(decodedPayload),
					DecodedPayload:          compressedPayload,
					IsCompiledLua:           isCompiledLua,
				})
			}
			resourceID++
			if len(resourceRows) == resourceInsertBatchSize {
				err = insertResourceBatch(ctx, transaction, resourceRows, serverDataRows)
				if err != nil {
					_ = r.Close()
					return fmt.Errorf("resourceBatch[%s:%d]: %w", packageInst.spec.name, ordinal, err)
				}
				resourceRows = resourceRows[:0]
				serverDataRows = serverDataRows[:0]
			}
		}
		err = r.Close()
		if err != nil {
			return fmt.Errorf("resourceClose[%s]: %w", packageInst.spec.name, err)
		}
	}
	if len(resourceRows) > 0 {
		err = insertResourceBatch(ctx, transaction, resourceRows, serverDataRows)
		if err != nil {
			return fmt.Errorf("resourceBatchFinal: %w", err)
		}
	}
	return nil
}

func sourceFingerprint(packages []inspectedPackage) string {
	contents := strings.Builder{}
	for _, packageInst := range packages {
		contents.WriteString(packageInst.spec.name)
		contents.WriteByte(0)
		contents.WriteString(packageInst.sha256)
		contents.WriteByte(0)
		contents.WriteString(fmt.Sprintf("%d\n", packageInst.fileSize))
	}
	digest := sha256.Sum256([]byte(contents.String()))
	return fmt.Sprintf("%x", digest)
}

func hasTable(ctx context.Context, database *sql.DB, tableName string) (bool, error) {
	var count int
	err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		tableName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("tableQuery: %w", err)
	}
	return count == 1, nil
}
