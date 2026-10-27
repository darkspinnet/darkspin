//go:build linux || darwin

package instance

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func acquireLock(lock *os.File) error {
	err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return fmt.Errorf("runtimeBusy: %w", ErrRunning)
	}
	if err != nil {
		return fmt.Errorf("fileLock: %w", err)
	}
	return nil
}
