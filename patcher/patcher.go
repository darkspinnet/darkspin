// Package patcher plans and applies filesystem updates from a remote manifest.
package patcher

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type patchManifest struct {
	Version        string      `yaml:"version"`
	DownloadPrefix string      `yaml:"downloadprefix"`
	Downloads      []patchFile `yaml:"downloads"`
}

type patchFile struct {
	Name string `yaml:"name"`
	MD5  string `yaml:"md5"`
	Size int64  `yaml:"size"`
}

// Options identifies the patch manifest and installation root.
type Options struct {
	ManifestURL string
	Root        string
	Client      *http.Client
}

// EventType identifies a patch lifecycle update.
type EventType string

const (
	EventDownload EventType = "download"
	EventComplete EventType = "complete"
)

// Event describes one patch lifecycle update.
type Event struct {
	Type          EventType
	Path          string
	Message       string
	Progress      int
	CompletedByte int64
	TotalByte     int64
}

// Dispatcher receives structured patch lifecycle updates.
type Dispatcher interface {
	Dispatch(Event)
}

// DispatchFunc adapts a function to Dispatcher.
type DispatchFunc func(Event)

// Dispatch invokes f when it is non-nil.
func (f DispatchFunc) Dispatch(event Event) {
	if f != nil {
		f(event)
	}
}

// Plan contains the immutable filesystem operations selected from a manifest.
type Plan struct {
	root         string
	baseURL      string
	client       *http.Client
	downloads    []patchFile
	downloadByte int64
}

// Build fetches a manifest and selects the required filesystem operations.
func Build(ctx context.Context, options Options) (*Plan, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if strings.TrimSpace(options.ManifestURL) == "" {
		return nil, errors.New("empty manifest URL")
	}
	if strings.TrimSpace(options.Root) == "" {
		return nil, errors.New("empty root")
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	manifest, baseURL, err := fetchPatchManifest(ctx, client, options.ManifestURL)
	if err != nil {
		return nil, fmt.Errorf("manifestFetch: %w", err)
	}
	if strings.TrimSpace(manifest.DownloadPrefix) != "" {
		baseURL = ensureURLSlash(manifest.DownloadPrefix)
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return nil, fmt.Errorf("rootResolve: %w", err)
	}
	plan := &Plan{root: root, baseURL: baseURL, client: client}
	for index, entry := range manifest.Downloads {
		err = ctx.Err()
		if err != nil {
			return nil, fmt.Errorf("planContext[%d]: %w", index, err)
		}
		isNeeded, checkErr := isPatchNeeded(plan.root, entry)
		if checkErr != nil {
			return nil, fmt.Errorf("fileCheck[%d]: %w", index, checkErr)
		}
		if !isNeeded {
			continue
		}
		plan.downloads = append(plan.downloads, entry)
		plan.downloadByte += normalizedPatchSize(entry.Size)
	}
	return plan, nil
}

// DownloadCount reports how many files require downloading.
func (plan *Plan) DownloadCount() int {
	if plan == nil {
		return 0
	}
	return len(plan.downloads)
}

// DownloadByte reports the total declared download size.
func (plan *Plan) DownloadByte() int64 {
	if plan == nil {
		return 0
	}
	return plan.downloadByte
}

func fetchPatchManifest(ctx context.Context, client *http.Client, manifestURL string) (patchManifest, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(manifestURL), nil)
	if err != nil {
		return patchManifest{}, "", fmt.Errorf("requestCreate: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return patchManifest{}, "", fmt.Errorf("requestSend: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return patchManifest{}, "", fmt.Errorf("manifest returned %s", response.Status)
	}
	manifest := patchManifest{}
	err = yaml.NewDecoder(io.LimitReader(response.Body, 8*1024*1024)).Decode(&manifest)
	if err != nil {
		return patchManifest{}, "", fmt.Errorf("manifestDecode: %w", err)
	}
	return manifest, manifestBaseURL(manifestURL), nil
}

// Apply performs every selected operation and dispatches structured updates.
func Apply(ctx context.Context, plan *Plan, dispatcher Dispatcher) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if plan == nil {
		return errors.New("nil plan")
	}
	completed := int64(0)
	total := plan.downloadByte
	if total < 1 {
		total = 1
	}
	for index, entry := range plan.downloads {
		err := downloadPatchFile(ctx, plan.client, plan.root, plan.baseURL, entry)
		if err != nil {
			return fmt.Errorf("download[%d]: %w", index, err)
		}
		completed += normalizedPatchSize(entry.Size)
		progress := int(completed * 100 / total)
		dispatch(dispatcher, Event{Type: EventDownload, Path: entry.Name, Message: "Patched " + entry.Name, Progress: progress, CompletedByte: completed, TotalByte: plan.downloadByte})
	}
	dispatch(dispatcher, Event{Type: EventComplete, Message: "Patch complete", Progress: 100, CompletedByte: completed, TotalByte: plan.downloadByte})
	return nil
}

func dispatch(dispatcher Dispatcher, event Event) {
	if dispatcher != nil {
		dispatcher.Dispatch(event)
	}
}

func isPatchNeeded(root string, entry patchFile) (bool, error) {
	path, err := safePatchPath(root, entry.Name)
	if err != nil {
		return false, fmt.Errorf("safePath: %w", err)
	}
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("fileStat: %w", err)
	}
	if strings.TrimSpace(entry.MD5) == "" {
		return false, nil
	}
	digest, err := patchFileMD5(path)
	if err != nil {
		return false, fmt.Errorf("fileDigest: %w", err)
	}
	return !strings.EqualFold(digest, entry.MD5), nil
}

func downloadPatchFile(
	ctx context.Context,
	client *http.Client,
	root string,
	baseURL string,
	entry patchFile,
) error {
	target, err := safePatchPath(root, entry.Name)
	if err != nil {
		return fmt.Errorf("targetPath: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(target), 0o755)
	if err != nil {
		return fmt.Errorf("targetMkdir: %w", err)
	}
	fileURL, err := url.JoinPath(baseURL, strings.ReplaceAll(entry.Name, "\\", "/"))
	if err != nil {
		return fmt.Errorf("downloadURL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("downloadRequest: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("downloadSend: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download %s returned %s", entry.Name, response.Status)
	}
	temporary := target + ".patching"
	output, err := os.Create(temporary)
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	writtenByte, copyErr := io.Copy(output, response.Body)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("downloadCopy: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("downloadClose: %w", closeErr)
	}
	if entry.Size > 0 && writtenByte != entry.Size {
		_ = os.Remove(temporary)
		return fmt.Errorf("downloadSize: got %d, want %d", writtenByte, entry.Size)
	}
	if strings.TrimSpace(entry.MD5) != "" {
		digest, digestErr := patchFileMD5(temporary)
		if digestErr != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("downloadDigest: %w", digestErr)
		}
		if !strings.EqualFold(digest, entry.MD5) {
			_ = os.Remove(temporary)
			return fmt.Errorf("MD5 mismatch for %s", entry.Name)
		}
	}
	err = replacePatchFile(temporary, target)
	if err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("downloadReplace: %w", err)
	}
	return nil
}

func replacePatchFile(temporary, target string) error {
	backup := target + ".patching.previous"
	err := os.Remove(backup)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backupRemove: %w", err)
	}
	_, err = os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		err = os.Rename(temporary, target)
		if err != nil {
			return fmt.Errorf("targetRename: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("targetStat: %w", err)
	}
	err = os.Rename(target, backup)
	if err != nil {
		return fmt.Errorf("backupRename: %w", err)
	}
	err = os.Rename(temporary, target)
	if err != nil {
		restoreErr := os.Rename(backup, target)
		if restoreErr != nil {
			return fmt.Errorf("backupRestore: %w", errors.Join(err, restoreErr))
		}
		return fmt.Errorf("targetRename: %w", err)
	}
	err = os.Remove(backup)
	if err != nil {
		return fmt.Errorf("backupCleanup: %w", err)
	}
	return nil
}

func safePatchPath(root, name string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("rootResolve: %w", err)
	}
	cleanName := filepath.Clean(strings.ReplaceAll(name, "/", string(os.PathSeparator)))
	if filepath.IsAbs(cleanName) || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe patch path %q", name)
	}
	target, err := filepath.Abs(filepath.Join(root, cleanName))
	if err != nil {
		return "", fmt.Errorf("targetResolve: %w", err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("targetRelative: %w", err)
	}
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe patch path %q", name)
	}
	return target, nil
}

func patchFileMD5(path string) (string, error) {
	r, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("digestOpen: %w", err)
	}
	defer r.Close()
	hash := md5.New()
	_, err = io.Copy(hash, r)
	if err != nil {
		return "", fmt.Errorf("digestRead: %w", err)
	}
	return strings.ToUpper(hex.EncodeToString(hash.Sum(nil))), nil
}

func manifestBaseURL(manifestURL string) string {
	index := strings.LastIndex(manifestURL, "/")
	if index < 0 {
		return ensureURLSlash(manifestURL)
	}
	return manifestURL[:index+1]
}

func ensureURLSlash(address string) string {
	if strings.HasSuffix(address, "/") {
		return address
	}
	return address + "/"
}

func normalizedPatchSize(size int64) int64 {
	if size < 1 {
		return 1
	}
	return size
}
