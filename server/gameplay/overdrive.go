package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

const campaignOverdriveMaximumEnergy = float64(100)
const campaignOverdriveBaseDrainPerSecond = float64(3.3)

type campaignOverdriveRuntime struct {
	registry *gameplaySessionRegistry
	now      func() time.Time
	logger   *log.Logger
}

func (r campaignOverdriveRuntime) handle(
	packet raknet.Packet, command raknet.ActionCommandData,
) ([][]byte, error) {
	if command.Common.Type != raknet.ActionOverdrive {
		return nil, errors.New("overdrive command invalid")
	}
	sessionKey := packet.Address.String()
	now := r.now()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isAccepted := isFound && peerSession.stage.IsDungeon() &&
		peerSession.binding.IsOverdriveUnlocked &&
		command.Common.ObjectID == peerSession.deployedObjectID &&
		peerSession.deployedHitPoint() > 0 &&
		!peerSession.isOverdriveSpent &&
		!peerSession.isOverdriveActiveAt(now)
	if !isAccepted {
		r.registry.mutex.Unlock()
		return nil, errors.New("overdrive unavailable")
	}
	duration := campaignOverdriveDuration(peerSession)
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp:                command.Common.Unknown[0],
		ResponseType:             raknet.ActionResponseAccepted,
		ObjectID:                 command.Common.ObjectID,
		SourceStartMilliseconds:  packet.SourceTime,
		SourceCommitMilliseconds: packet.SourceTime,
		SourceEndMilliseconds:    packet.SourceTime + uint64(duration/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("overdriveAcknowledge: %w", err)
	}
	peerSession.isOverdriveSpent = true
	peerSession.overdriveExpiresAt = now.Add(duration)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if r.logger != nil {
		r.logger.Printf(
			"RakNet campaign Overdrive activated game=%d user=%d duration=%s",
			peerSession.binding.GameID, peerSession.binding.UserID, duration,
		)
	}
	return [][]byte{ackPacket}, nil
}

func campaignOverdriveDuration(peerSession gameplayPeerSession) time.Duration {
	creatureIndex := peerSession.deployedCreatureIndex
	if creatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return campaignOverdriveBaseDuration()
	}
	duration, err := game.ResolveOverdriveDuration(
		campaignOverdriveMaximumEnergy, campaignOverdriveBaseDrainPerSecond,
		peerSession.binding.Creatures[creatureIndex].OverdriveDurationIncrease,
	)
	if err != nil {
		return campaignOverdriveBaseDuration()
	}
	return duration
}

func campaignOverdriveBaseDuration() time.Duration {
	duration, err := game.ResolveOverdriveDuration(
		campaignOverdriveMaximumEnergy, campaignOverdriveBaseDrainPerSecond, 0,
	)
	if err != nil {
		return 30 * time.Second
	}
	return duration
}
