package driver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/3leaps/docpresser/internal/model"
)

func buildFaultDriver(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "faultdriver")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	buildCmd := exec.Command("go", "build", "-o", binPath, "./testdata/faultdriver")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build faultdriver failed: %v\n%s", err, string(out))
	}
	return binPath
}

func TestHandshakeRecordsDriverIdentity(t *testing.T) {
	binPath := buildFaultDriver(t)
	// The CLI name differs from the driver's own subject so the two cannot be confused.
	client, err := NewClient("fault-probe="+binPath, "target")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if _, err := client.Metadata(); err == nil {
		t.Fatal("Metadata succeeded before Handshake; a placeholder version must never be recorded")
	}
	if err := client.Handshake(context.Background()); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}
	meta, err := client.Metadata()
	if err != nil {
		t.Fatalf("Metadata failed: %v", err)
	}
	if meta.SubjectVersion != "faultdriver-1.0.0" || meta.Language != "go" || meta.Engine != "faultdriver" ||
		len(meta.Formats) != 1 || meta.Formats[0] != "yaml" || len(meta.Operations) != 1 || meta.Operations[0] != "load_value" {
		t.Fatalf("Metadata did not record the handshake: %+v", meta)
	}
	if meta.DriverSubject != "faultdriver" {
		t.Fatalf("Metadata did not record the driver's subject: %+v", meta)
	}
	if meta.Name != "fault-probe" || meta.Role != "target" || meta.DriverSHA256 != client.DriverSHA256 {
		t.Fatalf("Metadata lost client identity: %+v", meta)
	}
}

func TestHandshakeRejectsFaultyReplies(t *testing.T) {
	binPath := buildFaultDriver(t)
	old := handshakeTimeout
	handshakeTimeout = 500 * time.Millisecond
	t.Cleanup(func() { handshakeTimeout = old })
	cases := map[string]string{
		"silent":         "empty reply",
		"bad_json":       "malformed reply",
		"wrong_protocol": "protocol version",
		"empty_version":  "empty subject_version",
		"no_formats":     "formats and operations must be non-empty",
		"blank_format":   "must not contain empty entries",
		"null_operation": "must not contain empty entries",
		"extra_field":    "malformed reply",
		"trailing":       "trailing data",
		"hang":           "timed out",
		"crash":          "exited abnormally (exit 3: faultdriver: engine unavailable)",
	}
	for mode, want := range cases {
		t.Run(mode, func(t *testing.T) {
			client, err := NewClient("faultdriver="+binPath+" --handshake="+mode, "target")
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}
			err = client.Handshake(context.Background())
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Handshake(%s) error = %v, want it to mention %q", mode, err, want)
			}
			if _, mErr := client.Metadata(); mErr == nil {
				t.Fatalf("Metadata succeeded after a failed handshake (%s)", mode)
			}
		})
	}
}

func TestFaultDriverAllSixModes(t *testing.T) {
	binPath := buildFaultDriver(t)

	client, err := NewClient("faultdriver="+binPath, "target")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if err := client.Handshake(context.Background()); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	modes := []struct {
		mode       string
		wantStatus string
		wantCode   string
	}{
		{mode: "clean_pass", wantStatus: "ok"},
		{mode: "refused_bounded", wantStatus: "refused_bounded"},
		{mode: "crash_sigabrt", wantStatus: "crash_signal"},
		{mode: "memory_spike", wantStatus: "oom_killed"},
		{mode: "infinite_hang", wantStatus: "timeout_exceeded"},
		{mode: "truncated_json", wantStatus: "protocol_error"},
		{mode: "version_mismatch", wantStatus: "protocol_error", wantCode: "subject_version_mismatch"},
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
			if tc.wantCode != "" && obs.ErrorCode != tc.wantCode {
				t.Fatalf("mode %s: ErrorCode = %q, want %q", tc.mode, obs.ErrorCode, tc.wantCode)
			}
		})
	}
}
