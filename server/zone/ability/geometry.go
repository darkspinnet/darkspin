package ability

import (
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func Distance(first game.Vec3, second game.Vec3) float32 {
	return zonegeometry.Distance(first, second)
}

func IsInvalidNumber(number float32) bool {
	return math.IsNaN(float64(number)) || math.IsInf(float64(number), 0)
}

func CursorTarget(
	enemies *zonenpc.Session, sourceObjectID uint32,
	cursorPosition game.Vec3, targetPosition game.Vec3, radius float32,
) uint32 {
	if enemies == nil || sourceObjectID == 0 || radius <= 0 {
		return 0
	}
	position := cursorPosition
	if !isReportedPosition(position) {
		position = targetPosition
	}
	if !isReportedPosition(position) {
		return 0
	}
	selectedObjectID := uint32(0)
	selectedDistance := float32(math.MaxFloat32)
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.Plan.ObjectID == sourceObjectID ||
			enemy.Faction != zonenpc.FactionNonPlayerAligned {
			continue
		}
		maximumDistance := radius + max(
			float32(0), enemy.Plan.NPCProfile.FootprintRadius,
		)
		distance := zonegeometry.Distance(position, enemy.Plan.Position)
		if distance > maximumDistance || distance > selectedDistance {
			continue
		}
		if distance == selectedDistance && selectedObjectID != 0 &&
			enemy.Plan.ObjectID > selectedObjectID {
			continue
		}
		selectedObjectID = enemy.Plan.ObjectID
		selectedDistance = distance
	}
	return selectedObjectID
}

func isReportedPosition(position game.Vec3) bool {
	return zonegeometry.IsReported(position)
}
