package game

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// BossTriggerActivation resolves the selected trigger's authored dwell and
// spatial boundary without using a filename or an unselected marker set.
func (d CampaignDirector) BossTriggerActivation(
	publication CampaignDirectorPublication,
) (Vec3, float32, time.Duration, error) {
	for _, markerSet := range d.MarkerSets {
		if markerSet.Ordinal != publication.MarkerSetOrdinal ||
			markerSet.Name != publication.MarkerSetName {
			continue
		}
		for _, trigger := range markerSet.Triggers {
			if trigger.MarkerID != publication.TriggerMarkerID ||
				trigger.Ordinal != publication.TriggerOrdinal ||
				trigger.SpawnTrigger == nil ||
				trigger.SpawnTrigger.TriggerVolume == nil {
				continue
			}
			for _, event := range trigger.Events {
				if event.Ordinal != publication.EventOrdinal ||
					event.CallbackName != publication.CallbackName ||
					event.EventName != publication.EventName ||
					event.TriggerRadius <= 0 {
					continue
				}
				seconds := trigger.SpawnTrigger.TriggerVolume.TimeToActivate
				if math.IsNaN(float64(seconds)) || math.IsInf(float64(seconds), 0) || seconds < 0 {
					return Vec3{}, 0, 0, errors.New("boss trigger dwell invalid")
				}
				return trigger.Position, event.TriggerRadius,
					time.Duration(float64(seconds) * float64(time.Second)), nil
			}
		}
	}
	return Vec3{}, 0, 0, fmt.Errorf("boss trigger %d unavailable", publication.TriggerMarkerID)
}
