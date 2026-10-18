//go:build scenario && windows

package desktop

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func isPlatformSupported() bool { return true }
func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

type processJob struct{ handle windows.Handle }

func containProcess(process *os.Process) (io.Closer, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("jobCreate: %w", err)
	}
	owner := &processJob{handle: job}
	jobLimit := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	jobLimit.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	count, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&jobLimit)), uint32(unsafe.Sizeof(jobLimit)))
	if err != nil || count == 0 {
		closeErr := owner.Close()
		if err == nil {
			err = errors.New("job limit was not accepted")
		}
		return nil, fmt.Errorf("jobLimits: %w", errors.Join(err, closeErr))
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(process.Pid))
	if err != nil {
		closeErr := owner.Close()
		return nil, fmt.Errorf("workerHandle: %w", errors.Join(err, closeErr))
	}
	err = windows.AssignProcessToJobObject(job, handle)
	closeErr := windows.CloseHandle(handle)
	if err != nil || closeErr != nil {
		jobCloseErr := owner.Close()
		return nil, fmt.Errorf("jobAssign: %w", errors.Join(err, closeErr, jobCloseErr))
	}
	return owner, nil
}

func (e *processJob) Close() error {
	if e.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(e.handle)
	if err != nil {
		return fmt.Errorf("jobClose: %w", err)
	}
	e.handle = 0
	return nil
}
