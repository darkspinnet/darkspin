package game

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/util"
)

type CampaignSceneryObstacle struct {
	MarkerID                    uint32
	Position                    Vec3
	Minimum                     Vec3
	Maximum                     Vec3
	Yaw                         float32
	IsBoundaryProjectionEnabled bool
}

// SceneryObstacles projects selected static collision placements.
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
			// Build 103 9D88A0 adds the scaled bounds center in world space,
			// without rotating that offset. 9E7620 then flattens the object's
			// quaternion to Z/W; 9F08A0 normalizes that planar orientation.
			// Its quarter-turn plus swapped X/Y dimensions is the same box
			// represented here without the backend's coordinate conversion.
			center := minimum.Add(maximum).Scale(0.5)
			halfSize := maximum.Sub(minimum).Scale(0.5)
			obstacles = append(obstacles, CampaignSceneryObstacle{
				MarkerID: definition.MarkerID,
				Position: definition.Position.Add(center),
				Minimum:  halfSize.Scale(-1), Maximum: halfSize,
				Yaw: campaignSceneryYaw(definition.Rotation),
				IsBoundaryProjectionEnabled: strings.EqualFold(e.Level, "nocturna_1") &&
					strings.EqualFold(definition.NounName, "PHYS_design_object_block_3x3.Noun"),
			})
		}
	}
	return obstacles, nil
}

// Build 103 5389B0 constructs Rz*Ry*Rx; navigation keeps only Z/W,
// including for tilted objects. Preserve the resulting heading rather than
// using the authored Z angle or dropping those placements entirely.
func campaignSceneryYaw(rotation Vec3) float32 {
	// Preserve 9D1DF0/5389B0's float32 stages. Near an upside-down
	// rotation Z/W can both approach zero; computing in float64 first
	// changes the normalized heading substantially for shipped rock piles.
	const tau = float32(2 * math.Pi)
	const degreeScale = float32(1.0 / 360)
	x := float32(rotation.X*tau) * degreeScale
	y := float32(rotation.Y*tau) * degreeScale
	z := float32(rotation.Z*tau) * degreeScale
	sineX, cosineX := math.Sincos(float64(x) * 0.5)
	sineY, cosineY := math.Sincos(float64(y) * 0.5)
	sineZ, cosineZ := math.Sincos(float64(z) * 0.5)
	sx, cx := float32(sineX), float32(cosineX)
	sy, cy := float32(sineY), float32(cosineY)
	sz, cz := float32(sineZ), float32(cosineZ)
	quaternionZ := float32(float32(sz*cx)*cy) - float32(float32(cz*sx)*sy)
	quaternionW := float32(float32(sz*sx)*sy) + float32(float32(cz*cx)*cy)
	return float32(2 * math.Atan2(float64(quaternionZ), float64(quaternionW)) * 180 / math.Pi)
}
