package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type nativeCrashFile struct {
	source     reportSource
	size       int64
	modifiedAt time.Time
}

type nativeCrashReport struct {
	directory string
	files     map[string]nativeCrashFile
}

// Build 103's exception writer uses the executable directory. Also inspect the
// installation root used as the client's working directory, without recursing
// through installed assets or including unrelated text and binary files.
func readNativeCrashFiles(directory string) (map[string]nativeCrashFile, error) {
	files := make(map[string]nativeCrashFile)
	paths := []string{directory, filepath.Dir(defaultGameExecutable(directory))}
	for _, path := range paths {
		candidates, err := os.ReadDir(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("crashDirectory: %w", err)
		}
		for _, candidate := range candidates {
			if !candidate.Type().IsRegular() || !isNativeCrashFilename(candidate.Name()) {
				continue
			}
			fi, err := candidate.Info()
			if err != nil {
				return nil, fmt.Errorf("crashStat: %w", err)
			}
			filePath := filepath.Join(path, candidate.Name())
			relativePath, err := filepath.Rel(directory, filePath)
			if err != nil {
				return nil, fmt.Errorf("crashRelative: %w", err)
			}
			files[filePath] = nativeCrashFile{
				source: reportSource{
					path: filePath,
					name: filepath.ToSlash(filepath.Join("native-crash", relativePath)),
				},
				size: fi.Size(), modifiedAt: fi.ModTime(),
			}
		}
	}
	return files, nil
}

func isNativeCrashFilename(name string) bool {
	name = strings.ToLower(name)
	if name == "exception.txt" || name == "minidump.mdmp" {
		return true
	}
	if !strings.HasPrefix(name, "exception report ") {
		return false
	}
	return strings.HasSuffix(name, " exception.txt") ||
		strings.HasSuffix(name, " minidump.mdmp")
}

func (e *App) snapshotNativeCrashReports() *nativeCrashReport {
	directory := e.gameDirectory()
	if directory == "" {
		return nil
	}
	files, err := readNativeCrashFiles(directory)
	if err != nil {
		e.log(fmt.Sprintf("Native crash report baseline unavailable: %v", err))
		return nil
	}
	return &nativeCrashReport{directory: directory, files: files}
}

// Native diagnostics are optional: an absent, locked, or unreadable dump must
// not prevent the ordinary client logs from being saved. Preserve the original
// files and use the normal report segmentation for oversized binary entries.
func (e *App) addNativeCrashReports(
	archive *zip.Writer, session *launcherReportSession,
) ([]reportFileRecord, []string) {
	if session == nil || !session.isNativeCrashReport || session.nativeCrash == nil {
		return nil, nil
	}
	baseline := session.nativeCrash
	files, err := readNativeCrashFiles(baseline.directory)
	if err != nil {
		e.log(fmt.Sprintf("Native crash report discovery failed: %v", err))
		return nil, []string{err.Error()}
	}
	paths := make([]string, 0, len(files))
	for path, current := range files {
		previous, isPreviousFound := baseline.files[path]
		if isPreviousFound && previous.size == current.size &&
			previous.modifiedAt.Equal(current.modifiedAt) {
			continue
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	records := make([]reportFileRecord, 0, len(paths))
	failures := make([]string, 0)
	for _, path := range paths {
		source := files[path].source
		record, err := addReportFile(archive, source)
		if err != nil {
			message := fmt.Sprintf("Native crash report %s could not be included: %v", source.name, err)
			e.log(message)
			failures = append(failures, message)
			continue
		}
		records = append(records, record)
	}
	return records, failures
}
