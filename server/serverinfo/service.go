// Package serverinfo owns server identity and bounded plain-text announcements.
package serverinfo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

var (
	ErrConflict            = errors.New("server identity changed; reload before saving")
	ErrInvalidIdentity     = errors.New("invalid server identity")
	ErrInvalidAnnouncement = errors.New("invalid server announcement")
	ErrRateLimited         = errors.New("wait ten seconds between server announcements")
	ErrAuditIncomplete     = errors.New("server operation outcome audit incomplete")
)

type Identity struct {
	Name        string
	Description string
	Welcome     string
	Rules       string
	ContactURL  string
}

type Snapshot struct {
	Identity Identity
	Revision string
}

type SetIdentityRequest struct {
	ExpectedRevision string
	Identity         Identity
}

type Announcement struct {
	ID                string
	Message           string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	Delivered         int
	IsDeliveryPartial bool
	IsAuditRecorded   bool
	Outcome           string
}

type IdentityStore interface {
	Read(context.Context) (Snapshot, error)
	Save(context.Context, string, Identity) (Snapshot, error)
}

// AuditPort verifies persisted actor status, ban, and credential revision when
// accepting an operation. Only fixed server.identity/server.announcement actions
// are supported. Completion is a trusted local callback after the side effect.
type AuditPort interface {
	BeginServerOperation(context.Context, accountaccess.Actor, string, string) (string, error)
	CompleteServerOperation(context.Context, string, bool, string) error
}

type BroadcastPort interface {
	Broadcast(context.Context, string) (int, error)
}

type Service struct {
	identityStore      IdentityStore
	auditPort          AuditPort
	broadcastPort      BroadcastPort
	mutex              sync.Mutex
	lastAnnouncementAt time.Time
	announcements      []Announcement
}

func New(identityStore IdentityStore, auditPort AuditPort, broadcastPort BroadcastPort) *Service {
	return &Service{identityStore: identityStore, auditPort: auditPort, broadcastPort: broadcastPort}
}

func (e *Service) Identity(ctx context.Context) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, errors.New("nil context")
	}
	snapshot, err := e.identityStore.Read(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("identityRead: %w", err)
	}
	return snapshot, nil
}

func (e *Service) SetIdentity(ctx context.Context, actor accountaccess.Actor, req SetIdentityRequest) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, errors.New("nil context")
	}
	err := ValidateIdentity(req.Identity)
	if err != nil {
		return Snapshot{}, fmt.Errorf("identityValidate: %w", err)
	}
	if req.ExpectedRevision == "" || len(req.ExpectedRevision) > 128 {
		return Snapshot{}, fmt.Errorf("identityRevision: %w", ErrConflict)
	}
	current, err := e.identityStore.Read(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("identityCurrent: %w", err)
	}
	if current.Revision != req.ExpectedRevision {
		return Snapshot{}, fmt.Errorf("identityChanged: %w", ErrConflict)
	}
	operationID, err := e.auditPort.BeginServerOperation(ctx, actor, "server.identity", "server identity update")
	if err != nil {
		return Snapshot{}, fmt.Errorf("identityAccept: %w", err)
	}
	snapshot, saveErr := e.identityStore.Save(ctx, req.ExpectedRevision, req.Identity)
	safeOutcome := "configuration_saved"
	if saveErr != nil {
		safeOutcome = "configuration_save_failed"
	}
	auditErr := e.completeOperation(ctx, operationID, saveErr == nil, safeOutcome)
	if saveErr != nil {
		return snapshot, fmt.Errorf("identitySave: %w", errors.Join(saveErr, auditErr))
	}
	if auditErr != nil {
		return snapshot, fmt.Errorf("identityAudit: %w", errors.Join(ErrAuditIncomplete, auditErr))
	}
	return snapshot, nil
}

func (e *Service) Announce(ctx context.Context, actor accountaccess.Actor, message string) (Announcement, error) {
	if ctx == nil {
		return Announcement{}, errors.New("nil context")
	}
	message, err := NormalizeAnnouncement(message)
	if err != nil {
		return Announcement{}, fmt.Errorf("announcementValidate: %w", err)
	}
	operationID, acceptedAt, err := e.acceptAnnouncement(ctx, actor)
	if err != nil {
		return Announcement{}, fmt.Errorf("announcementAccept: %w", err)
	}
	delivered, broadcastErr := e.broadcastPort.Broadcast(ctx, message)
	if delivered < 0 {
		delivered = 0
		broadcastErr = errors.Join(broadcastErr, errors.New("broadcast returned a negative delivery count"))
	}
	announcement := Announcement{
		ID: operationID, Message: message, CreatedAt: acceptedAt,
		ExpiresAt: acceptedAt.Add(time.Hour), Delivered: delivered, Outcome: "delivered",
	}
	if broadcastErr != nil {
		announcement.Outcome = "failed"
		if delivered > 0 {
			announcement.Outcome = "partial"
			announcement.IsDeliveryPartial = true
		}
	}
	safeOutcome := announcement.Outcome + ";delivered=" + strconv.Itoa(delivered)
	auditErr := e.completeOperation(ctx, operationID, broadcastErr == nil, safeOutcome)
	announcement.IsAuditRecorded = auditErr == nil
	e.recordAnnouncement(announcement)
	if broadcastErr != nil {
		return announcement, fmt.Errorf("announcementBroadcast: %w", errors.Join(broadcastErr, auditErr))
	}
	if auditErr != nil {
		return announcement, fmt.Errorf("announcementAudit: %w", errors.Join(ErrAuditIncomplete, auditErr))
	}
	return announcement, nil
}

func (e *Service) acceptAnnouncement(ctx context.Context, actor accountaccess.Actor) (string, time.Time, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	now := time.Now().UTC()
	if !e.lastAnnouncementAt.IsZero() && now.Sub(e.lastAnnouncementAt) < 10*time.Second {
		return "", time.Time{}, ErrRateLimited
	}
	operationID, err := e.auditPort.BeginServerOperation(ctx, actor, "server.announcement", "server announcement")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("announcementAuthorize: %w", err)
	}
	e.lastAnnouncementAt = time.Now().UTC()
	return operationID, e.lastAnnouncementAt, nil
}

func (e *Service) completeOperation(ctx context.Context, operationID string, isSuccess bool, safeOutcome string) error {
	completionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := e.auditPort.CompleteServerOperation(completionContext, operationID, isSuccess, safeOutcome)
	if err != nil {
		return fmt.Errorf("operationAudit: %w", err)
	}
	return nil
}
