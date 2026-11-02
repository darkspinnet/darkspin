package gameplay

import (
	"bytes"
	"testing"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

func TestArenaChannelDeliveryAndRetirement(t *testing.T) {
	source := gameplayPeerSession{}
	source.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 11, UserID: 1, Team: 1}
	source.generation = 1
	source.isArenaCombatActive = true
	enemy := source
	enemy.binding.UserID = 2
	enemy.binding.Team = 2
	otherMatch := source
	otherMatch.binding.GameID = 22
	otherMatch.binding.UserID = 3
	campaign := source
	campaign.binding.Mode = game.ModeChain
	campaign.binding.UserID = 4
	registry := &gameplaySessionRegistry{
		sessions:     map[string]gameplayPeerSession{"source": source, "enemy": enemy, "otherMatch": otherMatch, "campaign": campaign},
		arenaMatches: map[uint32]*arenaMatchState{11: {isCombatActive: true}},
	}
	pool := newAttachedEffectPool()
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	effects, err := registry.allocateAbilityChannelEffectsLocked(source, pool, []uint32{99})
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	creation, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{ObjectID: 99, Slot: 1, Asset: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	err = registry.publishChannelEffectsLocked(effects, [][]byte{creation})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	loadingSource := registry.sessions["source"]
	loadingPackets, loadingBatchID := loadingSource.pendingPackets()
	if len(loadingPackets) != 0 || loadingBatchID != 0 {
		t.Fatal("active arena attachment overtook its recipient actor baseline")
	}
	effects.Retire()
	registry.commitChannelRemovalsLocked()
	pool.ReleaseObject(99)
	slot, isAllocated := pool.Allocate(99)
	if !isAllocated || slot == 0 {
		t.Fatalf("uncommitted removal reused held slot: slot=%d allocated=%t", slot, isAllocated)
	}
	isReleased := pool.Release(99, slot)
	if !isReleased {
		t.Fatal("release probe slot")
	}
	for _, key := range []string{"source", "enemy"} {
		member := registry.sessions[key]
		packets, batchID := member.pendingPackets()
		if len(packets) != 2 || !bytes.Equal(packets[0], creation) {
			t.Fatalf("%s missing ordered create/removal: %x", key, packets)
		}
		member.commitPendingPackets(batchID)
		registry.sessions[key] = member
		registry.commitChannelRemovalsLocked()
		if key == "source" && len(registry.channelRemovals) == 0 {
			t.Fatal("released attachment before opposing recipient committed removal")
		}
	}
	for _, key := range []string{"otherMatch", "campaign"} {
		member := registry.sessions[key]
		packets, batchID := member.pendingPackets()
		if len(packets) != 0 || batchID != 0 {
			t.Fatalf("channel leaked to %s: %x", key, packets)
		}
	}
	slot, isAllocated = pool.Allocate(99)
	if !isAllocated || slot != 0 {
		t.Fatalf("committed removal did not release slot zero: slot=%d allocated=%t", slot, isAllocated)
	}
}
