//go:build scenario && !windows

package main

import "errors"

func createDebugPrivateDirectory(path string) error {
	return errors.New("scenario debug supervisor requires Windows")
}
