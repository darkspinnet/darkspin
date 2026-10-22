package npc

import "strings"

const captainSpawnScale = float32(1.4)

// SpawnObjectScale applies the normal spawn size independently of /scale.
// Authored placement scale is retained; the noun's graphics scale is client-owned.
func (e SpawnPlan) SpawnObjectScale() float32 {
	objectScale := e.PlacementScale
	if objectScale == 0 {
		objectScale = 1
	}
	if e.IsCaptain || strings.Contains(strings.ToLower(e.NounName), "_captain") {
		objectScale *= captainSpawnScale
	}
	return objectScale
}

// RuntimeObjectScale adds an optional developer preview to the normal spawn size.
func (e SpawnPlan) RuntimeObjectScale() float32 {
	objectScale := e.SpawnObjectScale()
	if e.ScalePreviewMultiplier > 0 {
		objectScale *= e.ScalePreviewMultiplier
	}
	return objectScale
}
