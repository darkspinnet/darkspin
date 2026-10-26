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

type DamageTargets interface {
	TargetReader
	Hit(zonenpc.HitRequest) (zonenpc.DamageResult, error)
}

type RecentDamageReader interface {
	RecentDamage(uint32, time.Time) float32
}
