package main

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type launcherPresentation interface {
	EmitStatus(context.Context, LauncherStatus)
	OpenURL(context.Context, string) error
	Quit(context.Context)
}

type wailsPresentation struct{}

func (wailsPresentation) EmitStatus(ctx context.Context, status LauncherStatus) {
	runtime.EventsEmit(ctx, "darkspinner:status", status)
}

func (wailsPresentation) OpenURL(ctx context.Context, address string) error {
	runtime.BrowserOpenURL(ctx, address)
	return nil
}

func (wailsPresentation) Quit(ctx context.Context) {
	runtime.Quit(ctx)
}

func (e *App) openURL(address string) error {
	if e.ctx == nil {
		return errors.New("launcher is not ready")
	}
	return e.presentation.OpenURL(e.ctx, address)
}

func (e *App) quit() {
	if e.ctx != nil {
		e.presentation.Quit(e.ctx)
	}
}
