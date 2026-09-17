package local

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/chat"
)

const (
	bugLogWindow       = 60 * time.Second
	maximumBugLogSize  = 16 * 1024 * 1024
	maximumBugSlugSize = 60
)

// BugReporter writes local bug archives beneath the server runtime directory.
type BugReporter struct {
	runtimePath string
	now         func() time.Time
}

type bugMetadata struct {
	Version      string          `json:"version"`
	Platform     string          `json:"platform"`
	CreatedAt    time.Time       `json:"created_at"`
	Description  string          `json:"description"`
	Context      chat.BugContext `json:"context"`
	ContextError string          `json:"context_error,omitempty"`
}

// NewBugReporter creates a filesystem-backed local bug reporter.
func NewBugReporter(runtimePath string) *BugReporter {
	return &BugReporter{runtimePath: runtimePath, now: time.Now}
}

// Report reserves an archive name and queues one atomic ZIP containing the
// description and recent logs without blocking the client's chat request.
func (e *BugReporter) Report(ctx context.Context, req chat.BugCommand) (string, error) {
	err := ctx.Err()
	if err != nil {
		return "", fmt.Errorf("contextCheck: %w", err)
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		return "", errors.New("bug description is empty")
	}
	runtimePath, err := filepath.Abs(e.runtimePath)
	if err != nil {
		return "", fmt.Errorf("runtimePath: %w", err)
	}
	bugPath := filepath.Join(runtimePath, "bugs")
	err = os.MkdirAll(bugPath, 0o755)
	if err != nil {
		return "", fmt.Errorf("bugMkdir: %w", err)
	}
	createdAt := e.now().UTC()
	version := strings.TrimSpace(buildinfo.Version)
	if version == "" {
		version = "unknown"
	}
	archiveName := fmt.Sprintf(
		"%s-darkspin-bug-%s-%s.zip", bugSlug(description), version,
		createdAt.Format("20060102T150405.000000000Z"),
	)
	archivePath := filepath.Join(bugPath, archiveName)
	output, err := os.CreateTemp(bugPath, ".bug-*.zip")
	if err != nil {
		return "", fmt.Errorf("bugTemp: %w", err)
	}
	temporaryPath := output.Name()
	if req.Context.CapturedAt.IsZero() {
		req.Context.CapturedAt = createdAt
	}
	if req.Context.UserID == 0 {
		req.Context.UserID = req.Sender.ID
		req.Context.UserName = req.Sender.Name
	}
	metadata := bugMetadata{
		Version: version, Platform: runtime.GOOS + "/" + runtime.GOARCH,
		CreatedAt: createdAt, Description: description,
		Context: req.Context, ContextError: req.ContextError,
	}
	reportContext := context.WithoutCancel(ctx)
	go func() {
		writeErr := writeBugArchive(
			reportContext, output, temporaryPath, archivePath,
			filepath.Join(runtimePath, "logs"), req, metadata, createdAt,
		)
		if writeErr != nil {
			writtenByteCount, stderrErr := fmt.Fprintf(
				os.Stderr, "Bug report %s failed: %v\n", archiveName, writeErr,
			)
			if stderrErr != nil || writtenByteCount == 0 {
				return
			}
		}
	}()
	return archiveName, nil
}

func writeBugArchive(
	ctx context.Context, output *os.File, temporaryPath string, archivePath string,
	logPath string, req chat.BugCommand, metadata bugMetadata, createdAt time.Time,
) error {
	isComplete := false
	defer func() {
		if !isComplete {
			_ = os.Remove(temporaryPath)
		}
	}()

	archive := zip.NewWriter(output)
	err := addBugText(archive, req)
	if err == nil {
		err = addBugMetadata(archive, metadata)
	}
	if err == nil {
		err = addRecentBugLogs(ctx, archive, logPath, createdAt)
	}
	if err != nil {
		_ = archive.Close()
		_ = output.Close()
		return fmt.Errorf("archiveWrite: %w", err)
	}
	err = archive.Close()
	if err != nil {
		_ = output.Close()
		return fmt.Errorf("archiveClose: %w", err)
	}
	err = output.Close()
	if err != nil {
		return fmt.Errorf("outputClose: %w", err)
	}
	err = os.Rename(temporaryPath, archivePath)
	if err != nil {
		return fmt.Errorf("archiveRename: %w", err)
	}
	isComplete = true
	return nil
}

func addBugText(archive *zip.Writer, req chat.BugCommand) error {
	w, err := archive.Create("bug.txt")
	if err != nil {
		return fmt.Errorf("textCreate: %w", err)
	}
	_, err = io.WriteString(w, strings.TrimSpace(req.Description)+"\n")
	if err != nil {
		return fmt.Errorf("textWrite: %w", err)
	}
	gameplay := req.Context
	_, err = fmt.Fprintf(
		w,
		"\nCaptured gameplay context\nActive: %t\nCrogenitor: %s (%d)\nMission: %s [%s]\nHero: %s, level %d, creature %d, noun %#x, squad index %d\nLocation: %.3f, %.3f, %.3f; nearest %s / %s (%.3f)\nGame: %d; difficulty %d; mode %s; stage %s; session generation %d; transport generation %d; zone generation %d\nResources: %.3f/%.3f health, %.3f/%.3f power\n",
		gameplay.IsGameplayActive, gameplay.UserName, gameplay.UserID,
		gameplay.Mission.Label, gameplay.Mission.Asset,
		gameplay.Hero.Name, gameplay.Hero.Level, gameplay.Hero.CreatureID,
		gameplay.Hero.NounID, gameplay.Hero.SquadIndex,
		gameplay.Location.X, gameplay.Location.Y, gameplay.Location.Z,
		gameplay.Location.NearestMarkerSet, gameplay.Location.NearestMarker,
		gameplay.Location.MarkerDistance, gameplay.GameID,
		gameplay.Mission.Difficulty, gameplay.Mission.Mode, gameplay.SessionStage,
		gameplay.SessionGeneration, gameplay.TransportGeneration,
		gameplay.ZoneGeneration,
		gameplay.Hero.HitPoint, gameplay.Hero.MaximumHitPoint,
		gameplay.Hero.PowerPoint, gameplay.Hero.MaximumPowerPoint,
	)
	if err != nil {
		return fmt.Errorf("contextWrite: %w", err)
	}
	return nil
}

func addBugMetadata(archive *zip.Writer, metadata bugMetadata) error {
	contents, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("metadataMarshal: %w", err)
	}
	w, err := archive.Create("bug.json")
	if err != nil {
		return fmt.Errorf("metadataCreate: %w", err)
	}
	_, err = w.Write(append(contents, '\n'))
	if err != nil {
		return fmt.Errorf("metadataWrite: %w", err)
	}
	return nil
}

func addRecentBugLogs(
	ctx context.Context, archive *zip.Writer, logPath string, createdAt time.Time,
) error {
	err := filepath.WalkDir(logPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("logWalk: %w", walkErr)
		}
		if entry.IsDir() && strings.EqualFold(entry.Name(), "snapshots") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".log" && extension != ".jsonl" {
			return nil
		}
		contextErr := ctx.Err()
		if contextErr != nil {
			return fmt.Errorf("contextCheck: %w", contextErr)
		}
		contents, readErr := recentBugLog(path, createdAt)
		if readErr != nil {
			return fmt.Errorf("logRead[%s]: %w", entry.Name(), readErr)
		}
		if len(contents) == 0 {
			return nil
		}
		relativePath, relativeErr := filepath.Rel(logPath, path)
		if relativeErr != nil {
			return fmt.Errorf("logRelative: %w", relativeErr)
		}
		w, createErr := archive.Create(filepath.ToSlash(filepath.Join("logs", relativePath)))
		if createErr != nil {
			return fmt.Errorf("logCreate: %w", createErr)
		}
		_, writeErr := w.Write(contents)
		if writeErr != nil {
			return fmt.Errorf("logWrite: %w", writeErr)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("logsWalk: %w", err)
	}
	return nil
}

func recentBugLog(path string, createdAt time.Time) ([]byte, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sourceOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return nil, fmt.Errorf("sourceStat: %w", err)
	}
	if fi.ModTime().Before(createdAt.Add(-bugLogWindow)) {
		return nil, nil
	}
	offset := fi.Size() - maximumBugLogSize
	if offset < 0 {
		offset = 0
	}
	contents, err := io.ReadAll(io.NewSectionReader(r, offset, fi.Size()-offset))
	if err != nil {
		return nil, fmt.Errorf("sourceRead: %w", err)
	}
	if offset > 0 {
		separator := bytes.IndexByte(contents, '\n')
		if separator < 0 {
			return nil, nil
		}
		contents = contents[separator+1:]
	}
	return recentBugLines(contents, createdAt), nil
}

func recentBugLines(contents []byte, createdAt time.Time) []byte {
	lines := bytes.Split(contents, []byte{'\n'})
	maximumElapsed := int64(-1)
	for _, line := range lines {
		_, elapsed, isElapsed := bugLineTime(line)
		if isElapsed && elapsed > maximumElapsed {
			maximumElapsed = elapsed
		}
	}
	cutoff := createdAt.Add(-bugLogWindow)
	elapsedCutoff := maximumElapsed - bugLogWindow.Milliseconds()
	var recent bytes.Buffer
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		timestamp, elapsed, isElapsed := bugLineTime(line)
		isTimestampRecent := !timestamp.IsZero() && !timestamp.Before(cutoff)
		isElapsedRecent := isElapsed && elapsed >= elapsedCutoff
		isUnknownRecent := timestamp.IsZero() && !isElapsed
		if isTimestampRecent || isElapsedRecent || isUnknownRecent {
			recent.Write(line)
			recent.WriteByte('\n')
		}
	}
	return recent.Bytes()
}

func bugLineTime(line []byte) (time.Time, int64, bool) {
	var metadata struct {
		Time   string `json:"time"`
		SentAt string `json:"sent_at"`
		TimeMS *int64 `json:"time_ms"`
	}
	if len(line) > 0 && line[0] == '{' {
		err := json.Unmarshal(line, &metadata)
		if err == nil {
			if metadata.TimeMS != nil {
				return time.Time{}, *metadata.TimeMS, true
			}
			textTime := metadata.Time
			if textTime == "" {
				textTime = metadata.SentAt
			}
			timestamp, parseErr := time.Parse(time.RFC3339Nano, textTime)
			if parseErr == nil {
				return timestamp, 0, false
			}
		}
	}
	field := bytes.Fields(line)
	if len(field) > 0 {
		timestamp, err := time.Parse(time.RFC3339Nano, string(field[0]))
		if err == nil {
			return timestamp, 0, false
		}
	}
	return time.Time{}, 0, false
}

func bugSlug(description string) string {
	var slug strings.Builder
	isSeparator := false
	for _, character := range strings.ToLower(description) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			if slug.Len() >= maximumBugSlugSize {
				break
			}
			slug.WriteRune(character)
			isSeparator = false
			continue
		}
		if slug.Len() == 0 || isSeparator {
			continue
		}
		slug.WriteByte('-')
		isSeparator = true
	}
	result := strings.Trim(slug.String(), "-")
	if result == "" {
		return "bug"
	}
	return result
}

var _ chat.BugReporter = (*BugReporter)(nil)
