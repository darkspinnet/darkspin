package gameplay

import (
	"encoding/binary"
	"testing"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// A launched attack retains its casting object even after its player deploys
// another hero. These offsets/masks are the two build-103 combat event shapes.
func TestArenaImpactKeepsCastSourceAfterHeroSwitch(t *testing.T) {
	fixtures := []struct {
		name           string
		absorbedDamage float32
		mask           byte
		sourceOffset   int
	}{
		{name: "ordinary", mask: 0x9b, sourceOffset: 12},
		{name: "shielded", absorbedDamage: 5, mask: 0xff, sourceOffset: 16},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			const castSourceID = uint32(17)
			const switchedHeroID = uint32(22)
			const targetID = uint32(42)
			source := gameplayPeerSession{}
			source.deployedObjectID = switchedHeroID
			targets := arenaAbilityTargets{
				source: source,
				hits: map[uint32]arenaAbilityHit{
					targetID: {sourceObjectID: castSourceID},
				},
			}
			packets, err := targets.packets([]zoneability.AreaResult{{
				Damage: zonenpc.DamageResult{ObjectID: targetID, Damage: 3,
					HitPoint: 10, AbsorbedDamage: fixture.absorbedDamage},
			}})
			if err != nil {
				t.Fatalf("publish impact: %v", err)
			}
			if len(packets) != 1 || len(packets[0]) < fixture.sourceOffset+4 {
				t.Fatalf("missing combat event: %x", packets)
			}
			packet := packets[0]
			if packet[0] != byte(raknet.CombatEvent) || packet[1] != fixture.mask {
				t.Fatalf("unexpected build-103 combat shape: %x", packet)
			}
			sourceID := binary.LittleEndian.Uint32(packet[fixture.sourceOffset:])
			if sourceID != castSourceID {
				t.Fatalf("impact source=%d, want cast source=%d after deployment of hero=%d",
					sourceID, castSourceID, switchedHeroID)
			}
		})
	}
}
