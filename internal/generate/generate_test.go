package generate

import (
	"path/filepath"
	"testing"

	"github.com/3leaps/docpresser/internal/pack"
)

func TestGenerate_DeterministicAcrossScenarios(t *testing.T) {
	scenarios := []string{
		"k8s-bundle",
		"openapi-graph",
		"diabolical-alias-pyramid",
		"yaml11-migration",
		"synthetic-workload",
	}
	for _, sc := range scenarios {
		t.Run(sc, func(t *testing.T) {
			dirA := filepath.Join(t.TempDir(), "a")
			dirB := filepath.Join(t.TempDir(), "b")

			packA, err := Generate(Options{
				Seed:     424242,
				Scenario: sc,
				Scale:    "small",
				OutDir:   dirA,
			})
			if err != nil {
				t.Fatalf("Generate A (%s) failed: %v", sc, err)
			}
			packB, err := Generate(Options{
				Seed:     424242,
				Scenario: sc,
				Scale:    "small",
				OutDir:   dirB,
			})
			if err != nil {
				t.Fatalf("Generate B (%s) failed: %v", sc, err)
			}
			if packA.PackSHA256 != packB.PackSHA256 {
				t.Fatalf("non-deterministic pack SHA-256 for %s: %s != %s", sc, packA.PackSHA256, packB.PackSHA256)
			}
			if _, err := pack.LoadAndValidatePack(dirA); err != nil {
				t.Fatalf("LoadAndValidatePack(%s) failed: %v", sc, err)
			}
		})
	}
}
