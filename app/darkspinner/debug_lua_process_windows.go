//go:build scenario && windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureDebugLuaProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// This job owns only the helper. Game/server processes never share its limits.
func containDebugLuaProcess(process *os.Process) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("luaJobCreate: %w", err)
	}
	limit := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{ProcessMemoryLimit: 256 * 1024 * 1024}
	limit.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
	limit.BasicLimitInformation.ActiveProcessLimit = 1
	count, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limit)), uint32(unsafe.Sizeof(limit)))
	if err != nil || count == 0 {
		closeErr := windows.CloseHandle(job)
		if err == nil {
			err = errors.New("helper job limits were not accepted")
		}
		return 0, fmt.Errorf("luaJobLimit: %w", errors.Join(err, closeErr))
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(process.Pid))
	if err != nil {
		closeErr := windows.CloseHandle(job)
		return 0, fmt.Errorf("luaJobProcess: %w", errors.Join(err, closeErr))
	}
	err = windows.AssignProcessToJobObject(job, handle)
	closeErr := windows.CloseHandle(handle)
	if err != nil || closeErr != nil {
		jobCloseErr := windows.CloseHandle(job)
		return 0, fmt.Errorf("luaJobAssign: %w", errors.Join(err, closeErr, jobCloseErr))
	}
	return job, nil
}

func closeDebugLuaJob(job windows.Handle) error {
	err := windows.CloseHandle(job)
	if err != nil {
		return fmt.Errorf("luaJobClose: %w", err)
	}
	return nil
}

func (e *debugSupervisor) closeLuaJob(job windows.Handle) {
	err := closeDebugLuaJob(job)
	if err != nil {
		e.logFailure("Lua helper job close", err)
	}
}
