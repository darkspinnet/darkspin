package npc

import "log"

// SpatialRadius supplies the noun sphere used by native GetObjectsInRadius.
// Plans without imported noun bounds retain their existing fallback radius.
func (e SpawnPlan) SpatialRadius() float32 {
	if e.NPCProfile.ActorFootprint == nil {
		return max(float32(0), e.NPCProfile.FootprintRadius)
	}
	scale := e.PlacementScale
	if scale == 0 {
		scale = 1
	}
	radius, err := e.NPCProfile.ActorFootprint.SpatialRadius(scale)
	if err != nil {
		log.Printf("NPC spatial radius noun=%q: %v", e.NounName, err)
		return 0
	}
	return radius
}
