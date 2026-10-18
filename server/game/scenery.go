package game

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/util"
)

type CampaignSceneryObstacle struct {
	MarkerID uint32
	Position Vec3
	Minimum  Vec3
	Maximum  Vec3
	Yaw      float32
}

// SceneryObstacles projects selected, upright static collision placements.
// Visual scenery with physicsType0 is deliberately excluded: maps author
// separate PHYS objects for irregular machinery rather than blocking its AABB.
func (e CampaignDirector) SceneryObstacles() ([]CampaignSceneryObstacle, error) {
	if !e.IsInitialLayoutSelected {
		return nil, errors.New("scenery layout unavailable")
	}
	obstacles := make([]CampaignSceneryObstacle, 0)
	for _, set := range e.MarkerSets {
		for _, definition := range set.Definitions {
			stem := strings.TrimSuffix(strings.ToLower(definition.NounName), ".noun")
			if !definition.IsCollisionEnabled || !e.StaticBlockersByInstance[util.HashID(stem)] {
				continue
			}
			// Tilted shapes need the full native 3D geometry adapter. Never turn
			// a sloping route or overhead prop into a guessed upright wall.
			if math.Abs(float64(definition.Rotation.X)) > 0.001 ||
				math.Abs(float64(definition.Rotation.Y)) > 0.001 {
				continue
			}
			footprint, isFound := e.NavigationFootprintForAsset(definition.NounName)
			if !isFound {
				return nil, fmt.Errorf("sceneryBounds[%d]: noun unavailable", definition.MarkerID)
			}
			minimum, maximum := footprint.Minimum, footprint.Maximum
			if footprint.SizeClass == 0 {
				minimum = Vec3{X: minimum.X * definition.Scale, Y: minimum.Y * definition.Scale, Z: minimum.Z * definition.Scale}
				maximum = Vec3{X: maximum.X * definition.Scale, Y: maximum.Y * definition.Scale, Z: maximum.Z * definition.Scale}
			} else {
				// Native preset ObjectExtents are full, unscaled dimensions.
				minimum = Vec3{X: -footprint.Extents.X / 2, Y: -footprint.Extents.Y / 2, Z: -footprint.Extents.Z / 2}
				maximum = Vec3{X: footprint.Extents.X / 2, Y: footprint.Extents.Y / 2, Z: footprint.Extents.Z / 2}
			}
			obstacles = append(obstacles, CampaignSceneryObstacle{
				MarkerID: definition.MarkerID,
				Position: definition.Position,
				Minimum:  minimum, Maximum: maximum,
				Yaw: definition.Rotation.Z,
			})
		}
	}
	return obstacles, nil
}
