//go:build scenario

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/darkspinnet/darkspin/server/scenario/control"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

const debugScriptLimit = 256 * 1024

func (e *debugSupervisor) serveScriptRun(w http.ResponseWriter, req *http.Request) {
	var command struct {
		Path string `json:"path"`
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 4096))
	if err == nil {
		err = decodeDebugJSON(payload, &command)
	}
	if err != nil {
		e.respond(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	script, source, err := readDebugScript(e.scriptRoot, command.Path)
	if err != nil || len(source) == 0 {
		e.respond(w, http.StatusBadRequest, map[string]string{"error": "select an available nonempty Lua script"})
		return
	}
	if !e.scriptMutex.TryLock() {
		e.respond(w, http.StatusConflict, map[string]string{"error": "wait for the current script to finish"})
		return
	}
	err = e.registry.BeginScript()
	if err != nil {
		e.scriptMutex.Unlock()
		e.respond(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	select {
	case e.scriptRequests <- script.Name:
		e.automation.Resume()
		e.respond(w, http.StatusAccepted, map[string]string{"status": "running"})
	case <-e.stop:
		e.scriptMutex.Unlock()
		e.respond(w, http.StatusConflict, map[string]string{"error": "session is stopping"})
	}
}

type debugScript struct {
	Name         string `json:"name"`
	SHA256       string `json:"sha256"`
	Size         int    `json:"size"`
	SnapshotPath string `json:"snapshot_path,omitempty"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

func (e *debugSupervisor) prepareScript(ctx context.Context, name string) {
	startedAt := time.Now().UTC()
	scriptDirectory := filepath.Join(e.reportDirectory, "scripts", startedAt.Format("20060102T150405.000000000"))
	script, source, err := readDebugScript(e.scriptRoot, name)
	if err == nil {
		err = os.MkdirAll(scriptDirectory, 0700)
		if err == nil {
			err = desktop.CheckAncestors(e.reportDirectory)
		}
		if err == nil {
			snapshotPath := filepath.Join(scriptDirectory, "source.lua")
			err = os.WriteFile(snapshotPath, source, 0600)
			if err == nil {
				script.SnapshotPath = snapshotPath
			}
		}
	}
	script.Status = "failed"
	result := &control.ScriptResult{Identity: control.ScriptIdentity{RelativePath: name, SHA256: script.SHA256},
		Outcome: control.ScriptFailed, StartedAt: startedAt}
	e.logLuaActivity("Starting " + name)
	if err != nil {
		result.Failure = &control.ScenarioError{Code: control.InvalidRequest, Context: "scriptPrepare", Cause: err}
	} else {
		failure := e.executeLua(ctx, script, source)
		if failure == nil {
			result.Outcome = control.ScriptPassed
			script.Status = "passed"
		} else {
			result.Failure = debugErrorDomain(failure.Failure)
			result.Traceback = failure.Traceback
			if failure.Failure.Code == "interrupted" {
				result.Outcome = control.ScriptInterrupted
				script.Status = "interrupted"
			}
		}
	}
	result.FinishedAt = time.Now().UTC()
	if result.Failure != nil {
		script.Error = result.Failure.Error()
	}
	e.logLuaActivity(fmt.Sprintf("Finished %s: %s in %.2fs", name, result.Outcome, result.FinishedAt.Sub(startedAt).Seconds()))
	if result.Failure != nil {
		e.logLuaActivity(result.Failure.Error())
	}
	if result.Traceback != "" {
		e.logLuaActivity(result.Traceback)
	}
	// The helper has exited, every synchronous operation has returned, and this
	// increment exposes no native queued or held input actions to settle.
	err = e.registry.EnterInspection(control.InspectionRequest{IsActionSettled: true, Result: result, Retention: 30 * time.Minute})
	if err != nil {
		e.logFailure("Scenario inspection transition", err)
	}
	if script.SnapshotPath != "" {
		err = writeDebugJSON(filepath.Join(e.reportDirectory, "script.json"), script)
		if err != nil {
			e.logFailure("Scenario script manifest", err)
		}
		err = writeDebugJSON(filepath.Join(e.reportDirectory, "result.json"), debugStatusDTO(e.registry.Snapshot()).ScriptResult)
		if err != nil {
			e.logFailure("Scenario result manifest", err)
		}
		err = writeDebugJSON(filepath.Join(scriptDirectory, "result.json"), debugStatusDTO(e.registry.Snapshot()).ScriptResult)
		if err != nil {
			e.logFailure("Scenario archived result", err)
		}
	}
}

func (e *debugSupervisor) logLuaActivity(message string) {
	count, err := fmt.Fprintln(os.Stderr, "Lua:", message)
	if err != nil || count == 0 { /* The durable script result remains available through the supervisor. */
	}
}

func (e *debugSupervisor) logFailure(step string, err error) {
	count, outputErr := fmt.Fprintln(os.Stderr, step+":", err)
	if outputErr != nil || count == 0 {
		// Stderr has no consumer; registry state remains available through the API.
	}
}

func readDebugScript(root, name string) (debugScript, []byte, error) {
	script := debugScript{Name: filepath.ToSlash(name)}
	if !filepath.IsLocal(name) || strings.ContainsAny(name, ":\\") || strings.ToLower(filepath.Ext(name)) != ".lua" {
		return script, nil, errors.New("script must be a relative Lua source path beneath the active Lua source directory")
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(name)), "/")
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." || part == "" {
			return script, nil, errors.New("script paths cannot contain traversal or empty path components")
		}
	}
	if strings.EqualFold(parts[0], "definitions") {
		return script, nil, errors.New("Lua definitions cannot be executed as scripts")
	}
	path := filepath.Join(root, name)
	err := desktop.CheckRegular(path)
	if err != nil {
		return script, nil, fmt.Errorf("scriptPath: %w", err)
	}
	r, err := os.Open(path)
	if err != nil {
		return script, nil, fmt.Errorf("scriptOpen: %w", err)
	}
	source, readErr := io.ReadAll(io.LimitReader(r, debugScriptLimit+1))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil {
		return script, nil, fmt.Errorf("scriptRead: %w", errors.Join(readErr, closeErr))
	}
	if len(source) > debugScriptLimit {
		return script, nil, errors.New("Lua source exceeds the 256 KiB limit")
	}
	if !utf8.Valid(source) || strings.IndexByte(string(source), 0) >= 0 {
		return script, nil, errors.New("script must be UTF-8 Lua source without NUL bytes")
	}
	digest := sha256.Sum256(source)
	script.SHA256 = hex.EncodeToString(digest[:])
	script.Size = len(source)
	return script, source, nil
}

func listDebugScripts(root string) ([]debugScript, error) {
	inventory := debugScriptInventory{root: root, scripts: []debugScript{}}
	err := desktop.CheckAncestors(root)
	if errors.Is(err, os.ErrNotExist) {
		return inventory.scripts, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scriptRoot: %w", err)
	}
	err = filepath.WalkDir(root, inventory.visit)
	if err != nil {
		return nil, fmt.Errorf("scriptList: %w", err)
	}
	return inventory.scripts, nil
}

type debugScriptInventory struct {
	root    string
	scripts []debugScript
}

func (e *debugScriptInventory) visit(path string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return fmt.Errorf("inventoryWalk: %w", walkErr)
	}
	if entry.IsDir() {
		if path != e.root && strings.EqualFold(entry.Name(), "definitions") {
			return filepath.SkipDir
		}
		return nil
	}
	if !strings.EqualFold(filepath.Ext(entry.Name()), ".lua") {
		return nil
	}
	if len(e.scripts) >= 1024 {
		return errors.New("script inventory exceeds 1024 source files")
	}
	name, err := filepath.Rel(e.root, path)
	if err != nil {
		return fmt.Errorf("inventoryRelative: %w", err)
	}
	script, source, scriptErr := readDebugScript(e.root, filepath.ToSlash(name))
	if scriptErr != nil {
		script.Status = "unavailable"
		script.Error = scriptErr.Error()
	} else {
		script.Status = "source_only"
		script.Size = len(source)
	}
	e.scripts = append(e.scripts, script)
	return nil
}
