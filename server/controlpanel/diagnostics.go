package controlpanel

import (
	"context"
	"errors"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/diagnostic"
)

type Diagnostic interface {
	List(context.Context, accountaccess.Actor, diagnostic.ListRequest) (diagnostic.List, error)
	Download(context.Context, accountaccess.Actor, diagnostic.FileRequest) (diagnostic.Download, error)
	Delete(context.Context, accountaccess.Actor, diagnostic.FileRequest) error
}

type diagnosticFileDTO struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

func (e *Service) diagnosticOperation(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if e.config.Diagnostic == nil {
		writeErrorCode(writer, http.StatusNotFound, "diagnostic_unavailable", "Diagnostics are unavailable.")
		return
	}
	if req.URL.Path == "/api/control/admin/diagnostics/list" {
		e.listDiagnostics(writer, req, actor)
		return
	}
	command := struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeDiagnosticError(writer, diagnostic.ErrInvalidRequest)
		return
	}
	commandRequest := diagnostic.FileRequest{Kind: command.Kind, Name: command.Name}
	if req.URL.Path == "/api/control/admin/diagnostics/download" {
		e.downloadDiagnostic(writer, req, actor, commandRequest)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	err = e.config.Diagnostic.Delete(ctx, actor, commandRequest)
	if err != nil {
		writeDiagnosticError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		IsDeleted bool `json:"is_deleted"`
	}{IsDeleted: true})
}

func (e *Service) listDiagnostics(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	command := struct {
		Kind  string `json:"kind"`
		After string `json:"after"`
		Limit int    `json:"limit"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeDiagnosticError(writer, diagnostic.ErrInvalidRequest)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	result, err := e.config.Diagnostic.List(ctx, actor, diagnostic.ListRequest{
		Kind: command.Kind, After: command.After, Limit: command.Limit,
	})
	if err != nil {
		writeDiagnosticError(writer, err)
		return
	}
	files := make([]diagnosticFileDTO, 0, len(result.Files))
	for _, file := range result.Files {
		files = append(files, diagnosticFileDTO{Name: file.Name, Size: file.Size, ModifiedAt: file.ModifiedAt.UTC()})
	}
	writeJSON(writer, http.StatusOK, struct {
		Files []diagnosticFileDTO `json:"files"`
		Next  string              `json:"next"`
	}{Files: files, Next: result.Next})
}

func (e *Service) downloadDiagnostic(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor, command diagnostic.FileRequest) {
	// Keep at most one bounded file response in memory until transmission ends.
	if !e.diagnosticDownloadMu.TryLock() {
		writeDiagnosticError(writer, diagnostic.ErrConflict)
		return
	}
	defer e.diagnosticDownloadMu.Unlock()
	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	result, err := e.config.Diagnostic.Download(ctx, actor, command)
	if err != nil {
		writeDiagnosticError(writer, err)
		return
	}
	name := path.Base(strings.ReplaceAll(result.File.Name, "\\", "/"))
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	writer.Header().Set("Content-Length", strconv.Itoa(len(result.Content)))
	written, err := writer.Write(result.Content)
	if err != nil {
		log.Printf("control panel diagnostic download interrupted after %d bytes", written)
	}
}

func writeDiagnosticError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, diagnostic.ErrInvalidRequest):
		writeErrorCode(writer, http.StatusBadRequest, "diagnostic_invalid_request", "Check the diagnostic file request.")
	case errors.Is(err, diagnostic.ErrForbidden), errors.Is(err, accountaccess.ErrAccessDenied):
		writeErrorCode(writer, http.StatusForbidden, "forbidden", "This operation is not permitted.")
	case errors.Is(err, diagnostic.ErrNotFound):
		writeErrorCode(writer, http.StatusNotFound, "diagnostic_not_found", "The diagnostic file is no longer available.")
	case errors.Is(err, diagnostic.ErrTooLarge):
		writeErrorCode(writer, http.StatusRequestEntityTooLarge, "diagnostic_too_large", "The diagnostic file is too large to download.")
	case errors.Is(err, diagnostic.ErrConflict):
		writeErrorCode(writer, http.StatusConflict, "diagnostic_conflict", "Another diagnostic operation is in progress or the file changed. Refresh before trying again.")
	default:
		writeErrorCode(writer, http.StatusServiceUnavailable, "diagnostic_unavailable", "Diagnostics are unavailable.")
	}
}
