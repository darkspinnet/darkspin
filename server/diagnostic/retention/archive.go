package retention

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const archiveTempPrefix = ".daily-log-"

func dailyLogDate(name string) (time.Time, bool) {
	switch filepath.Ext(name) {
	case ".txt", ".zip":
		name = strings.TrimSuffix(name, filepath.Ext(name))
	case ".tmp":
		// Interrupted archive attempts expire with the same date as their log.
		if !strings.HasPrefix(name, archiveTempPrefix) || len(name) <= len(archiveTempPrefix)+len(time.DateOnly)+1 {
			return time.Time{}, false
		}
		name = strings.TrimPrefix(name, archiveTempPrefix)
		if name[len(time.DateOnly)] != '-' {
			return time.Time{}, false
		}
		name = name[:len(time.DateOnly)]
	default:
		return time.Time{}, false
	}
	date, err := time.Parse(time.DateOnly, name)
	return date, err == nil && date.Format(time.DateOnly) == name
}

func archiveDailyLog(ctx context.Context, root *os.Root, name string, fi os.FileInfo) (resultErr error) {
	stem := strings.TrimSuffix(name, ".txt")
	archiveName := stem + ".zip"
	_, err := root.Lstat(archiveName)
	if err == nil {
		// An existing archive may predate the current source. Preserve both.
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("archiveStat: %w", err)
	}
	tempName := archiveTempPrefix + stem + "-" + rand.Text() + ".tmp"
	w, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("archiveCreate: %w", err)
	}
	defer func() {
		removeErr := root.Remove(tempName)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("archiveTempRemove: %w", removeErr))
		}
	}()
	writeErr := writeDailyLogZIP(ctx, root, w, name, fi)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("archiveFinish: %w", errors.Join(writeErr, closeErr))
	}
	err = root.Chtimes(tempName, fi.ModTime(), fi.ModTime())
	if err != nil {
		return fmt.Errorf("archiveTime: %w", err)
	}
	err = checkArchiveSource(ctx, root, name, fi)
	if err != nil {
		return fmt.Errorf("sourceBeforePublish: %w", err)
	}
	// Publish the completed file atomically without replacing another archive.
	err = root.Link(tempName, archiveName)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("archivePublish: %w", err)
	}
	err = checkArchiveSource(ctx, root, name, fi)
	if err != nil {
		return fmt.Errorf("sourceBeforeRemove: %w", err)
	}
	err = root.Remove(name)
	if err != nil {
		return fmt.Errorf("archiveSourceRemove: %w", err)
	}
	return nil
}

func writeDailyLogZIP(ctx context.Context, root *os.Root, w *os.File, name string, fi os.FileInfo) (resultErr error) {
	r, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("sourceOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("sourceClose: %w", closeErr))
		}
	}()
	openedFI, err := r.Stat()
	if err != nil {
		return fmt.Errorf("sourceHandleStat: %w", err)
	}
	if !isSameLog(fi, openedFI) {
		return errors.New("daily log changed before compression")
	}
	header, err := zip.FileInfoHeader(openedFI)
	if err != nil {
		return fmt.Errorf("zipHeader: %w", err)
	}
	header.Name = name
	header.Method = zip.Deflate
	archive := zip.NewWriter(w)
	entry, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("zipEntry: %w", err)
	}
	count, copyErr := io.CopyN(entry, archiveReader{ctx: ctx, r: r}, fi.Size())
	zipErr := archive.Close()
	if copyErr != nil || zipErr != nil {
		return fmt.Errorf("zipWrite[%d]: %w", count, errors.Join(copyErr, zipErr))
	}
	err = w.Sync()
	if err != nil {
		return fmt.Errorf("archiveSync: %w", err)
	}
	return nil
}

func checkArchiveSource(ctx context.Context, root *os.Root, name string, fi os.FileInfo) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("archiveContext: %w", err)
	}
	currentFI, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("sourceStat: %w", err)
	}
	if !isSameLog(fi, currentFI) {
		return errors.New("daily log changed during compression")
	}
	return nil
}

func isSameLog(fi, currentFI os.FileInfo) bool {
	return currentFI.Mode().IsRegular() && os.SameFile(fi, currentFI) &&
		fi.Size() == currentFI.Size() && fi.ModTime().Equal(currentFI.ModTime())
}

type archiveReader struct {
	ctx context.Context
	r   *os.File
}

func (e archiveReader) Read(payload []byte) (int, error) {
	err := e.ctx.Err()
	if err != nil {
		return 0, fmt.Errorf("archiveReadContext: %w", err)
	}
	count, err := e.r.Read(payload)
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("archiveRead: %w", err)
	}
	return count, nil
}
