package opencode

import (
	"os"
	"os/exec"
)

// runCLI invokes the opencode CLI. DataDir is only an execution hint; the
// CLI resolves its own data directory.
func runCLI(dataDir string, args ...string) (string, error) {
	cmd := exec.Command("opencode", args...)
	if dataDir != "" {
		cmd.Env = append(os.Environ(),
			"XDG_DATA_HOME="+parentOf(dataDir))
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func parentOf(dir string) string {
	for i := len(dir) - 1; i > 0; i-- {
		if dir[i] == '\\' || dir[i] == '/' {
			return dir[:i]
		}
	}
	return dir
}
