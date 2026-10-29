package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonedifficulty "github.com/darkspinnet/darkspin/server/zone/difficulty"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const corruptorDeathJobNoun = "ScaldronBoss_Stage2DeathJob.Noun"

// Chunk446 belongs to the shared world, not its dead boss's action generation
// or the peer which delivered the last hit. The registry lock owns this run.
type corruptorDeathRun struct {
	runtime      campaignNPCActionRuntime
	world        *zone.Zone
	job          zonenpc.SpawnPlan
	faction      zonenpc.Faction
	startedAt    time.Time
	timestamp    uint64
	birthPackets [][]byte
	targets      []uint32
	modifiers    map[uint32]zoneeffect.Modifier
	companions   map[uint32]zonecompanion.Actor
	stopContext  func() bool
	isArmed      bool
	isComplete   bool
	isFinishing  bool
	isScanned    bool
	isAdmitted   bool
	isJobRetired bool
}

func (e *gameplayPeerSession) acceptCorruptorDeath(source zonenpc.Snapshot) (*corruptorDeathRun, error) {
	if !source.IsDefeated || !source.IsCorruptorStageTwo || source.Plan.OwnerObjectID != 0 ||
		!zonenpc.IsCorruptorNoun(source.Plan.NounName) {
		return nil, nil
	}
	profile, err := zonedifficulty.ProjectOwnedSummon(e.zone.DirectorDefinition(), corruptorDeathJobNoun)
	if err != nil {
		return nil, fmt.Errorf("deathJobProfile: %w", err)
	}
	graph := profile.AIGraph
	if !profile.IsClassKnown || !profile.AreClassAttributeBasesKnown || graph == nil ||
		!graph.IsResolved || graph.PassiveAbility == nil || *graph.PassiveAbility != "ScaldronBoss_Stage2Death" {
		return nil, errors.New("Corruptor death job installed class/passive unavailable")
	}
	objectID, err := e.reserveCampaignObjectID()
	if err != nil {
		return nil, fmt.Errorf("deathJobReserve: %w", err)
	}
	plan := zonenpc.SpawnPlan{ObjectID: objectID, OwnerObjectID: source.Plan.ObjectID,
		NounName: corruptorDeathJobNoun, Position: source.Plan.Position, NPCProfile: profile,
		IsIntroductionHidden: true, IsFixture: true, IsEncounterAuxiliary: true,
		IsOwnedDeathPersistent: true, IsLootSuppressed: true, IsExperienceSuppressed: true}
	packets, err := npcraknet.Spawn(plan)
	if err != nil {
		return nil, fmt.Errorf("deathJobMarshal: %w", err)
	}
	return &corruptorDeathRun{world: e.zone, job: plan, faction: source.Faction,
		startedAt: time.Now(), birthPackets: packets,
		modifiers: make(map[uint32]zoneeffect.Modifier), companions: make(map[uint32]zonecompanion.Actor)}, nil
}

func (e *corruptorDeathRun) key() string {
	return fmt.Sprintf("corruptor-death:%d", e.job.ObjectID)
}

func (e campaignNPCActionRuntime) startCorruptorDeath(run *corruptorDeathRun, timestamp uint64) error {
	if run == nil {
		return nil
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if run.isArmed || run.isComplete {
		return nil
	}
	run.runtime = e
	run.timestamp = timestamp
	if e.timer == nil || e.now == nil || e.modifierPool == nil || run.world.Context() == nil ||
		!run.world.IsActive() || e.registry.isClosed {
		run.isComplete = true
		return errors.New("Corruptor death world authority unavailable")
	}
	err := run.world.NPCs().Add([]zonenpc.SpawnPlan{run.job}, run.job.OwnerObjectID)
	if err != nil {
		run.isComplete = true
		return fmt.Errorf("deathJobAdd: %w", err)
	}
	run.isAdmitted = true
	run.isArmed = true
	run.stopContext = context.AfterFunc(run.world.Context(), run.cancel)
	// Every recipient's FIFO receives the exact hidden birth before the
	// world timeline can publish a modifier or a scripted kill from this job.
	run.queueLocked(run.birthPackets)
	err = run.scheduleLocked(run.startedAt.Add(4 * time.Second))
	if err != nil {
		retireErr := run.retireLocked()
		return fmt.Errorf("deathJobAdmission: %w", errors.Join(err, retireErr))
	}
	return nil
}

func (e *corruptorDeathRun) scheduleLocked(at time.Time) error {
	err := e.world.Timeline().Schedule(e.key(), max(time.Duration(0), at.Sub(e.runtime.now())), e.advance, e.runtime.timer.Schedule)
	if err != nil {
		return fmt.Errorf("deathTimeline: %w", err)
	}
	return nil
}

func (e *corruptorDeathRun) queueLocked(packets [][]byte) {
	if !e.world.IsActive() || e.runtime.registry.isClosed || len(packets) == 0 {
		return
	}
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone != e.world || member.isZoneTerminal() {
			continue
		}
		err := member.queueCampaignPresentation(packets)
		if err != nil {
			e.runtime.logger.Printf("Corruptor death publication job=%d peer=%s: %v", e.job.ObjectID, sessionKey, err)
		}
		e.runtime.registry.sessions[sessionKey] = member
	}
}

func (e *corruptorDeathRun) scanLocked(now time.Time) error {
	targets, err := e.world.NPCs().DeathCinematicTargets(e.job.ObjectID, e.job.Position)
	if err != nil {
		return fmt.Errorf("deathTargets: %w", err)
	}
	e.targets = nil
	e.isScanned = true
	for _, target := range targets {
		err = e.holdNPCLocked(target, now)
		if err != nil {
			return fmt.Errorf("deathHold: %w", err)
		}
		if target.Faction == e.faction {
			e.targets = append(e.targets, target.Plan.ObjectID)
		}
	}
	// Player pets with actual imported NPC classes also receive the broad
	// hold. They are not friendly to this nonplayer-aligned death job.
	if e.world.Companion() != nil {
		for _, target := range e.world.Companion().SecondAggroCinematicTargets(e.job.Position, now) {
			err = e.holdCompanionLocked(target, now)
			if err != nil {
				return fmt.Errorf("deathPetHold: %w", err)
			}
		}
	}
	return nil
}

func (e *corruptorDeathRun) advance() {
	e.runtime.registry.mutex.Lock()
	if e.isComplete {
		e.runtime.registry.mutex.Unlock()
		return
	}
	if !e.world.IsActive() || e.runtime.registry.isClosed {
		err := e.retireLocked()
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			e.runtime.logger.Printf("Corruptor death inactive retirement job=%d: %v", e.job.ObjectID, err)
		}
		return
	}
	job, isJobFound := e.world.NPCs().NPC(e.job.ObjectID)
	if !isJobFound || job.IsDefeated || !job.IsPublished {
		e.isComplete = true
		e.runtime.registry.mutex.Unlock()
		return
	}
	now := e.runtime.now()
	var err error
	var kill *corruptorDeathKill
	if e.isFinishing {
		kill, err = e.killLocked(job, now)
		e.isComplete = true
	} else {
		if len(e.targets) == 0 && !e.isScanned {
			err = e.scanLocked(now)
		}
		if err == nil && len(e.targets) == 0 {
			e.isFinishing = true
			err = e.scheduleLocked(now.Add(2 * time.Second))
		} else if err == nil {
			objectID := e.targets[0]
			e.targets = e.targets[1:]
			target, isFound := e.world.NPCs().NPC(objectID)
			if isFound && target.IsPublished && !target.IsBirthPending &&
				!target.IsDefeated && target.HitPoint > 0 && target.Faction == e.faction {
				nounType := target.Plan.NPCProfile.NounType
				if nounType == game.NounTypeCreature || nounType == game.NounTypeDestructibleOrnament {
					kill, err = e.killLocked(target, now)
				}
			}
			if err == nil && len(e.targets) == 0 {
				err = e.scanLocked(now)
			}
			if err == nil {
				delay := time.Duration((0.1 + e.world.NPCRandom().Float64()*0.1) * float64(time.Second))
				err = e.scheduleLocked(now.Add(delay))
			}
		}
	}
	if err != nil {
		err = errors.Join(err, e.retireLocked())
	}
	e.runtime.registry.mutex.Unlock()
	if kill != nil {
		kill.publish()
	}
	if err != nil {
		e.runtime.logger.Printf("Corruptor Stage2 death job=%d: %v", e.job.ObjectID, err)
	}
}
