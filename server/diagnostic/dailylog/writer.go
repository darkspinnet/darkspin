// Package dailylog writes raw diagnostic output to UTC daily log files.
package dailylog

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Writer appends to runtimePath/logs/logs/YYYY-MM-DD.txt. The caller owns
// timestamps and formatting. Rotate may be called while idle to advance the date.
type Writer struct {
	mu          sync.Mutex
	runtimePath string
	root        *os.Root
	output      *os.File
	date        string
	isClosed    bool
}

// New opens today's log beneath an existing, trusted runtime directory.
func New(runtimePath string) (*Writer, error) {
	root, err := openLogRoot(runtimePath)
	if err != nil {
		return nil, fmt.Errorf("writerRoot: %w", err)
	}
	e := &Writer{root: root, runtimePath: runtimePath}
	err = e.rotate(time.Now().UTC())
	if err != nil {
		closeErr := e.Close()
		return nil, fmt.Errorf("writerOpen: %w", errors.Join(err, closeErr))
	}
	return e, nil
}

// Write appends payload unchanged, reopening a deleted or replaced active log.
func (e *Writer) Write(payload []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isClosed {
		return 0, fmt.Errorf("writeClosed: %w", os.ErrClosed)
	}
	err := e.rotate(time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("writeRotate: %w", err)
	}
	n, err := e.output.Write(payload)
	if err != nil {
		return n, fmt.Errorf("logWrite: %w", err)
	}
	return n, nil
}

// Rotate opens the current UTC day's log even when no output is being written.
func (e *Writer) Rotate() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isClosed {
		return fmt.Errorf("rotateClosed: %w", os.ErrClosed)
	}
	err := e.rotate(time.Now().UTC())
	if err != nil {
		return fmt.Errorf("logRotate: %w", err)
	}
	return nil
}

// Close flushes and closes the active log and its directory handle.
func (e *Writer) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isClosed {
		return nil
	}
	e.isClosed = true
	outputErr := closeOutput(e.output)
	rootErr := e.root.Close()
	err := errors.Join(outputErr, rootErr)
	if err != nil {
		return fmt.Errorf("writerClose: %w", err)
	}
	return nil
}

func (e *Writer) rotate(now time.Time) error {
	err := e.refreshRoot()
	if err != nil {
		return fmt.Errorf("rootRefresh: %w", err)
	}
	date := now.UTC().Format(time.DateOnly)
	name := date + ".txt"
	if e.output != nil && e.date == date {
		fi, err := e.root.Lstat(name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("activeStat: %w", err)
		}
		if err == nil {
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("activeType: %s is not a regular file", name)
			}
			currentFI, statErr := e.output.Stat()
			if statErr != nil {
				return fmt.Errorf("outputStat: %w", statErr)
			}
			if os.SameFile(fi, currentFI) {
				return nil
			}
		}
	}
	output, err := openOutput(e.root, name)
	if err != nil {
		return fmt.Errorf("dailyOpen: %w", err)
	}
	previous := e.output
	e.output = output
	e.date = date
	err = closeOutput(previous)
	if err != nil {
		return fmt.Errorf("previousClose: %w", err)
	}
	return nil
}

func (e *Writer) refreshRoot() error {
	root, err := openLogRoot(e.runtimePath)
	if err != nil {
		return fmt.Errorf("rootOpen: %w", err)
	}
	fi, statErr := e.root.Stat(".")
	rootFI, rootStatErr := root.Stat(".")
	if statErr != nil || rootStatErr != nil {
		closeErr := root.Close()
		return fmt.Errorf("rootStat: %w", errors.Join(statErr, rootStatErr, closeErr))
	}
	if os.SameFile(fi, rootFI) {
		closeErr := root.Close()
		if closeErr != nil {
			return fmt.Errorf("duplicateClose: %w", closeErr)
		}
		return nil
	}
	previous := e.root
	e.root = root
	err = previous.Close()
	if err != nil {
		return fmt.Errorf("previousClose: %w", err)
	}
	return nil
}

func openOutput(root *os.Root, name string) (*os.File, error) {
	fi, err := root.Lstat(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("logStat: %w", err)
	}
	if err == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("logType: %s is not a regular file", name)
	}
	output, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("logOpen: %w", err)
	}
	err = validateOutput(root, name, output)
	if err != nil {
		closeErr := output.Close()
		return nil, fmt.Errorf("logValidate: %w", errors.Join(err, closeErr))
	}
	return output, nil
}

func validateOutput(root *os.Root, name string, output *os.File) error {
	fi, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("entryStat: %w", err)
	}
	outputFI, err := output.Stat()
	if err != nil {
		return fmt.Errorf("handleStat: %w", err)
	}
	if !fi.Mode().IsRegular() || !outputFI.Mode().IsRegular() ||
		!os.SameFile(fi, outputFI) {
		return fmt.Errorf("logIdentity: %s is not the opened regular file", name)
	}
	return nil
}

func closeOutput(output *os.File) error {
	if output == nil {
		return nil
	}
	syncErr := output.Sync()
	closeErr := output.Close()
	if syncErr != nil {
		syncErr = fmt.Errorf("logSync: %w", syncErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("logClose: %w", closeErr)
	}
	return errors.Join(syncErr, closeErr)
}

func openLogRoot(runtimePath string) (*os.Root, error) {
	root, err := os.OpenRoot(runtimePath)
	if err != nil {
		return nil, fmt.Errorf("runtimeOpen: %w", err)
	}
	for range 2 {
		child, openErr := openLogDirectory(root)
		closeErr := root.Close()
		if openErr != nil || closeErr != nil {
			if child != nil {
				childCloseErr := child.Close()
				closeErr = errors.Join(closeErr, childCloseErr)
			}
			return nil, fmt.Errorf("directoryOpen: %w", errors.Join(openErr, closeErr))
		}
		root = child
	}
	return root, nil
}

func openLogDirectory(parent *os.Root) (*os.Root, error) {
	err := parent.Mkdir("logs", 0o700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("directoryCreate: %w", err)
	}
	fi, err := parent.Lstat("logs")
	if err != nil {
		return nil, fmt.Errorf("directoryStat: %w", err)
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("directoryType: logs is not a plain directory")
	}
	root, err := parent.OpenRoot("logs")
	if err != nil {
		return nil, fmt.Errorf("directoryHandle: %w", err)
	}
	err = validateDirectory(parent, root)
	if err != nil {
		closeErr := root.Close()
		return nil, fmt.Errorf("directoryValidate: %w", errors.Join(err, closeErr))
	}
	return root, nil
}

func validateDirectory(parent, root *os.Root) error {
	fi, err := parent.Lstat("logs")
	if err != nil {
		return fmt.Errorf("entryStat: %w", err)
	}
	rootFI, err := root.Stat(".")
	if err != nil {
		return fmt.Errorf("handleStat: %w", err)
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || !os.SameFile(fi, rootFI) {
		return fmt.Errorf("directoryIdentity: logs is not the opened directory")
	}
	return nil
}
