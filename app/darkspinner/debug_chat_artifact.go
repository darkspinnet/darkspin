//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

type debugChatArtifactRoot struct {
	Role        string `json:"role"`
	Path        string `json:"path"`
	IsAvailable bool   `json:"is_available"`
	Reason      string `json:"reason,omitempty"`
}

type debugChatArtifact struct {
	Role       string    `json:"role"`
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	ByteSize   int64     `json:"byte_size"`
	ModifiedAt time.Time `json:"modified_at"`
}

type debugChatArtifactSnapshot struct {
	CapturedAt time.Time               `json:"captured_at"`
	Source     string                  `json:"source"`
	Roots      []debugChatArtifactRoot `json:"roots"`
	Files      []debugChatArtifact     `json:"files"`
}

func observeDebugChatArtifacts(ctx context.Context, host *debugHost, owner debugWorldOwner) (debugChatArtifactSnapshot, error) {
	result := debugChatArtifactSnapshot{CapturedAt: time.Now().UTC(), Source: "owned_artifact_metadata",
		Roots: []debugChatArtifactRoot{}, Files: []debugChatArtifact{}}
	roots := []debugChatArtifactRoot{
		{Role: "server_bug", Path: filepath.Join(host.runtimePath, "bugs")},
		{Role: "server_snapshot", Path: filepath.Join(host.server.SnapshotControlDirectory(), "snapshots")},
		{Role: "client_diagnostic", Path: filepath.Dir(owner.client.TracePath)},
	}
	for _, root := range roots {
		if root.Path == "" || root.Path == "." || !filepath.IsAbs(root.Path) ||
			(!isDebugChatChild(host.runtimePath, root.Path) && !isDebugChatChild(host.reportDirectory, root.Path)) {
			root.Reason = "artifact root is unavailable or outside this owned run"
			result.Roots = append(result.Roots, root)
			continue
		}
		err := desktop.CheckAncestors(root.Path)
		if err != nil {
			root.Reason = fmt.Sprintf("artifact root unavailable: %v", err)
			result.Roots = append(result.Roots, root)
			continue
		}
		files, walkErr := readDebugChatArtifacts(ctx, root)
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				root.Reason = "artifact directory has not been created"
				result.Roots = append(result.Roots, root)
				continue
			}
			return result, fmt.Errorf("artifactWalk: %w", walkErr)
		}
		root.IsAvailable = true
		result.Roots = append(result.Roots, root)
		result.Files = append(result.Files, files...)
	}
	return result, nil
}

func isDebugChatChild(parent, path string) bool {
	if parent == "" {
		return false
	}
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func readDebugChatArtifacts(ctx context.Context, root debugChatArtifactRoot) ([]debugChatArtifact, error) {
	files := make([]debugChatArtifact, 0)
	entryCount := 0
	err := filepath.WalkDir(root.Path, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("artifactEntry: %w", walkErr)
		}
		contextErr := ctx.Err()
		if contextErr != nil {
			return fmt.Errorf("artifactContext: %w", contextErr)
		}
		entryCount++
		if entryCount > 4096 {
			return errors.New("owned artifact inventory exceeded 4096 entries")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("owned artifact inventory contains a symbolic link")
		}
		ancestorErr := desktop.CheckAncestors(path)
		if ancestorErr != nil {
			return fmt.Errorf("artifactAncestor: %w", ancestorErr)
		}
		if entry.IsDir() {
			return nil
		}
		fi, statErr := entry.Info()
		if statErr != nil {
			return fmt.Errorf("artifactInfo: %w", statErr)
		}
		if fi.Mode().IsRegular() {
			files = append(files, debugChatArtifact{Role: root.Role, Path: path, Name: entry.Name(), ByteSize: fi.Size(), ModifiedAt: fi.ModTime().UTC()})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("artifactInventory: %w", err)
	}
	return files, nil
}
