// Package containment supervises out-of-process subject drivers with wall-clock
// timeouts, process-group termination, isolated scratch directories, output caps,
// and child resource telemetry.
package containment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultTimeout        = 5 * time.Second
	defaultMaxOutputBytes = 8 << 20
)

// Spec configures a single contained subprocess execution.
type Spec struct {
	Command        string
	Args           []string
	Stdin          []byte
	Timeout        time.Duration
	MaxRSSBytes    int64
	MaxOutputBytes int64
	ExtraEnv       []string
}

// Result captures the outcome and telemetry of a contained subprocess execution.
type Result struct {
	Stdout         []byte
	Stderr         []byte
	ExitCode       int
	Signal         string
	DurationNs     int64
	PeakRSSBytes   int64
	TimedOut       bool
	OOMKilled      bool
	Crashed        bool
	OutputExceeded bool
}

type boundedBuffer struct {
	buf      bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	origLen := len(p)
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.exceeded = true
		return origLen, nil
	}
	if int64(len(p)) > remaining {
		b.exceeded = true
		_, _ = b.buf.Write(p[:remaining])
		return origLen, nil
	}
	_, _ = b.buf.Write(p)
	return origLen, nil
}

// Run executes spec inside an isolated temporary directory with timeout and resource supervision.
func Run(ctx context.Context, spec Spec) (*Result, error) {
	if strings.TrimSpace(spec.Command) == "" {
		return nil, fmt.Errorf("containment command is required")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxOut := spec.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = defaultMaxOutputBytes
	}

	scratchDir, err := os.MkdirTemp("", "docpresser-run-*")
	if err != nil {
		return nil, fmt.Errorf("create containment scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratchDir) }()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// #nosec G204 -- subject driver command is explicitly configured by the operator
	cmd := exec.CommandContext(runCtx, spec.Command, spec.Args...)
	cmd.Dir = scratchDir
	cmd.Env = buildScrubbedEnv(scratchDir, spec.MaxRSSBytes, spec.ExtraEnv)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}

	if len(spec.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}

	stdoutBuf := &boundedBuffer{limit: maxOut}
	stderrBuf := &boundedBuffer{limit: maxOut}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	start := time.Now()
	runErr := cmd.Run()
	durationNs := time.Since(start).Nanoseconds()

	res := &Result{
		Stdout:         stdoutBuf.buf.Bytes(),
		Stderr:         stderrBuf.buf.Bytes(),
		DurationNs:     durationNs,
		OutputExceeded: stdoutBuf.exceeded,
	}

	peakRSS, sigName := extractProcessTelemetry(cmd.ProcessState)
	res.PeakRSSBytes = peakRSS
	res.Signal = sigName

	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	} else if runErr != nil {
		res.ExitCode = -1
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		return res, nil
	}

	stderrStr := strings.ToLower(string(res.Stderr))
	isOOMText := strings.Contains(stderrStr, "out of memory") ||
		strings.Contains(stderrStr, "memoryerror") ||
		strings.Contains(stderrStr, "cannot allocate memory")

	if isOOMText || (spec.MaxRSSBytes > 0 && res.PeakRSSBytes > spec.MaxRSSBytes) || (sigName == "killed" && !res.TimedOut) || res.ExitCode == 137 {
		res.OOMKilled = true
		return res, nil
	}

	if sigName != "" || (runErr != nil && res.ExitCode != 0) {
		res.Crashed = true
	}

	if runErr != nil && cmd.ProcessState == nil && !res.TimedOut {
		return nil, fmt.Errorf("launch driver %q: %w", spec.Command, runErr)
	}
	return res, nil
}

func buildScrubbedEnv(scratchDir string, maxRSSBytes int64, extra []string) []string {
	pathVal := os.Getenv("PATH")
	env := []string{
		"PATH=" + pathVal,
		"HOME=" + scratchDir,
		"TMPDIR=" + scratchDir,
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}
	if maxRSSBytes > 0 {
		env = append(env, fmt.Sprintf("GOMEMLIMIT=%dB", maxRSSBytes))
	}
	env = append(env, extra...)
	return env
}
