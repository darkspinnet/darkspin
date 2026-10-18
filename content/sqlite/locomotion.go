package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/content/render/prop"
)

// LocomotionFallbackProperties retains authored operands and absence. Native
// sub_9D4160 uses 30 for each missing property in a present resource; without
// that resource its static defaults remain 30/30/360. Neither path uses the
// reflected noun defaults 200/500/1440. Runtime consumers own that decision.
type LocomotionFallbackProperties struct {
	IsResourcePresent bool
	Acceleration      *float32
	Deceleration      *float32
	TurnRate          *float32
}

func (e *Store) LocomotionFallbackProperties(ctx context.Context) (LocomotionFallbackProperties, error) {
	if e == nil || e.database == nil || ctx == nil {
		return LocomotionFallbackProperties{}, errors.New("locomotion property store or context unavailable")
	}
	var properties LocomotionFallbackProperties
	err := e.database.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM content_source_resource AS resource
		JOIN content_source_package AS package ON package.id=resource.content_source_package_id
		WHERE package.package_name=? AND resource.type_id=? AND resource.group_id=? AND resource.instance_id=?)`,
		directorCompositionPackage, int64(prop.ResourceType), int64(directorCompositionIdentity),
		int64(directorCompositionIdentity)).Scan(&properties.IsResourcePresent)
	if err != nil {
		return LocomotionFallbackProperties{}, fmt.Errorf("locomotionResourceQuery: %w", err)
	}
	if !properties.IsResourcePresent {
		return properties, nil
	}
	fields := []**float32{&properties.Acceleration, &properties.Deceleration, &properties.TurnRate}
	for index, propertyID := range []uint32{0xee91716a, 0x3b27f50b, 0x8cd645ab} {
		number, propertyErr := e.directorFloat(ctx, propertyID)
		if errors.Is(propertyErr, sql.ErrNoRows) {
			continue
		}
		if propertyErr != nil {
			return LocomotionFallbackProperties{}, fmt.Errorf("locomotionProperty[%#x]: %w", propertyID, propertyErr)
		}
		*fields[index] = &number
	}
	return properties, nil
}
