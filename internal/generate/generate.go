// Package generate produces deterministic synthetic structured-document packs
// using a seeded SplitMix64 PRNG.
package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/3leaps/docpresser/internal/model"
	"github.com/3leaps/docpresser/internal/pack"
)

// Options configures deterministic synthetic pack generation.
type Options struct {
	Seed     uint64
	Scenario string
	Scale    string
	OutDir   string
}

type splitMix64 struct {
	state uint64
}

func newSplitMix64(seed uint64) *splitMix64 {
	return &splitMix64{state: seed}
}

func (r *splitMix64) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *splitMix64) intn(n int) int {
	if n <= 1 {
		return 0
	}
	return int(r.next() % uint64(n))
}

func (r *splitMix64) token(prefix string) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteByte('-')
	v := r.next()
	for i := 0; i < 6; i++ {
		b.WriteByte(alphabet[v%uint64(len(alphabet))])
		v /= uint64(len(alphabet))
	}
	return b.String()
}

// Generate creates a deterministic synthetic pack in opts.OutDir.
func Generate(opts Options) (*pack.LoadedPack, error) {
	scale := opts.Scale
	if scale == "" {
		scale = "small"
	}
	switch scale {
	case "small", "medium", "large":
	default:
		return nil, fmt.Errorf("unsupported scale %q (want small, medium, or large)", scale)
	}

	rng := newSplitMix64(opts.Seed)
	cases := make([]model.PackCase, 0, 16)
	inputs := make(map[string][]byte)

	tier := "workload"
	formatFamily := "yaml"

	switch opts.Scenario {
	case "k8s-bundle":
		addK8sBundleCases(rng, scale, &cases, inputs)
	case "openapi-graph":
		formatFamily = "mixed"
		addOpenAPIGraphCases(rng, scale, &cases, inputs)
	case "diabolical-alias-pyramid":
		tier = "stress"
		addDiabolicalCases(rng, scale, &cases, inputs)
	case "yaml11-migration":
		tier = "migration"
		addYAML11MigrationCases(rng, &cases, inputs)
	case "synthetic-workload", "all":
		formatFamily = "mixed"
		addK8sBundleCases(rng, scale, &cases, inputs)
		addOpenAPIGraphCases(rng, scale, &cases, inputs)
		addDiabolicalCases(rng, scale, &cases, inputs)
		addYAML11MigrationCases(rng, &cases, inputs)
	default:
		return nil, fmt.Errorf("unsupported scenario %q (want k8s-bundle, openapi-graph, diabolical-alias-pyramid, yaml11-migration, or synthetic-workload)", opts.Scenario)
	}

	sort.Slice(cases, func(i, j int) bool {
		return cases[i].CaseID < cases[j].CaseID
	})

	meta := model.Pack{
		PackKind:     "synthetic",
		FormatFamily: formatFamily,
		Tier:         tier,
		Scenario:     opts.Scenario,
		Generator:    model.Generator,
		Seed:         strconv.FormatUint(opts.Seed, 10),
		Scale:        scale,
	}
	return pack.WritePack(opts.OutDir, meta, cases, inputs)
}

func scaleMultiplier(scale string) int {
	switch scale {
	case "medium":
		return 3
	case "large":
		return 6
	default:
		return 1
	}
}

func addK8sBundleCases(rng *splitMix64, scale string, cases *[]model.PackCase, inputs map[string][]byte) {
	count := scaleMultiplier(scale)
	for i := 0; i < count; i++ {
		appName := rng.token("svc")
		replicas := 1 + rng.intn(5)
		port := 8000 + rng.intn(1000)

		yamlInput := fmt.Sprintf(
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\ndata:\n  port: \"%d\"\n  replicas: %d\n",
			appName, port, replicas,
		)
		canonical := fmt.Sprintf(
			`[{"entries":[[{"t":"str","v":"apiVersion"},{"t":"str","v":"v1"}],[{"t":"str","v":"kind"},{"t":"str","v":"ConfigMap"}],[{"t":"str","v":"metadata"},{"entries":[[{"t":"str","v":"name"},{"t":"str","v":%q}]],"t":"map"}],[{"t":"str","v":"data"},{"entries":[[{"t":"str","v":"port"},{"t":"str","v":%q}],[{"t":"str","v":"replicas"},{"t":"int","v":"%d"}]],"t":"map"}]],"t":"map"}]`,
			appName, strconv.Itoa(port), replicas,
		)
		caseID := fmt.Sprintf("synth.k8s.configmap.%02d", i+1)
		relPath := fmt.Sprintf("inputs/yaml/k8s/%s.yaml", caseID)
		raw := []byte(yamlInput)
		inputs[relPath] = raw
		*cases = append(*cases, model.PackCase{
			CaseID:      caseID,
			Format:      "yaml",
			Axis:        "correctness",
			Profile:     "synthetic-workload",
			Operation:   "load_value",
			InputPath:   relPath,
			InputSHA256: sha256Hex(raw),
			InputBytes:  int64(len(raw)),
			Expected: model.CaseExpectation{
				Outcome:        "accept",
				CanonicalValue: canonical,
				OutputSHA256:   sha256Hex([]byte(canonical)),
			},
			Tags:        []string{"k8s-bundle", scale},
			Description: "Deterministic synthetic Kubernetes ConfigMap manifest",
		})
	}

	badYAML := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: [unterminated\n")
	badRel := "inputs/yaml/k8s/synth.k8s.malformed.yaml"
	inputs[badRel] = badYAML
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.k8s.malformed",
		Format:      "yaml",
		Axis:        "correctness",
		Profile:     "synthetic-workload",
		Operation:   "load_value",
		InputPath:   badRel,
		InputSHA256: sha256Hex(badYAML),
		InputBytes:  int64(len(badYAML)),
		Expected: model.CaseExpectation{
			Outcome:    "reject",
			ErrorPhase: "syntax",
		},
		Tags:        []string{"k8s-bundle", "negative"},
		Description: "Unterminated flow sequence in synthetic Kubernetes manifest must be rejected",
	})
}

func addOpenAPIGraphCases(rng *splitMix64, scale string, cases *[]model.PackCase, inputs map[string][]byte) {
	count := scaleMultiplier(scale)
	for i := 0; i < count; i++ {
		opID := rng.token("op")
		status := 200 + rng.intn(2)
		yamlSpec := fmt.Sprintf(
			"openapi: 3.1.0\ninfo:\n  title: %s\n  version: 1.0.0\npaths:\n  /health:\n    get:\n      operationId: %s\n      responses:\n        \"%d\":\n          description: ok\n",
			opID, opID, status,
		)
		canonical := fmt.Sprintf(
			`[{"entries":[[{"t":"str","v":"openapi"},{"t":"str","v":"3.1.0"}],[{"t":"str","v":"info"},{"entries":[[{"t":"str","v":"title"},{"t":"str","v":%q}],[{"t":"str","v":"version"},{"t":"str","v":"1.0.0"}]],"t":"map"}],[{"t":"str","v":"paths"},{"entries":[[{"t":"str","v":"/health"},{"entries":[[{"t":"str","v":"get"},{"entries":[[{"t":"str","v":"operationId"},{"t":"str","v":%q}],[{"t":"str","v":"responses"},{"entries":[[{"t":"str","v":"%d"},{"entries":[[{"t":"str","v":"description"},{"t":"str","v":"ok"}]],"t":"map"}]],"t":"map"}]],"t":"map"}]],"t":"map"}]],"t":"map"}]],"t":"map"}]`,
			opID, opID, status,
		)
		caseID := fmt.Sprintf("synth.openapi.spec.%02d", i+1)
		relPath := fmt.Sprintf("inputs/yaml/openapi/%s.yaml", caseID)
		raw := []byte(yamlSpec)
		inputs[relPath] = raw
		*cases = append(*cases, model.PackCase{
			CaseID:      caseID,
			Format:      "yaml",
			Axis:        "correctness",
			Profile:     "synthetic-workload",
			Operation:   "load_value",
			InputPath:   relPath,
			InputSHA256: sha256Hex(raw),
			InputBytes:  int64(len(raw)),
			Expected: model.CaseExpectation{
				Outcome:        "accept",
				CanonicalValue: canonical,
				OutputSHA256:   sha256Hex([]byte(canonical)),
			},
			Tags:        []string{"openapi-graph", scale},
			Description: "Deterministic synthetic OpenAPI 3.1 specification document",
		})
	}

	jcsInput := []byte("{\"b\":1,\"a\":-0.0,\"c\":\"ok\"}\n")
	jcsCanonical := `{"a":0,"b":1,"c":"ok"}`
	jcsRel := "inputs/json/openapi/synth.openapi.jcs-canonical.json"
	inputs[jcsRel] = jcsInput
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.openapi.jcs-canonical",
		Format:      "jcs",
		Axis:        "correctness",
		Profile:     "synthetic-workload",
		Operation:   "project_jcs",
		InputPath:   jcsRel,
		InputSHA256: sha256Hex(jcsInput),
		InputBytes:  int64(len(jcsInput)),
		Expected: model.CaseExpectation{
			Outcome:        "accept",
			CanonicalValue: jcsCanonical,
			OutputSHA256:   sha256Hex([]byte(jcsCanonical)),
		},
		Tags:        []string{"openapi-graph", "jcs", "i-json"},
		Description: "RFC 8785 JCS canonicalization normalizes key order and -0.0",
	})
}

func addDiabolicalCases(rng *splitMix64, scale string, cases *[]model.PackCase, inputs map[string][]byte) {
	levels := 10 + scaleMultiplier(scale)*2
	var aliasPyramid strings.Builder
	_, _ = fmt.Fprintf(&aliasPyramid, "a0: &a0 %q\n", rng.token("leaf"))
	for i := 1; i <= levels; i++ {
		prev := fmt.Sprintf("*a%d", i-1)
		_, _ = fmt.Fprintf(&aliasPyramid, "a%d: &a%d [%s, %s, %s, %s]\n", i, i, prev, prev, prev, prev)
	}
	pyramidBytes := []byte(aliasPyramid.String())
	pyramidRel := "inputs/yaml/diabolical/synth.diabolical.alias-pyramid.yaml"
	inputs[pyramidRel] = pyramidBytes
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.diabolical.alias-pyramid",
		Format:      "yaml",
		Axis:        "containment",
		Profile:     "diabolical-containment",
		Operation:   "load_value",
		InputPath:   pyramidRel,
		InputSHA256: sha256Hex(pyramidBytes),
		InputBytes:  int64(len(pyramidBytes)),
		Expected: model.CaseExpectation{
			Outcome:    "refused_bounded",
			ErrorPhase: "limit",
		},
		Budgets: &model.CaseBudgets{
			MaxWallMs:             2000,
			MaxRSSBytes:           64 * 1024 * 1024,
			MaxOutputBytes:        4 * 1024 * 1024,
			MaxAmplificationRatio: 32.0,
		},
		Tags:        []string{"diabolical", "alias-amplification", "billion-laughs"},
		Description: "Exponential alias expansion pyramid must be refused within bounded memory and wall time",
	})

	depth := 160
	deepNest := []byte(strings.Repeat("[", depth) + "1" + strings.Repeat("]", depth) + "\n")
	deepRel := "inputs/yaml/diabolical/synth.diabolical.deep-nesting.yaml"
	inputs[deepRel] = deepNest
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.diabolical.deep-nesting",
		Format:      "yaml",
		Axis:        "containment",
		Profile:     "diabolical-containment",
		Operation:   "load_value",
		InputPath:   deepRel,
		InputSHA256: sha256Hex(deepNest),
		InputBytes:  int64(len(deepNest)),
		Expected: model.CaseExpectation{
			Outcome:    "refused_bounded",
			ErrorPhase: "limit",
		},
		Budgets: &model.CaseBudgets{
			MaxWallMs:             2000,
			MaxRSSBytes:           64 * 1024 * 1024,
			MaxOutputBytes:        4 * 1024 * 1024,
			MaxAmplificationRatio: 32.0,
		},
		Tags:        []string{"diabolical", "deep-nesting"},
		Description: "Deeply nested flow sequence exceeding recursion depth ceiling must be refused without stack overflow",
	})

	var quadAnchor strings.Builder
	v0, v1, v2, v3 := rng.token("v"), rng.token("v"), rng.token("v"), rng.token("v")
	for i := 0; i < 2600; i++ {
		_, _ = fmt.Fprintf(&quadAnchor, "k%04d: &shared [%q, %q, %q, %q]\n", i, v0, v1, v2, v3)
		_, _ = fmt.Fprintf(&quadAnchor, "ref%04d: [*shared, *shared, *shared, *shared]\n", i)
	}
	quadBytes := []byte(quadAnchor.String())
	quadRel := "inputs/yaml/diabolical/synth.diabolical.anchor-churn.yaml"
	inputs[quadRel] = quadBytes
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.diabolical.anchor-churn",
		Format:      "yaml",
		Axis:        "containment",
		Profile:     "diabolical-containment",
		Operation:   "load_value",
		InputPath:   quadRel,
		InputSHA256: sha256Hex(quadBytes),
		InputBytes:  int64(len(quadBytes)),
		Expected: model.CaseExpectation{
			Outcome:    "refused_bounded",
			ErrorPhase: "limit",
		},
		Budgets: &model.CaseBudgets{
			MaxWallMs:             2000,
			MaxRSSBytes:           64 * 1024 * 1024,
			MaxOutputBytes:        4 * 1024 * 1024,
			MaxAmplificationRatio: 32.0,
		},
		Tags:        []string{"diabolical", "anchor-churn"},
		Description: "Repeated anchor redefinition and alias fan-out exceeding alias event limits must be refused boundedly",
	})
}

func addYAML11MigrationCases(_ *splitMix64, cases *[]model.PackCase, inputs map[string][]byte) {
	// 1. Norway problem bare scalars (NO, on, off, yes)
	norwayYAML := []byte("country: NO\nswitch: off\nenabled: yes\n")
	norwayYAML12 := `[{"entries":[[{"t":"str","v":"country"},{"t":"str","v":"NO"}],[{"t":"str","v":"switch"},{"t":"str","v":"off"}],[{"t":"str","v":"enabled"},{"t":"str","v":"yes"}]],"t":"map"}]`
	norwayYAML11 := `[{"entries":[[{"t":"str","v":"country"},{"t":"bool","v":false}],[{"t":"str","v":"switch"},{"t":"bool","v":false}],[{"t":"str","v":"enabled"},{"t":"bool","v":true}]],"t":"map"}]`
	norwayRel := "inputs/yaml/migration/synth.migration.norway-booleans.yaml"
	inputs[norwayRel] = norwayYAML
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.migration.norway-booleans",
		Format:      "yaml",
		Axis:        "migration_divergence",
		Profile:     "yaml11-migration",
		Operation:   "load_value",
		InputPath:   norwayRel,
		InputSHA256: sha256Hex(norwayYAML),
		InputBytes:  int64(len(norwayYAML)),
		Expected: model.CaseExpectation{
			Outcome:               "accept",
			CanonicalValue:        norwayYAML12,
			OutputSHA256:          sha256Hex([]byte(norwayYAML12)),
			YAML12CanonicalSHA256: sha256Hex([]byte(norwayYAML12)),
			YAML11DivergentSHA256: sha256Hex([]byte(norwayYAML11)),
			MigrationCategory:     "norway_boolean",
			ExpectedHazards:       []string{"yaml11_boolean_scalar"},
		},
		Tags:        []string{"yaml11-migration", "norway_boolean"},
		Description: "Bare scalars NO, off, and yes are strings in YAML 1.2 Core but booleans in YAML 1.1",
	})

	// 2. Legacy octal bare scalar (010, 0755)
	octalYAML := []byte("mode: 0755\ncount: 010\n")
	octalYAML12 := `[{"entries":[[{"t":"str","v":"mode"},{"t":"int","v":"755"}],[{"t":"str","v":"count"},{"t":"int","v":"10"}]],"t":"map"}]`
	octalYAML11 := `[{"entries":[[{"t":"str","v":"mode"},{"t":"int","v":"493"}],[{"t":"str","v":"count"},{"t":"int","v":"8"}]],"t":"map"}]`
	octalRel := "inputs/yaml/migration/synth.migration.legacy-octal.yaml"
	inputs[octalRel] = octalYAML
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.migration.legacy-octal",
		Format:      "yaml",
		Axis:        "migration_divergence",
		Profile:     "yaml11-migration",
		Operation:   "load_value",
		InputPath:   octalRel,
		InputSHA256: sha256Hex(octalYAML),
		InputBytes:  int64(len(octalYAML)),
		Expected: model.CaseExpectation{
			Outcome:               "accept",
			CanonicalValue:        octalYAML12,
			OutputSHA256:          sha256Hex([]byte(octalYAML12)),
			YAML12CanonicalSHA256: sha256Hex([]byte(octalYAML12)),
			YAML11DivergentSHA256: sha256Hex([]byte(octalYAML11)),
			MigrationCategory:     "octal_number",
			ExpectedHazards:       []string{"yaml11_octal_scalar"},
		},
		Tags:        []string{"yaml11-migration", "octal_number"},
		Description: "Leading-zero integers 0755 and 010 are decimal in YAML 1.2 Core (0o prefix required for octal) but octal in YAML 1.1",
	})

	// 3. Sexagesimal bare scalar (10:20)
	sexaYAML := []byte("window: 10:20\n")
	sexaYAML12 := `[{"entries":[[{"t":"str","v":"window"},{"t":"str","v":"10:20"}]],"t":"map"}]`
	sexaYAML11 := `[{"entries":[[{"t":"str","v":"window"},{"t":"int","v":"620"}]],"t":"map"}]`
	sexaRel := "inputs/yaml/migration/synth.migration.sexagesimal.yaml"
	inputs[sexaRel] = sexaYAML
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.migration.sexagesimal",
		Format:      "yaml",
		Axis:        "migration_divergence",
		Profile:     "yaml11-migration",
		Operation:   "load_value",
		InputPath:   sexaRel,
		InputSHA256: sha256Hex(sexaYAML),
		InputBytes:  int64(len(sexaYAML)),
		Expected: model.CaseExpectation{
			Outcome:               "accept",
			CanonicalValue:        sexaYAML12,
			OutputSHA256:          sha256Hex([]byte(sexaYAML12)),
			YAML12CanonicalSHA256: sha256Hex([]byte(sexaYAML12)),
			YAML11DivergentSHA256: sha256Hex([]byte(sexaYAML11)),
			MigrationCategory:     "sexagesimal_number",
			ExpectedHazards:       []string{"yaml11_sexagesimal_scalar"},
		},
		Tags:        []string{"yaml11-migration", "sexagesimal_number"},
		Description: "Base-60 literal 10:20 is a string in YAML 1.2 Core but integer 620 in YAML 1.1",
	})

	// 4. Merge key (<<)
	mergeYAML := []byte("base: &b\n  x: 1\nmerged:\n  <<: *b\n  y: 2\n")
	mergeYAML12 := `[{"entries":[[{"t":"str","v":"base"},{"entries":[[{"t":"str","v":"x"},{"t":"int","v":"1"}]],"t":"map"}],[{"t":"str","v":"merged"},{"entries":[[{"t":"str","v":"<<"},{"entries":[[{"t":"str","v":"x"},{"t":"int","v":"1"}]],"t":"map"}],[{"t":"str","v":"y"},{"t":"int","v":"2"}]],"t":"map"}]],"t":"map"}]`
	mergeYAML11 := `[{"entries":[[{"t":"str","v":"base"},{"entries":[[{"t":"str","v":"x"},{"t":"int","v":"1"}]],"t":"map"}],[{"t":"str","v":"merged"},{"entries":[[{"t":"str","v":"x"},{"t":"int","v":"1"}],[{"t":"str","v":"y"},{"t":"int","v":"2"}]],"t":"map"}]],"t":"map"}]`
	mergeRel := "inputs/yaml/migration/synth.migration.merge-key.yaml"
	inputs[mergeRel] = mergeYAML
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.migration.merge-key",
		Format:      "yaml",
		Axis:        "migration_divergence",
		Profile:     "yaml-core",
		Operation:   "load_value",
		InputPath:   mergeRel,
		InputSHA256: sha256Hex(mergeYAML),
		InputBytes:  int64(len(mergeYAML)),
		Expected: model.CaseExpectation{
			Outcome:               "accept",
			CanonicalValue:        mergeYAML12,
			OutputSHA256:          sha256Hex([]byte(mergeYAML12)),
			YAML12CanonicalSHA256: sha256Hex([]byte(mergeYAML12)),
			YAML11DivergentSHA256: sha256Hex([]byte(mergeYAML11)),
			MigrationCategory:     "merge_key",
			ExpectedHazards:       []string{"yaml11_merge_key"},
		},
		Tags:        []string{"yaml11-migration", "merge_key"},
		Description: "Unquoted << key is literal in strict YAML 1.2 Core and splices mappings in YAML 1.1 / merge-enabled loaders",
	})

	// 5. Quoted YAML11Safe round-trip (must not diverge across YAML 1.2 and YAML 1.1)
	safeYAML := []byte("country: \"NO\"\nswitch: \"off\"\nwindow: \"10:20\"\n")
	safeCanonical := `[{"entries":[[{"t":"str","v":"country"},{"t":"str","v":"NO"}],[{"t":"str","v":"switch"},{"t":"str","v":"off"}],[{"t":"str","v":"window"},{"t":"str","v":"10:20"}]],"t":"map"}]`
	safeRel := "inputs/yaml/migration/synth.migration.yaml11safe-quoted.yaml"
	inputs[safeRel] = safeYAML
	*cases = append(*cases, model.PackCase{
		CaseID:      "synth.migration.yaml11safe-quoted",
		Format:      "yaml",
		Axis:        "migration_divergence",
		Profile:     "yaml11-migration",
		Operation:   "load_value",
		InputPath:   safeRel,
		InputSHA256: sha256Hex(safeYAML),
		InputBytes:  int64(len(safeYAML)),
		Expected: model.CaseExpectation{
			Outcome:               "accept",
			CanonicalValue:        safeCanonical,
			OutputSHA256:          sha256Hex([]byte(safeCanonical)),
			YAML12CanonicalSHA256: sha256Hex([]byte(safeCanonical)),
			RequireNoDivergence:   true,
			MigrationCategory:     "yaml11safe_roundtrip",
		},
		Tags:        []string{"yaml11-migration", "yaml11safe"},
		Description: "Yaml11Safe quoted scalars must produce identical string values across YAML 1.2 and YAML 1.1 parsers",
	})
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
