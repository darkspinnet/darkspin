package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func withDestructibleDoorDeath(snapshot zonenpc.Snapshot, definition zoneNPCDeathDefinition) zoneNPCDeathDefinition {
	if !snapshot.Plan.IsFixture || !game.IsDestructibleDoor(snapshot.Plan.NounName, snapshot.Plan.NPCProfile) {
		return definition
	}
	// Build103 chunk127 emits only its positioned burst, waits ten seconds,
	// then deletes. No blast damage, dead graphics, physics removal or corpse
	// animation is requested. The original noun retains its native death event.
	definition.isRemnantRetained = false
	definition.isImmediateDelete = false
	definition.isCollisionRetained = true
	definition.isDeathAnimationSuppressed = true
	definition.ordinaryDeathAnimation = ""
	definition.corpseFadeDelay = 0
	definition.graphicsState = 0
	definition.explosionEffectName = "detonate_impact_firetype.ServerEventDef"
	definition.deleteDelay = 10 * time.Second
	return definition
}
