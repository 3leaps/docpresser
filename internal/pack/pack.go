// Package pack manages deterministic offline corpus packs and synthetic workload packs.
package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/3leaps/docpresser/internal/model"
	v0 "github.com/3leaps/docpresser/schemas/docpresser/v0"
)

// LoadedPack holds a validated pack manifest, its raw SHA-256, and its ordered cases.
type LoadedPack struct {
	Pack       model.Pack
	PackSHA256 string
	Cases      []model.PackCase
}

// WritePack writes inputs, cases.ndjson, and pack.json into outDir using confined root I/O.
func WritePack(outDir string, meta model.Pack, cases []model.PackCase, inputs map[string][]byte) (*LoadedPack, error) {
	if len(cases) == 0 {
		return nil, fmt.Errorf("pack must contain at least one case")
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return nil, fmt.Errorf("create pack directory: %w", err)
	}
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return nil, fmt.Errorf("open pack root: %w", err)
	}
	defer func() { _ = root.Close() }()

	seenCaseIDs := make(map[string]struct{}, len(cases))
	var casesBuf bytes.Buffer
	for i, c := range cases {
		if err := ValidateCase(c); err != nil {
			return nil, fmt.Errorf("case %d (%s): %w", i, c.CaseID, err)
		}
		if _, exists := seenCaseIDs[c.CaseID]; exists {
			return nil, fmt.Errorf("duplicate case_id %q", c.CaseID)
		}
		seenCaseIDs[c.CaseID] = struct{}{}

		rawInput, ok := inputs[c.InputPath]
		if !ok {
			return nil, fmt.Errorf("case %q references missing input %q", c.CaseID, c.InputPath)
		}
		if int64(len(rawInput)) != c.InputBytes {
			return nil, fmt.Errorf("case %q input_bytes %d does not match actual %d", c.CaseID, c.InputBytes, len(rawInput))
		}
		actualSHA := sha256Hex(rawInput)
		if actualSHA != c.InputSHA256 {
			return nil, fmt.Errorf("case %q input_sha256 %s does not match actual %s", c.CaseID, c.InputSHA256, actualSHA)
		}

		line, err := json.Marshal(c)
		if err != nil {
			return nil, fmt.Errorf("marshal case %q: %w", c.CaseID, err)
		}
		casesBuf.Write(line)
		casesBuf.WriteByte('\n')
	}

	inputPaths := make([]string, 0, len(inputs))
	for p := range inputs {
		if err := validateRelativeInputPath(p); err != nil {
			return nil, err
		}
		inputPaths = append(inputPaths, p)
	}
	sort.Strings(inputPaths)

	files := make([]model.PackFile, 0, 1+len(inputPaths))
	casesBytes := casesBuf.Bytes()
	if err := writeRootFile(root, "cases.ndjson", casesBytes); err != nil {
		return nil, err
	}
	files = append(files, model.PackFile{
		Path:    "cases.ndjson",
		SHA256:  sha256Hex(casesBytes),
		Bytes:   int64(len(casesBytes)),
		Records: len(cases),
	})

	var totalInputBytes int64
	for _, relPath := range inputPaths {
		data := inputs[relPath]
		if err := writeRootFile(root, relPath, data); err != nil {
			return nil, err
		}
		totalInputBytes += int64(len(data))
		files = append(files, model.PackFile{
			Path:    relPath,
			SHA256:  sha256Hex(data),
			Bytes:   int64(len(data)),
			Records: 1,
		})
	}

	meta.Version = model.Version
	meta.State = "complete"
	meta.SchemaSHA256 = v0.SchemaSHA256()
	meta.CaseCount = len(cases)
	meta.TotalInputBytes = totalInputBytes
	meta.Files = files

	packBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal pack.json: %w", err)
	}
	packBytes = append(packBytes, '\n')
	if err := v0.ValidatePackBytes(packBytes); err != nil {
		return nil, fmt.Errorf("validate pack.json: %w", err)
	}
	if err := writeRootFile(root, "pack.json", packBytes); err != nil {
		return nil, err
	}

	return &LoadedPack{
		Pack:       meta,
		PackSHA256: sha256Hex(packBytes),
		Cases:      cases,
	}, nil
}

// LoadAndValidatePack reads and verifies pack.json, cases.ndjson, and all listed input files in packDir.
func LoadAndValidatePack(packDir string) (*LoadedPack, error) {
	root, err := os.OpenRoot(packDir)
	if err != nil {
		return nil, fmt.Errorf("open pack directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	packBytes, err := readRootFile(root, "pack.json")
	if err != nil {
		return nil, fmt.Errorf("read pack.json: %w", err)
	}
	var meta model.Pack
	if err := v0.Decode(packBytes, "pack.schema.json", &meta); err != nil {
		return nil, fmt.Errorf("invalid pack.json: %w", err)
	}
	if meta.SchemaSHA256 != v0.SchemaSHA256() {
		return nil, fmt.Errorf("pack schema_sha256 %s does not match embedded %s", meta.SchemaSHA256, v0.SchemaSHA256())
	}

	fileMap := make(map[string]model.PackFile, len(meta.Files))
	var actualInputBytes int64
	for _, f := range meta.Files {
		if _, dup := fileMap[f.Path]; dup {
			return nil, fmt.Errorf("duplicate file entry %q in pack.json", f.Path)
		}
		if f.Path != "cases.ndjson" {
			if err := validateRelativeInputPath(f.Path); err != nil {
				return nil, fmt.Errorf("invalid file entry %q: %w", f.Path, err)
			}
			actualInputBytes += f.Bytes
		}
		raw, err := readRootFile(root, f.Path)
		if err != nil {
			return nil, fmt.Errorf("read pack file %q: %w", f.Path, err)
		}
		if int64(len(raw)) != f.Bytes {
			return nil, fmt.Errorf("pack file %q byte count mismatch: got %d, want %d", f.Path, len(raw), f.Bytes)
		}
		gotSHA := sha256Hex(raw)
		if gotSHA != f.SHA256 {
			return nil, fmt.Errorf("pack file %q sha256 mismatch: got %s, want %s", f.Path, gotSHA, f.SHA256)
		}
		fileMap[f.Path] = f
	}

	casesFile, ok := fileMap["cases.ndjson"]
	if !ok {
		return nil, fmt.Errorf("pack.json files inventory is missing cases.ndjson")
	}
	if actualInputBytes != meta.TotalInputBytes {
		return nil, fmt.Errorf("pack.json total_input_bytes %d does not match sum of input files %d", meta.TotalInputBytes, actualInputBytes)
	}

	casesBytes, err := readRootFile(root, "cases.ndjson")
	if err != nil {
		return nil, fmt.Errorf("read cases.ndjson: %w", err)
	}
	cases, err := ParseCasesNDJSON(casesBytes)
	if err != nil {
		return nil, err
	}
	if len(cases) != meta.CaseCount || len(cases) != casesFile.Records {
		return nil, fmt.Errorf("case count mismatch: cases.ndjson has %d, pack.json expects %d", len(cases), meta.CaseCount)
	}

	for _, c := range cases {
		f, ok := fileMap[c.InputPath]
		if !ok {
			return nil, fmt.Errorf("case %q input_path %q is not listed in pack.json files", c.CaseID, c.InputPath)
		}
		if f.SHA256 != c.InputSHA256 || f.Bytes != c.InputBytes {
			return nil, fmt.Errorf("case %q input metadata does not match pack.json entry for %q", c.CaseID, c.InputPath)
		}
	}

	return &LoadedPack{
		Pack:       meta,
		PackSHA256: sha256Hex(packBytes),
		Cases:      cases,
	}, nil
}

// ParseCasesNDJSON parses and validates cases.ndjson bytes.
func ParseCasesNDJSON(data []byte) ([]model.PackCase, error) {
	trimmed := bytes.TrimRight(data, "\r\n")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("cases.ndjson is empty")
	}
	lines := bytes.Split(trimmed, []byte{'\n'})
	cases := make([]model.PackCase, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for i, line := range lines {
		var c model.PackCase
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("decode cases.ndjson line %d: %w", i+1, err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("trailing JSON on cases.ndjson line %d", i+1)
		}
		if err := ValidateCase(c); err != nil {
			return nil, fmt.Errorf("invalid case on line %d (%s): %w", i+1, c.CaseID, err)
		}
		if _, exists := seen[c.CaseID]; exists {
			return nil, fmt.Errorf("duplicate case_id %q on line %d", c.CaseID, i+1)
		}
		seen[c.CaseID] = struct{}{}
		cases = append(cases, c)
	}
	return cases, nil
}

// ValidateCase checks structural and semantic invariants of a PackCase.
func ValidateCase(c model.PackCase) error {
	if strings.TrimSpace(c.CaseID) == "" {
		return fmt.Errorf("case_id is required")
	}
	switch c.Format {
	case "yaml", "json", "i-json", "jcs", "xml", "xhtml", "svg", "ooxml", "pdf":
	default:
		return fmt.Errorf("unsupported format %q", c.Format)
	}
	switch c.Axis {
	case "correctness", "containment", "migration_divergence":
	default:
		return fmt.Errorf("unsupported axis %q", c.Axis)
	}
	if strings.TrimSpace(c.Profile) == "" {
		return fmt.Errorf("profile is required")
	}
	switch c.Operation {
	case "load_value", "load_events", "emit_canonical", "project_ijson", "project_jcs", "lint_migration", "extract_text":
	default:
		return fmt.Errorf("unsupported operation %q", c.Operation)
	}
	if err := validateRelativeInputPath(c.InputPath); err != nil {
		return err
	}
	if len(c.InputSHA256) != 64 {
		return fmt.Errorf("input_sha256 must be a 64-character hex digest")
	}
	if c.InputBytes < 0 {
		return fmt.Errorf("input_bytes must be non-negative")
	}
	switch c.Expected.Outcome {
	case "accept", "reject", "refused_bounded":
	default:
		return fmt.Errorf("unsupported expected.outcome %q", c.Expected.Outcome)
	}
	return nil
}

func validateRelativeInputPath(p string) error {
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean != p || !strings.HasPrefix(p, "inputs/") || strings.Contains(p, "..") {
		return fmt.Errorf("input path %q must be a clean relative path under inputs/", p)
	}
	return nil
}

func writeRootFile(root *os.Root, relPath string, data []byte) error {
	dir := filepath.Dir(relPath)
	if dir != "." && dir != "" {
		if err := mkdirAllInRoot(root, dir); err != nil {
			return err
		}
	}
	f, err := root.OpenFile(relPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open %q for write: %w", relPath, err)
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write %q: %w", relPath, writeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("sync %q: %w", relPath, syncErr)
	}
	return closeErr
}

func mkdirAllInRoot(root *os.Root, relDir string) error {
	parts := strings.Split(filepath.ToSlash(relDir), "/")
	cur := ""
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if cur == "" {
			cur = part
		} else {
			cur = cur + "/" + part
		}
		if err := root.Mkdir(cur, 0o750); err != nil && !os.IsExist(err) {
			if info, statErr := root.Stat(cur); statErr == nil && info.IsDir() {
				continue
			}
			return fmt.Errorf("mkdir %q in pack root: %w", cur, err)
		}
	}
	return nil
}

func readRootFile(root *os.Root, relPath string) ([]byte, error) {
	f, err := root.Open(relPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file (mode %s)", relPath, info.Mode().String())
	}
	return io.ReadAll(f)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
