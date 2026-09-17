//go:build windows

package main

import "golang.org/x/sys/windows"

func isProcessIDRunning(processID uint32) bool {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, processID)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	result, err := windows.WaitForSingleObject(process, 0)
	return err == nil && result == uint32(windows.WAIT_TIMEOUT)
}
