//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"
)

const webkitDisableDMABUFRendererEnvironment = "WEBKIT_DISABLE_DMABUF_RENDERER"

func configureWebViewEnvironment() error {
	_, isDMABUFConfigured := os.LookupEnv(webkitDisableDMABUFRendererEnvironment)
	if isDMABUFConfigured || !isWaylandSession() {
		return nil
	}
	err := os.Setenv(webkitDisableDMABUFRendererEnvironment, "1")
	if err != nil {
		return fmt.Errorf("dmabufDisable: %w", err)
	}
	return nil
}

func isWaylandSession() bool {
	sessionType := strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE"))
	waylandDisplay := strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY"))
	return strings.EqualFold(sessionType, "wayland") || waylandDisplay != ""
}
