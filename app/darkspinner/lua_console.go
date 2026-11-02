package main

import (
	"fmt"
	appwindow "github.com/darkspinnet/darkspin/window"
)

func acquireLauncherInstance(arguments []string) (func(), bool, error) {
	// A scenario launcher and its private workers share an executable name.
	// Generic peer discovery must not mistake a worker or another launcher's
	// session for the requested new Lua window.
	if launcherLuaRequested(arguments) {
		return releaseLuaLauncherInstance, false, nil
	}
	release, isExisting, err := appwindow.Acquire(appwindow.InstanceOptions{MutexName: `Local\darkspin-darkspinner-single-instance-v1`})
	if err != nil {
		return nil, false, fmt.Errorf("launcherInstance: %w", err)
	}
	return release, isExisting, nil
}

func releaseLuaLauncherInstance() {}

type LuaScriptChoice struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type LuaConsoleLine struct {
	Sequence uint64 `json:"sequence"`
	Time     string `json:"time"`
	Text     string `json:"text"`
}

type LuaConsoleSnapshot struct {
	IsAvailable    bool              `json:"isAvailable"`
	IsSelected     bool              `json:"isSelected"`
	IsActive       bool              `json:"isActive"`
	Phase          string            `json:"phase"`
	Script         string            `json:"script"`
	RunID          string            `json:"runId"`
	ElapsedMS      int64             `json:"elapsedMs"`
	Failure        string            `json:"failure"`
	InventoryError string            `json:"inventoryError"`
	Scripts        []LuaScriptChoice `json:"scripts"`
	Lines          []LuaConsoleLine  `json:"lines"`
}
