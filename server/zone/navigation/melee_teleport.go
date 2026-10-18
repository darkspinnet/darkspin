package navigation

import (
	"errors"

	"github.com/darkspinnet/darkspin/server/game"
	basenavigation "github.com/darkspinnet/darkspin/server/navigation"
)

// MeleeTeleportDestination resolves a selected target's authored position to
// a walkable contact point. A destructible's center can be inside its collider.
func MeleeTeleportDestination(
	mesh *basenavigation.Mesh, source game.Vec3, target game.Vec3,
	footprintRadius float32, strikeRange float32,
) (game.Vec3, bool, error) {
	if footprintRadius < 0 || strikeRange <= 0 {
		return game.Vec3{}, false, errors.New("melee teleport input invalid")
	}
	if mesh == nil {
		return source, false, nil
	}
	planLayer, isLayerFound := mesh.SelectFootprintLayer(footprintRadius)
	if !isLayerFound {
		return source, false, nil
	}
	// The blink may cross disconnected terrain. The source supplies only the
	// approach direction for contact with the target's collider.
	landing, err := mesh.ProjectInteractionTarget(
		basenavigation.Projection{Position: basenavigation.Vec3(source)},
		basenavigation.Vec3(target),
		basenavigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: strikeRange,
		},
	)
	if err != nil {
		return source, false, nil
	}
	return game.Vec3(landing.Position), true, nil
}
