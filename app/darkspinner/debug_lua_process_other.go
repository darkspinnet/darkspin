//go:build scenario && !windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

func configureDebugLuaProcess(command *exec.Cmd) {}
func containDebugLuaProcess(process *os.Process) (uintptr, error) {
	return 0, errors.New("Lua helper containment requires Windows")
}
func closeDebugLuaJob(job uintptr) error           { return nil }
func (e *debugSupervisor) closeLuaJob(job uintptr) {}
