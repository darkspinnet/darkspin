package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/bootstrap"
	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/controlpanel"
	"github.com/darkspinnet/darkspin/server/game"
)

const updateCheckCooldown = time.Minute

// panelUpdate owns one bounded, shared observation; reading it never contacts
// the manifest service or starts a maintenance operation.
type panelUpdate struct {
	accessService *accountaccess.Service
	manifestURL   string
	mu            sync.Mutex
	snapshot      controlpanel.UpdateSnapshot
	nextCheckAt   time.Time
}

func newPanelUpdate(configPath string, config *game.Config, isManaged bool, accessService *accountaccess.Service) *panelUpdate {
	snapshot := controlpanel.UpdateSnapshot{
		InstalledVersion: buildinfo.Version, InstalledBuildID: buildinfo.ID, CheckStatus: "unknown",
	}
	// Checking metadata does not require supervisor-owned installation support.
	// Use the same configured source as bootstrap without accepting a request URL.
	manifestURL := config.String(game.ConfigBootstrapManifestURL)
	rootPath := os.Getenv("DARKSPIN_BOOTSTRAP_ROOT")
	if isManaged && rootPath != "" && filepath.IsAbs(rootPath) &&
		filepath.Clean(configPath) == filepath.Join(rootPath, "runtime", game.DefaultConfigFilename) {
		// Use the running supervisor's effective configuration and identity,
		// including command-line overrides, rather than a replaced binary on disk.
		snapshot.BootstrapVersion = updateIdentityLabel(os.Getenv("DARKSPIN_BOOTSTRAP_VERSION"))
		snapshot.BootstrapBuildID = updateIdentityLabel(os.Getenv("DARKSPIN_BOOTSTRAP_BUILD_ID"))
		effectiveURL, isPresent := os.LookupEnv("DARKSPIN_BOOTSTRAP_MANIFEST_URL")
		// The supervisor may deliberately disable or override the on-disk source.
		manifestURL = ""
		if isPresent {
			manifestURL = effectiveURL
		}
	}
	manifestURL = strings.TrimSpace(manifestURL)
	manifestSource, err := url.Parse(manifestURL)
	if err != nil || manifestSource.Scheme != "https" || manifestSource.Host == "" ||
		manifestSource.User != nil || manifestSource.Fragment != "" {
		manifestURL = ""
	}
	snapshot.IsCheckSupported = manifestURL != ""
	if snapshot.IsCheckSupported {
		// There is no channel setting in the bootstrap manifest contract.
		snapshot.Channel = "configured"
	} else {
		snapshot.CheckStatus = "unsupported"
	}
	return &panelUpdate{accessService: accessService, manifestURL: manifestURL, snapshot: snapshot}
}

func updateIdentityLabel(label string) string {
	if len(label) > 256 {
		return ""
	}
	for _, character := range label {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._-+", character) {
			continue
		}
		return ""
	}
	return label
}

func (e *panelUpdate) IsCheckSupported() bool {
	return e.manifestURL != ""
}

func (e *panelUpdate) Snapshot(ctx context.Context) (controlpanel.UpdateSnapshot, error) {
	err := ctx.Err()
	if err != nil {
		return controlpanel.UpdateSnapshot{}, fmt.Errorf("updateContext: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshot, nil
}

func (e *panelUpdate) Check(ctx context.Context, actor accountaccess.Actor) (controlpanel.UpdateSnapshot, error) {
	if e.accessService == nil {
		return controlpanel.UpdateSnapshot{}, errors.New("update authorization unavailable")
	}
	access, err := e.accessService.Authorize(ctx, actor.UserID, actor.Revision, true)
	if err != nil {
		return controlpanel.UpdateSnapshot{}, fmt.Errorf("updateAuthorize: %w", err)
	}
	if access.UserID != actor.UserID {
		return controlpanel.UpdateSnapshot{}, fmt.Errorf("updateActor: %w", accountaccess.ErrAccessDenied)
	}
	e.mu.Lock()
	if !e.IsCheckSupported() || e.snapshot.IsCheckInProgress || time.Now().Before(e.nextCheckAt) {
		snapshot := e.snapshot
		e.mu.Unlock()
		return snapshot, nil
	}
	e.snapshot.IsCheckInProgress = true
	e.snapshot.CheckStatus = "checking"
	e.snapshot.FailureCode = ""
	e.snapshot.CandidateVersion = ""
	e.snapshot.CandidateBuildID = ""
	e.mu.Unlock()

	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	observation, err := e.checkManifest(checkCtx)
	checkedAt := time.Now().UTC()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snapshot.IsCheckInProgress = false
	e.snapshot.CheckedAt = &checkedAt
	e.nextCheckAt = checkedAt.Add(updateCheckCooldown)
	if err != nil {
		e.snapshot.CheckStatus = "failed"
		e.snapshot.FailureCode = "check_failed"
		if errors.Is(err, context.DeadlineExceeded) {
			e.snapshot.FailureCode = "check_timeout"
		} else if errors.Is(err, context.Canceled) {
			e.snapshot.FailureCode = "check_cancelled"
		}
		return e.snapshot, nil
	}
	e.snapshot.CandidateVersion = observation.Version
	e.snapshot.CandidateBuildID = observation.BuildID
	e.snapshot.CheckStatus = "available"
	if observation.IsCurrent {
		e.snapshot.CheckStatus = "current"
	}
	return e.snapshot, nil
}

func (e *panelUpdate) checkManifest(ctx context.Context) (bootstrap.UpdateOverview, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return bootstrap.UpdateOverview{}, fmt.Errorf("updateExecutable: %w", err)
	}
	observation, err := bootstrap.CheckOverview(ctx, e.manifestURL, executablePath)
	if err != nil {
		return bootstrap.UpdateOverview{}, fmt.Errorf("updateOverview: %w", err)
	}
	return observation, nil
}
