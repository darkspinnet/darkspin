package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/snapshot"
	sharedudp "github.com/darkspinnet/darkspin/server/udp"
)

type snapshotTransportProvider struct {
	server *sharedudp.SharedServer
}

// SnapshotControlConnection returns ephemeral local Fang launch credentials.
func (e *Server) SnapshotControlConnection() (endpoint string, token string) {
	if e == nil || e.snapshot == nil {
		return "", ""
	}
	return e.snapshot.ControlConnection()
}

// SnapshotClientControl scopes local capture credentials to the launch profile.
func (e *Server) SnapshotClientControl(ctx context.Context, loginName string) (endpoint string, token string, err error) {
	if e == nil || e.userManager == nil {
		return "", "", errors.New("snapshot control user manager unavailable")
	}
	userID, err := e.userManager.FindUserID(ctx, loginName)
	if err != nil {
		return "", "", fmt.Errorf("controlUser: %w", err)
	}
	endpoint, token, err = e.SnapshotControlForUser(userID)
	if err != nil {
		return "", "", fmt.Errorf("controlClient: %w", err)
	}
	return endpoint, token, nil
}

// SnapshotControlForUser scopes credentials to an already resolved owned user.
func (e *Server) SnapshotControlForUser(userID int64) (endpoint string, token string, err error) {
	if e == nil || e.snapshot == nil {
		return "", "", errors.New("snapshot control service unavailable")
	}
	endpoint, token, err = e.snapshot.ControlConnectionForUser(userID)
	if err != nil {
		return "", "", fmt.Errorf("controlScope: %w", err)
	}
	return endpoint, token, nil
}

// SnapshotControlDirectory is the Fang diagnostic root selected by the recorder.
func (e *Server) SnapshotControlDirectory() string {
	if e == nil || e.snapshot == nil {
		return ""
	}
	return e.snapshot.ControlDirectory()
}

func (e snapshotTransportProvider) SnapshotTransportDiagnostics(
	remote string, generation uint64, now time.Time,
) (snapshot.TransportDiagnosticsState, bool) {
	if e.server == nil {
		return snapshot.TransportDiagnosticsState{}, false
	}
	diagnostics, isFound := e.server.SnapshotDiagnostics(remote, generation, now)
	if !isFound {
		return snapshot.TransportDiagnosticsState{}, false
	}
	peer := diagnostics.Peer
	return snapshot.TransportDiagnosticsState{
		Remote: remote, PeerGeneration: peer.Generation,
		IsConnected:             peer.IsConnected,
		PendingDatagramCount:    peer.PendingDatagramCount,
		OldestPendingDatagramMS: peer.OldestPendingDatagramAge.Milliseconds(),
		FutureDatagramCount:     peer.FutureDatagramCount,
		MissingDatagramCount:    peer.MissingDatagramCount,
		PendingOrderCount:       peer.PendingOrderCount,
		SplitAssemblyCount:      peer.SplitAssemblyCount, SplitByteCount: peer.SplitByteCount,
		GameplayWorkerQueueDepth:  diagnostics.QueueDepth,
		CurrentTraceID:            diagnostics.CurrentTraceID,
		CurrentRequestID:          diagnostics.CurrentRequestID,
		CurrentQueueDelayMS:       diagnostics.CurrentQueueDelay.Milliseconds(),
		CurrentDispatchDurationMS: diagnostics.CurrentDispatchDuration.Milliseconds(),
		LastDispatchDurationMS:    diagnostics.LastDispatchDuration.Milliseconds(),
		LastFailure:               diagnostics.LastFailure,
	}, true
}

type snapshotNotifier struct {
	servers []*blaze.Server
}

func (e snapshotNotifier) NotifySnapshot(
	ctx context.Context, notice snapshot.Notice,
) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("noticeContext: %w", err)
	}
	if notice.Actor.UserID == 0 || notice.Message == "" {
		return errors.New("snapshot notice invalid")
	}
	var notifyErrors []error
	deliveryCount := 0
	for index, server := range e.servers {
		if server == nil {
			continue
		}
		var serverDeliveryCount int
		serverDeliveryCount, err = server.NotifySystemChat(
			notice.Actor.UserID, notice.Actor.GameID, notice.Message,
		)
		deliveryCount += serverDeliveryCount
		if err != nil {
			notifyErrors = append(notifyErrors, fmt.Errorf("noticeServer[%d]: %w", index, err))
		}
	}
	if deliveryCount == 0 {
		notifyErrors = append(notifyErrors, errors.New("notice recipient session unavailable"))
	}
	return errors.Join(notifyErrors...)
}

// SetSnapshotMode applies a persisted launcher mode to the running service.
func (s *Server) SetSnapshotMode(rawMode string) error {
	if s == nil || s.snapshot == nil || s.config == nil {
		return errors.New("sync snapshot service unavailable")
	}
	mode, err := snapshot.ParseMode(rawMode)
	if err != nil {
		return fmt.Errorf("snapshotMode: %w", err)
	}
	s.snapshot.SetMode(mode)
	err = s.config.Set(game.ConfigSnapshotMode, string(mode))
	if err != nil {
		return fmt.Errorf("snapshotConfig: %w", err)
	}
	return nil
}
