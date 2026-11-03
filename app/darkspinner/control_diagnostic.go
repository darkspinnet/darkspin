package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/darkspinnet/darkspin/server/diagnostic"
)

type RemoteControlDiagnostic struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type RemoteControlDiagnosticList struct {
	Files []RemoteControlDiagnostic `json:"files"`
	Next  string                    `json:"next"`
}

type RemoteControlDiagnosticDownload struct {
	Path string `json:"path"`
}

func (e *App) ListRemoteControlDiagnostics(serverAddress, identity, kind, after string) (RemoteControlDiagnosticList, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlDiagnosticList{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/diagnostics/list", map[string]any{
		"kind": kind, "after": after, "limit": 25,
	}, &result)
	if err != nil {
		return RemoteControlDiagnosticList{}, fmt.Errorf("diagnosticList: %w", err)
	}
	return result, nil
}

func (e *App) DeleteRemoteControlDiagnostic(serverAddress, identity, kind, name string) error {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := struct {
		IsDeleted bool `json:"is_deleted"`
	}{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/diagnostics/delete", map[string]string{
		"kind": kind, "name": name,
	}, &result)
	if err != nil {
		return fmt.Errorf("diagnosticDelete: %w", err)
	}
	if !result.IsDeleted {
		return errors.New("server did not confirm diagnostic deletion")
	}
	return nil
}

// DownloadRemoteControlDiagnostic saves a bounded server file locally. Neither
// native dialogs nor diagnostic contents cross the launcher UI bridge.
func (e *App) DownloadRemoteControlDiagnostic(serverAddress, identity, kind, name string) (RemoteControlDiagnosticDownload, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	address, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticAddress: %w", err)
	}
	if e.controlPanel.token == "" || e.controlPanel.address != address || e.controlPanel.identity != strings.TrimSpace(identity) {
		return RemoteControlDiagnosticDownload{}, errors.New("panel session does not match the selected account")
	}
	if !time.Now().Before(e.controlPanel.expiresAt) {
		e.controlPanel = remoteControlSession{}
		return RemoteControlDiagnosticDownload{}, errors.New("controlPanel:unauthorized")
	}
	if (kind != "log" && kind != "bug" && kind != "snapshot") || name == "" || len(name) > 512 ||
		!filepath.IsLocal(name) || strings.Contains(name, "\\") || path.Clean(name) != name {
		return RemoteControlDiagnosticDownload{}, errors.New("controlPanel:invalid_request")
	}
	ctx := e.lifecycleCtx
	if ctx == nil {
		ctx = context.TODO()
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	payload, err := json.Marshal(map[string]string{"kind": kind, "name": name})
	if err != nil {
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticMarshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/api/control/admin/diagnostics/download", bytes.NewReader(payload))
	if err != nil {
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticRequest: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Darkspin-Client-Version", Version)
	req.Header.Set("Authorization", "Bearer "+e.controlPanel.token)
	client := remoteHTTPClient(60 * time.Second)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticSend: %w", err)
	}
	defer closeDiagnosticResponse(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err = remoteControlResponseError(response)
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			e.controlPanel = remoteControlSession{}
		}
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticResponse: %w", err)
	}
	if response.ContentLength > diagnostic.MaxDownloadSize {
		return RemoteControlDiagnosticDownload{}, errors.New("controlPanel:diagnostic_too_large")
	}
	logDirectory, reportDirectory, err := e.reportPaths()
	if err != nil {
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticDirectory: %w", err)
	}
	if logDirectory == "" || reportDirectory == "" {
		return RemoteControlDiagnosticDownload{}, errors.New("launcher report directories are unavailable")
	}
	directory := filepath.Join(reportDirectory, "server-downloads")
	err = os.MkdirAll(directory, 0o700)
	if err != nil {
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticMkdir: %w", err)
	}
	// Each download gets its own directory, preserving the report's extension
	// and split-archive filename without overwriting an earlier download.
	directory, err = os.MkdirTemp(directory, reportSlug(address)+"-*")
	if err != nil {
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticTemp: %w", err)
	}
	filename := "_" + strings.Map(diagnosticFilenameRune, path.Base(name))
	destination := filepath.Join(directory, filename)
	err = saveDiagnosticDownload(response, destination)
	if err != nil {
		removeErr := os.Remove(directory)
		if removeErr != nil {
			e.log(fmt.Sprintf("Diagnostic download directory cleanup failed: %v", removeErr))
		}
		return RemoteControlDiagnosticDownload{}, fmt.Errorf("diagnosticSave: %w", err)
	}
	return RemoteControlDiagnosticDownload{Path: destination}, nil
}

func diagnosticFilenameRune(character rune) rune {
	if unicode.IsControl(character) || strings.ContainsRune(`<>:"/\|?*`, character) {
		return '_'
	}
	return character
}

func saveDiagnosticDownload(response *http.Response, destination string) error {
	w, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("downloadCreate: %w", err)
	}
	count, copyErr := io.Copy(w, io.LimitReader(response.Body, diagnostic.MaxDownloadSize+1))
	closeErr := w.Close()
	if copyErr == nil && closeErr == nil && count <= diagnostic.MaxDownloadSize &&
		(response.ContentLength < 0 || count == response.ContentLength) {
		return nil
	}
	removeErr := os.Remove(destination)
	if removeErr != nil {
		return fmt.Errorf("downloadCleanup: %w", removeErr)
	}
	if copyErr != nil {
		return fmt.Errorf("downloadRead: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("downloadClose: %w", closeErr)
	}
	if count > diagnostic.MaxDownloadSize {
		return errors.New("controlPanel:diagnostic_too_large")
	}
	return errors.New("diagnostic download was incomplete")
}

func closeDiagnosticResponse(body io.ReadCloser) {
	err := body.Close()
	if err != nil {
		// The response is no longer used; its close failure cannot be retried.
		return
	}
}
