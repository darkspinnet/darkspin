package gameplay

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func TestArenaLethalPassiveOwnsCastingLifetime(t *testing.T) {
	fixtures := []struct {
		name             string
		isLethal         bool
		isSwapped        bool
		isStaleTransport bool
		isReplaced       bool
		wantStack        uint32
	}{
		{name: "lethal", isLethal: true, wantStack: 1},
		{name: "nonlethal"},
		{name: "swapped", isLethal: true, isSwapped: true},
		{name: "transport", isLethal: true, isStaleTransport: true},
		{name: "replacement", isLethal: true, isReplaced: true},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			registry := newGameplaySessionRegistry(nil, time.Now, nil)
			member := gameplayPeerSession{}
			member.generation, member.transportGeneration, member.deployedObjectID = 1, 1, 1
			member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 1, UserID: 1, Team: 1}
			member.binding.Creatures[0] = game.GameplayCreature{Noun: 7, HitPoint: 100, PowerPoint: 50,
				PassiveAbility: util.HashID("SoulRavagerPassive"), PassiveMaximumStack: 5,
				PassiveStackPerKill: 1, PassiveKillDamageIncrease: 0.05}
			var err error
			member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, ManaPoints: 50, IsAvailable: true}}, 0)
			if err != nil {
				t.Fatalf("squad: %v", err)
			}
			source := member
			if fixture.isSwapped {
				member.deployedObjectID = 2
			}
			if fixture.isStaleTransport {
				member.transportGeneration++
			}
			if fixture.isReplaced {
				member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, ManaPoints: 50, IsAvailable: true}}, 0)
				if err != nil {
					t.Fatalf("replacement: %v", err)
				}
			}
			registry.sessions["source"] = member
			registry.arenaMatches[1] = &arenaMatchState{isCombatActive: true}
			targets := arenaAbilityTargets{runtime: campaignAbilityCommandRuntime{registry: registry}, source: source,
				hits: map[uint32]arenaAbilityHit{4: {sourceObjectID: 1, isLethal: fixture.isLethal}}}
			targets.finish()
			targets.finish()
			current := registry.sessions["source"]
			if current.passiveKillStack[0] != fixture.wantStack {
				t.Fatalf("passive stack=%d, want %d", current.passiveKillStack[0], fixture.wantStack)
			}
			if current.binding.Creatures[0].DamageProfile.DamageBuff != float32(fixture.wantStack)*0.05 {
				t.Fatal("kill bonus differs from owned stack count")
			}
			if fixture.wantStack > 0 && (current.soulRavagerOwnerObjectID != 1 || current.soulRavagerVisualStack != fixture.wantStack) {
				t.Fatal("accepted kill did not update native soul presentation")
			}
			if fixture.wantStack > 0 {
				packets, deathErr := current.applyArenaDamageHitPoints(0, 1000)
				if deathErr != nil {
					t.Fatalf("lethal transition: %v", deathErr)
				}
				if current.passiveKillStack[0] != 0 || current.binding.Creatures[0].DamageProfile.DamageBuff != 0 ||
					current.soulRavagerOwnerObjectID != 0 || len(packets) == 0 {
					t.Fatal("death retained the kill bonus or its native soul objects")
				}
			}
		})
	}
}

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
