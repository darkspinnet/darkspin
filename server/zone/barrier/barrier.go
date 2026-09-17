package barrier

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
)

type Plan struct {
	objectID      uint32
	markerSetName string
	marker        game.CampaignDirectorMarker
}

type ObjectPublication struct {
	ObjectID           uint32
	NounName           string
	Position           game.Vec3
	Rotation           game.Vec3
	Scale              float32
	IsCollisionEnabled bool
	IsVisible          bool
}

func PlanSets(
	barrierSets []game.CampaignHordeBarrierSet, firstObjectID uint32,
) (map[string][]Plan, uint32, error) {
	if firstObjectID == 0 || firstObjectID >= zoneobject.ProjectileIDStart {
		return nil, firstObjectID, errors.New("horde barriers invalid")
	}
	plansByMarkerSet := make(map[string][]Plan, len(barrierSets))
	nextObjectID := firstObjectID
	for setIndex, set := range barrierSets {
		key := strings.ToLower(set.MarkerSetName)
		if key == "" || len(set.Markers) == 0 || plansByMarkerSet[key] != nil {
			return nil, firstObjectID, fmt.Errorf("hordeBarrierSet[%d]: invalid", setIndex)
		}
		plans := make([]Plan, 0, len(set.Markers))
		for markerIndex, marker := range set.Markers {
			if nextObjectID >= zoneobject.ProjectileIDStart || marker.MarkerID == 0 ||
				marker.NounName == "" || !zonepopulation.IsFinitePosition(marker.Position) ||
				marker.Scale <= 0 {
				return nil, firstObjectID, fmt.Errorf("hordeBarrierMarker[%d][%d]: invalid",
					setIndex, markerIndex)
			}
			plans = append(plans, Plan{
				objectID: nextObjectID, markerSetName: set.MarkerSetName, marker: marker,
			})
			nextObjectID++
		}
		plansByMarkerSet[key] = plans
	}
	return plansByMarkerSet, nextObjectID, nil
}

func CreatePublication(plans []Plan) ([]ObjectPublication, error) {
	if len(plans) == 0 {
		return nil, errors.New("horde barrier create: invalid plans")
	}
	publication := make([]ObjectPublication, 0, len(plans))
	for planIndex, plan := range plans {
		if plan.objectID == 0 || plan.marker.NounName == "" ||
			!zonepopulation.IsFinitePosition(plan.marker.Position) || plan.marker.Scale <= 0 {
			return nil, fmt.Errorf("hordeBarrierCreate[%d]: invalid", planIndex)
		}
		publication = append(publication, ObjectPublication{
			ObjectID:           plan.objectID,
			NounName:           plan.marker.NounName,
			Position:           plan.marker.Position,
			Rotation:           plan.marker.Rotation,
			Scale:              plan.marker.Scale,
			IsCollisionEnabled: plan.marker.IsCollisionEnabled,
			IsVisible:          plan.marker.IsVisible,
		})
	}
	return publication, nil
}

func DeletePublication(plans []Plan) ([]uint32, error) {
	if len(plans) == 0 {
		return nil, errors.New("horde barrier delete: invalid plans")
	}
	objectIDs := make([]uint32, 0, len(plans))
	for planIndex, plan := range plans {
		if plan.objectID == 0 {
			return nil, fmt.Errorf("hordeBarrierDelete[%d]: invalid", planIndex)
		}
		objectIDs = append(objectIDs, plan.objectID)
	}
	return objectIDs, nil
}
