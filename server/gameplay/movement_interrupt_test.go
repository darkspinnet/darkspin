package gameplay

import (
	"net"
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
)

func TestArenaMovementStopsOwnedChannelWithoutRefund(t *testing.T) {
	now := time.Unix(100, 0)
	registry := newGameplaySessionRegistry(nil, func() time.Time { return now }, nil)
	member := gameplayPeerSession{}
	member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 11, UserID: 1}
	member.binding.Creatures[0] = game.GameplayCreature{Noun: 7, HitPoint: 100, PowerPoint: 50}
	member.generation, member.transportGeneration = 1, 1
	member.deployedObjectID = 1
	member.stage.EnterDungeon()
	member.isArenaCombatActive = true
	var err error
	member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 40, ManaPoints: 30, IsAvailable: true}}, 0)
	if err != nil {
		t.Fatalf("squad: %v", err)
	}
	reservation, isReserved := member.abilityReleaseSession().Reserve(now, 4*time.Second)
	if !isReserved {
		t.Fatalf("reserve release: %+v", reservation)
	}
	cancelCount := 0
	run := &heroHealingTicksRun{isChanneled: true, isCommitted: true, sourceObjectID: 1,
		cancel: func() { cancelCount++ }}
	member.heroHealingTicks = run
	address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	registry.sessions[address.String()] = member
	registry.arenaMatches[11] = &arenaMatchState{isCombatActive: true}
	runtime := gameplayMovementRuntime{campaign: campaignMovementCommandRuntime{registry: registry,
		action: campaignActionAuthority{registry: registry}, now: func() time.Time { return now }}}
	command := raknet.ActionCommandData{Movement: &raknet.ActionMovementData{GoalPosition: raknet.Vector3{X: 10, Y: 20}, GoalFlags: 0x20}}
	command.Common.ObjectID = 9
	command.Common.Position = raknet.Vector3{X: 10, Y: 20}
	packets, err := runtime.handleArena(raknet.Packet{Address: address, SourceTime: 1000}, command, member)
	if err == nil || len(packets) != 0 || cancelCount != 0 {
		t.Fatal("foreign movement interrupted the active channel")
	}
	command.Common.ObjectID = 1
	packets, err = runtime.handleArena(raknet.Packet{Address: address, SourceTime: 1000}, command, member)
	if err != nil {
		t.Fatalf("owned stop: %v", err)
	}
	current := registry.sessions[address.String()]
	if cancelCount != 1 || !run.isStopped || current.heroHealingTicks != nil ||
		!current.abilityReleaseSession().IsReady(now) || len(packets) == 0 {
		t.Fatal("owned stop did not retire the channel and release its action lock")
	}
	if current.deployedManaPoint() != 30 || current.deployedHitPoint() != 40 {
		t.Fatal("interruption changed paid resources or delivered another healing tick")
	}
}
