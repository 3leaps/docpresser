package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"runtime"
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

	cleanRunDir := filepath.Join(t.TempDir(), "clean-run")
	_, err = Run(context.Background(), Options{
		PackDir:      cleanPackDir,
		Profile:      "diabolical-containment",
		RunID:        "fault-clean-run",
		SubjectSpecs: []string{"faultdriver=" + binPath},
		OutDir:       cleanRunDir,
	})
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
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
