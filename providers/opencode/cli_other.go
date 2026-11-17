//go:build !windows

package opencode

import "os/exec"

// hideConsole is a no-op off Windows — those platforms don't flash a console
// when a GUI app spawns a child process.
func hideConsole(cmd *exec.Cmd) {}
