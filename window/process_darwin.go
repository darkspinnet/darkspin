//go:build darwin

package window

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Running returns process IDs whose executable matches executableName.
func Running(executableName string) ([]uint32, error) {
	executableName = strings.TrimSpace(executableName)
	if executableName == "" {
		return nil, errors.New("empty executable name")
	}
	output, err := exec.Command("/bin/ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return nil, fmt.Errorf("processList: %w", err)
	}
	processIDs := make([]uint32, 0)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		separator := strings.IndexAny(line, " \t")
		if separator < 0 {
			continue
		}
		command := strings.TrimSpace(line[separator:])
		if !strings.EqualFold(filepath.Base(command), executableName) {
			continue
		}
		processID, err := strconv.ParseUint(line[:separator], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("processID: %w", err)
		}
		processIDs = append(processIDs, uint32(processID))
	}
	return processIDs, nil
}
