package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/3leaps/docpresser/internal/generate"
	"github.com/3leaps/docpresser/internal/oracle"
	"github.com/3leaps/docpresser/internal/pack"
)

func buildStandaloneDriver(t *testing.T, driverRelDir, binName string) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), binName)
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	absDriverDir, err := filepath.Abs(driverRelDir)
	if err != nil {
		t.Fatalf("resolve driver dir %s: %v", driverRelDir, err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = absDriverDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build driver %s failed: %v\n%s", driverRelDir, err, string(out))
	}
	return binPath
}

func TestWave1Profiles_EndToEndQualification(t *testing.T) {
	ctx := context.Background()
	yamlprimsBin := buildStandaloneDriver(t, "../../drivers/yamlprims-go", "driver-yamlprims-go")
	yamlv3Bin := buildStandaloneDriver(t, "../../drivers/go-yamlv3", "driver-go-yamlv3")
	pyDriverPath, err := filepath.Abs("../../drivers/python-yaml/driver.py")
	if err != nil {
		t.Fatalf("resolve python driver path: %v", err)
	}

	// 1. Conformance profile (bound structcorpus fixture)
	t.Run("conformance", func(t *testing.T) {
		corpusDir := filepath.Join(t.TempDir(), "structcorpus")
		if err := os.MkdirAll(filepath.Join(corpusDir, "manifests"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(corpusDir, "fixtures", "yaml"), 0o750); err != nil {
			t.Fatal(err)
		}
		fixture := []byte("a: 1\nb: hello\n")
		fixtureSum := sha256.Sum256(fixture)
		if err := os.WriteFile(filepath.Join(corpusDir, "fixtures", "yaml", "simple.yaml"), fixture, 0o600); err != nil {
			t.Fatal(err)
		}
		caseLine := fmt.Sprintf(`{"id":"yaml.smoke.simple","format":"yaml","category":"scalar-resolution","execution":{"tier":"smoke","hazards":[]},"input":{"path":"fixtures/yaml/simple.yaml","bytes":%d,"sha256":%q},"expectations":{"value":[{"profile":"yamlprims-core","profile_revision":3,"documents":[{"entries":[[{"t":"str","v":"a"},{"t":"int","v":"1"}],[{"t":"str","v":"b"},{"t":"str","v":"hello"}]],"t":"map"}]}]},"review":{"rationale":"Smoke conformance fixture"}}`+"\n",
			len(fixture), hex.EncodeToString(fixtureSum[:]))
		if err := os.WriteFile(filepath.Join(corpusDir, "manifests", "cases.jsonl"), []byte(caseLine), 0o600); err != nil {
			t.Fatal(err)
		}
		// bind-corpus requires a clean committed checkout.
		for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "fixture"}} {
			base := []string{"-C", corpusDir, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}
			cmd := exec.Command("git", append(base, args...)...)
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}

		packDir := filepath.Join(t.TempDir(), "pack-conformance")
		if _, err := pack.BindCorpus(ctx, corpusDir, "smoke", packDir); err != nil {
			t.Fatalf("BindCorpus failed: %v", err)
		}

		runDir := filepath.Join(t.TempDir(), "run-conformance")
		if _, err := Run(ctx, Options{
			PackDir:       packDir,
			Profile:       "conformance",
			RunID:         "wave1-conformance",
			SubjectSpecs:  []string{"yamlprims-go=" + yamlprimsBin},
			BaselineSpecs: []string{"go-yamlv3=" + yamlv3Bin},
			OutDir:        runDir,
		}); err != nil {
			t.Fatalf("Run(conformance) failed: %v", err)
		}

		report, err := oracle.Verify(ctx, runDir, packDir)
		if err != nil {
			t.Fatalf("Verify(conformance) failed: %v", err)
		}
		if report.Verdict != "pass" || report.Correctness.Passed != 2 {
			t.Fatalf("unexpected conformance report: verdict=%s passed=%d findings=%+v",
				report.Verdict, report.Correctness.Passed, report.Findings)
		}
	})

	// 2. Diabolical containment profile
	t.Run("diabolical-containment", func(t *testing.T) {
		packDir := filepath.Join(t.TempDir(), "pack-diabolical")
		if _, err := generate.Generate(generate.Options{
			Seed:     424242,
			Scenario: "diabolical-alias-pyramid",
			Scale:    "small",
			OutDir:   packDir,
		}); err != nil {
			t.Fatalf("Generate(diabolical-alias-pyramid) failed: %v", err)
		}

		runDir := filepath.Join(t.TempDir(), "run-diabolical")
		if _, err := Run(ctx, Options{
			PackDir:      packDir,
			Profile:      "diabolical-containment",
			RunID:        "wave1-diabolical",
			SubjectSpecs: []string{"yamlprims-go=" + yamlprimsBin},
			OutDir:       runDir,
		}); err != nil {
			t.Fatalf("Run(diabolical-containment) failed: %v", err)
		}

		report, err := oracle.Verify(ctx, runDir, packDir)
		if err != nil {
			t.Fatalf("Verify(diabolical-containment) failed: %v", err)
		}
		if report.Verdict != "pass" || report.Containment.Verdict != "refused_bounded" || report.Containment.RefusedBounded != 3 {
			t.Fatalf("unexpected diabolical report: verdict=%s containment=%+v findings=%+v",
				report.Verdict, report.Containment, report.Findings)
		}
	})

	// 3. YAML 1.1 -> 1.2 migration divergence profile
	t.Run("yaml11-migration", func(t *testing.T) {
		packDir := filepath.Join(t.TempDir(), "pack-migration")
		if _, err := generate.Generate(generate.Options{
			Seed:     424242,
			Scenario: "yaml11-migration",
			Scale:    "small",
			OutDir:   packDir,
		}); err != nil {
			t.Fatalf("Generate(yaml11-migration) failed: %v", err)
		}

		runDir := filepath.Join(t.TempDir(), "run-migration")
		if _, err := Run(ctx, Options{
			PackDir:      packDir,
			Profile:      "yaml11-migration",
			RunID:        "wave1-migration",
			SubjectSpecs: []string{"yamlprims-go=" + yamlprimsBin},
			BaselineSpecs: []string{
				// The driver's built-in YAML 1.1 parser is the deterministic YAML 1.1
				// baseline here; PyYAML is not a test dependency.
				"python-builtin-yaml11=python3 " + pyDriverPath + " --engine builtin-yaml11",
				"go-yamlv3=" + yamlv3Bin,
			},
			OutDir: runDir,
		}); err != nil {
			t.Fatalf("Run(yaml11-migration) failed: %v", err)
		}

		report, err := oracle.Verify(ctx, runDir, packDir)
		if err != nil {
			t.Fatalf("Verify(yaml11-migration) failed: %v", err)
		}
		if report.Verdict != "pass" {
			t.Fatalf("expected pass verdict on yaml11-migration, got %s (findings=%+v)", report.Verdict, report.Findings)
		}
		if report.MigrationDivergence.TotalFindings < 4 {
			t.Fatalf("expected at least 4 YAML 1.1 baseline divergences, got %d", report.MigrationDivergence.TotalFindings)
		}
		md := oracle.SummarizeMarkdown(*report)
		if !strings.Contains(md, "norway_boolean") || !strings.Contains(md, "sexagesimal_number") {
			t.Fatalf("markdown summary missing migration divergence categories:\n%s", md)
		}
	})

	// 4. Synthetic workload profile (k8s + openapi + JCS + diabolical + migration)
	t.Run("synthetic-workload", func(t *testing.T) {
		packDir := filepath.Join(t.TempDir(), "pack-workload")
		if _, err := generate.Generate(generate.Options{
			Seed:     424242,
			Scenario: "synthetic-workload",
			Scale:    "small",
			OutDir:   packDir,
		}); err != nil {
			t.Fatalf("Generate(synthetic-workload) failed: %v", err)
		}

		runDir := filepath.Join(t.TempDir(), "run-workload")
		if _, err := Run(ctx, Options{
			PackDir:      packDir,
			Profile:      "synthetic-workload",
			RunID:        "wave1-workload",
			SubjectSpecs: []string{"yamlprims-go=" + yamlprimsBin},
			OutDir:       runDir,
		}); err != nil {
			t.Fatalf("Run(synthetic-workload) failed: %v", err)
		}

		report, err := oracle.Verify(ctx, runDir, packDir)
		if err != nil {
			t.Fatalf("Verify(synthetic-workload) failed: %v", err)
		}
		if report.Verdict != "pass" || report.Correctness.Passed != 12 {
			t.Fatalf("unexpected synthetic-workload report: verdict=%s passed=%d findings=%+v",
				report.Verdict, report.Correctness.Passed, report.Findings)
		}
	})
}
