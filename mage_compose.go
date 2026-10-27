//go:build mage

package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Compose packages standalone Docker Compose setup without building any binaries.
// CI can pin the bootstrap image with DARKSPIN_COMPOSE_IMAGE.
func (Darkrun) Compose() error {
	err := Version()
	if err != nil {
		return fmt.Errorf("composeVersion: %w", err)
	}
	entries, err := composeBundleEntries()
	if err != nil {
		return fmt.Errorf("composeSources: %w", err)
	}
	payload, err := composeArchive(entries)
	if err != nil {
		return fmt.Errorf("composeArchive: %w", err)
	}
	outputPath := filepath.Join("bin", "deployment")
	err = os.MkdirAll(outputPath, 0o755)
	if err != nil {
		return fmt.Errorf("outputCreate: %w", err)
	}
	archivePath := filepath.Join(outputPath, "darkrun-compose-v"+buildVersion()+".zip")
	err = os.WriteFile(archivePath, payload, 0o644)
	if err != nil {
		return fmt.Errorf("archiveSave: %w", err)
	}
	// CI uploads these explicitly named entries so its downloadable artifact is
	// the setup ZIP itself, rather than a ZIP containing another ZIP.
	bundlePath := filepath.Join(outputPath, "bundle")
	err = os.MkdirAll(bundlePath, 0o755)
	if err != nil {
		return fmt.Errorf("bundleDirectory: %w", err)
	}
	for _, entry := range entries {
		err = os.WriteFile(filepath.Join(bundlePath, entry.name), entry.payload, 0o644)
		if err != nil {
			return fmt.Errorf("bundleSave[%s]: %w", entry.name, err)
		}
	}
	for _, filename := range []string{"compose.update.yaml", "README.txt"} {
		err = os.Remove(filepath.Join(bundlePath, filename))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("obsoleteRemove[%s]: %w", filename, err)
		}
	}
	_, err = fmt.Printf("Packaged %s (setup files only; no binaries built)\n", archivePath)
	if err != nil {
		return fmt.Errorf("archiveOutput: %w", err)
	}
	return nil
}

type composeEntry struct {
	name    string
	payload []byte
}

func composeBundleEntries() ([]composeEntry, error) {
	// Never walk deployment/: local .env files, runtimes and developer build
	// definitions must not become release inputs.
	filenames := []string{"compose.yaml", ".env.example", "README.md"}
	entries := make([]composeEntry, 0, len(filenames)+1)
	for _, filename := range filenames {
		payload, err := composeBundleFile(filename)
		if err != nil {
			return nil, fmt.Errorf("bundleRead[%s]: %w", filename, err)
		}
		entries = append(entries, composeEntry{name: filename, payload: payload})
	}
	entries = append(entries, composeEntry{name: "VERSION", payload: []byte(buildVersion() + "\n")})
	return entries, nil
}

func composeArchive(entries []composeEntry) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range entries {
		err := appendComposeEntry(archive, entry.name, entry.payload)
		if err != nil {
			closeErr := archive.Close()
			return nil, fmt.Errorf("bundleEntry[%s]: %w", entry.name, errors.Join(err, closeErr))
		}
	}
	err := archive.Close()
	if err != nil {
		return nil, fmt.Errorf("bundleFinish: %w", err)
	}
	return buffer.Bytes(), nil
}

func composeBundleFile(filename string) ([]byte, error) {
	path := filepath.Join("deployment", filename)
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("sourceStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("bundle source must be a regular file")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("sourceRead: %w", err)
	}
	document := strings.ReplaceAll(string(payload), "\r\n", "\n")
	var environmentKey, originalImage string
	switch filename {
	case "compose.yaml":
		environmentKey = "DARKSPIN_COMPOSE_IMAGE"
		originalImage = "ghcr.io/darkspinnet/darkrun-bootstrap:1"
	default:
		return []byte(document), nil
	}
	imageReference := strings.TrimSpace(os.Getenv(environmentKey))
	if imageReference == "" {
		return []byte(document), nil
	}
	// Keep image overrides inside their YAML/Compose scalar, without allowing
	// interpolation, quoting or additional settings through the packaging input.
	pattern := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$`)
	if !pattern.MatchString(imageReference) {
		return nil, fmt.Errorf("invalid image reference in %s", environmentKey)
	}
	if strings.Count(document, originalImage) != 1 {
		return nil, fmt.Errorf("expected one default image in %s", filename)
	}
	return []byte(strings.Replace(document, originalImage, imageReference, 1)), nil
}

func appendComposeEntry(archive *zip.Writer, filename string, payload []byte) error {
	header := &zip.FileHeader{Name: filename, Method: zip.Deflate}
	header.SetMode(0o644)
	header.SetModTime(time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC))
	entry, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("entryCreate: %w", err)
	}
	written, err := entry.Write(payload)
	if err != nil {
		return fmt.Errorf("entryWrite: %w", err)
	}
	if written != len(payload) {
		return errors.New("incomplete bundle entry write")
	}
	return nil
}
