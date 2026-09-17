//go:build windows

package main

import "golang.org/x/sys/windows"

func ensureStandardUser() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return nil
	}
	return errElevatedLaunch
}
