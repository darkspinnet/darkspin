package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/content/render/prop"
)

const campaignMajorStageCountProperty = uint32(0x4392e73c)
const campaignMinorStageCountProperty = uint32(0xe1ce1738)
const sidekickProgressionGapProperty = uint32(0x37a8a855)
const forceSidekickingProperty = uint32(0xf92b4fbd)

// Authored operands stay nullable; effective settings use sub_9D4160's
// compiled defaults only when the corresponding property is absent.
type RewardProgressionTuning struct {
	MajorStageCount        uint32
	MinorStageCount        uint32
	SidekickProgressionGap int32
	IsSidekickingForced    bool
	Authored               RewardProgressionProperties
}

type RewardProgressionProperties struct {
	ContentSourceResourceID *int64
	MajorStageCount         *uint32
	MinorStageCount         *uint32
	SidekickProgressionGap  *int32
	IsSidekickingForced     *bool
}

func validateRewardProgressionProperty(propertyID uint32, propertyType uint16, payload []byte) error {
	if propertyID == forceSidekickingProperty {
		if propertyType != prop.TypeBool || len(payload) != 1 || payload[0] > 1 {
			return errors.New("invalid sidekicking boolean")
		}
		return nil
	}
	// Installed resources encode both native int32 operands as property type
	// 0x000A. Keep the stored type and interpret their bits as signed operands.
	if (propertyType != prop.TypeInt32 && propertyType != prop.TypeUInt32) || len(payload) != 4 {
		return fmt.Errorf("progressionInteger[%#x]: type %#x size %d", propertyID, propertyType, len(payload))
	}
	if (propertyID == campaignMinorStageCountProperty || propertyID == campaignMajorStageCountProperty) && int32(binary.BigEndian.Uint32(payload)) <= 0 {
		return errors.New("campaign minor-stage count must be positive")
	}
	return nil
}

func (e *Store) RewardProgressionTuning(ctx context.Context) (RewardProgressionTuning, error) {
	if e == nil || e.database == nil || ctx == nil {
		return RewardProgressionTuning{}, errors.New("reward progression store or context unavailable")
	}
	tuning := RewardProgressionTuning{MajorStageCount: 10, MinorStageCount: 4, SidekickProgressionGap: 4}
	var resourceID int64
	err := e.database.QueryRowContext(ctx, `SELECT resource.id FROM content_source_resource AS resource
		JOIN content_source_package AS package ON package.id=resource.content_source_package_id
		WHERE package.package_name=? AND resource.ordinal=0 AND resource.type_id=?
		AND resource.group_id=? AND resource.instance_id=?`, directorCompositionPackage,
		prop.ResourceType, directorCompositionIdentity, directorCompositionIdentity).Scan(&resourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return tuning, nil
	}
	if err != nil {
		return RewardProgressionTuning{}, fmt.Errorf("progressionSource: %w", err)
	}
	tuning.Authored.ContentSourceResourceID = &resourceID
	for _, propertyID := range []uint32{campaignMajorStageCountProperty, campaignMinorStageCountProperty, sidekickProgressionGapProperty, forceSidekickingProperty} {
		propertyType, payload, propertyErr := e.directorProperty(ctx, propertyID)
		if errors.Is(propertyErr, sql.ErrNoRows) {
			continue
		}
		if propertyErr != nil {
			return RewardProgressionTuning{}, fmt.Errorf("progressionProperty[%#x]: %w", propertyID, propertyErr)
		}
		err = validateRewardProgressionProperty(propertyID, propertyType, payload)
		if err != nil {
			return RewardProgressionTuning{}, fmt.Errorf("progressionStored: %w", err)
		}
		switch propertyID {
		case campaignMajorStageCountProperty:
			count := binary.BigEndian.Uint32(payload)
			tuning.MajorStageCount = count
			tuning.Authored.MajorStageCount = &count
		case campaignMinorStageCountProperty:
			count := binary.BigEndian.Uint32(payload)
			tuning.MinorStageCount = count
			tuning.Authored.MinorStageCount = &count
		case sidekickProgressionGapProperty:
			gap := int32(binary.BigEndian.Uint32(payload))
			tuning.SidekickProgressionGap = gap
			tuning.Authored.SidekickProgressionGap = &gap
		case forceSidekickingProperty:
			isForced := payload[0] != 0
			tuning.IsSidekickingForced = isForced
			tuning.Authored.IsSidekickingForced = &isForced
		}
	}
	return tuning, nil
}
