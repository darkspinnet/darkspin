//go:build windows && amd64 && loader

package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed fangloader.exe
var embeddedLoader []byte

// Windows shares the same x86 system DLL mappings between local processes.
// Resolve in an x86 process: the amd64 launcher's loader address cannot be used
// as the entry point of a thread in the 32-bit game.
func gameLoaderAddress() (uintptr, error) {
	directory, err := os.MkdirTemp("", "darkspinner-loader-")
	if err != nil {
		return 0, fmt.Errorf("loaderTemp: %w", err)
	}
	defer removeLoaderDirectory(directory)
	path := filepath.Join(directory, "fangloader.exe")
	err = os.WriteFile(path, embeddedLoader, 0o700)
	if err != nil {
		return 0, fmt.Errorf("loaderWrite: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.Output()
	if err != nil {
		return 0, fmt.Errorf("loaderRun: %w", err)
	}
	address, err := strconv.ParseUint(strings.TrimSpace(string(output)), 16, 32)
	if err != nil {
		return 0, fmt.Errorf("loaderAddress: %w", err)
	}
	if address == 0 {
		return 0, fmt.Errorf("empty x86 loader address")
	}
	return uintptr(address), nil
}

func removeLoaderDirectory(directory string) {
	err := os.RemoveAll(directory)
	if err != nil {
		log.Printf("loader cleanup: %v", err)
	}
}
