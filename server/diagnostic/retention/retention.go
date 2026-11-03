// Package retention removes expired files from the dedicated server's diagnostics.
package retention

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const retentionAge = 7 * 24 * time.Hour

type sweep struct {
	cutoff         time.Time
	firstDate      time.Time
	protectedPaths map[string]bool
}

// Sweep expires diagnostics below runtimePath/logs/logs, runtimePath/logs/traces,
// and runtimePath/bugs. Daily logs retain the current UTC date and six preceding
// dates; trace and bug files retain seven days by modification time. Absolute
// protectedPaths identify files currently owned by active writers.
func Sweep(ctx context.Context, runtimePath string, protectedPaths []string) error {
	err := sweepTrees(ctx, runtimePath, protectedPaths, true)
	if err != nil {
		return fmt.Errorf("diagnosticSweep: %w", err)
	}
	return nil
}

// SweepLogs expires only dated daily logs, allowing the bootstrap process to
// retain logs without touching artifacts owned by its running server child.
func SweepLogs(ctx context.Context, runtimePath string) error {
	err := sweepTrees(ctx, runtimePath, nil, false)
	if err != nil {
		return fmt.Errorf("logSweep: %w", err)
	}
	return nil
}

func sweepTrees(ctx context.Context, runtimePath string, protectedPaths []string, isArtifactsIncluded bool) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("sweepContext: %w", err)
	}
	absPath, err := filepath.Abs(runtimePath)
	if err != nil {
		return fmt.Errorf("runtimePath: %w", err)
	}
	now := time.Now().UTC()
	e := sweep{
		cutoff:         now.Add(-retentionAge),
		firstDate:      time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -6),
		protectedPaths: make(map[string]bool, len(protectedPaths)),
	}
	for _, protectedPath := range protectedPaths {
		if !filepath.IsAbs(protectedPath) {
			return fmt.Errorf("protectedPath: absolute path required: %q", protectedPath)
		}
		e.protectedPaths[pathKey(protectedPath)] = true
	}
	root, err := os.OpenRoot(absPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("runtimeOpen: %w", err)
	}
	var errs []error
	relPaths := []string{filepath.Join("logs", "logs")}
	if isArtifactsIncluded {
		relPaths = append(relPaths, filepath.Join("logs", "traces"), "bugs")
	}
	for _, relPath := range relPaths {
		err = ctx.Err()
		if err != nil {
			errs = append(errs, fmt.Errorf("treeContext: %w", err))
			break
		}
		err = e.tree(ctx, root, absPath, relPath, relPath != filepath.Join("logs", "logs"))
		if err != nil {
			errs = append(errs, fmt.Errorf("treeSweep[%s]: %w", relPath, err))
		}
	}
	err = root.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("runtimeClose: %w", err))
	}
	return errors.Join(errs...)
}

// Open each fixed path component independently so links are never traversed.
func (e *sweep) tree(ctx context.Context, root *os.Root, absPath, relPath string, isRecursive bool) error {
	parts := strings.Split(relPath, string(filepath.Separator))
	part := parts[0]
	fi, err := root.Lstat(part)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("treeStat: %w", err)
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	childRoot, err := root.OpenRoot(part)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("treeOpen: %w", err)
	}
	isSame, identityErr := sameDirectory(childRoot, fi)
	if identityErr != nil {
		err = identityErr
	} else if !isSame {
		err = nil
	} else if len(parts) > 1 {
		err = e.tree(ctx, childRoot, filepath.Join(absPath, part), filepath.Join(parts[1:]...), isRecursive)
	} else {
		err = e.entries(ctx, childRoot, filepath.Join(absPath, part), isRecursive)
	}
	closeErr := childRoot.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("treeClose: %w", closeErr)
	}
	return errors.Join(err, closeErr)
}

func (e *sweep) entries(ctx context.Context, root *os.Root, absPath string, isRecursive bool) error {
	r, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("directoryOpen: %w", err)
	}
	var errs []error
	for {
		err = ctx.Err()
		if err != nil {
			errs = append(errs, fmt.Errorf("directoryContext: %w", err))
			break
		}
		entries, readErr := r.ReadDir(128)
		for _, entry := range entries {
			err = ctx.Err()
			if err != nil {
				errs = append(errs, fmt.Errorf("entryContext: %w", err))
				break
			}
			err = e.entry(ctx, root, absPath, entry.Name(), isRecursive)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("entrySweep[%s]: %w", entry.Name(), err))
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && !errors.Is(readErr, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("directoryRead: %w", readErr))
			}
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	err = r.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("directoryClose: %w", err))
	}
	return errors.Join(errs...)
}

func (e *sweep) entry(ctx context.Context, root *os.Root, absPath, name string, isRecursive bool) error {
	if name == "." || name == ".." || filepath.Base(name) != name || !filepath.IsLocal(name) {
		return nil
	}
	if e.protectedPaths[pathKey(filepath.Join(absPath, name))] {
		return nil
	}
	fi, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("entryStat: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if fi.IsDir() {
		if !isRecursive {
			return nil
		}
		return e.directory(ctx, root, absPath, name, fi)
	}
	if !fi.Mode().IsRegular() {
		return nil
	}
	if isRecursive {
		if fi.ModTime().After(e.cutoff) {
			return nil
		}
	} else {
		date, parseErr := time.Parse("2006-01-02.txt", name)
		if parseErr != nil || date.Format("2006-01-02.txt") != name || !date.Before(e.firstDate) {
			return nil
		}
	}
	currentFI, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("removeStat: %w", err)
	}
	if !currentFI.Mode().IsRegular() || !os.SameFile(fi, currentFI) || (isRecursive && currentFI.ModTime().After(e.cutoff)) {
		return nil
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("removeContext: %w", err)
	}
	err = root.Remove(name)
	if err != nil {
		return fmt.Errorf("entryRemove: %w", err)
	}
	return nil
}

func (e *sweep) directory(ctx context.Context, root *os.Root, absPath, name string, fi os.FileInfo) error {
	childRoot, err := root.OpenRoot(name)
	if err != nil {
		return fmt.Errorf("childOpen: %w", err)
	}
	isSame, identityErr := sameDirectory(childRoot, fi)
	var sweepErr error
	if identityErr != nil {
		sweepErr = identityErr
	} else if isSame {
		sweepErr = e.entries(ctx, childRoot, filepath.Join(absPath, name), true)
	}
	isEmpty := false
	var emptyErr error
	if isSame && !fi.ModTime().After(e.cutoff) && sweepErr == nil {
		isEmpty, emptyErr = directoryEmpty(childRoot)
	}
	closeErr := childRoot.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("childClose: %w", closeErr)
	}
	err = errors.Join(sweepErr, emptyErr, closeErr)
	if err != nil {
		return fmt.Errorf("childSweep: %w", err)
	}
	if !isEmpty {
		return nil
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("childContext: %w", err)
	}
	currentFI, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("childRestat: %w", err)
	}
	if !currentFI.IsDir() || currentFI.Mode()&os.ModeSymlink != 0 || !os.SameFile(fi, currentFI) {
		return nil
	}
	err = root.Remove(name)
	if err != nil {
		return fmt.Errorf("childRemove: %w", err)
	}
	return nil
}

func sameDirectory(root *os.Root, fi os.FileInfo) (bool, error) {
	openedFI, err := root.Stat(".")
	if err != nil {
		return false, fmt.Errorf("directoryIdentity: %w", err)
	}
	return os.SameFile(fi, openedFI), nil
}

func directoryEmpty(root *os.Root) (bool, error) {
	r, err := root.Open(".")
	if err != nil {
		return false, fmt.Errorf("emptyOpen: %w", err)
	}
	entries, readErr := r.ReadDir(1)
	closeErr := r.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("emptyClose: %w", closeErr)
	}
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if readErr != nil {
		readErr = fmt.Errorf("emptyRead: %w", readErr)
	}
	return len(entries) == 0, errors.Join(readErr, closeErr)
}

func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
