//go:build scenario

package gameplay

import (
	"bytes"
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

func TestScenarioCleanupRetirementFanout(t *testing.T) {
	for _, name := range []string{"alreadyFanout", "ownerOnly", "mixed", "oldIdentical"} {
		t.Run(name, func(t *testing.T) {
			registry := newGameplaySessionRegistry(nil, time.Now, nil)
			for index, key := range []string{"owner", "first", "second", "foreign", "campaign", "rejoin"} {
				member := gameplayPeerSession{}
				member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7, UserID: uint64(index + 1)}
				if key == "foreign" {
					member.binding.GameID = 8
				}
				if key == "campaign" {
					member.binding.Mode = game.ModeChain
				}
				member.isRejoinPending = key == "rejoin"
				registry.sessions[key] = member
			}
			packets := [][]byte{{0x9c, 1}, {0x9c, 2}, {0x9c, 3}}
			if name == "oldIdentical" {
				first := registry.sessions["first"]
				first.queuePackets(packets)
				registry.sessions["first"] = first
			}
			previousBatchIDs := registry.scenarioCleanupBatchIDsLocked(7)
			owner := registry.sessions["owner"]
			owner.queuePackets(packets)
			registry.sessions["owner"] = owner
			for _, key := range []string{"first", "second"} {
				member := registry.sessions[key]
				if name == "alreadyFanout" {
					member.queuePackets(packets)
				}
				if name == "mixed" {
					index := 0
					if key == "second" {
						index = 1
					}
					member.queuePackets([][]byte{packets[index]})
				}
				registry.sessions[key] = member
			}
			registry.queueScenarioCleanupLocked(7, 1, packets, previousBatchIDs)
			for _, key := range []string{"owner", "first", "second"} {
				member := registry.sessions[key]
				for _, packet := range packets {
					count := scenarioCleanupPacketCount(member, packet)
					expected := 1
					if name == "oldIdentical" && key == "first" {
						expected = 2
					}
					if count != expected {
						t.Fatalf("%s packet %x count=%d want=%d", key, packet, count, expected)
					}
				}
			}
			for _, key := range []string{"foreign", "campaign", "rejoin"} {
				if len(registry.sessions[key].pendingPacketBatches) != 0 {
					t.Fatalf("cleanup leaked to %s", key)
				}
			}
		})
	}
}

func scenarioCleanupPacketCount(member gameplayPeerSession, packet []byte) int {
	count := 0
	for _, batch := range member.pendingPacketBatches {
		for _, queued := range batch.packets {
			if bytes.Equal(queued, packet) {
				count++
			}
		}
	}
	return count
}
