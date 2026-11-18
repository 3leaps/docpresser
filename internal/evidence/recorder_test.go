package evidence

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3leaps/docpresser/internal/generate"
	"github.com/3leaps/docpresser/internal/model"
	"github.com/3leaps/docpresser/internal/oracle"
)

func TestRecorderEndToEndWithOracle(t *testing.T) {
	packDir := filepath.Join(t.TempDir(), "pack")
	loaded, err := generate.Generate(generate.Options{
		Seed:     7,
		Scenario: "k8s-bundle",
		Scale:    "small",
		OutDir:   packDir,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	runDir := filepath.Join(t.TempDir(), "run")
	rec, err := NewRecorder(runDir, "run-001", "synthetic-workload", loaded.PackSHA256, []model.SubjectMetadata{
		{
			Name:           "yamlprims-go",
			Role:           "target",
			DriverSubject:  "yamlprims-go",
			SubjectVersion: "v0.2.1 (go1.26.10)",
			Language:       "go",
			Engine:         "yamlprims-go",
			Formats:        []string{"yaml", "json"},
			Operations:     []string{"load_value"},
			DriverSHA256:   strings.Repeat("c", 64),
		},
	})
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}

	for _, c := range loaded.Cases {
		status := "ok"
		if c.Expected.Outcome == "reject" {
			status = "error"
		}
		obs := model.Observation{
			CaseID:         c.CaseID,
			Subject:        "yamlprims-go",
			Role:           "target",
			Operation:      c.Operation,
			InputSHA256:    c.InputSHA256,
			InputBytes:     c.InputBytes,
			Status:         status,
			OutputSHA256:   c.Expected.OutputSHA256,
			OutputBytes:    int64(len(c.Expected.CanonicalValue)),
			CanonicalValue: c.Expected.CanonicalValue,
			ErrorPhase:     c.Expected.ErrorPhase,
			DurationNs:     50000,
			PeakRSSBytes:   2048000,
			ExitCode:       0,
		}
		if err := rec.Record(obs); err != nil {
			t.Fatalf("Record(%s) failed: %v", c.CaseID, err)
		}
	}

	if _, err := rec.Complete("complete"); err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	report, err := oracle.Verify(context.Background(), runDir, packDir)
	if err != nil {
		t.Fatalf("oracle.Verify failed: %v", err)
	}
	if report.Verdict != "pass" {
		t.Fatalf("Verdict = %q, want pass (findings=%+v)", report.Verdict, report.Findings)
	}
}
