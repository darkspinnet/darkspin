package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
)

const campaignClockPublishInterval = time.Second

type campaignClockRun struct {
	registry            *gameplaySessionRegistry
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	transportGeneration uint64
	sourceTime          uint64
	sourceStartedAt     time.Time
	now                 func() time.Time
	logger              *log.Logger
}

func campaignElapsedMilliseconds(currentZone *zone.Zone, now time.Time) uint64 {
	elapsed := currentZone.Elapsed(now)
	if elapsed <= 0 {
		return 0
	}
	return uint64(elapsed / time.Millisecond)
}

func campaignGameType(binding game.GameplayBinding) raknet.GameType {
	if binding.Mode == game.ModeArena {
		return raknet.GameTypeArena
	}
	return raknet.GameTypeChain
}

func marshalCampaignClock(
	binding game.GameplayBinding, gameTime uint64, timeElapsed uint64,
) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.GameStateMessage{
		Data: raknet.GameStateData{
			GameTime: gameTime, TimeElapsed: timeElapsed,
			State: raknet.GameDungeon, Type: campaignGameType(binding),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("campaignClockMarshal: %w", err)
	}
	return packet, nil
}

func (e campaignClockRun) schedule() error {
	if e.registry == nil || e.sessionKey == "" || e.generation == 0 ||
		e.transportGeneration == 0 ||
		(e.packet.ScheduleGroupResult == nil && e.packet.ScheduleGroup == nil) ||
		e.now == nil {
		return errors.New("campaign clock unavailable")
	}
	producer := raknet.ScheduledPacketProducer{
		Delay: campaignClockPublishInterval, Produce: e.publish,
	}
	cancel, err := e.packet.Autonomous().ScheduleProducers(
		[]raknet.ScheduledPacketProducer{producer},
	)
	if err != nil {
		return fmt.Errorf("campaignClockSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("campaign clock cancellation unavailable")
	}
	return nil
}

func (e campaignClockRun) publish() ([][]byte, error) {
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.transportGeneration == e.transportGeneration &&
		!peerSession.isRejoinPending && peerSession.stage.IsDungeon() &&
		peerSession.zone != nil
	e.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	now := e.now()
	sourceElapsed := now.Sub(e.sourceStartedAt)
	if sourceElapsed < 0 {
		sourceElapsed = 0
	}
	zoneElapsed := peerSession.zone.Elapsed(now)
	objectiveErr := peerSession.zone.UpdateElapsedObjective(zoneElapsed)
	if objectiveErr != nil && e.logger != nil {
		e.logger.Printf(
			"RakNet campaign elapsed objective omitted game=%d user=%d remote=%s: %v",
			peerSession.binding.GameID, peerSession.binding.UserID,
			e.sessionKey, objectiveErr,
		)
	}
	packet, err := marshalCampaignClock(
		peerSession.binding,
		e.sourceTime+uint64(sourceElapsed/time.Millisecond),
		campaignElapsedMilliseconds(peerSession.zone, now),
	)
	if err != nil {
		return nil, fmt.Errorf("campaignClockPublish: %w", err)
	}
	scheduleErr := e.schedule()
	if scheduleErr != nil && e.logger != nil {
		e.logger.Printf(
			"RakNet campaign clock stopped game=%d user=%d remote=%s: %v",
			peerSession.binding.GameID, peerSession.binding.UserID,
			e.sessionKey, scheduleErr,
		)
	}
	return [][]byte{packet}, nil
}

func (r gameplaySetupRuntime) campaignClockRun(
	packet raknet.Packet, peerSession gameplayPeerSession,
) campaignClockRun {
	return campaignClockRun{
		registry: r.registry, packet: packet.Autonomous(),
		sessionKey: packet.Address.String(), generation: peerSession.generation,
		transportGeneration: peerSession.transportGeneration,
		sourceTime:          packet.SourceTime, sourceStartedAt: r.now(),
		now: r.now, logger: r.logger,
	}
}

func (r gameplaySetupRuntime) startCampaignClock(
	packet raknet.Packet, peerSession gameplayPeerSession,
) error {
	run := r.campaignClockRun(packet, peerSession)
	err := packet.AfterResponseCommit(func() {
		scheduleErr := run.schedule()
		if scheduleErr != nil && r.logger != nil {
			r.logger.Printf(
				"RakNet campaign clock start failed game=%d user=%d remote=%s: %v",
				peerSession.binding.GameID, peerSession.binding.UserID,
				packet.Address, scheduleErr,
			)
		}
	})
	if err != nil {
		return fmt.Errorf("campaignClockCommit: %w", err)
	}
	return nil
}

func (r gameplaySetupRuntime) scheduleCampaignClock(
	packet raknet.Packet, peerSession gameplayPeerSession,
) {
	run := r.campaignClockRun(packet, peerSession)
	err := run.schedule()
	if err != nil && r.logger != nil {
		r.logger.Printf(
			"RakNet campaign clock resume failed game=%d user=%d remote=%s: %v",
			peerSession.binding.GameID, peerSession.binding.UserID,
			packet.Address, err,
		)
	}
}
