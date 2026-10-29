package gameplay

import (
	"fmt"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Exact hit feedback belongs to the fixture publication, just like its lethal
// feedback. A separate projection queue could deliver a source delete first.
func (e campaignFixtureBlastStep) publishOwnedHit(target zonenpc.Snapshot, damage zonenpc.DamageResult) ([][]byte, error) {
	packets, err := npcraknet.Hit(npcraknet.HitRequest{
		SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: target.Plan.ObjectID,
		Damage: damage.Damage, HitPoint: damage.HitPoint,
	})
	if err != nil {
		return nil, fmt.Errorf("fixtureHitEncode: %w", err)
	}
	e.publication.recordPreparedPackets(packets)
	err = e.runtime.projection.recordEnemyDamageObjective(e.sessionKey, e.generation, zonenpc.DamageEvent{
		SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: target.Plan.ObjectID,
		Position: target.Plan.Position, Damage: damage.Damage, HitPoint: damage.HitPoint,
	})
	if err != nil {
		return packets, fmt.Errorf("fixtureHitObjective: %w", err)
	}
	return packets, nil
}
