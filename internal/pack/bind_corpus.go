package pack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/3leaps/docpresser/internal/model"
)

type structcorpusRecord struct {
	ID        string                `json:"id"`
	Format    string                `json:"format"`
	Category  string                `json:"category"`
	Input     structcorpusInput     `json:"input"`
	Execution structcorpusExecution `json:"execution"`
	Expect    structcorpusExpect    `json:"expectations"`
	Review    structcorpusReview    `json:"review"`
}

type structcorpusInput struct {
	Path   string `json:"path"`
	Recipe string `json:"recipe"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type structcorpusExecution struct {
	Tier    string               `json:"tier"`
	Hazards []string             `json:"hazards"`
	Budgets *structcorpusBudgets `json:"budgets,omitempty"`
}

type structcorpusBudgets struct {
	MaxWallMs      int64 `json:"max_wall_ms,omitempty"`
	MaxRSSBytes    int64 `json:"max_rss_bytes,omitempty"`
	MaxOutputBytes int64 `json:"max_output_bytes,omitempty"`
}

type structcorpusExpect struct {
	Value []structcorpusValueExpect `json:"value"`
}

type structcorpusValueExpect struct {
	Profile         string             `json:"profile"`
	ProfileRevision int                `json:"profile_revision"`
	Documents       json.RawMessage    `json:"documents,omitempty"`
	Error           *structcorpusError `json:"error,omitempty"`
}

type structcorpusError struct {
	Phase string `json:"phase"`
	Kind  string `json:"kind"`
	Limit string `json:"limit"`
}

type structcorpusReview struct {
	Rationale string `json:"rationale"`
}

// BindCorpus binds a 3leaps/structcorpus checkout at the requested tier into an offline pack.
func BindCorpus(ctx context.Context, corpusRoot, tier, outDir string) (*LoadedPack, error) {
	allowedTiers, err := allowedTiersFor(tier)
	if err != nil {
		return nil, err
	}
	absCorpus, err := filepath.Abs(corpusRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve corpus path: %w", err)
	}
	info, err := os.Stat(absCorpus)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("corpus root %q is not an accessible directory", corpusRoot)
	}

	commit := detectCorpusCommit(ctx, absCorpus)

	cases := make([]model.PackCase, 0, 256)
	inputs := make(map[string][]byte)

	staticCasesPath := filepath.Join(absCorpus, "manifests", "cases.jsonl")
	if _, statErr := os.Stat(staticCasesPath); statErr != nil {
		return nil, fmt.Errorf("missing manifests/cases.jsonl in corpus root: %w", statErr)
	}
	corpusOSRoot, err := os.OpenRoot(absCorpus)
	if err != nil {
		return nil, fmt.Errorf("open corpus root: %w", err)
	}
	defer func() { _ = corpusOSRoot.Close() }()

	staticData, err := readRootFile(corpusOSRoot, "manifests/cases.jsonl")
	if err != nil {
		return nil, fmt.Errorf("read manifests/cases.jsonl: %w", err)
	}
	if err := appendStructcorpusLines(staticData, allowedTiers, tier, func(rec structcorpusRecord) (string, []byte, error) {
		raw, readErr := readRootFile(corpusOSRoot, rec.Input.Path)
		if readErr != nil {
			return "", nil, fmt.Errorf("read static fixture %q: %w", rec.Input.Path, readErr)
		}
		base := filepath.Base(rec.Input.Path)
		packRel := filepath.ToSlash(filepath.Join("inputs", rec.Format, "fixtures", base))
		return packRel, raw, nil
	}, &cases, inputs); err != nil {
		return nil, err
	}

	corpusTool := filepath.Join(absCorpus, "tools", "corpus.py")
	if _, toolErr := os.Stat(corpusTool); toolErr == nil {
		tmpParent, mkErr := os.MkdirTemp("", "docpresser-hydrate-*")
		if mkErr != nil {
			return nil, fmt.Errorf("create temp hydrate parent: %w", mkErr)
		}
		defer func() { _ = os.RemoveAll(tmpParent) }()

		hydratedDir := filepath.Join(tmpParent, "hydrated")
		hydrateCmd := exec.CommandContext(ctx, "python3", corpusTool, "hydrate", "--tier", tier, "--output", hydratedDir)
		hydrateCmd.Dir = absCorpus
		if out, runErr := hydrateCmd.CombinedOutput(); runErr != nil {
			return nil, fmt.Errorf("structcorpus hydrate failed: %w (%s)", runErr, strings.TrimSpace(string(out)))
		}
		verifyCmd := exec.CommandContext(ctx, "python3", corpusTool, "verify", "--root", hydratedDir, "--repo", absCorpus)
		verifyCmd.Dir = absCorpus
		if out, runErr := verifyCmd.CombinedOutput(); runErr != nil {
			return nil, fmt.Errorf("structcorpus verify failed: %w (%s)", runErr, strings.TrimSpace(string(out)))
		}

		hydratedRoot, openErr := os.OpenRoot(hydratedDir)
		if openErr != nil {
			return nil, fmt.Errorf("open hydrated root: %w", openErr)
		}
		defer func() { _ = hydratedRoot.Close() }()

		hydratedManifest, readErr := readRootFile(hydratedRoot, "cases.jsonl")
		if readErr != nil {
			return nil, fmt.Errorf("read hydrated cases.jsonl: %w", readErr)
		}
		if err := appendStructcorpusLines(hydratedManifest, allowedTiers, tier, func(rec structcorpusRecord) (string, []byte, error) {
			idx := strings.LastIndex(rec.ID, ".")
			if idx <= 0 || idx == len(rec.ID)-1 {
				return "", nil, fmt.Errorf("hydrated case id %q missing recipe prefix", rec.ID)
			}
			recipeID := rec.ID[:idx]
			caseSuffix := rec.ID[idx+1:]
			relInHydrated := filepath.ToSlash(filepath.Join("inputs", rec.Format, recipeID, caseSuffix+".yaml"))
			raw, fErr := readRootFile(hydratedRoot, relInHydrated)
			if fErr != nil {
				return "", nil, fmt.Errorf("read hydrated input %q: %w", relInHydrated, fErr)
			}
			return relInHydrated, raw, nil
		}, &cases, inputs); err != nil {
			return nil, err
		}
	}

	sort.Slice(cases, func(i, j int) bool {
		return cases[i].CaseID < cases[j].CaseID
	})

	meta := model.Pack{
		PackKind:      "structcorpus",
		FormatFamily:  "yaml",
		Tier:          tier,
		CorpusCommit:  commit,
		CorpusProfile: "yamlprims-core",
	}
	return WritePack(outDir, meta, cases, inputs)
}

func allowedTiersFor(tier string) (map[string]bool, error) {
	switch tier {
	case "smoke":
		return map[string]bool{"smoke": true}, nil
	case "conformance":
		return map[string]bool{"smoke": true, "conformance": true}, nil
	case "stress":
		return map[string]bool{"smoke": true, "conformance": true, "stress": true}, nil
	default:
		return nil, fmt.Errorf("unsupported structcorpus tier %q (want smoke, conformance, or stress)", tier)
	}
}

func detectCorpusCommit(ctx context.Context, absCorpus string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", absCorpus, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err == nil {
		candidate := strings.TrimSpace(string(out))
		if len(candidate) == 40 {
			return candidate
		}
	}
	return model.StructcorpusPinnedCommit
}

func appendStructcorpusLines(
	data []byte,
	allowedTiers map[string]bool,
	boundTier string,
	resolveInput func(rec structcorpusRecord) (string, []byte, error),
	cases *[]model.PackCase,
	inputs map[string][]byte,
) error {
	trimmed := bytes.TrimRight(data, "\r\n")
	if len(trimmed) == 0 {
		return nil
	}
	lines := bytes.Split(trimmed, []byte{'\n'})
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec structcorpusRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("parse structcorpus line %d: %w", i+1, err)
		}
		if !allowedTiers[rec.Execution.Tier] {
			continue
		}
		packRel, raw, err := resolveInput(rec)
		if err != nil {
			return err
		}
		if int64(len(raw)) != rec.Input.Bytes || sha256Hex(raw) != rec.Input.SHA256 {
			return fmt.Errorf("structcorpus case %q input digest/length mismatch", rec.ID)
		}
		inputs[packRel] = raw
		pc, err := mapStructcorpusCase(rec, packRel, boundTier)
		if err != nil {
			return fmt.Errorf("map structcorpus case %q: %w", rec.ID, err)
		}
		*cases = append(*cases, pc)
	}
	return nil
}

func mapStructcorpusCase(rec structcorpusRecord, packRel, boundTier string) (model.PackCase, error) {
	if len(rec.Expect.Value) == 0 {
		return model.PackCase{}, fmt.Errorf("missing value expectations")
	}
	selected := selectValueExpectation(rec.Expect.Value)
	axis := "correctness"
	if rec.Execution.Tier == "stress" || (selected.Error != nil && selected.Error.Phase == "limit") {
		axis = "containment"
	}

	var exp model.CaseExpectation
	if selected.Error != nil {
		if axis == "containment" {
			exp.Outcome = "refused_bounded"
		} else {
			exp.Outcome = "reject"
		}
		exp.ErrorPhase = selected.Error.Phase
		exp.ErrorCode = selected.Error.Kind
		if exp.ErrorCode == "" {
			exp.ErrorCode = selected.Error.Limit
		}
	} else {
		exp.Outcome = "accept"
		var compact bytes.Buffer
		if err := json.Compact(&compact, selected.Documents); err != nil {
			return model.PackCase{}, fmt.Errorf("compact expected documents: %w", err)
		}
		exp.CanonicalValue = compact.String()
		exp.OutputSHA256 = sha256Hex(compact.Bytes())
	}

	var budgets *model.CaseBudgets
	if rec.Execution.Budgets != nil {
		budgets = &model.CaseBudgets{
			MaxWallMs:      rec.Execution.Budgets.MaxWallMs,
			MaxRSSBytes:    rec.Execution.Budgets.MaxRSSBytes,
			MaxOutputBytes: rec.Execution.Budgets.MaxOutputBytes,
		}
	} else if axis == "containment" {
		budgets = &model.CaseBudgets{
			MaxWallMs:             5000,
			MaxRSSBytes:           128 * 1024 * 1024,
			MaxOutputBytes:        16 * 1024 * 1024,
			MaxAmplificationRatio: 64.0,
		}
	}

	tags := []string{rec.Category, rec.Execution.Tier}
	tags = append(tags, rec.Execution.Hazards...)

	profile := selected.Profile
	if profile == "" {
		profile = boundTier
	}

	return model.PackCase{
		CaseID:      rec.ID,
		Format:      rec.Format,
		Axis:        axis,
		Profile:     profile,
		Operation:   "load_value",
		InputPath:   packRel,
		InputSHA256: rec.Input.SHA256,
		InputBytes:  rec.Input.Bytes,
		Expected:    exp,
		Budgets:     budgets,
		Tags:        tags,
		Description: rec.Review.Rationale,
	}, nil
}

func selectValueExpectation(vals []structcorpusValueExpect) structcorpusValueExpect {
	best := vals[0]
	for _, v := range vals[1:] {
		if v.Profile == "yamlprims-core" && (best.Profile != "yamlprims-core" || v.ProfileRevision > best.ProfileRevision) {
			best = v
		}
	}
	return best
}
