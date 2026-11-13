//go:build !unix

package containment

import (
	"os"
	"os/exec"
)

func configureProcessGroup(_ *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func extractProcessTelemetry(_ *os.ProcessState) (peakRSSBytes int64, signalName string) {
	return 0, ""
}
