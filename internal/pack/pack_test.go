package pack

import (
	"context"
	"os"
	"path/filepath"
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

func TestBindCorpus_MinimalFixture(t *testing.T) {
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
	caseLine := `{"id":"yaml.smoke.simple","format":"yaml","category":"scalar-resolution","execution":{"tier":"smoke","hazards":[]},"input":{"path":"fixtures/yaml/simple.yaml","bytes":5,"sha256":"` + fixtureSHA + `"},"expectations":{"value":[{"profile":"yamlprims-core","profile_revision":3,"documents":[{"t":"map","entries":[[{"t":"str","v":"a"},{"t":"int","v":"1"}]]}]}]},"review":{"rationale":"Simple smoke fixture"}}` + "\n"
	if err := os.WriteFile(filepath.Join(corpusDir, "manifests", "cases.jsonl"), []byte(caseLine), 0o600); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "bound-pack")
	loaded, err := BindCorpus(context.Background(), corpusDir, "smoke", outDir)
	if err != nil {
		t.Fatalf("BindCorpus failed: %v", err)
	}
	if loaded.Pack.CaseCount != 1 {
		t.Fatalf("CaseCount = %d, want 1", loaded.Pack.CaseCount)
	}
	if _, err := LoadAndValidatePack(outDir); err != nil {
		t.Fatalf("LoadAndValidatePack on bound pack failed: %v", err)
	}
}
