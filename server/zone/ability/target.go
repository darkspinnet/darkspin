package ability

import (
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// TargetReader is the hostile-object view consumed by the shared ability
// geometry. An Arena implementation projects player objects into this view;
// their resources remain owned by the player squad, never an NPC session.
type TargetReader interface {
	NPC(uint32) (zonenpc.Snapshot, bool)
	LiveSnapshots() []zonenpc.Snapshot
}

// IsHostileTarget is the shared live-target boundary for hero attack selection,
// admission and impact. Visibility and authored targeting exceptions remain
// owned by their existing ability and NPC policies.
func IsHostileTarget(target zonenpc.Snapshot) bool {
	return target.Plan.ObjectID != 0 && target.IsPublished && !target.IsBirthPending &&
		!target.IsDefeated && target.HitPoint > 0 && target.Faction == zonenpc.FactionNonPlayerAligned
}

type DamageTargets interface {
	TargetReader
	Hit(zonenpc.HitRequest) (zonenpc.DamageResult, error)
}

type RecentDamageReader interface {
	RecentDamage(uint32, time.Time) float32
}
