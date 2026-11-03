// Package localstore confines diagnostic access to runtime logs and bug reports.
package localstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/server/diagnostic"
)

const maxScannedEntries = 10000

type Store struct {
	runtimePath string
}

func New(runtimePath string) *Store {
	return &Store{runtimePath: runtimePath}
}

func closeRoot(root *os.Root) {
	err := root.Close()
	if err != nil {
		// A read-only directory handle has no pending writes to recover.
	}
}

func closeReader(r *os.File) {
	err := r.Close()
	if err != nil {
		// Read-only handles have no pending writes to recover.
	}
}

func storageError(step string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", step, diagnostic.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", step, errors.Join(diagnostic.ErrUnavailable, err))
}

func (e *Store) openRoot(ctx context.Context, kind string) (*os.Root, error) {
	if ctx == nil {
		return nil, diagnostic.ErrInvalidRequest
	}
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("rootContext: %w", err)
	}
	err = diagnostic.ValidateKind(kind)
	if err != nil {
		return nil, fmt.Errorf("rootKind: %w", err)
	}
	if e.runtimePath == "" {
		return nil, diagnostic.ErrUnavailable
	}
	runtimeRoot, err := os.OpenRoot(e.runtimePath)
	if err != nil {
		return nil, storageError("runtimeOpen", err)
	}
	defer closeRoot(runtimeRoot)
	dir := "logs"
	if kind == "bug" {
		dir = "bugs"
	}
	fi, err := runtimeRoot.Lstat(dir)
	if err != nil {
		return nil, storageError("rootStat", err)
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return nil, diagnostic.ErrUnavailable
	}
	root, err := runtimeRoot.OpenRoot(dir)
	if err != nil {
		return nil, storageError("rootOpen", err)
	}
	return root, nil
}

func isAllowed(kind, name string) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".log", ".txt", ".json", ".jsonl", ".dmp":
		return true
	case ".zip":
		return kind == "bug"
	}
	if kind != "bug" || len(ext) != 4 || !strings.EqualFold(path.Ext(strings.TrimSuffix(name, path.Ext(name))), ".zip") {
		return false
	}
	// Existing split ZIP parts are downloadable as individual files.
	for _, digit := range ext[1:] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return ext != ".000"
}

func inspect(root *os.Root, req diagnostic.FileRequest) (os.FileInfo, error) {
	err := diagnostic.ValidateName(req.Name)
	if err != nil {
		return nil, fmt.Errorf("inspectName: %w", err)
	}
	if !isAllowed(req.Kind, req.Name) {
		return nil, diagnostic.ErrInvalidRequest
	}
	parts := strings.Split(req.Name, "/")
	for index := range parts {
		name := strings.Join(parts[:index+1], "/")
		fi, statErr := root.Lstat(name)
		if statErr != nil {
			return nil, storageError("inspectStat", statErr)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, diagnostic.ErrInvalidRequest
		}
		if index < len(parts)-1 {
			if !fi.IsDir() {
				return nil, diagnostic.ErrNotFound
			}
			continue
		}
		if !fi.Mode().IsRegular() {
			return nil, diagnostic.ErrInvalidRequest
		}
		return fi, nil
	}
	return nil, diagnostic.ErrInvalidRequest
}

func metadata(name string, fi os.FileInfo) diagnostic.File {
	return diagnostic.File{Name: name, Size: fi.Size(), ModifiedAt: fi.ModTime().UTC()}
}

func (e *Store) List(ctx context.Context, req diagnostic.ListRequest) (diagnostic.List, error) {
	if req.Limit < 1 || req.Limit > 100 {
		return diagnostic.List{}, diagnostic.ErrInvalidRequest
	}
	if req.After != "" {
		err := diagnostic.ValidateName(req.After)
		if err != nil {
			return diagnostic.List{}, fmt.Errorf("listAfter: %w", err)
		}
	}
	root, err := e.openRoot(ctx, req.Kind)
	if errors.Is(err, diagnostic.ErrNotFound) {
		return diagnostic.List{Files: []diagnostic.File{}}, nil
	}
	if err != nil {
		return diagnostic.List{}, fmt.Errorf("listRoot: %w", err)
	}
	defer closeRoot(root)
	scan := scanner{root: root, kind: req.Kind, files: make([]diagnostic.File, 0)}
	err = scan.walk(ctx, ".", 0)
	if err != nil {
		return diagnostic.List{}, fmt.Errorf("listScan: %w", err)
	}
	sort.Slice(scan.files, func(i, j int) bool { return scan.files[i].Name < scan.files[j].Name })
	result := diagnostic.List{Files: make([]diagnostic.File, 0, req.Limit)}
	for _, entry := range scan.files {
		if entry.Name <= req.After {
			continue
		}
		if len(result.Files) == req.Limit {
			result.Next = result.Files[len(result.Files)-1].Name
			break
		}
		result.Files = append(result.Files, entry)
	}
	return result, nil
}

type scanner struct {
	root    *os.Root
	kind    string
	files   []diagnostic.File
	scanned int
}

func (e *scanner) walk(ctx context.Context, dir string, depth int) error {
	if depth > 16 {
		return diagnostic.ErrTooLarge
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("scanContext: %w", err)
	}
	r, err := e.root.Open(dir)
	if err != nil {
		return storageError("scanOpen", err)
	}
	defer closeReader(r)
	for {
		entries, readErr := r.ReadDir(128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return storageError("scanRead", readErr)
		}
		for _, entry := range entries {
			e.scanned++
			if e.scanned > maxScannedEntries {
				return diagnostic.ErrTooLarge
			}
			err = ctx.Err()
			if err != nil {
				return fmt.Errorf("entryContext: %w", err)
			}
			name := path.Join(dir, entry.Name())
			err = diagnostic.ValidateName(name)
			if err != nil {
				continue
			}
			fi, statErr := e.root.Lstat(name)
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil {
				return storageError("entryStat", statErr)
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if fi.IsDir() {
				err = e.walk(ctx, name, depth+1)
				if err != nil {
					return fmt.Errorf("scanChild: %w", err)
				}
				continue
			}
			if fi.Mode().IsRegular() && isAllowed(e.kind, name) {
				e.files = append(e.files, metadata(name, fi))
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (e contextReader) Read(payload []byte) (int, error) {
	err := e.ctx.Err()
	if err != nil {
		return 0, fmt.Errorf("readContext: %w", err)
	}
	count, err := e.r.Read(payload)
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("readFile: %w", err)
	}
	return count, nil
}

func (e *Store) Download(ctx context.Context, req diagnostic.FileRequest) (diagnostic.Download, error) {
	root, err := e.openRoot(ctx, req.Kind)
	if err != nil {
		return diagnostic.Download{}, fmt.Errorf("downloadRoot: %w", err)
	}
	defer closeRoot(root)
	fi, err := inspect(root, req)
	if err != nil {
		return diagnostic.Download{}, fmt.Errorf("downloadInspect: %w", err)
	}
	if fi.Size() > diagnostic.MaxDownloadSize {
		return diagnostic.Download{}, diagnostic.ErrTooLarge
	}
	r, err := root.Open(req.Name)
	if err != nil {
		return diagnostic.Download{}, storageError("downloadOpen", err)
	}
	defer closeReader(r)
	openedFI, err := r.Stat()
	if err != nil {
		return diagnostic.Download{}, storageError("downloadStat", err)
	}
	if !openedFI.Mode().IsRegular() || !os.SameFile(fi, openedFI) {
		return diagnostic.Download{}, diagnostic.ErrConflict
	}
	if openedFI.Size() > diagnostic.MaxDownloadSize {
		return diagnostic.Download{}, diagnostic.ErrTooLarge
	}
	payload, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, r: r}, diagnostic.MaxDownloadSize+1))
	if err != nil {
		return diagnostic.Download{}, fmt.Errorf("downloadRead: %w", errors.Join(diagnostic.ErrUnavailable, err))
	}
	if len(payload) > diagnostic.MaxDownloadSize {
		return diagnostic.Download{}, diagnostic.ErrTooLarge
	}
	err = ctx.Err()
	if err != nil {
		return diagnostic.Download{}, fmt.Errorf("downloadContext: %w", err)
	}
	file := metadata(req.Name, openedFI)
	file.Size = int64(len(payload))
	return diagnostic.Download{File: file, Content: payload}, nil
}

func (e *Store) Delete(ctx context.Context, req diagnostic.FileRequest) error {
	root, err := e.openRoot(ctx, req.Kind)
	if err != nil {
		return fmt.Errorf("deleteRoot: %w", err)
	}
	defer closeRoot(root)
	fi, err := inspect(root, req)
	if err != nil {
		return fmt.Errorf("deleteInspect: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return diagnostic.ErrInvalidRequest
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("deleteContext: %w", err)
	}
	err = root.Remove(req.Name)
	if err != nil {
		return storageError("deleteRemove", err)
	}
	return nil
}
