package containment

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRun_OutputCapAndTimeout(t *testing.T) {
	ctx := context.Background()

	// 1. Clean execution with output cap
	res, err := Run(ctx, Spec{
		Command:        "sh",
		Args:           []string{"-c", "printf 'hello world'"},
		Timeout:        2 * time.Second,
		MaxOutputBytes: 5,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.OutputExceeded || string(res.Stdout) != "hello" {
		t.Fatalf("expected output cap at 5 bytes ('hello'), got exceeded=%v stdout=%q", res.OutputExceeded, string(res.Stdout))
	}

	// 2. Timeout kills child process group
	resTimeout, err := Run(ctx, Spec{
		Command: "sh",
		Args:    []string{"-c", "sleep 10"},
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run timeout failed: %v", err)
	}
	if !resTimeout.TimedOut {
		t.Fatalf("expected TimedOut=true, got %+v", resTimeout)
	}

	// 3. OOM detection from stderr
	resOOM, err := Run(ctx, Spec{
		Command: "sh",
		Args:    []string{"-c", "echo 'fatal error: runtime: out of memory' >&2; exit 2"},
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run OOM failed: %v", err)
	}
	if !resOOM.OOMKilled || !strings.Contains(string(resOOM.Stderr), "out of memory") {
		t.Fatalf("expected OOMKilled=true, got %+v", resOOM)
	}
}
