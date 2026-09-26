package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const (
	tutorialHordeFirstWaveDelay = 2 * time.Second
	tutorialHordeNextWaveDelay  = 1500 * time.Millisecond
	tutorialHordeResultDelay    = 2 * time.Second
	tutorialHordeRadius         = float32(30)
	tutorialHordeMarkerSetName  = "tutorial-horde"
)

const (
	tutorialHordeIncomingClientEventID = uint32(0x1d42121d)
	tutorialHordeDefeatedClientEventID = uint32(0x8047eaf4)
)

var tutorialHordeDirectorPosition = game.Vec3{
	X: -350.04453, Y: -223.36415, Z: 10.08803,
}

type tutorialHordeSession struct {
	waves           [][]zonenpc.SpawnPlan
	activeObjectIDs map[uint32]struct{}
	nextWave        int
	isTriggered     bool
	isWaveScheduled bool
	isComplete      bool
}

func planTutorialHorde(
	director game.CampaignDirector, firstObjectID uint32, programs Programs,
) (*tutorialHordeSession, uint32, error) {
	nouns := [4][4]string{
		{"TutorialBasicDiseased.Noun", "TutorialBasicRanged.Noun", "TutorialBasicPoison.Noun", "TutorialBasicRanged.Noun"},
		{"TutorialBasicPoison.Noun", "TutorialSloth.Noun", "TutorialBasicDiseased.Noun", "TutorialBasicRanged.Noun"},
		{"TutorialBasicRanged.Noun", "TutorialBasicPoison.Noun", "TutorialSloth.Noun", "TutorialBasicDiseased.Noun"},
		{"TutorialSloth.Noun", "TutorialSpecialOne.Noun", "TutorialBasicRanged.Noun", "TutorialBasicPoison.Noun"},
	}
	positions := [4]game.Vec3{
		{X: -355.26929, Y: -207.71077, Z: 10.08800},
		{X: -345.89496, Y: -239.54680, Z: 10.16637},
		{X: -334.68289, Y: -217.83920, Z: 10.08800},
		{X: -365.40617, Y: -228.88910, Z: 10.08800},
	}
	waves := make([][]zonenpc.SpawnPlan, 0, len(nouns))
	nextObjectID := firstObjectID
	for waveIndex, waveNouns := range nouns {
		plans := make([]zonenpc.SpawnPlan, 0, len(waveNouns))
		for actorIndex, nounName := range waveNouns {
			nounKey := strings.ToLower(nounName)
			profile, isFound := director.NPCProfilesByNoun[nounKey]
			if !isFound || !profile.IsKnown || profile.HitPoint <= 0 {
				return nil, firstObjectID, fmt.Errorf(
					"profile[%s]: missing", nounName,
				)
			}
			plan := zonenpc.SpawnPlan{
				ObjectID: nextObjectID, NounName: nounName,
				Position:      positions[actorIndex],
				LocusID:       uint32(waveIndex*len(waveNouns) + actorIndex + 1),
				MarkerSetName: tutorialHordeMarkerSetName,
				Kind:          sim.DirectorLocusHorde,
				Introduction:  zonenpc.SpawnIntroductionFloorWarp,
				NPCProfile:    profile,
			}
			if waveIndex == 1 && actorIndex == 1 {
				plan.Experience = 60
			}
			if waveIndex == 2 && actorIndex == 1 {
				plan.Experience = 54
			}
			plans = append(plans, plan)
			nextObjectID++
		}
		err := attachTutorialActorActionProfiles(plans, programs)
		if err != nil {
			return nil, firstObjectID, fmt.Errorf(
				"action[%d]: %w", waveIndex+1, err,
			)
		}
		waves = append(waves, plans)
	}
	return &tutorialHordeSession{
		waves: waves, activeObjectIDs: make(map[uint32]struct{}),
	}, nextObjectID, nil
}

func (e *tutorialHordeSession) reserveStart(previous game.Vec3, current game.Vec3) bool {
	if e == nil || e.isTriggered || e.isComplete ||
		!zonegeometry.SegmentIntersectsSphere(
			previous, current, tutorialHordeDirectorPosition, tutorialHordeRadius,
		) {
		return false
	}
	e.isTriggered = true
	e.isWaveScheduled = true
	e.nextWave = 1
	return true
}

func (e *tutorialHordeSession) rollbackStart() {
	if e == nil || len(e.activeObjectIDs) != 0 || e.nextWave != 1 {
		return
	}
	e.isTriggered = false
	e.isWaveScheduled = false
	e.nextWave = 0
}

func (e *tutorialHordeSession) scheduledWave() ([]zonenpc.SpawnPlan, int, bool) {
	if e == nil || !e.isWaveScheduled || e.isComplete || e.nextWave < 1 ||
		e.nextWave > len(e.waves) || len(e.activeObjectIDs) != 0 {
		return nil, 0, false
	}
	return e.waves[e.nextWave-1], e.nextWave, true
}

func (e *tutorialHordeSession) activate(plans []zonenpc.SpawnPlan) {
	e.activeObjectIDs = make(map[uint32]struct{}, len(plans))
	for _, plan := range plans {
		e.activeObjectIDs[plan.ObjectID] = struct{}{}
	}
	e.isWaveScheduled = false
}

func (e *tutorialHordeSession) observeDefeat(objectID uint32) (bool, bool) {
	if e == nil || objectID == 0 || e.isComplete {
		return false, false
	}
	if _, isFound := e.activeObjectIDs[objectID]; !isFound {
		return false, false
	}
	delete(e.activeObjectIDs, objectID)
	if len(e.activeObjectIDs) != 0 {
		return false, false
	}
	if e.nextWave >= len(e.waves) {
		e.isComplete = true
		return false, true
	}
	e.nextWave++
	e.isWaveScheduled = true
	return true, false
}

type tutorialHordeWaveStep struct {
	registry   *gameplaySessionRegistry
	npc        campaignNPCActionRuntime
	logger     *log.Logger
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	delay      time.Duration
}

func (s tutorialHordeWaveStep) produce() ([][]byte, error) {
	s.registry.mutex.Lock()
	peerSession, isFound := s.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.binding.Mode == game.ModeTutorial &&
		!peerSession.isZoneTerminal() && peerSession.tutorialHorde != nil
	if !isCurrent {
		s.registry.mutex.Unlock()
		return nil, nil
	}
	plans, wave, isReady := peerSession.tutorialHorde.scheduledWave()
	if !isReady {
		s.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := npcraknet.TargetedSpawns(plans, peerSession.deployedObjectID)
	if err != nil {
		s.registry.mutex.Unlock()
		return nil, fmt.Errorf("tutorialHordeMarshal: %w", err)
	}
	err = peerSession.zone.NPCs().Add(plans, peerSession.deployedObjectID)
	if err != nil {
		s.registry.mutex.Unlock()
		return nil, fmt.Errorf("tutorialHordeAdd: %w", err)
	}
	peerSession.tutorialHorde.activate(plans)
	s.registry.sessions[s.sessionKey] = peerSession
	s.registry.mutex.Unlock()
	err = peerSession.zone.PublishNPCSpawn(
		zoneprojection.NPCSpawn{
			Plans: plans, TargetObjectID: peerSession.deployedObjectID,
		},
		peerSession.binding.UserID, peerSession.generation,
	)
	if err != nil {
		return nil, fmt.Errorf("tutorialHordePublish: %w", err)
	}
	actionPackets, err := s.npc.scheduleFirstActions(
		s.packet, s.sessionKey, s.generation, plans,
		s.timestamp+uint64(s.delay/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("tutorialHordeAction: %w", err)
	}
	if s.logger != nil {
		s.logger.Printf(
			"RakNet tutorial horde wave=%d spawned enemies=%d",
			wave, len(plans),
		)
	}
	return append(packets, actionPackets...), nil
}

func scheduleTutorialHordeWave(
	packet raknet.Packet, step tutorialHordeWaveStep,
) error {
	producer := raknet.ScheduledPacketProducer{
		Delay: step.delay, Produce: step.produce,
	}
	producers := step.registry.producerGuard.scheduledProducers(
		step.sessionKey, []raknet.ScheduledPacketProducer{producer},
	)
	cancel, err := packet.Autonomous().ScheduleProducers(producers)
	if err != nil {
		return fmt.Errorf("tutorialHordeSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("tutorial horde cancellation unavailable")
	}
	return nil
}

func marshalTutorialHordeAlert(clientEventID uint32, activeObjectID uint32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		ObjectID:      activeObjectID,
		Position:      raknet.Vector3(tutorialHordeDirectorPosition),
		ClientEventID: clientEventID,
	})
	if err != nil {
		return nil, fmt.Errorf("tutorialHordeAlert: %w", err)
	}
	return packet, nil
}

type tutorialHordeCompletionStep struct {
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
}

func (s tutorialHordeCompletionStep) produce() ([][]byte, error) {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.binding.Mode == game.ModeTutorial &&
		peerSession.tutorialHorde != nil && peerSession.tutorialHorde.isComplete &&
		!peerSession.isZoneTerminal()
	if !isCurrent {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	completionPacket, bossObjectID, err :=
		peerSession.applyDeveloperTutorialVictoryCommand()
	if err == nil {
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("tutorialHordeComplete: %w", err)
	}
	if s.runtime.logger != nil {
		s.runtime.logger.Printf(
			"RakNet tutorial horde completed boss=%d for %s; awaiting Return to Ship",
			bossObjectID, s.sessionKey,
		)
	}
	return [][]byte{completionPacket}, nil
}

func (r campaignDamageRuntime) scheduleTutorialHordeCompletion(
	packet raknet.Packet, sessionKey string, generation uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.tutorialHorde != nil && peerSession.tutorialHorde.isComplete
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	if r.gameplayJoin == nil {
		return nil, errors.New("tutorial horde progression unavailable")
	}
	err := r.gameplayJoin.MarkTutorialComplete(
		context.Background(), int64(peerSession.binding.UserID),
		peerSession.binding.GameID,
	)
	if err != nil {
		return nil, fmt.Errorf("tutorialHordeMark: %w", err)
	}
	alertPacket, err := marshalTutorialHordeAlert(
		tutorialHordeDefeatedClientEventID, peerSession.deployedObjectID,
	)
	if err != nil {
		rollbackErr := r.gameplayJoin.RollbackTutorialComplete(
			context.Background(), int64(peerSession.binding.UserID),
			peerSession.binding.GameID,
		)
		return nil, fmt.Errorf(
			"tutorialHordeAlert: %w", errors.Join(err, rollbackErr),
		)
	}
	step := tutorialHordeCompletionStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
	}
	producer := raknet.ScheduledPacketProducer{
		Delay: tutorialHordeResultDelay, Produce: step.produce,
	}
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{producer},
	)
	cancel, scheduleErr := packet.Autonomous().ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("tutorial horde completion cancellation unavailable")
	}
	if scheduleErr == nil {
		return [][]byte{alertPacket}, nil
	}
	completionPackets, completionErr := step.produce()
	if completionErr != nil {
		rollbackErr := r.gameplayJoin.RollbackTutorialComplete(
			context.Background(), int64(peerSession.binding.UserID),
			peerSession.binding.GameID,
		)
		return nil, fmt.Errorf(
			"tutorialHordeFallback: %w",
			errors.Join(scheduleErr, completionErr, rollbackErr),
		)
	}
	return append([][]byte{alertPacket}, completionPackets...), nil
}
