//go:build (linux || darwin) && !bindings

package window

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// InstanceOptions controls single-instance discovery and replacement.
type InstanceOptions struct {
	MutexName string
}

// Acquire enforces a named single instance for the current executable.
func Acquire(options InstanceOptions) (func(), bool, error) {
	if strings.TrimSpace(options.MutexName) == "" {
		return nil, false, errors.New("empty mutex name")
	}
	cachePath, err := os.UserCacheDir()
	if err != nil {
		return nil, false, fmt.Errorf("cachePath: %w", err)
	}
	lockPath := filepath.Join(cachePath, "darkspin")
	err = os.MkdirAll(lockPath, 0o755)
	if err != nil {
		return nil, false, fmt.Errorf("lockMkdir: %w", err)
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(options.MutexName))
	lockPath = filepath.Join(lockPath, fmt.Sprintf("instance-%x.lock", hash.Sum64()))
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("lockOpen: %w", err)
	}
	err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		_ = lock.Close()
		return func() {}, true, nil
	}
	if err != nil {
		_ = lock.Close()
		return nil, false, fmt.Errorf("lockAcquire: %w", err)
	}
	release := func() {
		_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		_ = lock.Close()
	}
	return release, false, nil
}
