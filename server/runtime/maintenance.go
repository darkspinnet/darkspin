package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze"
	"github.com/darkspinnet/darkspin/server/buildinfo"
)

func (e *Server) runMaintenanceSchedule(ctx context.Context, cancel context.CancelFunc) {
	defer close(e.maintenanceDone)
	for ctx.Err() == nil {
		isDue, err := e.maintenanceControl.RunSchedule(ctx, *e.maintenanceSchedule)
		if err != nil {
			e.logger.Printf("Scheduled maintenance failed; retaining server: %v", err)
			continue
		}
		if !isDue {
			return
		}
		e.lifecycleMu.Lock()
		if !e.isClosed && ctx.Err() == nil {
			e.isMaintenanceUpdate = true
			cancel()
		}
		e.lifecycleMu.Unlock()
		return
	}
}

func (e *Server) writeMaintenanceRequest(ctx context.Context) error {
	if !e.isMaintenanceUpdate || ctx.Err() != nil {
		return nil
	}
	if e.closeErr != nil {
		return fmt.Errorf("maintenanceClose: %w", e.closeErr)
	}
	if !e.maintenanceControl.IsScheduledUpdatePending() {
		return nil
	}
	req := struct {
		Version         int       `json:"version"`
		RequestedAt     time.Time `json:"requested_at"`
		BuildID         string    `json:"build_id"`
		IsCleanShutdown bool      `json:"is_clean_shutdown"`
	}{Version: 1, RequestedAt: time.Now().UTC(), BuildID: buildinfo.ID, IsCleanShutdown: true}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("requestEncode: %w", err)
	}
	path := e.maintenanceSchedule.UpdateRequestPath
	w, err := os.CreateTemp(filepath.Dir(path), ".update-request-*")
	if err != nil {
		return fmt.Errorf("requestCreate: %w", err)
	}
	defer e.removeMaintenanceTemp(w.Name())
	written, writeErr := w.Write(payload)
	if written != len(payload) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	syncErr := w.Sync()
	closeErr := w.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("requestWrite: %w", errors.Join(writeErr, syncErr, closeErr))
	}
	if ctx.Err() != nil {
		return nil
	}
	err = os.Rename(w.Name(), path)
	if err != nil {
		return fmt.Errorf("requestInstall: %w", err)
	}
	err = syncMaintenanceDirectory(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("requestDirectory: %w", err)
	}
	return nil
}

func syncMaintenanceDirectory(path string) error {
	// Bootstrap scheduling is Linux-only. Windows directory handles do not
	// support os.File.Sync; preserve ordinary server compilation there.
	if stdruntime.GOOS != "linux" {
		return nil
	}
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("directoryOpen: %w", err)
	}
	syncErr := r.Sync()
	closeErr := r.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("directorySync: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}

func (e *Server) removeMaintenanceTemp(path string) {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		e.logger.Printf("Maintenance temporary request cleanup failed: %v", err)
	}
}

func (e *Server) broadcastMaintenance(ctx context.Context, message string) (int, error) {
	delivered, err := blaze.BroadcastSystemChat(ctx, e.maintenanceServers, message)
	if err != nil {
		return delivered, fmt.Errorf("maintenanceBroadcast: %w", err)
	}
	return delivered, nil
}
