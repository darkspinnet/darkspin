package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	accesssql "github.com/darkspinnet/darkspin/server/accountaccess/sqlstore"
	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/controlpanel"
	recaphttp "github.com/darkspinnet/darkspin/server/http"
	"github.com/darkspinnet/darkspin/server/serverinfo"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type accountAccessProvider interface {
	AccountAccess() *accesssql.Store
}

// OpenLocalControlPanel creates a bounded session for an existing local profile.
// Only the launcher owning this Server can call this in-process entry point.
func (e *Server) OpenLocalControlPanel(ctx context.Context, loginName string) (string, error) {
	if ctx == nil {
		return "", errors.New("nil panel context")
	}
	if e == nil || e.userManager == nil || e.controlPanel == nil {
		return "", errors.New("local control panel is unavailable")
	}
	userID, err := e.userManager.FindUserID(ctx, loginName)
	if err != nil {
		return "", fmt.Errorf("panelAccount: %w", err)
	}
	token, err := e.controlPanel.OpenLocalSession(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("panelLocal: %w", err)
	}
	return token, nil
}

func controlPanelRoute(handler http.Handler) recaphttp.Handler {
	return func(writer http.ResponseWriter, req *http.Request, uri *recaphttp.URI) {
		handler.ServeHTTP(writer, req)
	}
}

type panelObservation struct {
	userManager  *sporenet.UserManager
	startedAt    time.Time
	name         string
	information  *serverinfo.Service
	healthReader *panelHealth
}

func (e *panelObservation) Identity(ctx context.Context, userID int64) (sporenet.UserIdentity, error) {
	summary, err := e.userManager.ProfileSummary(ctx, userID)
	if err != nil {
		return sporenet.UserIdentity{}, fmt.Errorf("panelProfile: %w", err)
	}
	return summary.Identity, nil
}

func (e *panelObservation) ServerSummary(ctx context.Context) (controlpanel.ServerSummary, error) {
	err := ctx.Err()
	if err != nil {
		return controlpanel.ServerSummary{}, fmt.Errorf("summaryContext: %w", err)
	}
	var identity serverinfo.Snapshot
	if e.information != nil {
		identity, err = e.information.Identity(ctx)
		if err != nil {
			return controlpanel.ServerSummary{}, fmt.Errorf("summaryIdentity: %w", err)
		}
	}
	health, err := e.healthReader.Health(ctx)
	if err != nil {
		return controlpanel.ServerSummary{}, fmt.Errorf("summaryHealth: %w", err)
	}
	name := identity.Identity.Name
	if name == "" {
		name = e.name
	}
	return controlpanel.ServerSummary{
		Name: name, Version: buildinfo.Version, StartedAt: e.startedAt,
		OnlineAccountCount: len(e.userManager.Users()),
		Description:        identity.Identity.Description, Welcome: identity.Identity.Welcome,
		Rules: identity.Identity.Rules, ContactURL: identity.Identity.ContactURL, HealthStatus: health.Status,
	}, nil
}

type accountAdmission struct {
	accessService *accountaccess.Service
}

func (e *accountAdmission) CheckAdmission(ctx context.Context, userID int64) error {
	access, err := e.accessService.Read(ctx, userID)
	if err != nil {
		return fmt.Errorf("admissionRead: %w", err)
	}
	if access.IsBlocked() {
		return accountaccess.ErrAccessDenied
	}
	err = accountaccess.CheckPeer(ctx, access)
	if err != nil {
		return fmt.Errorf("admissionPeer: %w", err)
	}
	return nil
}

func (e *accountAdmission) CredentialRevision(ctx context.Context, userID int64) (int64, error) {
	access, err := e.accessService.Read(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("credentialRead: %w", err)
	}
	if access.IsBlocked() {
		return 0, accountaccess.ErrAccessDenied
	}
	err = accountaccess.CheckPeer(ctx, access)
	if err != nil {
		return 0, fmt.Errorf("credentialPeer: %w", err)
	}
	return access.Revision, nil
}

func (e *accountAdmission) ValidateCredential(ctx context.Context, userID, revision int64) error {
	currentRevision, err := e.CredentialRevision(ctx, userID)
	if err != nil {
		return fmt.Errorf("credentialRevision: %w", err)
	}
	if revision < 0 || revision == 0 && currentRevision != 1 || revision > 0 && revision != currentRevision {
		return accountaccess.ErrAccessDenied
	}
	return nil
}
