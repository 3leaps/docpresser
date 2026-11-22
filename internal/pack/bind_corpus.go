package pack

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // git object ids
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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
	Tier    string   `json:"tier"`
	Hazards []string `json:"hazards"`
	// Budgets is kept raw so an explicit null is distinguishable from an
	// absent object; parseBudgets validates it.
	Budgets json.RawMessage `json:"budgets,omitempty"`
}

// structcorpusBudgets mirrors structcorpus case.v1 execution.budgets; a zero
// field was not declared.
type structcorpusBudgets struct {
	WallMs      int64
	MemoryBytes int64
	OutputBytes int64
}

// parseBudgets validates execution.budgets against case.v1: absent is
// allowed; otherwise it must be an object of at least one known field, each
// a positive integer. Null, unknown, non-integer and non-positive values fail
// the bind instead of becoming absent budgets.
func parseBudgets(raw json.RawMessage) (*structcorpusBudgets, error) {
	if raw == nil {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("execution.budgets: null is not a budget object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("execution.budgets: %w", err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("execution.budgets: no budget declared")
	}
	var out structcorpusBudgets
	for name, value := range fields {
		var dst *int64
		switch name {
		case "wall_ms":
			dst = &out.WallMs
		case "memory_bytes":
			dst = &out.MemoryBytes
		case "output_bytes":
			dst = &out.OutputBytes
		default:
			return nil, fmt.Errorf("execution.budgets: unknown field %q", name)
		}
		dec := json.NewDecoder(bytes.NewReader(value))
		dec.UseNumber()
		var n any
		if err := dec.Decode(&n); err != nil {
			return nil, fmt.Errorf("execution.budgets.%s: %w", name, err)
		}
		num, ok := n.(json.Number)
		if !ok {
			return nil, fmt.Errorf("execution.budgets.%s: %s is not an integer", name, value)
		}
		v, err := num.Int64()
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("execution.budgets.%s: %s is not a positive integer", name, value)
		}
		*dst = v
	}
	return &out, nil
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

	commit, err := cleanCorpusCommit(ctx, absCorpus)
	if err != nil {
		return nil, err
	}
	// Every byte bound below comes from a snapshot of that commit's tree,
	// never from the working tree, so ignored, assume-unchanged or
	// concurrently edited files cannot enter the pack.
	snapshotParent, err := os.MkdirTemp("", "docpresser-corpus-*")
	if err != nil {
		return nil, fmt.Errorf("create corpus snapshot directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(snapshotParent) }()
	snapshot := filepath.Join(snapshotParent, "structcorpus")
	if err := exportCommitTree(ctx, absCorpus, commit, snapshot); err != nil {
		return nil, err
	}

	cases := make([]model.PackCase, 0, 256)
	inputs := make(map[string][]byte)

	staticCasesPath := filepath.Join(snapshot, "manifests", "cases.jsonl")
	if _, statErr := os.Stat(staticCasesPath); statErr != nil {
		return nil, fmt.Errorf("missing manifests/cases.jsonl in corpus root: %w", statErr)
	}
	corpusOSRoot, err := os.OpenRoot(snapshot)
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

	corpusTool := filepath.Join(snapshot, "tools", "corpus.py")
	if _, toolErr := os.Stat(corpusTool); toolErr == nil {
		tmpParent, mkErr := os.MkdirTemp("", "docpresser-hydrate-*")
		if mkErr != nil {
			return nil, fmt.Errorf("create temp hydrate parent: %w", mkErr)
		}
		defer func() { _ = os.RemoveAll(tmpParent) }()

		hydratedDir := filepath.Join(tmpParent, "hydrated")
		hydrateCmd := exec.CommandContext(ctx, "python3", corpusTool, "hydrate", "--tier", tier, "--output", hydratedDir)
		hydrateCmd.Dir = snapshot
		if out, runErr := hydrateCmd.CombinedOutput(); runErr != nil {
			return nil, fmt.Errorf("structcorpus hydrate failed: %w (%s)", runErr, strings.TrimSpace(string(out)))
		}
		verifyCmd := exec.CommandContext(ctx, "python3", corpusTool, "verify", "--root", hydratedDir, "--repo", snapshot)
		verifyCmd.Dir = snapshot
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

// corpusGitEnv is the process environment without GIT_* variables, so
// repository, work-tree, index, object-store and configuration redirection
// cannot make git inspect anything other than the corpus root. Replacement
// objects are disabled as well (see corpusGitArgs).
func corpusGitEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(kv), "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_REPLACE_OBJECTS=1")
}

// corpusGitArgs prefixes every corpus git command: refs/replace must not
// substitute a commit, tree or blob for the one the pack records.
func corpusGitArgs(absCorpus string, args ...string) []string {
	return append([]string{"--no-replace-objects", "-C", absCorpus}, args...)
}

// gitBlobID returns the git object id of a blob with the given content, in
// the hash family of wantID (SHA-1 for 40 hex digits, SHA-256 for 64).
func gitBlobID(data []byte, wantID string) string {
	header := []byte("blob " + strconv.Itoa(len(data)) + "\x00")
	if len(wantID) == 64 {
		h := sha256.New()
		h.Write(header)
		h.Write(data)
		return hex.EncodeToString(h.Sum(nil))
	}
	h := sha1.New() //nolint:gosec // git object ids, not a security hash choice
	h.Write(header)
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func corpusGit(ctx context.Context, absCorpus string, stdout io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", corpusGitArgs(absCorpus, args...)...)
	cmd.Env = corpusGitEnv()
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func corpusGitOutput(ctx context.Context, absCorpus string, args ...string) (string, error) {
	var out bytes.Buffer
	if err := corpusGit(ctx, absCorpus, &out, args...); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// cleanCorpusCommit returns the commit checked out at absCorpus. absCorpus
// must be the top level of its own git work tree (the same directory git
// resolves, not a parent repository's) with no staged, unstaged or untracked
// changes. The bound bytes come from a snapshot of this commit, so the clean
// check only keeps a pack from silently omitting local edits.
func cleanCorpusCommit(ctx context.Context, absCorpus string) (string, error) {
	top, err := corpusGitOutput(ctx, absCorpus, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("structcorpus root %q is not a git checkout: %w", absCorpus, err)
	}
	topInfo, err := os.Stat(top)
	if err != nil {
		return "", fmt.Errorf("structcorpus work tree %q: %w", top, err)
	}
	rootInfo, err := os.Stat(absCorpus)
	if err != nil {
		return "", fmt.Errorf("structcorpus root %q: %w", absCorpus, err)
	}
	if !os.SameFile(topInfo, rootInfo) {
		return "", fmt.Errorf("structcorpus root %q is not the top of its git work tree (%s)", absCorpus, top)
	}
	commit, err := corpusGitOutput(ctx, absCorpus, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("structcorpus checkout has no commit: %w", err)
	}
	if !isHexID(commit) {
		return "", fmt.Errorf("structcorpus HEAD %q is not a full commit id", commit)
	}
	status, err := corpusGitOutput(ctx, absCorpus, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if status != "" {
		return "", fmt.Errorf("structcorpus checkout %s has uncommitted changes; bind requires a clean checkout", absCorpus)
	}
	return commit, nil
}

func isHexID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// exportCommitTree writes every file of commit's tree under dst using git
// plumbing (ls-tree and cat-file), so the snapshot holds exactly the
// committed blobs: no working-tree, index, ignored-file or export-attribute
// effects. Symbolic links and submodules are refused.
func exportCommitTree(ctx context.Context, absCorpus, commit, dst string) error {
	var listing bytes.Buffer
	if err := corpusGit(ctx, absCorpus, &listing, "ls-tree", "-r", "-z", "--full-tree", commit); err != nil {
		return fmt.Errorf("list structcorpus commit %s: %w", commit, err)
	}
	type entry struct{ mode, object, path string }
	var entries []entry
	for _, rec := range bytes.Split(bytes.TrimSuffix(listing.Bytes(), []byte{0}), []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, path, ok := bytes.Cut(rec, []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 3 {
			return fmt.Errorf("unexpected ls-tree record %q", rec)
		}
		mode, kind, object := fields[0], fields[1], fields[2]
		if kind != "blob" || (mode != "100644" && mode != "100755") {
			return fmt.Errorf("structcorpus commit %s: %s %s (%s) is not a regular file", commit, kind, path, mode)
		}
		entries = append(entries, entry{mode, object, string(path)})
	}
	if len(entries) == 0 {
		return fmt.Errorf("structcorpus commit %s has no files", commit)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return fmt.Errorf("create corpus snapshot: %w", err)
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("open corpus snapshot: %w", err)
	}
	defer func() { _ = root.Close() }()

	var objects bytes.Buffer
	for _, e := range entries {
		objects.WriteString(e.object)
		objects.WriteByte('\n')
	}
	cmd := exec.CommandContext(ctx, "git", corpusGitArgs(absCorpus, "cat-file", "--batch")...)
	cmd.Env = corpusGitEnv()
	cmd.Stdin = &objects
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git cat-file: %w", err)
	}
	reader := bufio.NewReader(stdout)
	readErr := func() error {
		for _, e := range entries {
			header, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read cat-file header for %s: %w", e.path, err)
			}
			parts := strings.Fields(header)
			if len(parts) != 3 || parts[0] != e.object || parts[1] != "blob" {
				return fmt.Errorf("unexpected cat-file header %q for %s", strings.TrimSpace(header), e.path)
			}
			size, err := strconv.ParseInt(parts[2], 10, 64)
			if err != nil || size < 0 {
				return fmt.Errorf("bad cat-file size %q for %s", parts[2], e.path)
			}
			data := make([]byte, size)
			if _, err := io.ReadFull(reader, data); err != nil {
				return fmt.Errorf("read blob %s: %w", e.path, err)
			}
			if b, err := reader.ReadByte(); err != nil || b != '\n' {
				return fmt.Errorf("blob %s not terminated", e.path)
			}
			// cat-file echoes the requested id; hash the bytes so a
			// substituted object cannot pass as the committed blob.
			if got := gitBlobID(data, e.object); got != e.object {
				return fmt.Errorf("blob %s content hashes to %s, not %s", e.path, got, e.object)
			}
			if err := root.MkdirAll(filepath.Dir(filepath.FromSlash(e.path)), 0o750); err != nil {
				return fmt.Errorf("create snapshot directory for %s: %w", e.path, err)
			}
			perm := os.FileMode(0o640)
			if e.mode == "100755" {
				perm = 0o750
			}
			if err := root.WriteFile(filepath.FromSlash(e.path), data, perm); err != nil {
				return fmt.Errorf("write snapshot file %s: %w", e.path, err)
			}
		}
		return nil
	}()
	if readErr != nil {
		// Stop cat-file before reaping it: it may be blocked writing the
		// remaining blobs into a pipe nobody will read.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return readErr
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		return fmt.Errorf("git cat-file: %w (%s)", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
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

	declared, err := parseBudgets(rec.Execution.Budgets)
	if err != nil {
		return model.PackCase{}, err
	}
	budgets := bindBudgets(declared, axis)

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

// Containment defaults for budgets a structcorpus case does not declare.
var defaultContainmentBudgets = model.CaseBudgets{
	MaxWallMs:             5000,
	MaxRSSBytes:           128 * 1024 * 1024,
	MaxOutputBytes:        16 * 1024 * 1024,
	MaxAmplificationRatio: 64.0,
}

// bindBudgets maps structcorpus budgets onto pack budgets. Declared values
// are kept exactly. On the containment axis, any budget the case does not
// declare takes the containment default, including the amplification ratio,
// which structcorpus does not express.
func bindBudgets(src *structcorpusBudgets, axis string) *model.CaseBudgets {
	if src == nil && axis != "containment" {
		return nil
	}
	var out model.CaseBudgets
	if axis == "containment" {
		out = defaultContainmentBudgets
	}
	if src != nil {
		if src.WallMs > 0 {
			out.MaxWallMs = src.WallMs
		}
		if src.MemoryBytes > 0 {
			out.MaxRSSBytes = src.MemoryBytes
		}
		if src.OutputBytes > 0 {
			out.MaxOutputBytes = src.OutputBytes
		}
	}
	return &out
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
