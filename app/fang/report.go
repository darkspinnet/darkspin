//go:build windows && cgo

package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/chat/local"
)

type remoteBugCapture struct {
	mu          sync.Mutex
	description string
	capturedAt  time.Time
}

var remoteBugReporter remoteBugCapture

//export GoFangReportBug
func GoFangReportBug(commandText *C.char) {
	command := C.GoString(commandText)
	separator := strings.IndexAny(command, " \t\r\n")
	if separator < 0 {
		return
	}
	description := strings.TrimSpace(command[separator:])
	if description == "" {
		return
	}
	serverAddress := os.Getenv("DARKSPIN_SERVER_ADDRESS")
	host, port, err := net.SplitHostPort(serverAddress)
	if err != nil || port == "" {
		return
	}
	ip := net.ParseIP(host)
	isRemoteServer := !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback())
	switch os.Getenv("DARKSPIN_REMOTE_SERVER") {
	case "1":
		isRemoteServer = true
	case "0":
		isRemoteServer = false
	}
	if !isRemoteServer {
		return
	}
	tracePath := os.Getenv("DARKSPIN_CLIENT_TRACE")
	if !filepath.IsAbs(tracePath) || filepath.Base(filepath.Dir(tracePath)) != "traces" {
		log.Print("Remote bug report capture unavailable: local client trace path is not configured")
		return
	}
	// Native chat conversion can visit the same submitted text more than once.
	// Keep one local capture for those adjacent observations.
	remoteBugReporter.mu.Lock()
	now := time.Now()
	if remoteBugReporter.description == description && now.Sub(remoteBugReporter.capturedAt) < 2*time.Second {
		remoteBugReporter.mu.Unlock()
		return
	}
	remoteBugReporter.description = description
	remoteBugReporter.capturedAt = now
	remoteBugReporter.mu.Unlock()
	runtimePath := filepath.Dir(filepath.Dir(filepath.Dir(tracePath)))
	go captureRemoteBug(runtimePath, serverAddress, description)
}

func captureRemoteBug(runtimePath, serverAddress, description string) {
	reporter := local.NewBugReporter(runtimePath)
	name, err := reporter.Report(context.Background(), chat.BugCommand{
		ServerAddress: serverAddress, IsRemoteConnection: true,
		Description:  description,
		ContextError: "Remote server gameplay context is captured separately on the host; this archive contains local client diagnostics.",
	})
	if err != nil {
		log.Printf("Remote bug report capture failed: %v", err)
		return
	}
	log.Printf("Local remote-session bug report queued: %s", filepath.Join(runtimePath, "bugs", name))
}
