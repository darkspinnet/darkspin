package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	server "github.com/darkspinnet/darkspin/server/runtime"
)

type reportClient struct {
	Mode     string   `json:"mode"`
	IsModded bool     `json:"is_modded"`
	Profile  string   `json:"profile,omitempty"`
	LaunchID string   `json:"launch_id,omitempty"`
	ModNames []string `json:"mod_names,omitempty"`
}

type launcherReportSession struct {
	client reportClient
	server *server.Server
}

func modReportNames() []string {
	return []string{"spore-creature-import@3", "fang-dbbf-stream@1", "fang-bound-baseline@3", "fang-bound-material@1", "spore-creature-audio@1", "fang-bound-scale@1", "spore-dance-audio@1", "fang-bound-fit@6", "fang-bound-portrait@1", "fang-bound-voice@1", "spore-editor-music@2", "spore-emote@3", "fang-noun-appearance@1", "fang-creature-editor@9", "fang-editor-scene@9", "fang-parts-palette@12", "fang-editor-preview@4"}
}

func (a *App) recordReportSession(req gameLaunchRequest, launchID string) {
	session := req.reportSession
	if session.client.Mode == "" {
		session.client = reportClient{Mode: "standard", Profile: req.account}
	}
	session.client.LaunchID = launchID
	a.mu.Lock()
	a.reportSession = session
	a.mu.Unlock()
	err := a.saveReportClient(session.client)
	if err != nil {
		a.log(fmt.Sprintf("Unable to persist bug-report client context: %v", err))
	}
}

func (a *App) saveReportClient(client reportClient) error {
	payload, err := json.MarshalIndent(client, "", "  ")
	if err != nil {
		return fmt.Errorf("clientMarshal: %w", err)
	}
	path := filepath.Join(a.gameDirectory(), "darkspin", "logs", "client-session.json")
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("clientMkdir: %w", err)
	}
	err = replaceLauncherConfig(path, append(payload, '\n'))
	if err != nil {
		return fmt.Errorf("clientWrite: %w", err)
	}
	return nil
}

func (a *App) readReportSession() (launcherReportSession, error) {
	a.mu.Lock()
	session := a.reportSession
	a.mu.Unlock()
	if session.client.Mode != "" {
		return session, nil
	}
	session.client.Mode = "unknown"
	path := filepath.Join(a.gameDirectory(), "darkspin", "logs", "client-session.json")
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return session, nil
	}
	if err != nil {
		return session, fmt.Errorf("clientRead: %w", err)
	}
	var client reportClient
	err = json.Unmarshal(payload, &client)
	if err != nil {
		return session, fmt.Errorf("clientDecode: %w", err)
	}
	session.client = client
	return session, nil
}

func modReportSources(logPath string, sources []reportSource) ([]reportSource, error) {
	modSources, err := reportSources(filepath.Join(filepath.Dir(logPath), "mod", "logs"))
	if err != nil {
		return nil, fmt.Errorf("modSources: %w", err)
	}
	filteredSources := make([]reportSource, 0, len(sources)+len(modSources))
	for _, source := range sources {
		if source.name == "logs/traces/server.jsonl" || source.name == "logs/chat.log" {
			continue
		}
		filteredSources = append(filteredSources, source)
	}
	for _, source := range modSources {
		source.name = "mod/" + source.name
		filteredSources = append(filteredSources, source)
	}
	return filteredSources, nil
}
