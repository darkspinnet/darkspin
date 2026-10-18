//go:build scenario

package gameplay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
)

// scenarioPeerPreparation travels with the admitted peer and is guarded by
// its existing registry mutex. No journal survives a new peer value or binding.
type scenarioPeerPreparation struct {
	scenarioPreparation scenarioPreparationRecord
}

type scenarioPreparationRecord struct {
	gameID              uint32
	userID              uint64
	peerGeneration      uint64
	transportGeneration uint64
	runSeed             uint64
	level               string
	message             raknet.PrepareForStartMessage
	preparePayload      [17]byte
	statusPayload       []byte
	statusTraceID       uint64
	statusSourceTime    uint64
	statusMessageID     uint8
	queuedAt            time.Time
	statusReceivedAt    time.Time
	acceptedAt          time.Time
	status              raknet.PlayerStatus
	reason              string
}

// ScenarioPrepareEvidence contains only allowlisted identity and loading
// observations. A queued packet is not evidence of a successful socket write
// or an echo of the seed actually consumed by the native client.
type ScenarioPrepareEvidence struct {
	UserID               uint64
	GameID               uint32
	PeerGeneration       uint64
	TransportGeneration  uint64
	GameplaySessionID    string
	PrepareQueuedAt      time.Time
	StatusReceivedAt     time.Time
	AcceptedAt           time.Time
	Status               uint32
	Progress             float32
	EffectiveLevelID     uint32
	EffectiveMapSeed     uint32
	EffectivePrepareMask uint32
	EffectiveOccurrence  uint32
	EffectiveDifficulty  uint32
	// PreparePayload includes the application message ID; StatusPayload is
	// the exact received payload following StatusMessageID.
	PreparePayload            [17]byte
	StatusPayload             []byte
	StatusTraceID             uint64
	StatusSourceTime          uint64
	StatusMessageID           uint8
	IsWirePublicationObserved bool
	IsClientSeedEchoObserved  bool
}

type scenarioPrepareProvider interface {
	ScenarioPrepareObservation(context.Context, int64) (scenario.Observation, ScenarioPrepareEvidence, error)
}

func (e Lifecycle) ScenarioPrepareObservation(
	ctx context.Context, userID int64,
) (scenario.Observation, ScenarioPrepareEvidence, error) {
	if ctx == nil || userID <= 0 {
		return scenario.Observation{}, ScenarioPrepareEvidence{}, errors.New("prepare observation requires context and isolated user")
	}
	err := ctx.Err()
	if err != nil {
		return scenario.Observation{}, ScenarioPrepareEvidence{}, fmt.Errorf("prepareContext: %w", err)
	}
	provider, isSupported := e.syncSnapshot.(scenarioPrepareProvider)
	if !isSupported {
		return scenario.Observation{
			Outcome: scenario.Inconclusive,
			Detail:  "live prepare observation provider is unavailable",
		}, ScenarioPrepareEvidence{}, nil
	}
	observation, evidence, err := provider.ScenarioPrepareObservation(ctx, userID)
	if err != nil {
		return scenario.Observation{}, ScenarioPrepareEvidence{}, fmt.Errorf("prepareProvider: %w", err)
	}
	return observation, evidence, nil
}

func (e *gameplayPeerSession) recordScenarioPrepareQueued(packet []byte, queuedAt time.Time) {
	if e == nil {
		return
	}
	mapSeed, prepareMask, isConfigured, err := e.binding.ScenarioMapInputs()
	if err != nil {
		e.scenarioPreparation = scenarioPreparationRecord{reason: "configured prepare inputs are invalid"}
		return
	}
	if !isConfigured {
		return
	}
	if len(packet) != 17 || packet[0] != byte(raknet.GamePrepareForStart) || queuedAt.IsZero() {
		e.scenarioPreparation = scenarioPreparationRecord{reason: "queued prepare packet is unavailable or malformed"}
		return
	}
	message := raknet.PrepareForStartMessage{
		Level:         binary.LittleEndian.Uint32(packet[1:5]),
		VariantSeed:   binary.LittleEndian.Uint32(packet[5:9]),
		ConditionMask: binary.LittleEndian.Uint32(packet[9:13]),
		LevelIndex:    binary.LittleEndian.Uint32(packet[13:17]),
	}
	expected, err := zonePrepareMessage(e.binding)
	if err != nil {
		e.scenarioPreparation = scenarioPreparationRecord{reason: "shared prepare message is unavailable"}
		return
	}
	if message != expected || message.VariantSeed != mapSeed || message.ConditionMask != prepareMask {
		e.scenarioPreparation = scenarioPreparationRecord{reason: "queued prepare bytes differ from configured shared inputs"}
		return
	}
	previous := e.scenarioPreparation
	if previous.isCurrent(*e) && previous.message == message && !previous.queuedAt.IsZero() {
		// Repeated ordinary preparation does not erase an earlier observation
		// of the same exact inputs for this admitted peer.
		return
	}
	e.scenarioPreparation = scenarioPreparationRecord{
		gameID: e.binding.GameID, userID: e.binding.UserID,
		peerGeneration: e.generation, transportGeneration: e.transportGeneration,
		runSeed: e.binding.RunSeed, level: e.binding.Level,
		message: message, queuedAt: queuedAt,
	}
	copy(e.scenarioPreparation.preparePayload[:], packet)
}

func (e scenarioPreparationRecord) isCurrent(peerSession gameplayPeerSession) bool {
	return e.gameID != 0 && e.gameID == peerSession.binding.GameID &&
		e.userID != 0 && e.userID == peerSession.binding.UserID &&
		e.peerGeneration != 0 && e.peerGeneration == peerSession.generation &&
		e.transportGeneration != 0 && e.transportGeneration == peerSession.transportGeneration &&
		e.runSeed == peerSession.binding.RunSeed && e.level == peerSession.binding.Level
}

func (e *gameplayPeerSession) recordScenarioPrepareStatus(
	packet raknet.Packet, status raknet.PlayerStatus, receivedAt time.Time,
) {
	if e == nil || status.Status != 8 || receivedAt.IsZero() ||
		packet.TransportGeneration != e.transportGeneration {
		return
	}
	record := e.scenarioPreparation
	if !record.isCurrent(*e) || record.queuedAt.IsZero() || receivedAt.Before(record.queuedAt) {
		return
	}
	if math.IsNaN(float64(status.Progress)) || math.IsInf(float64(status.Progress), 0) {
		e.scenarioPreparation.reason = "incoming loading status has non-finite progress"
		return
	}
	// This is a decoded incoming status only. The successful ordinary
	// startCampaign continuation records acceptance separately.
	e.scenarioPreparation.statusReceivedAt = receivedAt
	e.scenarioPreparation.status = status
	e.scenarioPreparation.statusPayload = append([]byte(nil), packet.Payload...)
	e.scenarioPreparation.statusTraceID = packet.TraceID
	e.scenarioPreparation.statusSourceTime = packet.SourceTime
	e.scenarioPreparation.statusMessageID = uint8(packet.ID)
}

func (e *gameplaySessionRegistry) recordScenarioPrepareResponse(
	packet raknet.Packet, peerSession gameplayPeerSession, setupPacket []byte,
) {
	if e == nil || packet.Address == nil {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	currentSession, isFound := e.sessions[packet.Address.String()]
	if !isFound || currentSession.generation != peerSession.generation ||
		currentSession.binding.GameID != peerSession.binding.GameID ||
		currentSession.transportGeneration != packet.TransportGeneration {
		return
	}
	currentSession.recordScenarioPrepareQueued(setupPacket, time.Now())
	e.sessions[packet.Address.String()] = currentSession
}

func (e *gameplaySessionRegistry) recordScenarioPrepareAccepted(
	packet raknet.Packet, peerSession gameplayPeerSession, status raknet.PlayerStatus,
) {
	if e == nil || packet.Address == nil || status.Status != 8 {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	currentSession, isFound := e.sessions[packet.Address.String()]
	if !isFound || currentSession.generation != peerSession.generation ||
		currentSession.binding.GameID != peerSession.binding.GameID ||
		currentSession.transportGeneration != packet.TransportGeneration || !currentSession.stage.IsDungeon() {
		return
	}
	record := currentSession.scenarioPreparation
	if !record.isCurrent(currentSession) || record.queuedAt.IsZero() ||
		record.statusReceivedAt.IsZero() || record.statusReceivedAt.Before(record.queuedAt) ||
		record.status != status {
		return
	}
	currentSession.scenarioPreparation.acceptedAt = time.Now()
	e.sessions[packet.Address.String()] = currentSession
}

func (e *gameplaySessionRegistry) ScenarioPrepareObservation(
	ctx context.Context, userID int64,
) (scenario.Observation, ScenarioPrepareEvidence, error) {
	peerSession, observation, err := e.scenarioSession(ctx, userID)
	if err != nil {
		return scenario.Observation{}, ScenarioPrepareEvidence{}, fmt.Errorf("prepareSession: %w", err)
	}
	if observation.Outcome != scenario.Passed {
		return observation, ScenarioPrepareEvidence{}, nil
	}
	observation.Outcome = scenario.Inconclusive
	observation.Detail = "ordinary campaign prepare acceptance has not been observed"
	record := peerSession.scenarioPreparation
	if record.reason != "" {
		observation.Detail = record.reason
		return observation, ScenarioPrepareEvidence{}, nil
	}
	mapSeed, prepareMask, isConfigured, err := peerSession.binding.ScenarioMapInputs()
	if err != nil {
		return scenario.Observation{}, ScenarioPrepareEvidence{}, fmt.Errorf("prepareInputs: %w", err)
	}
	if !isConfigured || !record.isCurrent(peerSession) || peerSession.binding.Mode != game.ModeChain ||
		peerSession.binding.IsWarped || peerSession.binding.IsCheckpointRestore {
		observation.Detail = "current gameplay binding has no fresh configured preparation observation"
		return observation, ScenarioPrepareEvidence{}, nil
	}
	evidence := ScenarioPrepareEvidence{
		UserID: record.userID, GameID: record.gameID,
		PeerGeneration: record.peerGeneration, TransportGeneration: record.transportGeneration,
		GameplaySessionID: observation.SessionID,
		PrepareQueuedAt:   record.queuedAt, StatusReceivedAt: record.statusReceivedAt,
		AcceptedAt: record.acceptedAt, Status: record.status.Status, Progress: record.status.Progress,
		EffectiveLevelID: record.message.Level, EffectiveMapSeed: record.message.VariantSeed,
		EffectivePrepareMask: record.message.ConditionMask, EffectiveOccurrence: record.message.LevelIndex,
		EffectiveDifficulty: peerSession.binding.Difficulty,
		PreparePayload:      record.preparePayload, StatusPayload: append([]byte(nil), record.statusPayload...),
		StatusTraceID: record.statusTraceID, StatusSourceTime: record.statusSourceTime,
		StatusMessageID: record.statusMessageID,
	}
	if record.queuedAt.IsZero() || record.statusReceivedAt.IsZero() || record.acceptedAt.IsZero() ||
		record.status.Status != 8 || record.statusReceivedAt.Before(record.queuedAt) ||
		record.acceptedAt.Before(record.statusReceivedAt) {
		return observation, evidence, nil
	}
	expected, err := zonePrepareMessage(peerSession.binding)
	if err != nil {
		return scenario.Observation{}, ScenarioPrepareEvidence{}, fmt.Errorf("prepareMessage: %w", err)
	}
	if record.message != expected || record.message.VariantSeed != mapSeed ||
		record.message.ConditionMask != prepareMask {
		observation.Detail = "accepted prepare journal differs from current shared configured inputs"
		return observation, evidence, nil
	}
	observation.Outcome = scenario.Passed
	observation.Detail = "ordinary client status 8 accepted after queued PrepareForStart; transport publication and client seed echo are unobserved"
	observation.Effective = &scenario.EffectiveInputs{
		Level: peerSession.binding.Level, Occurrence: record.message.LevelIndex,
		Difficulty: peerSession.binding.Difficulty, MapSeed: record.message.VariantSeed,
		PrepareMask:          record.message.ConditionMask,
		PopulationProvenance: fmt.Sprintf("ordinary server run seed %d; population derivation is not fully controlled", record.runSeed),
		UnknownRNGOwners:     []string{"native client seed consumption", "population random-stream derivation", "loot random stream"},
	}
	// Client loading acceptance does not establish an authoritative zone epoch.
	return observation, evidence, nil
}
