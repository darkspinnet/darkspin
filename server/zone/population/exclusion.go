package population

import "github.com/darkspinnet/darkspin/server/game"

type exclusionVolume struct {
	position game.Vec3
	radius   float32
}

// spawnExclusionVolumes follows sub_9F5B50/sub_9F54E0: kinds 6 and 9
// register authored volumes even in marker sets that contain no spawn pool.
func spawnExclusionVolumes(director game.CampaignDirector) []exclusionVolume {
	var volumes []exclusionVolume
	for _, markerSet := range director.MarkerSets {
		for _, marker := range markerSet.Markers {
			if marker.IsExclusionVolume {
				volumes = append(volumes, exclusionVolume{
					position: marker.Position, radius: marker.ExclusionRadius,
				})
			}
		}
		for _, trigger := range markerSet.Triggers {
			if trigger.IsExclusionVolume {
				volumes = append(volumes, exclusionVolume{
					position: trigger.Position, radius: trigger.ExclusionRadius,
				})
			}
		}
	}
	return volumes
}

// isSpikeExcluded mirrors sub_9F7E10's spatial query (sub_A162F0): strict
// 3D sphere overlap against each candidate's indexed sphere, not containment
// of the marker's center. No encounter RNG or occupancy decision is involved.
func isSpikeExcluded(marker game.CampaignDirectorMarker, volumes []exclusionVolume) bool {
	for _, volume := range volumes {
		x := marker.Position.X - volume.position.X
		y := marker.Position.Y - volume.position.Y
		z := marker.Position.Z - volume.position.Z
		radius := float32(volume.radius + marker.SpatialRadius)
		distanceSquared := float32(float32(z*z+y*y) + float32(x*x))
		if float32(radius*radius) > distanceSquared {
			return true
		}
	}
	return false
}
