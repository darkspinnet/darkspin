package object

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

type SceneryPlan struct {
	objectID uint32
	marker   game.CampaignDirectorMarker
}

type SceneryPublication struct {
	ObjectID           uint32
	NounName           string
	Position           game.Vec3
	Rotation           game.Vec3
	Scale              float32
	IsCollisionEnabled bool
	IsVisible          bool
}

func PlanScenery(
	markers []game.CampaignDirectorMarker,
) ([]SceneryPlan, error) {
	plans := make([]SceneryPlan, 0, len(markers))
	for markerIndex, marker := range markers {
		if marker.MarkerID == 0 || marker.NounName == "" || marker.Scale <= 0 {
			return nil, fmt.Errorf("sceneryMarker[%d]: invalid", markerIndex)
		}
		plans = append(plans, SceneryPlan{objectID: marker.MarkerID, marker: marker})
	}
	return plans, nil
}

func PublishScenery(plan SceneryPlan) (SceneryPublication, error) {
	if plan.objectID == 0 || plan.marker.MarkerID == 0 ||
		plan.marker.NounName == "" || plan.marker.Scale <= 0 {
		return SceneryPublication{}, errors.New("scenery object invalid")
	}
	return SceneryPublication{
		ObjectID: plan.objectID, NounName: plan.marker.NounName,
		Position: plan.marker.Position, Rotation: plan.marker.Rotation,
		Scale: plan.marker.Scale, IsCollisionEnabled: plan.marker.IsCollisionEnabled,
		IsVisible: plan.marker.IsVisible,
	}, nil
}
