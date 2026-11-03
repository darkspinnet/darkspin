// Package diagnostic owns administrator access to existing runtime diagnostics.
package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

const MaxDownloadSize = 64 << 20

var (
	ErrInvalidRequest = errors.New("invalid diagnostic request")
	ErrNotFound       = errors.New("diagnostic file not found")
	ErrUnavailable    = errors.New("diagnostic storage unavailable")
	ErrForbidden      = errors.New("diagnostic access denied")
	ErrTooLarge       = errors.New("diagnostic limit exceeded")
	ErrConflict       = errors.New("diagnostic operation already in progress")
)

type File struct {
	Name       string
	Size       int64
	ModifiedAt time.Time
}

type ListRequest struct {
	Kind  string
	After string
	Limit int
}

type FileRequest struct {
	Kind string
	Name string
}

type List struct {
	Files []File
	Next  string
}

type Download struct {
	File    File
	Content []byte
}

type Store interface {
	List(context.Context, ListRequest) (List, error)
	Download(context.Context, FileRequest) (Download, error)
	Delete(context.Context, FileRequest) error
}

type Audit interface {
	Authorize(context.Context, int64, int64, bool) (accountaccess.Access, error)
	BeginServerOperation(context.Context, accountaccess.Actor, string, string) (string, error)
	CompleteServerOperation(context.Context, string, bool, string) error
}

type Service struct {
	store Store
	audit Audit
	mu    sync.Mutex
}

func New(store Store, audit Audit) *Service {
	return &Service{store: store, audit: audit}
}

// ValidateName accepts only a bounded, canonical relative slash path. The same
// restriction keeps a complete file reference within the audit reason limit.
func ValidateName(name string) error {
	if name == "" || len(name) > 480 || !utf8.ValidString(name) ||
		strings.ContainsAny(name, "\\:") || strings.HasPrefix(name, "/") ||
		path.Clean(name) != name || name == "." {
		return ErrInvalidRequest
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return ErrInvalidRequest
		}
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return ErrInvalidRequest
		}
	}
	return nil
}

func ValidateKind(kind string) error {
	if kind != "log" && kind != "bug" {
		return ErrInvalidRequest
	}
	return nil
}

func (e *Service) authorize(ctx context.Context, actor accountaccess.Actor) error {
	if ctx == nil {
		return ErrInvalidRequest
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("requestContext: %w", err)
	}
	if e.store == nil || e.audit == nil {
		return ErrUnavailable
	}
	access, err := e.audit.Authorize(ctx, actor.UserID, actor.Revision, true)
	if err != nil {
		if errors.Is(err, accountaccess.ErrAccessDenied) || errors.Is(err, accountaccess.ErrNotFound) {
			return fmt.Errorf("actorAccess: %w", ErrForbidden)
		}
		return fmt.Errorf("actorRead: %w", errors.Join(ErrUnavailable, err))
	}
	if access.UserID != actor.UserID || access.Revision != actor.Revision || !access.IsAdmin() {
		return ErrForbidden
	}
	return nil
}

func (e *Service) List(ctx context.Context, actor accountaccess.Actor, req ListRequest) (List, error) {
	err := e.authorize(ctx, actor)
	if err != nil {
		return List{}, fmt.Errorf("listAuthorize: %w", err)
	}
	err = ValidateKind(req.Kind)
	if err != nil {
		return List{}, fmt.Errorf("listKind: %w", err)
	}
	if req.After != "" {
		err = ValidateName(req.After)
		if err != nil {
			return List{}, fmt.Errorf("listCursor: %w", err)
		}
	}
	if req.Limit == 0 {
		req.Limit = 100
	}
	if req.Limit < 1 || req.Limit > 100 {
		return List{}, ErrInvalidRequest
	}
	if !e.mu.TryLock() {
		return List{}, ErrConflict
	}
	defer e.mu.Unlock()
	result, err := e.store.List(ctx, req)
	if err != nil {
		return List{}, fmt.Errorf("listFiles: %w", err)
	}
	return result, nil
}

func (e *Service) begin(ctx context.Context, actor accountaccess.Actor, req FileRequest, action string) (string, error) {
	err := e.authorize(ctx, actor)
	if err != nil {
		return "", fmt.Errorf("fileAuthorize: %w", err)
	}
	err = ValidateKind(req.Kind)
	if err != nil {
		return "", fmt.Errorf("fileKind: %w", err)
	}
	err = ValidateName(req.Name)
	if err != nil {
		return "", fmt.Errorf("fileName: %w", err)
	}
	operationID, err := e.audit.BeginServerOperation(ctx, actor, action, req.Kind+":"+req.Name)
	if err != nil {
		if errors.Is(err, accountaccess.ErrAccessDenied) || errors.Is(err, accountaccess.ErrNotFound) {
			return "", fmt.Errorf("fileAccept: %w", ErrForbidden)
		}
		return "", fmt.Errorf("fileAudit: %w", errors.Join(ErrUnavailable, err))
	}
	return operationID, nil
}

func (e *Service) complete(ctx context.Context, operationID string, operationErr error) error {
	outcome := "file operation completed"
	if operationErr != nil {
		outcome = "file operation failed"
	}
	// Record the outcome even when the requesting client disconnected.
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := e.audit.CompleteServerOperation(auditCtx, operationID, operationErr == nil, outcome)
	if err != nil {
		return fmt.Errorf("fileOutcome: %w", errors.Join(ErrUnavailable, err, operationErr))
	}
	if operationErr != nil {
		return fmt.Errorf("fileOperation: %w", operationErr)
	}
	return nil
}

func (e *Service) Download(ctx context.Context, actor accountaccess.Actor, req FileRequest) (Download, error) {
	if !e.mu.TryLock() {
		return Download{}, ErrConflict
	}
	defer e.mu.Unlock()
	operationID, err := e.begin(ctx, actor, req, "diagnostic.download")
	if err != nil {
		return Download{}, fmt.Errorf("downloadBegin: %w", err)
	}
	result, operationErr := e.store.Download(ctx, req)
	if operationErr == nil && (len(result.Content) > MaxDownloadSize || result.File.Size > MaxDownloadSize) {
		operationErr = ErrTooLarge
	}
	err = e.complete(ctx, operationID, operationErr)
	if err != nil {
		return Download{}, fmt.Errorf("downloadComplete: %w", err)
	}
	return result, nil
}

func (e *Service) Delete(ctx context.Context, actor accountaccess.Actor, req FileRequest) error {
	if !e.mu.TryLock() {
		return ErrConflict
	}
	defer e.mu.Unlock()
	operationID, err := e.begin(ctx, actor, req, "diagnostic.delete")
	if err != nil {
		return fmt.Errorf("deleteBegin: %w", err)
	}
	operationErr := e.store.Delete(ctx, req)
	err = e.complete(ctx, operationID, operationErr)
	if err != nil {
		return fmt.Errorf("deleteComplete: %w", err)
	}
	return nil
}
