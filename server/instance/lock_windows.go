package instance

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func acquireLock(lock *os.File) error {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(windows.Handle(lock.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return fmt.Errorf("runtimeBusy: %w", ErrRunning)
	}
	if err != nil {
		return fmt.Errorf("fileLock: %w", err)
	}
	return nil
}
