package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3leaps/docpresser/internal/model"
	v0 "github.com/3leaps/docpresser/schemas/docpresser/v0"
)

func TestOracleDependencyBoundary(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", "{{join .Deps \"\\n\"}}", "github.com/3leaps/docpresser/internal/oracle")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list deps failed: %v\n%s", err, string(out))
	}
	deps := strings.Split(strings.TrimSpace(string(out)), "\n")
	forbiddenSubstrings := []string{
		"internal/driver",
		"internal/containment",
		"internal/runner",
		"internal/generate",
		"internal/evidence",
		"internal/pack",
		"gopkg.in/yaml",
		"encoding/xml",
		"os/exec",
	}
	for _, dep := range deps {
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(dep, forbidden) {
				t.Fatalf("oracle dependency boundary violated: imported %q (matches forbidden %q)", dep, forbidden)
			}
		}
	}
}

func TestVerifyRequiresBothRunAndPack(t *testing.T) {
	ctx := context.Background()
	if _, err := Verify(ctx, "", "some-pack"); err == nil {
		t.Fatal("Verify succeeded without --run")
	}
	if _, err := Verify(ctx, "some-run", ""); err == nil {
		t.Fatal("Verify succeeded without --pack")
	}
}

func TestCorpusV0Specimens(t *testing.T) {
	corpusRoot := filepath.Join("..", "..", "corpus", "v0")
	if os.Getenv("DOCPRESSER_BOOTSTRAP_CORPUS_V0") == "1" {
		if err := bootstrapCorpusV0(corpusRoot); err != nil {
			t.Fatalf("bootstrap corpus/v0: %v", err)
		}
	}

	specs := []struct {
		name            string
		wantVerdict     string
		wantFindingCode string
		wantContain     string
		wantMigration   int
	}{
		{
			name:        "valid-smoke",
			wantVerdict: "pass",
			wantContain: "not_applicable",
		},
		{
			name:            "mutated-pack-sha256",
			wantVerdict:     "fail",
			wantFindingCode: "pack_sha256_mismatch",
		},
		{
			name:            "missing-case",
			wantVerdict:     "fail",
			wantFindingCode: "missing_case",
		},
		{
			name:            "spurious-case",
			wantVerdict:     "fail",
			wantFindingCode: "spurious_case",
		},
		{
			name:            "wrong-digest",
			wantVerdict:     "fail",
			wantFindingCode: "digest_mismatch",
		},
		{
			name:            "incomplete-manifest",
			wantVerdict:     "fail",
			wantFindingCode: "incomplete_run",
		},
		{
			name:            "containment-rss-breach",
			wantVerdict:     "fail",
			wantFindingCode: "rss_budget_exceeded",
			wantContain:     "amplification_exceeded",
		},
		{
			name:            "migration-divergence",
			wantVerdict:     "fail",
			wantFindingCode: "unexpected_migration_divergence",
			wantMigration:   1,
		},
	}

	for _, tc := range specs {
		t.Run(tc.name, func(t *testing.T) {
			base := filepath.Join(corpusRoot, tc.name)
			packDir := filepath.Join(base, "pack")
			runDir := filepath.Join(base, "run")

			report, err := Verify(context.Background(), runDir, packDir)
			if err != nil {
				t.Fatalf("Verify(%s) error: %v", tc.name, err)
			}
			if report.Verdict != tc.wantVerdict {
				t.Fatalf("Verdict = %q, want %q (findings=%+v)", report.Verdict, tc.wantVerdict, report.Findings)
			}
			if tc.wantContain != "" && report.Containment.Verdict != tc.wantContain {
				t.Fatalf("Containment.Verdict = %q, want %q", report.Containment.Verdict, tc.wantContain)
			}
			if tc.wantMigration > 0 && report.MigrationDivergence.TotalFindings != tc.wantMigration {
				t.Fatalf("MigrationDivergence.TotalFindings = %d, want %d", report.MigrationDivergence.TotalFindings, tc.wantMigration)
			}
			if tc.wantFindingCode != "" {
				found := false
				for _, f := range report.Findings {
					if f.Code == tc.wantFindingCode {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected finding code %q in %+v", tc.wantFindingCode, report.Findings)
				}
			}
			md := SummarizeMarkdown(*report)
			if !strings.Contains(md, report.RunID) {
				t.Fatalf("SummarizeMarkdown missing RunID %q", report.RunID)
			}
		})
	}
}

func bootstrapCorpusV0(corpusRoot string) error {
	inputYAML := []byte("enabled: true\ncount: 42\n")
	inputSHA := sha256Hex(inputYAML)
	canonicalOut := `[{"documents":[{"entries":[[{"t":"str","v":"enabled"},{"t":"bool","v":true}],[{"t":"str","v":"count"},{"t":"int","v":"42"}]],"t":"map"}]}]`
	outSHA := sha256Hex([]byte(canonicalOut))

	baseCase := model.PackCase{
		CaseID:      "specimen.yaml.smoke-01",
		Format:      "yaml",
		Axis:        "correctness",
		Profile:     "smoke",
		Operation:   "load_value",
		InputPath:   "inputs/yaml/smoke-01.yaml",
		InputSHA256: inputSHA,
		InputBytes:  int64(len(inputYAML)),
		Expected: model.CaseExpectation{
			Outcome:        "accept",
			CanonicalValue: canonicalOut,
			OutputSHA256:   outSHA,
		},
		Description: "Hand-authored smoke specimen case",
	}

	baseObs := model.Observation{
		Version:        model.Version,
		CaseID:         baseCase.CaseID,
		Subject:        "yamlprims-go",
		Role:           "target",
		Operation:      "load_value",
		InputSHA256:    inputSHA,
		InputBytes:     int64(len(inputYAML)),
		Status:         "ok",
		OutputSHA256:   outSHA,
		OutputBytes:    int64(len(canonicalOut)),
		DocumentCount:  1,
		CanonicalValue: canonicalOut,
		DurationNs:     125000,
		PeakRSSBytes:   4096000,
		ExitCode:       0,
	}

	writeSpecimen := func(name string, cases []model.PackCase, obsList []model.Observation, mutateManifest func(*model.RunManifest)) error {
		specDir := filepath.Join(corpusRoot, name)
		packDir := filepath.Join(specDir, "pack")
		runDir := filepath.Join(specDir, "run")
		if err := os.MkdirAll(filepath.Join(packDir, "inputs", "yaml"), 0o750); err != nil {
			return err
		}
		if err := os.MkdirAll(runDir, 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(packDir, "inputs", "yaml", "smoke-01.yaml"), inputYAML, 0o600); err != nil {
			return err
		}

		var casesBuf bytes.Buffer
		for _, c := range cases {
			line, err := json.Marshal(c)
			if err != nil {
				return err
			}
			casesBuf.Write(line)
			casesBuf.WriteByte('\n')
		}
		casesBytes := casesBuf.Bytes()
		if err := os.WriteFile(filepath.Join(packDir, "cases.ndjson"), casesBytes, 0o600); err != nil {
			return err
		}

		packDoc := model.Pack{
			Version:         model.Version,
			State:           "complete",
			PackKind:        "structcorpus",
			FormatFamily:    "yaml",
			Tier:            "smoke",
			CorpusCommit:    model.StructcorpusPinnedCommit,
			CorpusProfile:   "yamlprims-core",
			SchemaSHA256:    v0.SchemaSHA256(),
			CaseCount:       len(cases),
			TotalInputBytes: int64(len(inputYAML)),
			Files: []model.PackFile{
				{
					Path:    "cases.ndjson",
					SHA256:  sha256Hex(casesBytes),
					Bytes:   int64(len(casesBytes)),
					Records: len(cases),
				},
				{
					Path:    "inputs/yaml/smoke-01.yaml",
					SHA256:  inputSHA,
					Bytes:   int64(len(inputYAML)),
					Records: 1,
				},
			},
		}
		packBytes, err := json.MarshalIndent(packDoc, "", "  ")
		if err != nil {
			return err
		}
		packBytes = append(packBytes, '\n')
		if err := os.WriteFile(filepath.Join(packDir, "pack.json"), packBytes, 0o600); err != nil {
			return err
		}

		var obsBuf bytes.Buffer
		for _, o := range obsList {
			line, err := json.Marshal(o)
			if err != nil {
				return err
			}
			obsBuf.Write(line)
			obsBuf.WriteByte('\n')
		}
		obsBytes := obsBuf.Bytes()
		if err := os.WriteFile(filepath.Join(runDir, "observations.ndjson"), obsBytes, 0o600); err != nil {
			return err
		}

		manifest := model.RunManifest{
			Version:            model.Version,
			DocpresserVersion:  "specimen",
			State:              "complete",
			RunID:              "specimen-" + name,
			Profile:            "smoke",
			PackSHA256:         sha256Hex(packBytes),
			SchemaSHA256:       v0.SchemaSHA256(),
			ObservationsSHA256: sha256Hex(obsBytes),
			ObservationCount:   len(obsList),
			StartedAt:          "2026-11-13T00:00:00Z",
			CompletedAt:        "2026-11-13T00:00:01Z",
			Host: model.HostMetadata{
				GOOS:              "darwin",
				GOARCH:            "arm64",
				GoVersion:         "go1.26.10",
				NumCPU:            8,
				CoordinatorSHA256: strings.Repeat("a", 64),
			},
			Subjects: []model.SubjectMetadata{
				{
					Name:           "yamlprims-go",
					Role:           "target",
					SubjectVersion: "v0.2.1 (go1.26.10)",
					Language:       "go",
					Engine:         "yamlprims-go",
					Formats:        []string{"yaml", "json"},
					Operations:     []string{"load_value"},
					DriverSHA256:   strings.Repeat("b", 64),
				},
			},
		}
		if mutateManifest != nil {
			mutateManifest(&manifest)
		}
		manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		manifestBytes = append(manifestBytes, '\n')
		return os.WriteFile(filepath.Join(runDir, "run-manifest.json"), manifestBytes, 0o600)
	}

	// 1. valid-smoke
	if err := writeSpecimen("valid-smoke", []model.PackCase{baseCase}, []model.Observation{baseObs}, nil); err != nil {
		return err
	}
	// 2. mutated-pack-sha256
	if err := writeSpecimen("mutated-pack-sha256", []model.PackCase{baseCase}, []model.Observation{baseObs}, func(m *model.RunManifest) {
		m.PackSHA256 = strings.Repeat("f", 64)
	}); err != nil {
		return err
	}
	// 3. missing-case
	secondCase := baseCase
	secondCase.CaseID = "specimen.yaml.smoke-02"
	if err := writeSpecimen("missing-case", []model.PackCase{baseCase, secondCase}, []model.Observation{baseObs}, nil); err != nil {
		return err
	}
	// 4. spurious-case
	spuriousObs := baseObs
	spuriousObs.CaseID = "specimen.yaml.uninvited"
	if err := writeSpecimen("spurious-case", []model.PackCase{baseCase}, []model.Observation{baseObs, spuriousObs}, nil); err != nil {
		return err
	}
	// 5. wrong-digest
	wrongObs := baseObs
	wrongObs.OutputSHA256 = strings.Repeat("d", 64)
	if err := writeSpecimen("wrong-digest", []model.PackCase{baseCase}, []model.Observation{wrongObs}, nil); err != nil {
		return err
	}
	// 6. incomplete-manifest
	if err := writeSpecimen("incomplete-manifest", []model.PackCase{baseCase}, []model.Observation{baseObs}, func(m *model.RunManifest) {
		m.State = "incomplete"
	}); err != nil {
		return err
	}
	// 7. containment-rss-breach
	containCase := baseCase
	containCase.Axis = "containment"
	containCase.Expected = model.CaseExpectation{
		Outcome:    "refused_bounded",
		ErrorPhase: "limit",
	}
	containCase.Budgets = &model.CaseBudgets{
		MaxWallMs:   1000,
		MaxRSSBytes: 8 * 1024 * 1024,
	}
	containObs := baseObs
	containObs.Status = "refused_bounded"
	containObs.ErrorPhase = "limit"
	containObs.PeakRSSBytes = 64 * 1024 * 1024
	if err := writeSpecimen("containment-rss-breach", []model.PackCase{containCase}, []model.Observation{containObs}, nil); err != nil {
		return err
	}
	// 8. migration-divergence
	migCase := baseCase
	migCase.Axis = "migration_divergence"
	migCase.Expected = model.CaseExpectation{
		Outcome:               "accept",
		CanonicalValue:        canonicalOut,
		OutputSHA256:          outSHA,
		YAML12CanonicalSHA256: outSHA,
		YAML11DivergentSHA256: strings.Repeat("e", 64),
		RequireNoDivergence:   true,
		MigrationCategory:     "norway_boolean",
	}
	migObs := baseObs
	migObs.OutputSHA256 = strings.Repeat("e", 64)
	return writeSpecimen("migration-divergence", []model.PackCase{migCase}, []model.Observation{migObs}, nil)
}
