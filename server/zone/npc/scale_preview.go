package npc

import (
	"errors"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type ScalePreviewRequest struct {
	Position   game.Vec3
	Radius     float32
	Multiplier float32
	NounFilter string
	IsReset    bool
}

type ScalePreview struct {
	Plan        SpawnPlan
	ObjectScale float32
}

func (e *Session) PreviewScales(req ScalePreviewRequest) ([]ScalePreview, error) {
	if e == nil || !zonegeometry.IsFinite(req.Position) || math.IsNaN(float64(req.Radius)) || math.IsInf(float64(req.Radius), 0) ||
		req.Radius <= 0 || req.Radius > 100 || math.IsNaN(float64(req.Multiplier)) ||
		math.IsInf(float64(req.Multiplier), 0) || (!req.IsReset && (req.Multiplier < 0.25 || req.Multiplier > 4)) {
		return nil, errors.New("invalid scale preview")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	previews := make([]ScalePreview, 0)
	nounFilter := strings.ToLower(req.NounFilter)
	for _, objectID := range e.objectIDs {
		npc := e.npcs[objectID]
		if !npc.IsPublished || npc.IsDefeated || npc.HitPoint <= 0 || npc.Plan.IsFixture ||
			npc.Faction != FactionNonPlayerAligned ||
			!strings.Contains(strings.ToLower(npc.Plan.NounName), nounFilter) ||
			npc.Plan.Position.Sub(req.Position).Length() > req.Radius {
			continue
		}
		npc.Plan.ScalePreviewMultiplier = 0
		if !req.IsReset {
			npc.Plan.ScalePreviewMultiplier = req.Multiplier
		}
		e.npcs[objectID] = npc
		previews = append(previews, ScalePreview{Plan: npc.Plan, ObjectScale: npc.Plan.RuntimeObjectScale()})
	}
	return previews, nil
}
