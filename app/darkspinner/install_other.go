//go:build !windows

package main

import "errors"

func isAutomaticRelocationSupported() bool {
	return false
}

func relocateAndRestart(string, string, []string) error {
	return errors.New("automatic relocation is supported on Windows only")
}

func replaceAndRestart(string, string) error {
	return errors.New("automatic replacement is supported on Windows only")
}

func restartAfterExit(string) error {
	return errors.New("automatic restart is supported on Windows only")
}
