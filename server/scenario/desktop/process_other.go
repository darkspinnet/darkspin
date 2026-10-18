//go:build scenario && !windows

package desktop

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

func isPlatformSupported() bool          { return false }
func configureProcess(command *exec.Cmd) {}
func containProcess(process *os.Process) (io.Closer, error) {
	return nil, errors.New("scenario process containment is unavailable on this platform")
}
