//go:build !scenario

package main

import (
	"context"
	"errors"
)

type launcherLua struct{}

func newLauncherLua(arguments []string) *launcherLua     { return nil }
func launcherLuaArguments(arguments []string) []string   { return arguments }
func launcherLuaRequested(arguments []string) bool       { return false }
func (a *App) startLauncherLua(ctx context.Context) bool { return false }
func (a *App) stopLauncherLua()                          {}
func (a *App) GetLuaConsole() LuaConsoleSnapshot         { return LuaConsoleSnapshot{} }
func (a *App) RunLuaScript(path string) error {
	return errors.New("Lua scenarios are excluded from this build")
}
func (a *App) StopLuaScript() error  { return errors.New("Lua scenarios are excluded from this build") }
func (a *App) KillLuaSession() error { return errors.New("Lua scenarios are excluded from this build") }
