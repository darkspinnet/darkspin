//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type launcherLua struct {
	mutex          sync.Mutex
	operationMutex sync.Mutex
	options        debugLaunchOptions
	state          LuaConsoleSnapshot
	child          *launcherLuaProcess
	sequence       uint64
}

func newLauncherLua(arguments []string) *launcherLua {
	e := &launcherLua{state: LuaConsoleSnapshot{IsAvailable: true, Phase: "idle", Scripts: []LuaScriptChoice{}, Lines: []LuaConsoleLine{}}, options: debugLaunchOptions{executionLimit: debugLuaExecutionLimit}}
	choices, inventoryErr := launcherLuaChoices()
	e.state.Scripts = choices
	if inventoryErr != nil {
		e.state.InventoryError = inventoryErr.Error()
	}
	filtered := launcherLuaOnlyArguments(arguments)
	if len(filtered) == 0 {
		return e
	}
	e.state.IsSelected = true
	options, err := parseDebugLaunchOptions(filtered)
	if err != nil {
		e.state.Failure = err.Error()
		e.state.Phase = "failed"
		return e
	}
	e.options = options
	e.state.Script = options.scriptName
	return e
}

func launcherLuaOnlyArguments(arguments []string) []string {
	filteredArguments := []string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--lua" || argument == "--lua-timeout" {
			filteredArguments = append(filteredArguments, argument)
			if index+1 < len(arguments) {
				index++
				filteredArguments = append(filteredArguments, arguments[index])
			}
			continue
		}
		if strings.HasPrefix(argument, "--lua=") || strings.HasPrefix(argument, "--lua-timeout=") {
			filteredArguments = append(filteredArguments, argument)
		}
	}
	return filteredArguments
}

func launcherLuaArguments(arguments []string) []string {
	filteredArguments := []string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--lua" || argument == "--lua-timeout" {
			index++
			continue
		}
		if strings.HasPrefix(argument, "--lua=") || strings.HasPrefix(argument, "--lua-timeout=") {
			continue
		}
		filteredArguments = append(filteredArguments, argument)
	}
	return filteredArguments
}

func launcherLuaRequested(arguments []string) bool {
	return len(launcherLuaOnlyArguments(arguments)) != 0
}

func (a *App) startLauncherLua(ctx context.Context) bool {
	if a.lua == nil || !a.lua.state.IsSelected {
		return false
	}
	a.selectLauncherLua()
	go a.startInitialLua(ctx)
	return true
}

func (a *App) selectLauncherLua() {
	a.mu.Lock()
	a.status.State = "lua"
	a.status.Message = "Lua scenario session"
	a.status.IsPlayReady = false
	a.status.IsServerOnline = false
	a.status.IsGameReady = false
	a.status.IsProfileStoreReady = false
	a.status.IsAutoPlayRequested = false
	a.emitStatusLocked()
	a.mu.Unlock()
}

func (a *App) startInitialLua(ctx context.Context) {
	a.lua.mutex.Lock()
	isFailed := a.lua.state.Failure != ""
	scriptName := a.lua.options.scriptName
	a.lua.mutex.Unlock()
	if isFailed || scriptName == "" {
		return
	}
	err := a.RunLuaScript(scriptName)
	if err != nil {
		a.lua.fail(err)
	}
}

func (a *App) RunLuaScript(path string) error {
	e := a.lua
	if e == nil {
		return errors.New("Lua scenarios are unavailable")
	}
	e.operationMutex.Lock()
	defer e.operationMutex.Unlock()
	e.mutex.Lock()
	choices := e.state.Scripts
	inventoryError := e.state.InventoryError
	e.mutex.Unlock()
	if inventoryError != "" {
		return fmt.Errorf("luaInventory: %s", inventoryError)
	}
	var err error
	selectedPath := ""
	for _, choice := range choices {
		if choice.Path == path || choice.Name == path {
			selectedPath = choice.Path
			break
		}
	}
	if selectedPath == "" {
		return errors.New("select an available Lua scenario")
	}
	a.mu.Lock()
	isBusy := a.isShuttingDown || a.gameDone != nil || a.cancel != nil || a.status.IsPatchActive
	initializeDone := a.serviceSet.initializeDone
	a.mu.Unlock()
	if isBusy {
		return errors.New("finish launcher operations and close its game before starting a Lua scenario")
	}
	e.mutex.Lock()
	isSelected := e.state.IsSelected
	child := e.child
	e.mutex.Unlock()
	if !isSelected {
		if initializeDone != nil {
			select {
			case <-initializeDone:
			default:
				return errors.New("wait for launcher preparation before starting Lua mode")
			}
		}
		a.stopServices()
		a.selectLauncherLua()
	}
	if child != nil {
		select {
		case <-child.done:
		default:
			err = child.runScript(a.ctx, selectedPath)
			if err != nil {
				return fmt.Errorf("luaReuse: %w", err)
			}
			return nil
		}
	}
	e.mutex.Lock()
	e.state.IsSelected = true
	e.state.Script = selectedPath
	e.state.Phase = "starting"
	e.state.Failure = ""
	e.state.ElapsedMS = 0
	e.state.RunID = ""
	e.mutex.Unlock()
	err = e.launch(a.ctx, selectedPath)
	if err != nil {
		e.fail(err)
		return fmt.Errorf("luaStart: %w", err)
	}
	return nil
}

func launcherLuaChoices() ([]LuaScriptChoice, error) {
	directory, err := executableDirectory()
	if err != nil {
		return nil, fmt.Errorf("luaDirectory: %w", err)
	}
	root, err := prepareDebugLuaAssets(directory)
	if err != nil {
		return nil, fmt.Errorf("luaInstall: %w", err)
	}
	scripts, err := listDebugScripts(root)
	if err != nil {
		return nil, fmt.Errorf("luaList: %w", err)
	}
	choices := make([]LuaScriptChoice, 0, len(scripts))
	pathsByName := make(map[string]string)
	for _, script := range scripts {
		if script.Status != "source_only" {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(script.Name), filepath.Ext(script.Name))
		key := strings.ToLower(name)
		previous, isDuplicate := pathsByName[key]
		if isDuplicate {
			return nil, fmt.Errorf("duplicate Lua name %q: %s and %s; rename one scenario", name, previous, script.Name)
		}
		pathsByName[key] = script.Name
		choices = append(choices, LuaScriptChoice{Name: name, Path: script.Name})
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Name < choices[j].Name })
	return choices, nil
}

func (a *App) GetLuaConsole() LuaConsoleSnapshot {
	e := a.lua
	if e == nil {
		return LuaConsoleSnapshot{}
	}
	e.refresh(a.ctx)
	e.mutex.Lock()
	defer e.mutex.Unlock()
	state := e.state
	state.Lines = append([]LuaConsoleLine{}, e.state.Lines...)
	return state
}

func (a *App) StopLuaScript() error {
	if a.lua == nil {
		return errors.New("Lua scenarios are unavailable")
	}
	e := a.lua
	e.operationMutex.Lock()
	defer e.operationMutex.Unlock()
	e.mutex.Lock()
	child := e.child
	e.mutex.Unlock()
	if child == nil {
		return nil
	}
	err := child.stop(a.ctx)
	if err != nil {
		return fmt.Errorf("luaStop: %w", err)
	}
	return nil
}

func (a *App) stopLauncherLua() {
	if a.lua == nil {
		return
	}
	err := a.StopLuaScript()
	if err != nil {
		a.log("Lua shutdown: " + err.Error())
	}
}

func (a *App) KillLuaSession() error {
	if a.lua == nil {
		return errors.New("Lua scenarios are unavailable")
	}
	e := a.lua
	e.mutex.Lock()
	child := e.child
	e.mutex.Unlock()
	if child == nil {
		return nil
	}
	err := child.kill(a.ctx)
	if err != nil {
		return fmt.Errorf("luaKill: %w", err)
	}
	return nil
}

func (e *launcherLua) fail(err error) {
	e.mutex.Lock()
	e.state.Failure = err.Error()
	e.state.Phase = "failed"
	e.mutex.Unlock()
	e.appendLine("Failed: " + err.Error())
}

func (e *launcherLua) appendLine(message string) {
	if len(message) > 4096 {
		message = message[:4096] + "…"
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.sequence++
	e.state.Lines = append(e.state.Lines, LuaConsoleLine{Sequence: e.sequence, Time: time.Now().UTC().Format(time.RFC3339Nano), Text: message})
	if len(e.state.Lines) > 1000 {
		e.state.Lines = append([]LuaConsoleLine(nil), e.state.Lines[len(e.state.Lines)-1000:]...)
	}
}
