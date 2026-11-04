package main

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/chat/local"
)

// The existing game supervisor owns this worker for the entire client lifetime,
// including remote and detached launches. No App, window, or local server is
// required. Only this launch's random mailbox identity can request collection.
type clientBugCapture struct {
	launchID      string
	directory     string
	runtimePath   string
	serverAddress string
	cancel        context.CancelFunc
	done          chan struct{}
}

func startClientBugCapture(ctx context.Context, serverAddress string) *clientBugCapture {
	host, port, err := net.SplitHostPort(serverAddress)
	if err != nil || port == "" {
		return nil
	}
	ip := net.ParseIP(host)
	isRemote := !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback())
	switch os.Getenv(remoteServerEnvironment) {
	case "1":
		isRemote = true
	case "0":
		isRemote = false
	}
	if !isRemote {
		return nil
	}
	launchID := os.Getenv(clientLaunchEnvironment)
	resultPath := os.Getenv(clientResultEnvironment)
	tracePath := os.Getenv("DARKSPIN_CLIENT_TRACE")
	if !clientLaunchIDPattern.MatchString(launchID) || !filepath.IsAbs(resultPath) || !filepath.IsAbs(tracePath) || filepath.Base(filepath.Dir(tracePath)) != "traces" {
		log.Print("Remote bug capture unavailable: owned client mailbox or trace path is missing")
		return nil
	}
	lifetime, cancel := context.WithCancel(ctx)
	capture := &clientBugCapture{launchID: launchID, directory: filepath.Dir(resultPath), runtimePath: filepath.Dir(filepath.Dir(filepath.Dir(tracePath))), serverAddress: serverAddress, cancel: cancel, done: make(chan struct{})}
	go capture.run(lifetime)
	return capture
}

func (e *clientBugCapture) stop() {
	if e == nil {
		return
	}
	e.cancel()
	<-e.done
}

func (e *clientBugCapture) run(ctx context.Context) {
	defer close(e.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		e.scan(ctx)
		select {
		case <-ctx.Done():
			e.scan(ctx)
			return
		case <-ticker.C:
		}
	}
}

func (e *clientBugCapture) scan(ctx context.Context) {
	paths, err := filepath.Glob(filepath.Join(e.directory, e.launchID+".bug.*.txt"))
	if err != nil {
		log.Printf("Client bug mailbox scan failed: %v", err)
		return
	}
	for _, path := range paths {
		fi, statErr := os.Stat(path)
		if statErr != nil || fi.Size() > 8192 {
			continue
		}
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		lines := strings.SplitN(string(payload), "\n", 2)
		if len(lines) != 2 {
			continue
		}
		pid, parseErr := strconv.ParseUint(lines[0], 10, 32)
		if parseErr != nil || pid == 0 {
			continue
		}
		description := strings.TrimSpace(lines[1])
		if description == "" {
			continue
		}
		err = os.Remove(path)
		if err != nil {
			log.Printf("Client bug mailbox consume failed: %v", err)
			continue
		}
		reporter := local.NewBugReporter(e.runtimePath)
		reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		name, reportErr := reporter.Report(reportCtx, chat.BugCommand{ServerAddress: e.serverAddress, IsRemoteConnection: true, Description: description,
			ContextError: "Remote server gameplay context is captured separately on the host; this archive contains local client diagnostics."})
		cancel()
		if reportErr != nil {
			log.Printf("Remote bug report capture failed: %v", reportErr)
			continue
		}
		log.Printf("Local remote-session bug report queued: %s", filepath.Join(e.runtimePath, "bugs", name))
	}
}
