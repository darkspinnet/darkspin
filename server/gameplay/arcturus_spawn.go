package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

type campaignArcturusState struct {
	center     game.Vec3
	nextLaunch uint64
	launch     *campaignArcturusLaunch
	turret     *campaignArcturusTurret
}

type campaignArcturusSpawnStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	state      *campaignArcturusState
}

func (e campaignNPCActionRuntime) startArcturusControllers(packet raknet.Packet,
	sessionKey string, generation uint64, plans []zonenpc.SpawnPlan, timestamp uint64,
) error {
	for _, plan := range plans {
		if zonenpc.ArcturusRank(plan.NounName) == 0 || plan.OwnerObjectID != 0 {
			continue
		}
		if packet.ScheduleFunc == nil {
			return errors.New("arcturus scheduler unavailable")
		}
		e.registry.mutex.Lock()
		current, isFound := e.registry.sessions[sessionKey]
		if !isFound || current.generation != generation || current.isZoneTerminal() {
			e.registry.mutex.Unlock()
			continue
		}
		if current.campaignArcturusStates == nil {
			current.campaignArcturusStates = make(map[uint32]*campaignArcturusState)
		}
		if current.campaignArcturusStates[plan.ObjectID] != nil {
			e.registry.mutex.Unlock()
			continue
		}
		state := &campaignArcturusState{center: plan.Position, nextLaunch: timestamp + 30000}
		current.campaignArcturusStates[plan.ObjectID] = state
		e.registry.sessions[sessionKey] = current
		e.registry.mutex.Unlock()
		step := campaignArcturusSpawnStep{runtime: e, packet: packet.Autonomous(),
			sessionKey: sessionKey, generation: generation, objectID: plan.ObjectID,
			timestamp: timestamp + 1000, state: state}
		err := step.packet.ScheduleFunc(time.Second, step.produce)
		if err != nil {
			e.registry.mutex.Lock()
			current = e.registry.sessions[sessionKey]
			if current.generation == generation && current.campaignArcturusStates[plan.ObjectID] == state {
				delete(current.campaignArcturusStates, plan.ObjectID)
			}
			e.registry.mutex.Unlock()
			return fmt.Errorf("arcturusSpawnSchedule: %w", err)
		}
	}
	return nil
}

func (e campaignArcturusSpawnStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || current.generation != e.generation || current.isZoneTerminal() ||
		current.campaignArcturusStates[e.objectID] != e.state {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	boss, isBossFound := current.zone.NPCs().NPC(e.objectID)
	if !isBossFound || boss.IsDefeated || boss.HitPoint <= 0 {
		// An active launch still owns its reveal/landing cleanup.
		if e.state.launch == nil {
			delete(current.campaignArcturusStates, e.objectID)
		}
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	plans := make([]zonenpc.SpawnPlan, 0, 1)
	packets := make([][]byte, 0)
	var spawnErr error
	if zonenpc.ArcturusRank(boss.Plan.NounName) > 1 && e.state.turret == nil && boss.TargetObjectID != 0 {
		packets, spawnErr = e.startTurret(&current, boss)
	}
	if boss.TargetObjectID != 0 && len(current.zone.LiveNPCTargets()) > 0 && current.zone.NPCRandom().Float64() < 0.2 {
		var scarabPackets [][]byte
		var scarabErr error
		plans, scarabPackets, scarabErr = current.spawnArcturusScarab(boss, e.state.center)
		packets = append(packets, scarabPackets...)
		spawnErr = errors.Join(spawnErr, scarabErr)
	}
	e.runtime.registry.sessions[e.sessionKey] = current
	e.runtime.registry.mutex.Unlock()
	// A missing profile or a rejected placement must not permanently stop the passive.
	if spawnErr != nil {
		e.runtime.logger.Printf("Arcturus passive spawn omitted source=%d: %v", e.objectID, spawnErr)
	}
	if len(plans) > 0 {
		actionPackets, err := e.runtime.scheduleFirstActions(e.packet, e.sessionKey, e.generation, plans, e.timestamp)
		if err != nil {
			e.runtime.logger.Printf("Arcturus scarab actions omitted source=%d: %v", e.objectID, err)
		} else {
			packets = append(packets, actionPackets...)
		}
	}
	e.timestamp += 1000
	err := e.packet.ScheduleFunc(time.Second, e.produce)
	if err != nil {
		return packets, fmt.Errorf("arcturusSpawnTick: %w", err)
	}
	return packets, nil
}

func (e *gameplayPeerSession) spawnArcturusScarab(boss zonenpc.Snapshot, center game.Vec3) ([]zonenpc.SpawnPlan, [][]byte, error) {
	rank := zonenpc.ArcturusRank(boss.Plan.NounName)
	classNames := [...]string{"citadelbasicsuicide.noun", "citadelbasicsuicide_2.noun", "citadelbasicsuicide_3.noun"}
	if rank < 1 || rank > len(classNames) {
		return nil, nil, errors.New("arcturus rank unavailable")
	}
	className := classNames[rank-1]
	profile, isProfileFound := e.zone.DirectorDefinition().NPCProfilesByNoun[className]
	action, isActionFound := zonenpc.ActionProfileForNoun(className)
	if !isProfileFound || !profile.IsKnown || !isActionFound {
		return nil, nil, errors.New("arcturus scarab class unavailable")
	}
	angle := e.zone.NPCRandom().Float64() * 2 * math.Pi
	distance := 4 + e.zone.NPCRandom().Float64()*8
	position := center
	position.X += float32(math.Cos(angle) * distance)
	position.Y += float32(math.Sin(angle) * distance)
	projected, isProjected, err := zonenavigation.ProjectPosition(e.zone.Navigation(), position, profile.FootprintRadius)
	if err != nil {
		return nil, nil, fmt.Errorf("scarabProject: %w", err)
	}
	if e.zone.Navigation() != nil && !isProjected {
		return nil, nil, nil
	}
	if isProjected {
		position = projected
	}
	objectID, err := e.reserveCampaignObjectID()
	if err != nil {
		return nil, nil, fmt.Errorf("scarabReserve: %w", err)
	}
	action.FirstAggroAbilityName = "FirstAggro_CitadelBossMinion"
	action.FirstAggroAnimationName = "ctd_boss_tc_minion_drop_in"
	action.FirstAggroDelay = 900 * time.Millisecond
	action.IsFirstAggroDurationKnown = true
	plans := []zonenpc.SpawnPlan{{ObjectID: objectID, OwnerObjectID: boss.Plan.ObjectID,
		NounName: fmt.Sprintf("CitadelBossMinon_v%d.Noun", rank), Position: position,
		LocusID: boss.Plan.LocusID, MarkerSetName: boss.Plan.MarkerSetName,
		IsRewardSuppressed: true, NPCProfile: profile, ActionProfile: action, IsActionKnown: true}}
	packets, err := npcraknet.TargetedSpawn(plans[0], boss.TargetObjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("scarabMarshal: %w", err)
	}
	err = e.zone.NPCs().Add(plans, boss.TargetObjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("scarabAdd: %w", err)
	}
	err = e.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{Plans: plans, TargetObjectID: boss.TargetObjectID}, e.binding.UserID, e.generation)
	if err != nil {
		rollbackErr := e.zone.NPCs().RollbackAdd(plans)
		return nil, nil, fmt.Errorf("scarabPublish: %w", errors.Join(err, rollbackErr))
	}
	return plans, packets, nil
}
