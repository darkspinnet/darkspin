//go:build scenario

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/content"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

type scenarioWebThumbnail struct {
	HeroNoun uint64 `json:"hero_noun"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// prepareScenarioWeb supplies the same server-owned static prerequisites as
// ordinary launcher preparation. It does not observe client loading or play.
func prepareScenarioWeb(ctx context.Context, req desktop.StartRequest, runtimePath string) (scenario.Artifact, error) {
	if ctx == nil {
		return scenario.Artifact{}, errors.New("scenario web preparation requires context")
	}
	err := ctx.Err()
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("webContext: %w", err)
	}
	if !filepath.IsAbs(runtimePath) || !strings.EqualFold(filepath.Clean(runtimePath),
		filepath.Join(req.Paths.CacheDirectory, "server")) {
		return scenario.Artifact{}, errors.New("scenario web runtime is outside the allocated server directory")
	}
	err = req.Definition.Validate()
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("webDefinition: %w", err)
	}
	cachePath := filepath.Join(runtimePath, "cache")
	err = desktop.CheckAncestors(cachePath)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("webAncestors: %w", err)
	}
	webPath := filepath.Join(cachePath, "www")
	webFI, err := os.Lstat(webPath)
	if err == nil {
		return scenario.Artifact{}, fmt.Errorf("scenario web cache already exists: %s", webFI.Name())
	}
	if !errors.Is(err, os.ErrNotExist) {
		return scenario.Artifact{}, fmt.Errorf("webFresh: %w", err)
	}
	staticPath := filepath.Join(webPath, "static")
	err = content.PrepareWeb(ctx, content.WebOptions{GamePath: req.GameDirectory, StaticPath: staticPath})
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("webPrepare: %w", err)
	}
	thumbnails := make([]scenarioWebThumbnail, 0, len(req.Definition.HeroIDs))
	for index, heroNoun := range req.Definition.HeroIDs {
		thumbnail, thumbnailErr := scenarioWebThumbnailEvidence(ctx, staticPath, heroNoun)
		if thumbnailErr != nil {
			return scenario.Artifact{}, fmt.Errorf("webThumbnail[%d]: %w", index, thumbnailErr)
		}
		thumbnails = append(thumbnails, thumbnail)
	}
	response := struct {
		SchemaVersion              uint32                 `json:"schema_version"`
		RunID                      string                 `json:"run_id"`
		PreparedAt                 time.Time              `json:"prepared_at"`
		Preparation                string                 `json:"preparation"`
		StaticPath                 string                 `json:"static_path"`
		Thumbnails                 []scenarioWebThumbnail `json:"thumbnails"`
		IsSuppliedDevelopmentSetup bool                   `json:"is_supplied_development_setup"`
		IsClientLoadObserved       bool                   `json:"is_client_load_observed"`
	}{SchemaVersion: scenario.SchemaVersion, RunID: req.RunID, PreparedAt: time.Now().UTC(),
		Preparation: "content.PrepareWeb", StaticPath: staticPath, Thumbnails: thumbnails,
		IsSuppliedDevelopmentSetup: true}
	payload, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("webMarshal: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("webPersistContext: %w", err)
	}
	artifact, err := writeScenarioHostArtifact(req.Paths.ReportDirectory, "web-setup.json", "web_setup", append(payload, '\n'))
	if err != nil {
		return artifact, fmt.Errorf("webPersist: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return artifact, fmt.Errorf("webFinishContext: %w", err)
	}
	return artifact, nil
}

func scenarioWebThumbnailEvidence(ctx context.Context, staticPath string, heroNoun uint64) (scenarioWebThumbnail, error) {
	path := filepath.Join(staticPath, "template_png", fmt.Sprintf("%d_thumb.png", heroNoun))
	err := desktop.CheckRegular(path)
	if err != nil {
		return scenarioWebThumbnail{}, fmt.Errorf("thumbnailInput: %w", err)
	}
	r, err := os.Open(path)
	if err != nil {
		return scenarioWebThumbnail{}, fmt.Errorf("thumbnailOpen: %w", err)
	}
	fi, statErr := r.Stat()
	header := make([]byte, 8)
	count, readErr := r.ReadAt(header, 0)
	closeErr := r.Close()
	if statErr != nil || readErr != nil || closeErr != nil {
		return scenarioWebThumbnail{}, fmt.Errorf("thumbnailRead: %w", errors.Join(statErr, readErr, closeErr))
	}
	if !fi.Mode().IsRegular() || fi.Size() <= 8 || fi.Size() > 1024*1024 || count != len(header) ||
		!bytes.Equal(header, []byte{'\x89', 'P', 'N', 'G', '\r', '\n', '\x1a', '\n'}) {
		return scenarioWebThumbnail{}, errors.New("selected scenario thumbnail is not a bounded regular PNG")
	}
	artifact, err := desktop.HashArtifact(ctx, "hero_thumbnail", path)
	if err != nil {
		return scenarioWebThumbnail{}, fmt.Errorf("thumbnailHash: %w", err)
	}
	currentFI, err := os.Lstat(path)
	if err != nil {
		return scenarioWebThumbnail{}, fmt.Errorf("thumbnailRestat: %w", err)
	}
	if !currentFI.Mode().IsRegular() || !os.SameFile(fi, currentFI) || fi.Size() != currentFI.Size() ||
		!fi.ModTime().Equal(currentFI.ModTime()) {
		return scenarioWebThumbnail{}, errors.New("selected scenario thumbnail changed while hashing")
	}
	return scenarioWebThumbnail{HeroNoun: heroNoun, Path: artifact.Path, Size: fi.Size(), SHA256: artifact.SHA256}, nil
}
