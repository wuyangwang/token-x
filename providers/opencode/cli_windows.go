//go:build windows

package opencode

import (
	"os/exec"
	"syscall"
)

// hideConsole stops Windows from flashing a console window when the GUI app
// spawns the CLI. CREATE_NO_WINDOW gives the child a hidden console; its
// output still reaches the pipes set up by CombinedOutput.
func hideConsole(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}
