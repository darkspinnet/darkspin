package gameplay

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
)

func clipProjectileObstacles(mesh *navigation.Mesh, segments []sim.ProjectileMotionSegment) []sim.ProjectileMotionSegment {
	if mesh == nil {
		return segments
	}
	for index, segment := range segments {
		start := game.Vec3(segment.Position)
		end := start.Add(game.Vec3(segment.Direction).Scale(segment.Distance))
		fraction, isHit := mesh.FirstObstacleContact(navigation.Vec3(start), navigation.Vec3(end))
		if isHit {
			segments[index].Distance *= fraction
			segments[index].IsBlocked = true
			return segments[:index+1]
		}
	}
	return segments
}
