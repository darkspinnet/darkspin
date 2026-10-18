package sqlite

import (
	"context"
	"fmt"
	"path"
	"strings"
)

type NounFootprint struct {
	InstanceID uint32
	NounName   string
	SizeClass  uint32
	ExtentX    float32
	ExtentY    float32
	ExtentZ    float32
	MinimumX   float32
	MinimumY   float32
	MinimumZ   float32
	MaximumX   float32
	MaximumY   float32
	MaximumZ   float32
}

// NounFootprints joins the supported ObjectExtents rows with general noun
// headers, preserving custom bounds and unsupported enum values for validation.
func (e *Store) NounFootprints(ctx context.Context) ([]NounFootprint, error) {
	nouns, err := e.NounNavigationEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("footprintNouns: %w", err)
	}
	extents, err := e.NounSpawnExtents(ctx)
	if err != nil {
		return nil, fmt.Errorf("footprintExtents: %w", err)
	}
	extentsByInstance := make(map[uint32]NounSpawnExtent, len(extents))
	for _, extent := range extents {
		stem := strings.TrimSuffix(strings.ToLower(extent.NounName), ".noun")
		extentsByInstance[hashID(stem)] = extent
	}
	extentsByClass := make(map[uint32]NounSpawnExtent)
	for _, noun := range nouns {
		extent, isFound := extentsByInstance[uint32(noun.InstanceID)]
		if isFound {
			extentsByClass[noun.PresetExtents] = extent
		}
	}
	physics, physicsErr := e.NounPhysicsCatalog(ctx)
	if physicsErr != nil {
		return nil, fmt.Errorf("footprintNames: %w", physicsErr)
	}
	namesByInstance := make(map[uint32]string, len(physics))
	for _, noun := range physics {
		stem := strings.TrimSuffix(strings.ToLower(noun.AssetName), ".noun")
		namesByInstance[hashID(stem)] = noun.AssetName
	}
	catalogs, catalogErr := e.AssetCatalogEntries(ctx)
	if catalogErr != nil {
		return nil, fmt.Errorf("footprintCatalog: %w", catalogErr)
	}
	for _, catalog := range catalogs {
		if catalog.AssetName == nil {
			continue
		}
		nounName := path.Base(strings.ReplaceAll(*catalog.AssetName, "\\", "/"))
		if !strings.EqualFold(path.Ext(nounName), ".noun") {
			continue
		}
		stem := nounName[:len(nounName)-len(".Noun")]
		namesByInstance[hashID(stem)] = nounName
	}
	footprints := make([]NounFootprint, 0, len(nouns))
	for _, noun := range nouns {
		if noun.InstanceID > uint64(^uint32(0)) {
			return nil, fmt.Errorf("footprintInstance[%d]: out of range", noun.ResourceID)
		}
		instanceID := uint32(noun.InstanceID)
		extent, isExtentFound := extentsByInstance[instanceID]
		nounName := extent.NounName
		if nounName == "" {
			nounName = namesByInstance[instanceID]
		}
		if !isExtentFound && noun.PresetExtents != 0 {
			extent = extentsByClass[noun.PresetExtents]
		}
		footprints = append(footprints, NounFootprint{
			InstanceID: instanceID, NounName: nounName, SizeClass: noun.PresetExtents,
			ExtentX: extent.X, ExtentY: extent.Y, ExtentZ: extent.Z,
			MinimumX: noun.MinimumX, MinimumY: noun.MinimumY, MinimumZ: noun.MinimumZ,
			MaximumX: noun.MaximumX, MaximumY: noun.MaximumY, MaximumZ: noun.MaximumZ,
		})
	}
	return footprints, nil
}
