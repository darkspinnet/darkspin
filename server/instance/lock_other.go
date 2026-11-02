//go:build !windows && !linux && !darwin

package instance

import (
	"fmt"
	"os"
	"runtime"
)

func acquireLock(lock *os.File) error {
	return fmt.Errorf("server runtime leases are unsupported on %s", runtime.GOOS)
}
