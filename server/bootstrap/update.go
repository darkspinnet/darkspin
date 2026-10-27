package bootstrap

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxBinarySize int64 = 256 << 20

type updateManifest struct {
	Version  string `json:"version"`
	BuildID  string `json:"build_id"`
	Platform string `json:"platform"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Format   string `json:"format"`
}

func validateHTTPS(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("urlParse: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("update URL must be HTTPS without credentials or fragment")
	}
	return nil
}

func checkRedirect(req *http.Request, previousRequests []*http.Request) error {
	if len(previousRequests) >= 5 {
		return errors.New("too many update redirects")
	}
	err := validateHTTPS(req.URL.String())
	if err != nil {
		return fmt.Errorf("redirectURL: %w", err)
	}
	// Download requests never carry tokens, cookies or ambient credentials.
	req.Header = make(http.Header)
	return nil
}

func download(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	err := validateHTTPS(rawURL)
	if err != nil {
		return nil, fmt.Errorf("downloadURL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("downloadRequest: %w", err)
	}
	client := &http.Client{Timeout: 3 * time.Minute, CheckRedirect: checkRedirect}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloadGet: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		closeErr := response.Body.Close()
		return nil, fmt.Errorf("downloadStatus: %w", errors.Join(fmt.Errorf("HTTP %d", response.StatusCode), closeErr))
	}
	if response.ContentLength > limit {
		closeErr := response.Body.Close()
		return nil, fmt.Errorf("downloadLength: %w", errors.Join(errors.New("download exceeds size limit"), closeErr))
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	closeErr := response.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("downloadRead: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("downloadClose: %w", closeErr)
	}
	if int64(len(payload)) > limit {
		return nil, errors.New("download exceeds size limit")
	}
	return payload, nil
}

// unpackSingle reads one explicitly named regular entry, never filesystem paths.
func unpackSingle(payload []byte, name string, limit int64) ([]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("zipOpen: %w", err)
	}
	if len(archive.File) != 1 {
		return nil, errors.New("update archive must contain exactly one entry")
	}
	entry := archive.File[0]
	if entry.Name != name || !entry.Mode().IsRegular() || entry.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("invalid update archive entry")
	}
	r, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("zipEntry: %w", err)
	}
	contents, err := io.ReadAll(io.LimitReader(r, limit+1))
	closeErr := r.Close()
	if err != nil {
		return nil, fmt.Errorf("zipRead: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("zipClose: %w", closeErr)
	}
	if int64(len(contents)) > limit {
		return nil, errors.New("update entry exceeds size limit")
	}
	return contents, nil
}

func fetchManifest(ctx context.Context, manifestURL string) (updateManifest, error) {
	var manifest updateManifest
	payload, err := download(ctx, manifestURL, 1<<20)
	if err != nil {
		return manifest, fmt.Errorf("manifestDownload: %w", err)
	}
	if bytes.HasPrefix(payload, []byte("PK")) {
		payload, err = unpackSingle(payload, "darkrun-update.json", 64<<10)
		if err != nil {
			return manifest, fmt.Errorf("manifestArchive: %w", err)
		}
	}
	err = json.Unmarshal(payload, &manifest)
	if err != nil {
		return manifest, fmt.Errorf("manifestDecode: %w", err)
	}
	if manifest.Platform != runtime.GOOS+"-"+runtime.GOARCH {
		return manifest, errors.New("update platform mismatch")
	}
	if manifest.Version == "" || manifest.BuildID == "" || len(manifest.BuildID) > 256 {
		return manifest, errors.New("update version/build identity missing or invalid")
	}
	checksum, err := hex.DecodeString(manifest.SHA256)
	if err != nil {
		return manifest, fmt.Errorf("manifestHash: %w", err)
	}
	if len(checksum) != sha256.Size {
		return manifest, errors.New("invalid update SHA256 length")
	}
	manifest.SHA256 = strings.ToLower(manifest.SHA256)
	if manifest.Format != "binary" && manifest.Format != "zip" {
		return manifest, errors.New("unsupported update format")
	}
	err = validateHTTPS(manifest.URL)
	if err != nil {
		return manifest, fmt.Errorf("manifestURL: %w", err)
	}
	return manifest, nil
}

// CheckUpdate downloads only after a complete matching snapshot exists. The
// transaction marker is durable before replacing darkrun; only the supervisor
// starts the candidate and decides when its first boot is healthy.
func (e *Storage) CheckUpdate(ctx context.Context, manifestURL, backupID string) (bool, error) {
	backup, err := e.backupPath(backupID)
	if err != nil {
		return false, fmt.Errorf("updateBackup: %w", err)
	}
	var snapshot snapshotManifest
	err = e.checkPath(filepath.Join(backup, "snapshot.json"))
	if err != nil {
		return false, fmt.Errorf("updateSnapshotPath: %w", err)
	}
	err = readJSON(filepath.Join(backup, "snapshot.json"), &snapshot)
	if err != nil {
		return false, fmt.Errorf("updateSnapshot: %w", err)
	}
	if !snapshot.IsComplete {
		return false, errors.New("update requires complete snapshot")
	}
	err = e.validateDatabaseSnapshot(backup, snapshot)
	if err != nil {
		return false, fmt.Errorf("updateDatabase: %w", err)
	}
	currentHash, err := hashFile(e.path("bin", "darkrun"))
	if err != nil {
		return false, fmt.Errorf("updateCurrent: %w", err)
	}
	if currentHash != snapshot.PreviousHash {
		return false, errors.New("snapshot does not match installed executable")
	}
	backupBinary := filepath.Join(backup, "bin", "darkrun")
	err = e.checkPath(backupBinary)
	if err != nil {
		return false, fmt.Errorf("updateBackupPath: %w", err)
	}
	backupHash, err := hashFile(backupBinary)
	if err != nil {
		return false, fmt.Errorf("updateBackupHash: %w", err)
	}
	if backupHash != currentHash {
		return false, errors.New("backup executable checksum mismatch")
	}
	manifest, err := fetchManifest(ctx, manifestURL)
	if err != nil {
		return false, fmt.Errorf("updateManifest: %w", err)
	}
	if manifest.SHA256 == currentHash {
		return false, nil
	}
	payload, err := download(ctx, manifest.URL, maxBinarySize+(1<<20))
	if err != nil {
		return false, fmt.Errorf("updateDownload: %w", err)
	}
	if manifest.Format == "zip" {
		payload, err = unpackSingle(payload, "darkrun", maxBinarySize)
		if err != nil {
			return false, fmt.Errorf("updateArchive: %w", err)
		}
	}
	if int64(len(payload)) > maxBinarySize || len(payload) == 0 {
		return false, errors.New("invalid update binary size")
	}
	checksum := sha256.Sum256(payload)
	if hex.EncodeToString(checksum[:]) != manifest.SHA256 {
		return false, errors.New("update binary checksum mismatch")
	}
	candidate := e.path("bin", "darkrun.candidate")
	err = e.checkPath(candidate)
	if err != nil {
		return false, fmt.Errorf("candidateCheck: %w", err)
	}
	err = writeSynced(candidate, payload, 0755)
	if err != nil {
		return false, fmt.Errorf("candidateWrite: %w", err)
	}
	snapshot.CandidateBuild = manifest.BuildID
	snapshot.CandidateHash = manifest.SHA256
	err = e.writeJSON(filepath.Join(backup, "snapshot.json"), snapshot)
	if err != nil {
		return false, fmt.Errorf("candidateManifest: %w", err)
	}
	err = e.writeJSON(e.path("firstboot.json"), updateTransaction{Backup: backupID, Phase: "first_boot"})
	if err != nil {
		return false, fmt.Errorf("candidateState: %w", err)
	}
	err = os.Rename(candidate, e.path("bin", "darkrun"))
	if err != nil {
		rollbackErr := e.RollbackContext(ctx)
		return false, fmt.Errorf("candidateInstall: %w", errors.Join(err, rollbackErr))
	}
	err = syncDirectory(e.path("bin"))
	if err != nil {
		rollbackErr := e.RollbackContext(ctx)
		return false, fmt.Errorf("candidateSync: %w", errors.Join(err, rollbackErr))
	}
	return true, nil
}
