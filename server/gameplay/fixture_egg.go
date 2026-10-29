package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const mutationEggDelay = 200 * time.Millisecond

func withSmallFixtureDeath(snapshot zonenpc.Snapshot, definition zoneNPCDeathDefinition) zoneNPCDeathDefinition {
	definition = withDestructibleDoorDeath(snapshot, definition)
	definition = withTripMineDeath(snapshot, definition)
	definition = withSilenceFieldDeath(snapshot, definition)
	isLight := snapshot.Plan.IsFixture && zonenpc.IsFactoryLight(snapshot.Plan.NounName)
	isEgg := snapshot.Plan.IsFixture && (zonenpc.IsMutationEgg(snapshot.Plan.NounName) || zonenpc.IsPoisonEgg(snapshot.Plan.NounName))
	if !isLight && !isEgg {
		return definition
	}
	definition.isRemnantRetained = true
	definition.isDeathAnimationSuppressed = true
	definition.ordinaryDeathAnimation = ""
	definition.corpseFadeDelay = 0
	definition.deleteDelay = 0
	if isLight {
		// Chunk992's zero-damage query must not become a damaging blast.
		definition.graphicsState = util.HashID("dead")
		definition.isCollisionRetained = false
		definition.explosionEffectName = "sentios_spotlight_explosion.ServerEventDef"
		return definition
	}
	// Chunk906 keeps the source through its delayed query; that publication
	// owns deletion after the hits, rather than a competing corpse timer.
	definition.graphicsState = 0
	definition.isCollisionRetained = true
	definition.explosionEffectName = "nocturna_alien_egg_plasma_explosion.ServerEventDef"
	if zonenpc.IsPoisonEgg(snapshot.Plan.NounName) {
		definition.explosionEffectName = "detonate_impact_firetype.ServerEventDef"
	}
	return definition
}

func (e campaignFixtureBlastStep) finishMutationEgg() ([][]byte, error) {
	if !zonenpc.IsMutationEgg(e.source.Plan.NounName) && !zonenpc.IsPoisonEgg(e.source.Plan.NounName) {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.source.Plan.ObjectID}})
	if err != nil {
		return nil, fmt.Errorf("eggDeleteMarshal: %w", err)
	}
	if !e.npc.FinishMutationEggDeath(e.source) {
		return nil, nil
	}
	err = e.recordOwnedDeath([]zonenpc.DeathEvent{{Kind: zonenpc.DeathDelete, TargetObjectID: e.source.Plan.ObjectID}})
	if err != nil {
		return nil, fmt.Errorf("eggDeleteRecord: %w", err)
	}
	packets := [][]byte{packet}
	e.publication.recordPreparedPackets(packets)
	return packets, nil
}
