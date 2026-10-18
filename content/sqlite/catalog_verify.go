package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

func verifyAssetCatalog(ctx context.Context, database *sql.DB) error {
	expectedSpawnCounts := map[string]int{
		"markerset": 2384, "level": 61, "nonplayerclass": 586, "levelconfig": 13,
	}
	var count, minimum, maximum, invalidSourceCount int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MIN(catalog.ordinal), -1),
		COALESCE(MAX(catalog.ordinal), -1), COALESCE(SUM(CASE WHEN package.package_name IS NULL
		OR package.package_name!=? OR resource.type_id!=? OR resource.group_id!=0
		OR resource.instance_id!=? THEN 1 ELSE 0 END), 0)
		FROM asset_catalog AS catalog
		LEFT JOIN content_source_resource AS resource ON resource.id=catalog.catalog_source_resource_id
		LEFT JOIN content_source_package AS package ON package.id=resource.content_source_package_id`,
		lootAssetPackage, assetCatalogType, assetCatalogInstance).Scan(&count, &minimum, &maximum, &invalidSourceCount)
	if err != nil {
		return fmt.Errorf("catalogCoverageQuery: %w", err)
	}
	if count != 13412 || minimum != 0 || maximum != count-1 || invalidSourceCount != 0 {
		return fmt.Errorf("catalogCoverage: count %d range %d..%d invalid sources %d", count, minimum, maximum, invalidSourceCount)
	}
	for extension, resourceType := range catalogResourceTypes {
		// The catalog enumerates authored assets, not the complete package
		// inventory. Validate its outbound links without requiring the reverse
		// relationship for resources that have no authored catalog entry.
		var catalogCount, linkedCount int
		err = database.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE
			WHEN resource.type_id=? AND resource.group_id=0 AND package.package_name=?
			THEN 1 ELSE 0 END), 0)
			FROM asset_catalog AS catalog
			LEFT JOIN content_source_resource AS resource ON resource.id=catalog.content_source_resource_id
			LEFT JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE LOWER(catalog.asset_name) LIKE ?`,
			resourceType, lootAssetPackage, "%."+extension).Scan(&catalogCount, &linkedCount)
		if err != nil {
			return fmt.Errorf("catalogLinkQuery[%s]: %w", extension, err)
		}
		if catalogCount != linkedCount {
			return fmt.Errorf("catalogLinkCoverage[%s]: catalog %d linked %d", extension, catalogCount, linkedCount)
		}
		expectedCount, isSpawnAsset := expectedSpawnCounts[extension]
		if isSpawnAsset && catalogCount != expectedCount {
			return fmt.Errorf("catalogSpawnCoverage[%s]: catalog %d expected %d", extension, catalogCount, expectedCount)
		}
	}
	return nil
}

func verifyDNARewardTuning(ctx context.Context, database *sql.DB) error {
	for _, propertyID := range dnaRewardPropertyIDs {
		var sourceCount int
		err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM dna_reward_property AS property
			JOIN content_source_resource AS resource ON resource.id=property.content_source_resource_id
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE property.property_id=? AND package.package_name=? AND resource.ordinal=0
			AND resource.type_id=? AND resource.group_id=? AND resource.instance_id=?`,
			propertyID, directorCompositionPackage, 0x00b1b104, directorCompositionIdentity, directorCompositionIdentity).Scan(&sourceCount)
		if err != nil {
			return fmt.Errorf("dnaSourceQuery[%#x]: %w", propertyID, err)
		}
		if sourceCount != 1 {
			return fmt.Errorf("dnaSourceCount[%#x]: %d", propertyID, sourceCount)
		}
	}
	return nil
}
