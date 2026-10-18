package npc

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/util"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// ConfigureNavigation supplies noun geometry once at zone creation. Restored
// actor objects retain their stored planLayer; new actors select at creation.
func (e *Session) ConfigureNavigation(mesh *navigation.Mesh, footprints map[uint32]game.NavigationFootprint) error {
	if e == nil {
		return errors.New("npc navigation unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.navigationMesh = mesh
	e.navigationFootprints = footprints
	for objectID, npc := range e.npcs {
		npc.Plan = e.attachActorFootprint(npc.Plan)
		e.npcs[objectID] = npc
		if !zonegeometry.IsFiniteScalar(npc.Navigation.Radius) || npc.Navigation.Radius < 0 {
			return fmt.Errorf("actorRadius[%d]: invalid", objectID)
		}
		if npc.Navigation.IsPresent && mesh != nil {
			layerInfo, isFound := mesh.LayerInfo(npc.Navigation.PlanLayer)
			if !isFound || layerInfo.PlanLayer != npc.Navigation.PlanLayer {
				return fmt.Errorf("actorLayer[%d]: unavailable", objectID)
			}
			continue
		}
		actor, err := e.navigationForPlan(npc.Plan)
		if err != nil {
			return fmt.Errorf("actorNavigation[%d]: %w", objectID, err)
		}
		npc.Navigation = actor
		e.npcs[objectID] = npc
	}
	return nil
}

func (e *Session) navigationForPlan(plan SpawnPlan) (navigation.ActorNavigation, error) {
	plan = e.attachActorFootprint(plan)
	radius, err := plan.actorFootprintRadius()
	if err != nil {
		return navigation.ActorNavigation{}, fmt.Errorf("planRadius: %w", err)
	}
	radius = max(radius, float32(0.1))
	mode := uint8(0)
	if plan.IsArena {
		mode = uint8(game.ModeArena)
	}
	actor := navigation.ActorNavigation{Radius: radius, Mode: mode}
	if e.navigationMesh == nil {
		return actor, nil
	}
	planLayer, isLayerFound := e.navigationMesh.SelectFootprintLayer(radius, mode)
	if !isLayerFound {
		return navigation.ActorNavigation{}, errors.New("actor navigation layer unavailable")
	}
	actor.PlanLayer = planLayer
	actor.IsPresent = true
	return actor, nil
}

func (e *Session) attachActorFootprint(plan SpawnPlan) SpawnPlan {
	stem := strings.TrimSuffix(strings.ToLower(plan.NounName), ".noun")
	footprint, isFound := e.navigationFootprints[util.HashID(stem)]
	if isFound {
		plan.NPCProfile.ActorFootprint = &footprint
	}
	return plan
}

func (e SpawnPlan) actorFootprintRadius() (float32, error) {
	if e.NPCProfile.ActorFootprint == nil {
		// Compatibility plans genuinely lacking noun geometry retain the old
		// radius, which may already include authored scale. Do not scale twice.
		return e.NPCProfile.FootprintRadius, nil
	}
	scale := e.PlacementScale
	if scale == 0 {
		scale = 1
	}
	radius, err := e.NPCProfile.ActorFootprint.ActorRadius(scale)
	if err != nil {
		return 0, fmt.Errorf("nounRadius: %w", err)
	}
	return radius, nil
}

// ActorFootprintRadius follows the live object scale, independently of the
// cached navigation layer and the noun's authored graphics scale.
func (e SpawnPlan) ActorFootprintRadius() float32 {
	radius, err := e.actorFootprintRadius()
	if err != nil {
		log.Printf("NPC actor footprint noun=%q: %v", e.NounName, err)
		return 0
	}
	return radius
}

func (e Snapshot) NavigationRadius() float32 {
	if e.Navigation.IsPresent || e.Navigation.Radius > 0 {
		return e.Navigation.Radius
	}
	return e.Plan.ActorFootprintRadius()
}
