package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/content/render/prop"
)

const resurrectionHealthFractionProperty = uint32(0x9e4b6c55)

type ResurrectionPickupTuning struct {
	HealthFraction          float32
	AuthoredHealthFraction  *float32
	ContentSourceResourceID *int64
	PropertyType            *uint16
}

func (e *Store) ResurrectionPickupTuning(ctx context.Context) (ResurrectionPickupTuning, error) {
	if e == nil || e.database == nil || ctx == nil {
		return ResurrectionPickupTuning{}, errors.New("resurrection tuning store or context unavailable")
	}
	tuning := ResurrectionPickupTuning{HealthFraction: 0.25}
	var propertyType uint16
	var payload []byte
	var sourceID int64
	err := e.database.QueryRowContext(ctx, `SELECT property_type, encoded_item, content_source_resource_id
		FROM director_composition_property WHERE property_id=?`, resurrectionHealthFractionProperty).
		Scan(&propertyType, &payload, &sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return tuning, nil
	}
	if err != nil {
		return ResurrectionPickupTuning{}, fmt.Errorf("resurrectionQuery: %w", err)
	}
	if propertyType != prop.TypeFloat || len(payload) != 4 {
		return ResurrectionPickupTuning{}, fmt.Errorf("resurrectionShape: type %#x size %d", propertyType, len(payload))
	}
	fraction := math.Float32frombits(binary.BigEndian.Uint32(payload))
	if !isFinite(fraction) {
		return ResurrectionPickupTuning{}, fmt.Errorf("resurrectionFraction: nonfinite")
	}
	tuning.HealthFraction = fraction
	tuning.AuthoredHealthFraction = &fraction
	tuning.ContentSourceResourceID = &sourceID
	tuning.PropertyType = &propertyType
	return tuning, nil
}
