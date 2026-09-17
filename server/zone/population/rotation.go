package population

import "github.com/darkspinnet/darkspin/server/game"

// GroupRotation follows GroupPositions: exact authored slots retain their
// rotations, while additional actors generated around an anchor start at zero
// Euler rotation and turn when their introduction or action accepts a target.
func GroupRotation(rotations []game.Vec3, index int) game.Vec3 {
	if index < 0 || index >= len(rotations) {
		return game.Vec3{}
	}
	return rotations[index]
}

// Population recipes can regroup or replace positions. Resolve their final
// slots against spawn markers so a relocated actor cannot inherit an unrelated
// anchor's rotation. Camera, light, and trigger transforms are excluded.
func assignCandidateRotations(candidates []candidate, director game.CampaignDirector) {
	for _, markerSet := range director.MarkerSets {
		rotationsByPosition := make(map[game.Vec3]game.Vec3)
		for _, marker := range markerSet.Markers {
			if !marker.IsSpawnKindKnown || (marker.SpawnKind != 7 && marker.SpawnKind != 8) {
				continue
			}
			rotationsByPosition[marker.Position] = marker.Rotation
		}
		for index := range candidates {
			current := &candidates[index]
			if current.markerSetOrdinal != markerSet.Ordinal || current.markerSetName != markerSet.Name {
				continue
			}
			current.rotations = make([]game.Vec3, len(current.positions))
			for positionIndex, position := range current.positions {
				current.rotations[positionIndex] = rotationsByPosition[position]
			}
		}
	}
}
