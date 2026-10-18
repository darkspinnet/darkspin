package gameplay

import (
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func campaignRayKillerFleeDestination(
	mesh *navigation.Mesh, enemy zonenpc.Snapshot, target game.Vec3,
	attackRange float32,
) (game.Vec3, bool, error) {
	source := enemy.Plan.Position
	separation := zonegeometry.Distance(source, target)
	distance := min(float32(8), max(float32(3), attackRange*0.75-separation))
	angle := math.Atan2(float64(source.Y-target.Y), float64(source.X-target.X))
	// Try away first, then sideways escape routes and shorter steps at walls.
	// Navigation projection must never turn a retreat back toward the hero.
	for _, stepScale := range []float32{1, 0.5} {
		for _, offset := range []float64{0, 30, -30, 60, -60, 90, -90} {
			direction := angle + offset*math.Pi/180
			candidate := game.Vec3{
				X: source.X + float32(math.Cos(direction))*distance*stepScale,
				Y: source.Y + float32(math.Sin(direction))*distance*stepScale,
				Z: source.Z,
			}
			destination, isFound, err := zoneaction.NPCDirectMovementDestination(
				mesh, source, candidate, enemy.NavigationRadius(), enemy.Navigation,
			)
			if err != nil {
				return game.Vec3{}, false, fmt.Errorf("retreatProject: %w", err)
			}
			if !isFound || zonegeometry.Distance(source, destination) < 1 ||
				zonegeometry.Distance(destination, target) < separation+1 {
				continue
			}
			return destination, true, nil
		}
	}
	return game.Vec3{}, false, nil
}
