//go:build unix

package containment

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	_ = cmd.Process.Kill()
}

func extractProcessTelemetry(state *os.ProcessState) (peakRSSBytes int64, signalName string) {
	if state == nil {
		return 0, ""
	}
	if usage, ok := state.SysUsage().(*syscall.Rusage); ok && usage != nil {
		rss := int64(usage.Maxrss)
		if runtime.GOOS == "darwin" {
			peakRSSBytes = rss
		} else {
			peakRSSBytes = rss * 1024
		}
	}
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		sig := ws.Signal()
		signalName = sig.String()
	}
	return peakRSSBytes, signalName
}
