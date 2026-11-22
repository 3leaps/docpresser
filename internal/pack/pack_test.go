package pack

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3leaps/docpresser/internal/model"
)

func TestWriteAndLoadPack_DetectsTampering(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "pack")
	raw := []byte("key: value\n")
	inputRel := "inputs/yaml/sample.yaml"

	cases := []model.PackCase{
		{
			CaseID:      "test.yaml.sample",
			Format:      "yaml",
			Axis:        "correctness",
			Profile:     "smoke",
			Operation:   "load_value",
			InputPath:   inputRel,
			InputSHA256: sha256Hex(raw),
			InputBytes:  int64(len(raw)),
			Expected: model.CaseExpectation{
				Outcome:        "accept",
				CanonicalValue: `[{"documents":[{"entries":[[{"t":"str","v":"key"},{"t":"str","v":"value"}]],"t":"map"}]}]`,
				OutputSHA256:   sha256Hex([]byte(`[{"documents":[{"entries":[[{"t":"str","v":"key"},{"t":"str","v":"value"}]],"t":"map"}]}]`)),
			},
		},
	}

	written, err := WritePack(outDir, model.Pack{
		PackKind:     "synthetic",
		FormatFamily: "yaml",
		Tier:         "smoke",
		Scenario:     "unit-test",
		Generator:    model.Generator,
		Seed:         "42",
		Scale:        "small",
	}, cases, map[string][]byte{inputRel: raw})
	if err != nil {
		t.Fatalf("WritePack failed: %v", err)
	}

	loaded, err := LoadAndValidatePack(outDir)
	if err != nil {
		t.Fatalf("LoadAndValidatePack failed: %v", err)
	}
	if loaded.PackSHA256 != written.PackSHA256 {
		t.Fatalf("PackSHA256 = %s, want %s", loaded.PackSHA256, written.PackSHA256)
	}

	// Tamper with input file and verify LoadAndValidatePack fails
	if err := os.WriteFile(filepath.Join(outDir, inputRel), []byte("key: tampered\n"), 0o600); err != nil {
		t.Fatalf("tamper input: %v", err)
	}
	if _, err := LoadAndValidatePack(outDir); err == nil {
		t.Fatal("LoadAndValidatePack succeeded on tampered input file")
	}
}

// corpusFixture writes a minimal structcorpus checkout holding one static
// case and returns its directory. When commit is true it is committed to a
// fresh git repository.
func corpusFixture(t *testing.T, execution string, commit bool) string {
	t.Helper()
	corpusDir := filepath.Join(t.TempDir(), "structcorpus")
	if err := os.MkdirAll(filepath.Join(corpusDir, "manifests"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(corpusDir, "fixtures", "yaml"), 0o750); err != nil {
		t.Fatal(err)
	}
	fixtureBytes := []byte("a: 1\n")
	fixtureSHA := sha256Hex(fixtureBytes)
	if err := os.WriteFile(filepath.Join(corpusDir, "fixtures", "yaml", "simple.yaml"), fixtureBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	caseLine := `{"id":"yaml.smoke.simple","format":"yaml","category":"scalar-resolution","execution":` + execution + `,"input":{"path":"fixtures/yaml/simple.yaml","bytes":5,"sha256":"` + fixtureSHA + `"},"expectations":{"value":[{"profile":"yamlprims-core","profile_revision":3,"documents":[{"t":"map","entries":[[{"t":"str","v":"a"},{"t":"int","v":"1"}]]}]}]},"review":{"rationale":"Simple smoke fixture"}}` + "\n"
	if err := os.WriteFile(filepath.Join(corpusDir, "manifests", "cases.jsonl"), []byte(caseLine), 0o600); err != nil {
		t.Fatal(err)
	}
	if commit {
		gitRun(t, corpusDir, "init", "-q")
		gitRun(t, corpusDir, "add", "-A")
		gitRun(t, corpusDir, "commit", "-q", "-m", "fixture")
	}
	return corpusDir
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

const smokeExecution = `{"tier":"smoke","hazards":[]}`

func TestBindCorpus_MinimalFixture(t *testing.T) {
	corpusDir := corpusFixture(t, smokeExecution, true)
	head := gitRun(t, corpusDir, "rev-parse", "HEAD")

	outDir := filepath.Join(t.TempDir(), "bound-pack")
	loaded, err := BindCorpus(context.Background(), corpusDir, "smoke", outDir)
	if err != nil {
		t.Fatalf("BindCorpus failed: %v", err)
	}
	if loaded.Pack.CaseCount != 1 {
		t.Fatalf("CaseCount = %d, want 1", loaded.Pack.CaseCount)
	}
	if loaded.Pack.CorpusCommit != head {
		t.Fatalf("CorpusCommit = %q, want checkout HEAD %q", loaded.Pack.CorpusCommit, head)
	}
	if _, err := LoadAndValidatePack(outDir); err != nil {
		t.Fatalf("LoadAndValidatePack on bound pack failed: %v", err)
	}
}

// TestBindCorpus_RequiresCleanCommittedCheckout shows a pack never records a
// commit whose files it did not read.
func TestBindCorpus_RequiresCleanCommittedCheckout(t *testing.T) {
	type corpusCase struct {
		setup func(t *testing.T) string
		want  string
	}
	cases := map[string]corpusCase{
		"not a git checkout": {want: "is not a git checkout", setup: func(t *testing.T) string {
			return corpusFixture(t, smokeExecution, false)
		}},
		"untracked file": {want: "uncommitted changes", setup: func(t *testing.T) string {
			dir := corpusFixture(t, smokeExecution, true)
			if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		"modified fixture": {want: "uncommitted changes", setup: func(t *testing.T) string {
			dir := corpusFixture(t, smokeExecution, true)
			if err := os.WriteFile(filepath.Join(dir, "fixtures", "yaml", "simple.yaml"), []byte("a: 2\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		"staged change": {want: "uncommitted changes", setup: func(t *testing.T) string {
			dir := corpusFixture(t, smokeExecution, true)
			if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			gitRun(t, dir, "add", "staged.txt")
			return dir
		}},
		"subdirectory of another repository": {want: "is not the top of its git work tree", setup: func(t *testing.T) string {
			outer := t.TempDir()
			gitRun(t, outer, "init", "-q")
			inner := corpusFixture(t, smokeExecution, false)
			dst := filepath.Join(outer, "structcorpus")
			if err := os.Rename(inner, dst); err != nil {
				t.Fatal(err)
			}
			gitRun(t, outer, "add", "-A")
			gitRun(t, outer, "commit", "-q", "-m", "outer")
			return dst
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := c.setup(t)
			_, err := BindCorpus(context.Background(), dir, "smoke", filepath.Join(t.TempDir(), "out"))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("BindCorpus(%s) error = %v, want %q", name, err, c.want)
			}
		})
	}
}

// TestBindCorpus_MapsStructcorpusBudgets checks the structcorpus budget field
// names bind to pack budgets and that a renamed field fails the bind.
func TestBindCorpus_MapsStructcorpusBudgets(t *testing.T) {
	execution := `{"tier":"smoke","hazards":["alias-expansion"],"budgets":{"wall_ms":2000,"memory_bytes":536870912,"output_bytes":65536}}`
	// A smoke-tier case with a limit-free expectation binds on the
	// correctness axis, so its declared budgets are carried exactly.
	dir := corpusFixture(t, execution, true)
	loaded, err := BindCorpus(context.Background(), dir, "smoke", filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("BindCorpus: %v", err)
	}
	got := loaded.Cases[0].Budgets
	want := model.CaseBudgets{MaxWallMs: 2000, MaxRSSBytes: 536870912, MaxOutputBytes: 65536}
	if got == nil || *got != want {
		t.Fatalf("budgets = %+v, want %+v", got, want)
	}

	for name, c := range map[string]struct{ execution, want string }{
		"old field names": {`{"tier":"smoke","hazards":[],"budgets":{"max_wall_ms":2000}}`, `unknown field "max_wall_ms"`},
		"unknown field":   {`{"tier":"smoke","hazards":[],"budgets":{"wall_ms":2000,"cpu_ms":5}}`, `unknown field "cpu_ms"`},
		"no budget":       {`{"tier":"smoke","hazards":[],"budgets":{}}`, "no positive budget"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := corpusFixture(t, c.execution, true)
			_, err := BindCorpus(context.Background(), dir, "smoke", filepath.Join(t.TempDir(), "out"))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("BindCorpus error = %v, want %q", err, c.want)
			}
		})
	}
}

func TestBindBudgets_ContainmentDefaults(t *testing.T) {
	partial := &structcorpusBudgets{WallMs: 2000, OutputBytes: 65536}
	got := bindBudgets(partial, "containment")
	want := model.CaseBudgets{MaxWallMs: 2000, MaxRSSBytes: defaultContainmentBudgets.MaxRSSBytes, MaxOutputBytes: 65536, MaxAmplificationRatio: 64.0}
	if *got != want {
		t.Fatalf("containment budgets = %+v, want %+v", *got, want)
	}
	if got := bindBudgets(nil, "containment"); *got != defaultContainmentBudgets {
		t.Fatalf("undeclared containment budgets = %+v", *got)
	}
	if got := bindBudgets(nil, "correctness"); got != nil {
		t.Fatalf("undeclared correctness budgets = %+v, want nil", *got)
	}
}
