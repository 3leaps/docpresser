package driver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/3leaps/docpresser/internal/model"
)

func TestFaultDriverAllSixModes(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "faultdriver")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	buildCmd := exec.Command("go", "build", "-o", binPath, "./testdata/faultdriver")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build faultdriver failed: %v\n%s", err, string(out))
	}

	client, err := NewClient("faultdriver="+binPath, "target")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	modes := []struct {
		mode       string
		wantStatus string
	}{
		{mode: "clean_pass", wantStatus: "ok"},
		{mode: "refused_bounded", wantStatus: "refused_bounded"},
		{mode: "crash_sigabrt", wantStatus: "crash_signal"},
		{mode: "memory_spike", wantStatus: "oom_killed"},
		{mode: "infinite_hang", wantStatus: "timeout_exceeded"},
		{mode: "truncated_json", wantStatus: "protocol_error"},
	}

	for _, tc := range modes {
		t.Run(tc.mode, func(t *testing.T) {
			raw := []byte(tc.mode + "\n")
			sum := sha256.Sum256(raw)
			wallMs := int64(2000)
			if tc.mode == "infinite_hang" {
				wallMs = 150
			}
			pc := model.PackCase{
				CaseID:      "fault." + tc.mode,
				Format:      "yaml",
				Axis:        "containment",
				Profile:     "diabolical-containment",
				Operation:   "load_value",
				InputPath:   "inputs/yaml/" + tc.mode + ".yaml",
				InputSHA256: hex.EncodeToString(sum[:]),
				InputBytes:  int64(len(raw)),
				Expected: model.CaseExpectation{
					Outcome: "refused_bounded",
				},
				Budgets: &model.CaseBudgets{
					MaxWallMs:   wallMs,
					MaxRSSBytes: 16 * 1024 * 1024,
				},
			}

			obs, err := client.ExecuteCase(context.Background(), pc, raw)
			if err != nil {
				t.Fatalf("ExecuteCase(%s) failed: %v", tc.mode, err)
			}
			if obs.Status != tc.wantStatus {
				t.Fatalf("mode %s: Status = %q, want %q (obs=%+v)", tc.mode, obs.Status, tc.wantStatus, obs)
			}
		})
	}
}
