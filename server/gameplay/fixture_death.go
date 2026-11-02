package gameplay

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zonedeath "github.com/darkspinnet/darkspin/server/zone/death"
	deathraknet "github.com/darkspinnet/darkspin/server/zone/death/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Build 103 StalagmiteDeath emits the dedicated particle, waits 0.2 seconds,
// then deletes the object; these untargetable crystals can still take area hits.
func isCryosStalagmite(nounName string) bool {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_prefab_cryos3_stalagmite_small.noun",
		"dest_prefab_cryos3_stalagmite_large.noun",
		"cryos_stalagmite_small_destructible.noun",
		"dest_cryos2_lvl2_stalagmite_small_destructible.noun",
		"dest_cryos_stalagmite_small_destructible.noun",
		"dest_prefab_cryos_stalagmite_small.noun",
		"dest_prefab_cryos_stalagmite_large.noun":
		return true
	default:
		return false
	}
}

type campaignFixtureDeath struct {
	run        *deathraknet.Run
	targetID   uint32
	acceptedAt time.Time
	world      *zone.Zone
}

// Fixture publications deliver exact death packets for both blasts and burn
// ticks. Record state and objectives without creating a second wire owner;
// subsequent corpse deadlines retain their normal world publication.
func (e campaignFixtureBlastStep) recordOwnedDeath(deaths []zonenpc.DeathEvent) error {
	registry := e.runtime.registry
	registry.mutex.RLock()
	member, isFound := registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation &&
		member.zone != nil && member.zone.NPCs() == e.npc
	world := member.zone
	registry.mutex.RUnlock()
	if !isCurrent {
		return errors.New("fixture death world replaced")
	}
	err := world.RecordNPCDeathObjectives(world.Context(), deaths)
	if err != nil {
		return fmt.Errorf("fixtureDeathObjectives: %w", err)
	}
	return nil
}

// Initial feedback is durable before corpse deadlines are admitted. These are
// world deadlines, so a lost carrier cannot erase an accepted lethal hit.
func (e *campaignFixtureDeath) schedule(blast campaignFixtureBlastStep) (*fixtureDeathAdmission, error) {
	runtime := blast.runtime.npc.death
	if runtime.timer == nil {
		runtime.timer = blast.runtime.registry.timer
	}
	registry := blast.runtime.registry
	registry.mutex.RLock()
	member, isFound := registry.sessions[blast.sessionKey]
	isCurrent := isFound && member.generation == blast.generation && member.zone != nil && member.zone.NPCs() == blast.npc
	world := member.zone
	registry.mutex.RUnlock()
	if !isCurrent || runtime.timer == nil || world.Death() == nil {
		return nil, errors.New("fixture death world timer unavailable")
	}
	death := world.Death()
	e.world = world
	isAdmitted := false
	defer e.releaseUnadmitted(&isAdmitted)
	err := death.Add(e.targetID, e.run)
	if err != nil {
		return nil, fmt.Errorf("fixtureDeathAdd: %w", err)
	}
	deadlines := e.run.Deadlines()
	admission := &fixtureDeathAdmission{
		steps: make([]campaignDeathDeadline, len(deadlines)), readyFlags: make([]bool, len(deadlines)),
	}
	for index, deadline := range deadlines {
		admission.steps[index] = campaignDeathDeadline{zone: world, objectID: e.targetID, run: e.run,
			deadline: deadline, isFinal: index == len(deadlines)-1, logger: runtime.logger}
	}
	for index, deadline := range deadlines {
		step := fixtureDeathDeadline{admission: admission, index: index}
		delay := max(time.Duration(0), e.acceptedAt.Add(deadline).Sub(blast.runtime.npc.now()))
		cancel, scheduleErr := runtime.timer.Schedule(delay, step.execute)
		if scheduleErr != nil || cancel == nil {
			admission.fail()
			if cancel != nil {
				cancel()
			}
			death.Remove(e.targetID, e.run)
			if scheduleErr != nil {
				return nil, fmt.Errorf("fixtureDeathTimer[%d]: %w", index, scheduleErr)
			}
			return nil, errors.New("fixture death cancellation unavailable")
		}
		err = death.AddCancel(e.targetID, e.run, zonedeath.Cancel(cancel))
		if err != nil {
			admission.fail()
			cancel()
			death.Remove(e.targetID, e.run)
			return nil, fmt.Errorf("fixtureDeathCancel[%d]: %w", index, err)
		}
	}
	isAdmitted = true
	return admission, nil
}

func (e *campaignFixtureDeath) releaseUnadmitted(isAdmitted *bool) {
	if !*isAdmitted {
		e.stop()
	}
}

func (e *campaignFixtureDeath) stop() {
	if e.world != nil && e.world.Death() != nil {
		e.world.Death().Remove(e.targetID, e.run)
	}
	e.run.Stop()
}

func activateFixtureDeaths(admissions *[]*fixtureDeathAdmission) {
	for _, admission := range *admissions {
		admission.admit()
	}
}

// Zero-delay callbacks may execute before Schedule returns. Keep them behind
// admission, and drain in authored order even if overdue timers arrive reversed.
type fixtureDeathAdmission struct {
	mutex      sync.Mutex
	steps      []campaignDeathDeadline
	readyFlags []bool
	nextIndex  int
	isAdmitted bool
	isFailed   bool
	isDraining bool
}

type fixtureDeathDeadline struct {
	admission *fixtureDeathAdmission
	index     int
}

func (e fixtureDeathDeadline) execute() {
	e.admission.mutex.Lock()
	e.admission.readyFlags[e.index] = true
	e.admission.mutex.Unlock()
	e.admission.drain()
}

func (e *fixtureDeathAdmission) admit() {
	e.mutex.Lock()
	e.isAdmitted = true
	e.mutex.Unlock()
	e.drain()
}

func (e *fixtureDeathAdmission) fail() {
	e.mutex.Lock()
	e.isFailed = true
	e.mutex.Unlock()
}

func (e *fixtureDeathAdmission) drain() {
	e.mutex.Lock()
	if !e.isAdmitted || e.isFailed || e.isDraining {
		e.mutex.Unlock()
		return
	}
	e.isDraining = true
	for !e.isFailed && e.nextIndex < len(e.steps) && e.readyFlags[e.nextIndex] {
		step := e.steps[e.nextIndex]
		e.nextIndex++
		e.mutex.Unlock()
		step.execute()
		e.mutex.Lock()
	}
	e.isDraining = false
	e.mutex.Unlock()
}
