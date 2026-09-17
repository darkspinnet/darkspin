package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/darkspinnet/darkspin/server/chat"
)

const maximumReportSlugSize = 60

type reportManifest struct {
	Version       string             `json:"version"`
	CreatedAt     string             `json:"created_at"`
	Platform      string             `json:"platform"`
	Title         string             `json:"title"`
	Description   string             `json:"description"`
	State         reportState        `json:"state"`
	Gameplay      chat.BugContext    `json:"gameplay"`
	GameplayError string             `json:"gameplay_error,omitempty"`
	Files         []reportFileRecord `json:"files"`
}

type reportState struct {
	Launcher           string `json:"launcher"`
	Message            string `json:"message,omitempty"`
	Identity           string `json:"identity,omitempty"`
	Auth               string `json:"auth"`
	Server             string `json:"server"`
	Patch              string `json:"patch"`
	Game               string `json:"game"`
	Avatar             string `json:"avatar"`
	AuthError          string `json:"auth_error,omitempty"`
	ServerError        string `json:"server_error,omitempty"`
	PatchError         string `json:"patch_error,omitempty"`
	GameError          string `json:"game_error,omitempty"`
	AvatarError        string `json:"avatar_error,omitempty"`
	LastRun            string `json:"last_run,omitempty"`
	IsLastRunFailure   bool   `json:"is_last_run_failure,omitempty"`
	IsCinematicSkipped bool   `json:"is_cinematic_skipped"`
}

type reportFileRecord struct {
	Name         string `json:"name"`
	OriginalSize int64  `json:"original_size"`
	IncludedSize int64  `json:"included_size"`
	IsTruncated  bool   `json:"is_truncated"`
}

type reportSource struct {
	path string
	name string
}

type ReportResult struct {
	Name      string `json:"name"`
	Directory string `json:"directory"`
	FileCount int    `json:"fileCount"`
}

// SendReport creates one titled local archive containing the user's account of
// the problem and every available ordinary log. Sync Snapshot artifacts are
// excluded because they are separate diagnostic reports. It never uploads the
// bundle or opens another application by itself.
func (a *App) SendReport(title string, description string) (ReportResult, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title == "" {
		return ReportResult{}, errors.New("report title is empty")
	}
	if description == "" {
		return ReportResult{}, errors.New("report description is empty")
	}
	logPath, reportDirectory, err := a.reportPaths()
	if err != nil {
		return ReportResult{}, fmt.Errorf("reportPaths: %w", err)
	}
	reportPath, records, err := a.createReportArchive(
		logPath, reportDirectory, title, description,
	)
	if err != nil {
		return ReportResult{}, fmt.Errorf("reportCreate: %w", err)
	}
	a.log(fmt.Sprintf("Report ready: %s (%d complete log files)", reportPath, len(records)))
	return ReportResult{
		Name: filepath.Base(reportPath), Directory: reportDirectory,
		FileCount: len(records),
	}, nil
}

func (a *App) reportPaths() (string, string, error) {
	gameDirectory := a.gameDirectory()
	if strings.TrimSpace(gameDirectory) == "" {
		var err error
		gameDirectory, err = executableDirectory()
		if err != nil {
			return "", "", fmt.Errorf("reportBase: %w", err)
		}
	}
	logPath := filepath.Join(gameDirectory, "darkspin", "logs")
	reportDirectory := filepath.Join(gameDirectory, "darkspin", "bugs")
	return logPath, reportDirectory, nil
}

// OpenReportFolder opens the shared directory used by Send Report and /bug.
func (a *App) OpenReportFolder() error {
	_, reportDirectory, err := a.reportPaths()
	if err != nil {
		return fmt.Errorf("reportPaths: %w", err)
	}
	err = os.MkdirAll(reportDirectory, 0o755)
	if err != nil {
		return fmt.Errorf("reportMkdir: %w", err)
	}
	err = revealReportDirectory(reportDirectory)
	if err != nil {
		return fmt.Errorf("reportReveal: %w", err)
	}
	return nil
}

func (a *App) createReportArchive(
	logPath string, reportDirectory string, title string, description string,
) (string, []reportFileRecord, error) {
	err := os.MkdirAll(reportDirectory, 0o755)
	if err != nil {
		return "", nil, fmt.Errorf("reportMkdir: %w", err)
	}
	createdAt := time.Now().UTC()
	reportName := fmt.Sprintf(
		"%s-darkspin-report-%s-%s.zip", reportSlug(title), Version,
		createdAt.Format("20060102T150405.000000000Z"),
	)
	reportPath := filepath.Join(reportDirectory, reportName)
	w, err := os.CreateTemp(reportDirectory, ".darkspin-report-*.zip")
	if err != nil {
		return "", nil, fmt.Errorf("reportTemp: %w", err)
	}
	temporaryPath := w.Name()
	isComplete := false
	defer func() {
		if !isComplete {
			_ = os.Remove(temporaryPath)
		}
	}()

	archive := zip.NewWriter(w)
	records := make([]reportFileRecord, 0, 5)
	manifest := a.reportManifest(createdAt, title, description, nil)
	err = addReportText(archive, title, description)
	if err != nil {
		_ = archive.Close()
		_ = w.Close()
		return "", nil, fmt.Errorf("reportText: %w", err)
	}
	sources, err := reportSources(logPath)
	if err != nil {
		_ = archive.Close()
		_ = w.Close()
		return "", nil, fmt.Errorf("sourceList: %w", err)
	}
	for _, source := range sources {
		record, addErr := addReportFile(archive, source)
		if addErr != nil {
			_ = archive.Close()
			_ = w.Close()
			return "", nil, fmt.Errorf("fileAdd: %w", addErr)
		}
		records = append(records, record)
	}
	manifest.Files = records
	err = addReportManifest(archive, manifest)
	if err != nil {
		_ = archive.Close()
		_ = w.Close()
		return "", nil, fmt.Errorf("manifestAdd: %w", err)
	}
	err = archive.Close()
	if err != nil {
		_ = w.Close()
		return "", nil, fmt.Errorf("archiveClose: %w", err)
	}
	err = w.Close()
	if err != nil {
		return "", nil, fmt.Errorf("reportClose: %w", err)
	}
	err = os.Rename(temporaryPath, reportPath)
	if err != nil {
		return "", nil, fmt.Errorf("reportRename: %w", err)
	}
	isComplete = true
	return reportPath, records, nil
}

func (a *App) reportManifest(
	createdAt time.Time, title string, description string,
	records []reportFileRecord,
) reportManifest {
	a.mu.Lock()
	status := a.status
	gameServer := a.serviceSet.gameServer
	ctx := a.lifecycleCtx
	a.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	gameplay := chat.BugContext{
		CapturedAt: createdAt, UserName: status.Identity,
	}
	gameplayError := ""
	if gameServer != nil {
		captured, err := gameServer.BugContext(ctx, status.Identity)
		if err != nil {
			gameplayError = err.Error()
		} else {
			gameplay = captured
		}
		if gameplay.CapturedAt.IsZero() {
			gameplay.CapturedAt = createdAt
		}
	}
	return reportManifest{
		Version: Version, CreatedAt: createdAt.Format(time.RFC3339), Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Title: title, Description: description,
		State: reportState{
			Launcher: status.State, Message: status.Message,
			Identity: status.Identity,
			Auth:     status.Auth, Server: status.Server, Patch: status.Patch,
			Game: status.Game, Avatar: status.Avatar, AuthError: status.AuthError,
			ServerError: status.ServerError, PatchError: status.PatchError,
			GameError: status.GameError, AvatarError: status.AvatarError,
			LastRun: status.LastRun, IsLastRunFailure: status.IsLastRunFailure,
			IsCinematicSkipped: status.IsCinematicSkipped,
		},
		Gameplay: gameplay, GameplayError: gameplayError,
		Files: records,
	}
}

func addReportText(archive *zip.Writer, title string, description string) error {
	w, err := archive.Create("report.txt")
	if err != nil {
		return fmt.Errorf("textCreate: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n\n%s\n", title, description)
	if err != nil {
		return fmt.Errorf("textWrite: %w", err)
	}
	return nil
}

func reportSlug(title string) string {
	var slug strings.Builder
	isSeparator := false
	for _, character := range strings.ToLower(title) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			if slug.Len() >= maximumReportSlugSize {
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
		return "report"
	}
	return result
}

func reportSources(logPath string) ([]reportSource, error) {
	sources := make([]reportSource, 0, 8)
	err := filepath.WalkDir(logPath, func(
		path string, entry os.DirEntry, walkErr error,
	) error {
		if walkErr != nil {
			return fmt.Errorf("sourceWalk: %w", walkErr)
		}
		if entry.IsDir() && strings.EqualFold(entry.Name(), "snapshots") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".log" && extension != ".json" && extension != ".jsonl" && extension != ".txt" {
			return nil
		}
		relativePath, relativeErr := filepath.Rel(logPath, path)
		if relativeErr != nil {
			return fmt.Errorf("sourceRelative: %w", relativeErr)
		}
		sources = append(sources, reportSource{
			path: path,
			name: filepath.ToSlash(filepath.Join("logs", relativePath)),
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return sources, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sourceList: %w", err)
	}
	return sources, nil
}

func addReportFile(archive *zip.Writer, source reportSource) (reportFileRecord, error) {
	r, err := os.Open(source.path)
	if err != nil {
		return reportFileRecord{}, fmt.Errorf("sourceOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return reportFileRecord{}, fmt.Errorf("sourceStat: %w", err)
	}
	header := &zip.FileHeader{Name: source.name, Method: zip.Deflate}
	header.SetModTime(fi.ModTime())
	w, err := archive.CreateHeader(header)
	if err != nil {
		return reportFileRecord{}, fmt.Errorf("entryCreate: %w", err)
	}
	_, err = io.Copy(w, r)
	if err != nil {
		return reportFileRecord{}, fmt.Errorf("entryCopy: %w", err)
	}
	return reportFileRecord{
		Name: source.name, OriginalSize: fi.Size(), IncludedSize: fi.Size(),
		IsTruncated: false,
	}, nil
}

func addReportManifest(archive *zip.Writer, manifest reportManifest) error {
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("manifestMarshal: %w", err)
	}
	w, err := archive.Create("report.json")
	if err != nil {
		return fmt.Errorf("manifestCreate: %w", err)
	}
	_, err = w.Write(append(contents, '\n'))
	if err != nil {
		return fmt.Errorf("manifestWrite: %w", err)
	}
	return nil
}

func revealReportDirectory(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("explorer.exe", path)
	case "darwin":
		command = exec.Command("open", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	err := command.Start()
	if err != nil {
		return fmt.Errorf("revealStart: %w", err)
	}
	return nil
}
