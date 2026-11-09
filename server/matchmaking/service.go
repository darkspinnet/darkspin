// Package matchmaking owns durable server preferences for AI-filled matches.
package matchmaking

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

type Settings struct {
	CampaignMatchmakingBotSize         uint32
	IsCampaignBotSquadMirroringEnabled bool
}

var ErrAuditIncomplete = errors.New("configuration saved but audit completion failed")

func Default() Settings {
	return Settings{CampaignMatchmakingBotSize: 1, IsCampaignBotSquadMirroringEnabled: true}
}

func Validate(req Settings) error {
	if req.CampaignMatchmakingBotSize < 1 || req.CampaignMatchmakingBotSize > 3 {
		return errors.New("campaign matchmaking bot size must be between 1 and 3")
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

type Service struct {
	store     Store
	auditPort AuditPort
	mu        sync.RWMutex
	settings  Settings
}

func New(store Store, auditPort AuditPort) *Service {
	return &Service{store: store, auditPort: auditPort, settings: Default()}
}

func (e *Service) Current() Settings {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.settings
}

func (e *Service) Read(ctx context.Context) (Settings, error) {
	if ctx == nil {
		return Settings{}, errors.New("nil context")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	settings, err := e.store.Read(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("settingsRead: %w", err)
	}
	err = Validate(settings)
	if err != nil {
		return Settings{}, fmt.Errorf("settingsValidate: %w", err)
	}
	e.settings = settings
	return settings, nil
}

// Set is reserved for the launcher-owned, trusted in-process configuration path.
func (e *Service) Set(ctx context.Context, req Settings) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := Validate(req)
	if err != nil {
		return fmt.Errorf("settingsValidate: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	err = e.store.Save(ctx, req)
	if err != nil {
		return fmt.Errorf("settingsSave: %w", err)
	}
	e.settings = req
	return nil
}

func (e *Service) SetAuthorized(ctx context.Context, actor accountaccess.Actor, req Settings) error {
	if ctx == nil || e.auditPort == nil {
		return errors.New("matchmaking administration unavailable")
	}
	err := Validate(req)
	if err != nil {
		return fmt.Errorf("settingsValidate: %w", err)
	}
	operationID, err := e.auditPort.BeginServerOperation(ctx, actor, "server.matchmaking", "matchmaking configuration update")
	if err != nil {
		return fmt.Errorf("settingsAccept: %w", err)
	}
	saveErr := e.Set(ctx, req)
	outcome := "configuration_saved"
	if saveErr != nil {
		outcome = "configuration_save_failed"
	}
	completionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	auditErr := e.auditPort.CompleteServerOperation(completionContext, operationID, saveErr == nil, outcome)
	if saveErr != nil {
		return fmt.Errorf("settingsUpdate: %w", errors.Join(saveErr, auditErr))
	}
	if auditErr != nil {
		return fmt.Errorf("settingsAudit: %w", errors.Join(ErrAuditIncomplete, auditErr))
	}
	return nil
}
