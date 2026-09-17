//go:build windows && (!amd64 || !loader)

package main

import (
	"errors"
	"fmt"
	"runtime"
)

func gameLoaderAddress() (uintptr, error) {
	if runtime.GOARCH != "386" {
		return 0, errors.New("build the 64-bit launcher with Mage to embed the x86 loader helper")
	}
	err := procLoadLibraryW.Find()
	if err != nil {
		return 0, fmt.Errorf("loaderFind: %w", err)
	}
	return procLoadLibraryW.Addr(), nil
}
