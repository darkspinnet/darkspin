package game

import (
	"fmt"
	"math"
	"strings"
)

// Keep server-owned Cryos cave fungi off the selected boss teleporter pad.
// Some authored smart-object alternatives overlap the fixed pad even with
// their exact source transforms. Omit those fixtures rather than inventing
// replacement transforms or changing the installed map and teleporter.
func (e CampaignDirector) isCryosFungusOnTeleporter(marker CampaignDirectorMarker) (bool, error) {
	if !strings.EqualFold(e.Level, cryosCaveLevel) ||
		!strings.EqualFold(marker.NounName, CryosFungusNoun) {
		return false, nil
	}
	fungusFootprint, isFound := e.NavigationFootprintForAsset(marker.NounName)
	if !isFound {
		return false, fmt.Errorf("fungusBounds: noun %q unavailable", marker.NounName)
	}
	radius, err := fungusFootprint.ActorRadius(marker.Scale)
	if err != nil {
		return false, fmt.Errorf("fungusRadius: %w", err)
	}
	for _, set := range e.MarkerSets {
		for _, definition := range set.Definitions {
			if !strings.EqualFold(definition.NounName, "BossSecurityTeleporter.Noun") {
				continue
			}
			padFootprint, isPadFound := e.NavigationFootprintForAsset(definition.NounName)
			if !isPadFound || padFootprint.SizeClass != 0 || definition.Scale <= 0 {
				return false, fmt.Errorf("padBounds[%d]: unavailable", definition.MarkerID)
			}
			minimum := padFootprint.Minimum.Scale(definition.Scale)
			maximum := padFootprint.Maximum.Scale(definition.Scale)
			if marker.Position.Z+fungusFootprint.Maximum.Z*marker.Scale < definition.Position.Z+minimum.Z ||
				marker.Position.Z+fungusFootprint.Minimum.Z*marker.Scale > definition.Position.Z+maximum.Z {
				continue
			}
			// The pad is horizontal. Rotate into its authored heading, then
			// compare its visual rectangle with the fungus's existing footprint.
			sine, cosine := math.Sincos(float64(campaignSceneryYaw(definition.Rotation)) * math.Pi / 180)
			delta := marker.Position.Sub(definition.Position)
			x := float64(delta.X)*cosine + float64(delta.Y)*sine
			y := -float64(delta.X)*sine + float64(delta.Y)*cosine
			distanceX := x - max(float64(minimum.X), min(x, float64(maximum.X)))
			distanceY := y - max(float64(minimum.Y), min(y, float64(maximum.Y)))
			if distanceX*distanceX+distanceY*distanceY <= float64(radius)*float64(radius) {
				return true, nil
			}
		}
	}
	return false, nil
}
