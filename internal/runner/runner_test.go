package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/3leaps/docpresser/internal/model"
	"github.com/3leaps/docpresser/internal/oracle"
	"github.com/3leaps/docpresser/internal/pack"
)

func TestRunnerAndOracle_FaultDriverEndToEnd(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "faultdriver")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	buildCmd := exec.Command("go", "build", "-o", binPath, "../driver/testdata/faultdriver")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build faultdriver failed: %v\n%s", err, string(out))
	}

	// 1. Clean pass + bounded refusal pack should pass verification
	cleanPackDir := filepath.Join(t.TempDir(), "clean-pack")
	passRaw := []byte("clean_pass\n")
	passSum := sha256.Sum256(passRaw)
	canonical := `[{"documents":[{"entries":[[{"t":"str","v":"mode"},{"t":"str","v":"clean_pass"}]],"t":"map"}]}]`
	canonSum := sha256.Sum256([]byte(canonical))

	refuseRaw := []byte("refused_bounded\n")
	refuseSum := sha256.Sum256(refuseRaw)

	_, err := pack.WritePack(cleanPackDir, model.Pack{
		PackKind:     "synthetic",
		FormatFamily: "yaml",
		Tier:         "stress",
		Scenario:     "fault-test",
		Generator:    model.Generator,
		Seed:         "1",
		Scale:        "small",
	}, []model.PackCase{
		{
			CaseID:      "fault.clean_pass",
			Format:      "yaml",
			Axis:        "correctness",
			Profile:     "conformance",
			Operation:   "load_value",
			InputPath:   "inputs/yaml/clean_pass.yaml",
			InputSHA256: hex.EncodeToString(passSum[:]),
			InputBytes:  int64(len(passRaw)),
			Expected: model.CaseExpectation{
				Outcome:        "accept",
				CanonicalValue: canonical,
				OutputSHA256:   hex.EncodeToString(canonSum[:]),
			},
		},
		{
			CaseID:      "fault.refused_bounded",
			Format:      "yaml",
			Axis:        "containment",
			Profile:     "diabolical-containment",
			Operation:   "load_value",
			InputPath:   "inputs/yaml/refused_bounded.yaml",
			InputSHA256: hex.EncodeToString(refuseSum[:]),
			InputBytes:  int64(len(refuseRaw)),
			Expected: model.CaseExpectation{
				Outcome:    "refused_bounded",
				ErrorPhase: "limit",
			},
			Budgets: &model.CaseBudgets{
				MaxWallMs:   1000,
				MaxRSSBytes: 128 * 1024 * 1024,
			},
		},
	}, map[string][]byte{
		"inputs/yaml/clean_pass.yaml":      passRaw,
		"inputs/yaml/refused_bounded.yaml": refuseRaw,
	})
	if err != nil {
		t.Fatalf("WritePack failed: %v", err)
	}

	// A failed handshake stops the run before any evidence is written.
	badRunDir := filepath.Join(t.TempDir(), "bad-handshake-run")
	if _, err := Run(context.Background(), Options{
		PackDir:      cleanPackDir,
		Profile:      "diabolical-containment",
		RunID:        "fault-bad-handshake",
		SubjectSpecs: []string{"faultdriver=" + binPath + " --handshake=empty_version"},
		OutDir:       badRunDir,
	}); err == nil || !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("runner.Run with a failing handshake: err = %v, want a handshake error", err)
	}
	for _, mode := range []string{"empty_version", "blank_format", "null_operation"} {
		dir := filepath.Join(t.TempDir(), "bad-"+mode)
		if _, err := Run(context.Background(), Options{
			PackDir:      cleanPackDir,
			Profile:      "diabolical-containment",
			RunID:        "fault-bad-" + mode,
			SubjectSpecs: []string{"faultdriver=" + binPath + " --handshake=" + mode},
			OutDir:       dir,
		}); err == nil || !strings.Contains(err.Error(), "handshake") {
			t.Fatalf("runner.Run with handshake mode %s: err = %v, want a handshake error", mode, err)
		}
		if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
			t.Fatalf("handshake mode %s left evidence at %s (stat err = %v)", mode, dir, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(badRunDir, "run-manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("a failed handshake must not leave a run-manifest.json (stat err = %v)", statErr)
	}

	cleanRunDir := filepath.Join(t.TempDir(), "clean-run")
	manifest, err := Run(context.Background(), Options{
		PackDir:      cleanPackDir,
		Profile:      "diabolical-containment",
		RunID:        "fault-clean-run",
		SubjectSpecs: []string{"faultdriver=" + binPath},
		OutDir:       cleanRunDir,
	})
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}
	if manifest.DocpresserVersion == "" {
		t.Fatal("run manifest does not record the docpresser version")
	}
	if len(manifest.Subjects) != 1 || manifest.Subjects[0].SubjectVersion != "faultdriver-1.0.0" ||
		manifest.Subjects[0].Engine != "faultdriver" || manifest.Subjects[0].Language != "go" {
		t.Fatalf("run manifest does not record the driver handshake: %+v", manifest.Subjects)
	}
	onDisk, err := os.ReadFile(filepath.Join(cleanRunDir, "run-manifest.json"))
	if err != nil {
		t.Fatalf("read run-manifest.json: %v", err)
	}
	if !strings.Contains(string(onDisk), `"subject_version": "faultdriver-1.0.0"`) {
		t.Fatalf("run-manifest.json on disk lacks the handshake subject_version:\n%s", onDisk)
	}

	report, err := oracle.Verify(context.Background(), cleanRunDir, cleanPackDir)
	if err != nil {
		t.Fatalf("oracle.Verify failed: %v", err)
	}
	if report.Verdict != "pass" || report.Containment.Verdict != "refused_bounded" {
		t.Fatalf("unexpected report on clean+bounded run: verdict=%s containment=%s findings=%+v",
			report.Verdict, report.Containment.Verdict, report.Findings)
	}
}
