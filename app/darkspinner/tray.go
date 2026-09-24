package main

import (
	"context"
	"runtime"

	"fyne.io/systray"
)

type headlessTray struct {
	app     *App
	address string
	cancel  context.CancelFunc
	ctx     context.Context
}

func runHeadlessTray(ctx context.Context, app *App, address string, cancel context.CancelFunc) {
	tray := &headlessTray{app: app, address: address, cancel: cancel, ctx: ctx}
	go tray.quitWhenDone(ctx)
	systray.Run(tray.ready, nil)
}

func (e *headlessTray) ready() {
	icon := darkSpinnerIcon
	if runtime.GOOS == "windows" {
		icon = darkSpinnerTrayIcon
	}
	systray.SetIcon(icon)
	systray.SetTitle("Darkspinner")
	systray.SetTooltip("Darkspinner is running")
	systray.SetOnTapped(e.open)
	openItem := systray.AddMenuItem("Open Darkspinner", "Open the launcher in your browser")
	systray.AddSeparator()
	shutdownItem := systray.AddMenuItem("Shut down Darkspinner", "Stop the game server and exit")
	go e.handleMenu(openItem.ClickedCh, shutdownItem.ClickedCh)
	e.open()
}

func (e *headlessTray) open() {
	err := openSystemBrowser(e.address)
	if err != nil {
		e.app.log("Browser launcher: " + err.Error())
	}
}

func (e *headlessTray) handleMenu(
	opened <-chan struct{}, shutdown <-chan struct{},
) {
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-opened:
			e.open()
		case <-shutdown:
			e.app.beginShutdown()
			e.cancel()
			return
		}
	}
}

func (e *headlessTray) quitWhenDone(ctx context.Context) {
	<-ctx.Done()
	systray.Quit()
}
