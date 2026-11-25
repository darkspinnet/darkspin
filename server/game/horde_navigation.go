package game

import "fmt"

// HordeNavigationObstacles uses the same build-103 fixed-object navigation box
// as scenery: 9E7620 admits fixed, physics-enabled, collidable objects and calls
// 9D88A0 for noun bounds. CloseDoor's static-noun fallback A64930 enables that
// collision flag; it does not substitute the teleporter's trigger volume.
func (e CampaignDirector) HordeNavigationObstacles() (map[string][]CampaignSceneryObstacle, error) {
	sets, err := e.HordeBarriers()
	if err != nil {
		return nil, fmt.Errorf("hordePlacements: %w", err)
	}
	obstaclesByMarkerSet := make(map[string][]CampaignSceneryObstacle, len(sets))
	for _, set := range sets {
		obstacles := make([]CampaignSceneryObstacle, 0, len(set.Markers))
		for _, marker := range set.Markers {
			footprint, isFound := e.NavigationFootprintForAsset(marker.NounName)
			if !isFound || footprint.SizeClass != 0 {
				return nil, fmt.Errorf("hordeBounds[%d]: custom noun bounds unavailable", marker.MarkerID)
			}
			minimum := footprint.Minimum.Scale(marker.Scale)
			maximum := footprint.Maximum.Scale(marker.Scale)
			center := minimum.Add(maximum).Scale(0.5)
			halfSize := maximum.Sub(minimum).Scale(0.5)
			obstacles = append(obstacles, CampaignSceneryObstacle{
				MarkerID: marker.MarkerID, Position: marker.Position.Add(center),
				Minimum: halfSize.Scale(-1), Maximum: halfSize,
				Yaw: campaignSceneryYaw(marker.Rotation),
			})
		}
		obstaclesByMarkerSet[set.MarkerSetName] = obstacles
	}
	return obstaclesByMarkerSet, nil
}
