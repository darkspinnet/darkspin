package gameplay

import (
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func (e campaignAbilityCommandRuntime) zetawattBeamPresentation(
	sourceObjectID uint32, sourcePosition game.Vec3, creature game.GameplayCreature, plan zoneability.AreaPlan,
) ([]byte, error) {
	// Build 103 queries the damage line from GetPosition, but publishes the
	// visual endpoint from GetCenterPoint on the caster's horizontal plane.
	center := sourcePosition
	footprint, isFootprintFound := e.program.NavigationFootprintsByNoun[creature.Noun]
	if isFootprintFound && footprint.SizeClass != 0 {
		// GetCenterPoint (103 9D6D40 -> 9D9420) uses the third
		// ObjectExtents dimension, not the raw noun bounding-box midpoint.
		// Player ObjectCreate owns scale 1 independently of graphics_scale.
		center.Z += footprint.Extents.Z * 0.5
	} else {
		physics, isPhysicsFound := e.program.NounPhysicsByID[creature.Noun]
		if isPhysicsFound {
			center.Z += physics.BoundMaximum.Z * 0.5
		}
	}
	direction := plan.Facing
	direction.Z = 0
	length := float32(math.Sqrt(float64(direction.X*direction.X + direction.Y*direction.Y)))
	endpoint := center
	if length > 0 {
		endpoint = center.Add(direction.Scale(plan.Definition.Distance / length))
	}
	packet, err := effectraknet.Beam(effectraknet.BeamRequest{
		AssetID: util.HashID(plan.Definition.ImpactEffectName), SourceObjectID: sourceObjectID, TargetPoint: endpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("zetawattBeam: %w", err)
	}
	return packet, nil
}
