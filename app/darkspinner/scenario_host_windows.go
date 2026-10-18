//go:build scenario && windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// Deny write/delete sharing for the entire snapshot copy. Merely checking for
// SQLite sidecars before an ordinary byte copy would race an active writer.
func openScenarioContent(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("contentName: %w", err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("contentLock: %w", err)
	}
	return os.NewFile(uintptr(handle), path), nil
}
