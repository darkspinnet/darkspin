// Package admissionconfig owns the durable server-wide player capacity.
package admissionconfig

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

type Settings struct {
	WorldPlayerLimit uint32
}

var ErrAuditIncomplete = errors.New("capacity saved but audit completion failed")

func Validate(req Settings) error {
	if req.WorldPlayerLimit == 0 {
		return errors.New("world player limit must be positive")
	}
	return nil
}

type Store interface {
	Read(context.Context) (Settings, error)
	Save(context.Context, Settings) error
}

type AuditPort interface {
	BeginServerOperation(context.Context, accountaccess.Actor, string, string) (string, error)
	CompleteServerOperation(context.Context, string, bool, string) error
}

type Limiter interface {
	SetWorldPlayerLimit(uint32)
}

type Service struct {
	mu        sync.Mutex
	store     Store
	auditPort AuditPort
	limiter   Limiter
}

func New(store Store, auditPort AuditPort, limiter Limiter) *Service {
	return &Service{store: store, auditPort: auditPort, limiter: limiter}
}

func (e *Service) Read(ctx context.Context) (Settings, error) {
	if ctx == nil {
		return Settings{}, errors.New("nil context")
	}
	settings, err := e.store.Read(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("capacityRead: %w", err)
	}
	err = Validate(settings)
	if err != nil {
		return Settings{}, fmt.Errorf("capacityValidate: %w", err)
	}
	return settings, nil
}

// Apply initializes admission before the runtime accepts player sessions.
func (e *Service) Apply(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	settings, err := e.Read(ctx)
	if err != nil {
		return fmt.Errorf("capacityLoad: %w", err)
	}
	e.limiter.SetWorldPlayerLimit(settings.WorldPlayerLimit)
	return nil
}

func (e *Service) SetAuthorized(ctx context.Context, actor accountaccess.Actor, req Settings) error {
	if ctx == nil || e.auditPort == nil {
		return errors.New("capacity administration unavailable")
	}
	err := Validate(req)
	if err != nil {
		return fmt.Errorf("capacityValidate: %w", err)
	}
	operationID, err := e.auditPort.BeginServerOperation(ctx, actor, "server.capacity", "server player capacity update")
	if err != nil {
		return fmt.Errorf("capacityAccept: %w", err)
	}
	e.mu.Lock()
	saveErr := e.store.Save(ctx, req)
	if saveErr == nil {
		e.limiter.SetWorldPlayerLimit(req.WorldPlayerLimit)
	}
	e.mu.Unlock()
	outcome := "configuration_saved"
	if saveErr != nil {
		outcome = "configuration_save_failed"
	}
	completionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	auditErr := e.auditPort.CompleteServerOperation(completionContext, operationID, saveErr == nil, outcome)
	if saveErr != nil {
		return fmt.Errorf("capacitySave: %w", errors.Join(saveErr, auditErr))
	}
	if auditErr != nil {
		return fmt.Errorf("capacityAudit: %w", errors.Join(ErrAuditIncomplete, auditErr))
	}
	return nil
}
