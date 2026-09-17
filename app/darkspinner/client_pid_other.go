//go:build !windows

package main

func isProcessIDRunning(processID uint32) bool {
	_ = processID
	return false
}
