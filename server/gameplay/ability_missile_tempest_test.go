package gameplay

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
)

type flakTestScheduler struct {
	jobs        []raknet.ScheduledPacketProducer
	failure     error
	isNilCancel bool
}

func (e *flakTestScheduler) cancel() {}

func (e *flakTestScheduler) schedule(jobs []raknet.ScheduledPacketProducer) (raknet.CancelSchedule, error) {
	if e.failure != nil {
		return nil, e.failure
	}
	e.jobs = append(e.jobs, jobs...)
	if e.isNilCancel {
		return nil, nil
	}
	return e.cancel, nil
}

func TestMissileFlakOwnership(t *testing.T) {
	registry := newGameplaySessionRegistry(nil, time.Now, nil)
	runtime := campaignAbilityCommandRuntime{registry: registry, modifierPool: newModifierPool(), effectPool: newAttachedEffectPool(), now: time.Now}
	member := gameplayPeerSession{}
	member.generation, member.transportGeneration, member.deployedObjectID = 2, 3, 1
	member.binding = game.GameplayBinding{Mode: game.ModeArena, GameID: 7}
	member.binding.Creatures[0] = game.GameplayCreature{Noun: 8, HitPoint: 100, MaximumHitPoint: 100}
	var err error
	member.squad, err = squad.New([squad.Size]squad.Character{{HitPoints: 100, IsAvailable: true}}, 0)
	if err != nil {
		t.Fatalf("squad: %v", err)
	}
	registry.sessions["source"] = member
	registry.sessions["peer"] = member
	foreign := member
	foreign.binding.GameID = 8
	registry.sessions["foreign"] = foreign
	scheduler := &flakTestScheduler{}
	packet := raknet.Packet{ScheduleGroup: scheduler.schedule}
	packets, err := runtime.applyMissileFlakLocked(packet, &member, "source", 2, 7, 20, 100)
	if err != nil || len(packets) != 0 {
		t.Fatalf("apply: packets=%d err=%v", len(packets), err)
	}
	run := member.missileFlakRun
	if run == nil || !run.isEffectAllocated || run.stackCount != 10 || member.binding.Creatures[0].TimingProfile.AttackSpeed != .5 || len(scheduler.jobs) != 1 {
		t.Fatal("rank7 cap or authored numerical contribution incorrect")
	}
	if len(registry.sessions["peer"].pendingPacketBatches) == 0 || len(registry.sessions["foreign"].pendingPacketBatches) != 0 {
		t.Fatal("buff publication crossed Arena realm")
	}
	attribute, err := run.attributes(member)
	if err != nil || len(attribute) != 11 || attribute[5] != 23 || math.Float32frombits(binary.LittleEndian.Uint32(attribute[6:10])) != .5 {
		t.Fatalf("attribute23: %x err=%v", attribute, err)
	}
	for _, field := range []string{"game", "peer", "transport", "object", "slot", "noun", "squad"} {
		changed := member
		switch field {
		case "game":
			changed.binding.GameID++
		case "peer":
			changed.generation++
		case "transport":
			changed.transportGeneration++
		case "object":
			changed.deployedObjectID++
		case "slot":
			changed.deployedCreatureIndex++
		case "noun":
			changed.binding.Creatures[0].Noun++
		case "squad":
			changed.squad = &squad.Session{}
		}
		if run.matches(changed) {
			t.Fatalf("%s inherited buff owner", field)
		}
	}
	for _, isNilCancel := range []bool{false, true} {
		scheduler.isNilCancel = isNilCancel
		if !isNilCancel {
			scheduler.failure = errors.New("schedule failed")
		} else {
			scheduler.failure = nil
		}
		packets, err = runtime.applyMissileFlakLocked(packet, &member, "source", 2, 8, 9, 200)
		if err == nil || len(packets) != 0 || member.missileFlakRun != run || member.binding.Creatures[0].TimingProfile.AttackSpeed != .5 || run.isReleased || !run.isEffectAllocated {
			t.Fatal("failed replacement changed accepted buff")
		}
	}
	scheduler.failure, scheduler.isNilCancel = nil, false
	packets, err = runtime.applyMissileFlakLocked(packet, &member, "source", 2, 8, 9, 300)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(packets) != 0 || member.missileFlakRun.stackCount != 4 || !run.isReleased || run.isEffectAllocated || member.binding.Creatures[0].TimingProfile.AttackSpeed != .48 {
		t.Fatal("rank8 replacement did not restore/apply exact contribution")
	}
	registry.sessions["source"] = member
	packets, err = scheduler.jobs[0].Produce()
	if err != nil || len(packets) != 0 || registry.sessions["source"].missileFlakRun != member.missileFlakRun {
		t.Fatal("stale expiry changed replacement")
	}
	packets, err = runtime.applyMissileFlakLocked(packet, &member, "source", 2, 8, 0, 400)
	if err != nil || len(packets) != 0 || member.missileFlakRun != nil || member.binding.Creatures[0].TimingProfile.AttackSpeed != 0 {
		t.Fatal("zero-destruction cast did not remove previous buff")
	}
	packets, err = runtime.applyMissileFlakLocked(packet, &member, "source", 2, 7, 2, 500)
	if err != nil || len(packets) != 0 {
		t.Fatalf("expiryApply: %v", err)
	}
	run = member.missileFlakRun
	registry.sessions["source"] = member
	packets, err = scheduler.jobs[len(scheduler.jobs)-1].Produce()
	member = registry.sessions["source"]
	if err != nil || len(packets) != 0 || member.missileFlakRun != nil || !run.isReleased || run.isEffectAllocated || member.binding.Creatures[0].TimingProfile.AttackSpeed != 0 {
		t.Fatal("expiry did not release owner and restore native/server attack baseline")
	}
	for index := 0; index < 16; index++ {
		slot, isAllocated := runtime.effectPool.Allocate(1)
		if !isAllocated {
			t.Fatalf("attachment leak after expiry at%d slot%d", index, slot)
		}
	}
}
