package accountaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type ServerOperationStore interface {
	BeginServerOperation(context.Context, Actor, string, string) (string, error)
	CompleteServerOperation(context.Context, string, bool, string) error
}

// TrustedServerOperationStore records commands from the authenticated local
// control channel and the server scheduler, without impersonating an account.
type TrustedServerOperationStore interface {
	BeginTrustedServerOperation(context.Context, string, string, string, string) (string, error)
}

func ValidateServerOperation(actor Actor, action, reason string) error {
	err := ValidateActor(actor)
	if err != nil {
		return fmt.Errorf("operationActor: %w", err)
	}
	err = validateServerOperationFields(action, reason)
	if err != nil {
		return fmt.Errorf("operationFields: %w", err)
	}
	return nil
}

func validateServerOperationFields(action, reason string) error {
	if action != "server.identity" && action != "server.announcement" && action != "server.matchmaking" && action != "server.capacity" &&
		action != "maintenance.restart" && action != "maintenance.update" &&
		action != "maintenance.hold" && action != "maintenance.cancel" &&
		action != "diagnostic.download" && action != "diagnostic.delete" {
		return fmt.Errorf("operationAction: %w", ErrInvalidRequest)
	}
	if strings.TrimSpace(reason) == "" || len(reason) > 512 || !isSafeOperationText(reason) {
		return fmt.Errorf("operationReason: %w", ErrInvalidRequest)
	}
	return nil
}

func ValidateTrustedServerOperation(actorType, action, reason string) error {
	if actorType != "local_operator" && actorType != "system" {
		return fmt.Errorf("trustedActor: %w", ErrInvalidRequest)
	}
	err := validateServerOperationFields(action, reason)
	if err != nil {
		return fmt.Errorf("trustedFields: %w", err)
	}
	return nil
}

// BeginTrustedServerOperation is a trusted runtime entry point. operationID may
// be supplied by the scheduler so its acceptance and durable countdown agree.
func (e *Service) BeginTrustedServerOperation(ctx context.Context, actorType, action, reason, operationID string) (string, error) {
	if ctx == nil {
		return "", errors.New("nil context")
	}
	err := ValidateTrustedServerOperation(actorType, action, reason)
	if err != nil {
		return "", fmt.Errorf("trustedValidate: %w", err)
	}
	store, isSupported := e.store.(TrustedServerOperationStore)
	if !isSupported {
		return "", errors.New("trusted server operation audit unavailable")
	}
	id, err := store.BeginTrustedServerOperation(ctx, actorType, action, reason, operationID)
	if err != nil {
		return "", fmt.Errorf("trustedAccept: %w", err)
	}
	return id, nil
}

func ValidateServerOutcome(safeOutcome string) error {
	if safeOutcome == "" || len(safeOutcome) > 256 || !isSafeOperationText(safeOutcome) {
		return ErrInvalidRequest
	}
	return nil
}

func isSafeOperationText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, character := range text {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

// BeginServerOperation authorizes current persisted administrator access and
// records accepted intent before an external configuration write or broadcast.
func (e *Service) BeginServerOperation(ctx context.Context, actor Actor, action, reason string) (string, error) {
	if ctx == nil {
		return "", errors.New("nil context")
	}
	err := ValidateServerOperation(actor, action, reason)
	if err != nil {
		return "", fmt.Errorf("operationValidate: %w", err)
	}
	store, isSupported := e.store.(ServerOperationStore)
	if !isSupported {
		return "", errors.New("server operation audit unavailable")
	}
	operationID, err := store.BeginServerOperation(ctx, actor, action, reason)
	if err != nil {
		return "", fmt.Errorf("operationAccept: %w", err)
	}
	return operationID, nil
}

// CompleteServerOperation is a trusted runtime callback. safeOutcome contains
// bounded allowlisted result metadata, never error text or request contents.
func (e *Service) CompleteServerOperation(ctx context.Context, operationID string, isSuccess bool, safeOutcome string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := ValidateServerOutcome(safeOutcome)
	if err != nil {
		return fmt.Errorf("outcomeValidate: %w", err)
	}
	store, isSupported := e.store.(ServerOperationStore)
	if !isSupported {
		return errors.New("server operation audit unavailable")
	}
	err = store.CompleteServerOperation(ctx, operationID, isSuccess, safeOutcome)
	if err != nil {
		return fmt.Errorf("operationComplete: %w", err)
	}
	return nil
}
