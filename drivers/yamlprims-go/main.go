// Command yamlprims-go is the standalone subject driver for 3leaps/yamlprims (Go)
// and 3leaps/canonprims (Go) implementing the docpresser-driver/v0 protocol.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/3leaps/canonprims/canon"
	"github.com/3leaps/yamlprims/yaml"
)

const protocolVersion = "docpresser-driver/v0"

type handshakeResponse struct {
	Version        string   `json:"version"`
	Subject        string   `json:"subject"`
	SubjectVersion string   `json:"subject_version"`
	Language       string   `json:"language"`
	Engine         string   `json:"engine"`
	Formats        []string `json:"formats"`
	Operations     []string `json:"operations"`
}

type caseBudgets struct {
	MaxWallMs             int64   `json:"max_wall_ms,omitempty"`
	MaxRSSBytes           int64   `json:"max_rss_bytes,omitempty"`
	MaxOutputBytes        int64   `json:"max_output_bytes,omitempty"`
	MaxAmplificationRatio float64 `json:"max_amplification_ratio,omitempty"`
}

type caseRequest struct {
	Version     string       `json:"version"`
	CaseID      string       `json:"case_id"`
	Format      string       `json:"format"`
	Operation   string       `json:"operation"`
	Profile     string       `json:"profile,omitempty"`
	InputBase64 string       `json:"input_base64"`
	Budgets     *caseBudgets `json:"budgets,omitempty"`
}

type caseResponse struct {
	Version        string   `json:"version"`
	CaseID         string   `json:"case_id"`
	Subject        string   `json:"subject,omitempty"`
	SubjectVersion string   `json:"subject_version,omitempty"`
	Status         string   `json:"status"`
	OutputSHA256   string   `json:"output_sha256,omitempty"`
	OutputBytes    int64    `json:"output_bytes,omitempty"`
	DocumentCount  int      `json:"document_count,omitempty"`
	EventCount     int      `json:"event_count,omitempty"`
	CanonicalValue string   `json:"canonical_value,omitempty"`
	ErrorPhase     string   `json:"error_phase,omitempty"`
	ErrorCode      string   `json:"error_code,omitempty"`
	ParseNs        int64    `json:"parse_ns,omitempty"`
	EmitNs         int64    `json:"emit_ns,omitempty"`
	HazardsFound   []string `json:"hazards_found,omitempty"`
}

// subjectVersion names both engines this driver runs: yamlprims for YAML and
// JSON loading, canonprims for the JCS projection.
func subjectVersion() string {
	return "yamlprims " + moduleVersion("github.com/3leaps/yamlprims") +
		", canonprims " + moduleVersion("github.com/3leaps/canonprims") +
		" (" + runtime.Version() + ")"
}

// moduleVersion reports the version of mod linked into this binary, read from
// the build information Go records, so it cannot drift from go.mod.
func moduleVersion(mod string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == mod {
			if dep.Replace != nil {
				return dep.Replace.Version + " (replaced)"
			}
			return dep.Version
		}
	}
	return "unknown"
}

func hasArg(want string) bool {
	for _, arg := range os.Args[1:] {
		if arg == want {
			return true
		}
	}
	return false
}

func main() {
	if hasArg("--docpresser-handshake") {
		resp := handshakeResponse{
			Version:        protocolVersion,
			Subject:        "yamlprims-go",
			SubjectVersion: subjectVersion(),
			Language:       "go",
			Engine:         "3leaps/yamlprims+canonprims",
			Formats:        []string{"yaml", "json", "i-json", "jcs"},
			Operations:     []string{"load_value", "project_jcs", "project_ijson"},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(resp)
		return
	}

	rawReq, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}

	var req caseRequest
	if err := json.Unmarshal(rawReq, &req); err != nil {
		os.Exit(2)
	}

	input, err := base64.StdEncoding.DecodeString(req.InputBase64)
	if err != nil {
		os.Exit(2)
	}

	resp := executeCase(req, input)
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
}

func executeCase(req caseRequest, input []byte) caseResponse {
	switch req.Operation {
	case "project_jcs":
		return executeJCS(req, input)
	case "project_ijson":
		return executeIJSON(req, input)
	case "load_value", "":
		return executeLoadValue(req, input)
	default:
		return caseResponse{
			Version:        protocolVersion,
			CaseID:         req.CaseID,
			Subject:        "yamlprims-go",
			SubjectVersion: subjectVersion(),
			Status:         "unsupported",
			ErrorPhase:     "driver",
			ErrorCode:      "unsupported_operation",
		}
	}
}

func executeJCS(req caseRequest, input []byte) caseResponse {
	start := time.Now()
	out, err := canon.Canonicalize(canon.JCS, input, canon.DefaultLimits())
	parseNs := time.Since(start).Nanoseconds()
	if err != nil {
		var ref canon.Refusal
		code := "jcs_refused"
		if errors.As(err, &ref) {
			code = string(ref.Code)
		}
		return caseResponse{
			Version:        protocolVersion,
			CaseID:         req.CaseID,
			Subject:        "yamlprims-go",
			SubjectVersion: subjectVersion(),
			Status:         "error",
			ErrorPhase:     "resolution",
			ErrorCode:      code,
			ParseNs:        parseNs,
		}
	}
	canonical := string(out)
	sum := sha256.Sum256(out)
	return caseResponse{
		Version:        protocolVersion,
		CaseID:         req.CaseID,
		Subject:        "yamlprims-go",
		SubjectVersion: subjectVersion(),
		Status:         "ok",
		CanonicalValue: canonical,
		OutputSHA256:   hex.EncodeToString(sum[:]),
		OutputBytes:    int64(len(out)),
		DocumentCount:  1,
		ParseNs:        parseNs,
	}
}

func executeIJSON(req caseRequest, input []byte) caseResponse {
	opts, _ := optionsForProfile(req.Profile)
	start := time.Now()
	ijsonStr, yErr := yaml.LoadIJSONBytes(input, opts)
	parseNs := time.Since(start).Nanoseconds()
	if yErr != nil {
		return yamlErrorToResponse(req, yErr, parseNs)
	}
	canonicalBytes := []byte(ijsonStr)
	sum := sha256.Sum256(canonicalBytes)
	return caseResponse{
		Version:        protocolVersion,
		CaseID:         req.CaseID,
		Subject:        "yamlprims-go",
		SubjectVersion: subjectVersion(),
		Status:         "ok",
		CanonicalValue: ijsonStr,
		OutputSHA256:   hex.EncodeToString(sum[:]),
		OutputBytes:    int64(len(canonicalBytes)),
		DocumentCount:  1,
		ParseNs:        parseNs,
	}
}

func executeLoadValue(req caseRequest, input []byte) caseResponse {
	opts, single := optionsForProfile(req.Profile)
	start := time.Now()
	var docs []yaml.Document
	var yErr *yaml.Error
	if single {
		d, err := yaml.LoadBytes(input, opts)
		if err != nil {
			yErr = err
		} else {
			docs = []yaml.Document{d}
		}
	} else {
		docs, yErr = yaml.LoadAllBytes(input, opts)
	}
	parseNs := time.Since(start).Nanoseconds()
	if yErr != nil {
		return yamlErrorToResponse(req, yErr, parseNs)
	}

	emitStart := time.Now()
	outDocs := make([]any, len(docs))
	for i := range docs {
		outDocs[i] = notation(docs[i].Root())
	}
	canonicalBytes, err := marshalCompact(outDocs)
	emitNs := time.Since(emitStart).Nanoseconds()
	if err != nil {
		os.Exit(2)
	}
	sum := sha256.Sum256(canonicalBytes)
	return caseResponse{
		Version:        protocolVersion,
		CaseID:         req.CaseID,
		Subject:        "yamlprims-go",
		SubjectVersion: subjectVersion(),
		Status:         "ok",
		CanonicalValue: string(canonicalBytes),
		OutputSHA256:   hex.EncodeToString(sum[:]),
		OutputBytes:    int64(len(canonicalBytes)),
		DocumentCount:  len(docs),
		ParseNs:        parseNs,
		EmitNs:         emitNs,
	}
}

func optionsForProfile(profile string) (yaml.Options, bool) {
	opts := yaml.DefaultOptions()
	single := true

	switch profile {
	case "yaml-core":
		opts.MergeKeys = yaml.MergeKeysAsKey
		single = false
	case "yamlprims-core-stream":
		opts.MergeKeys = yaml.MergeKeysReject
		single = false
	case "yamlprims-core-merge-apply":
		opts.MergeKeys = yaml.MergeKeysApply
		single = true
	case "yamlprims-core-merge-as-key":
		opts.MergeKeys = yaml.MergeKeysAsKey
		single = true
	case "yamlprims-core-limits":
		opts.Limits = yaml.Limits{
			MaxInputBytes:     65536,
			MaxDepth:          8,
			MaxNodes:          64,
			MaxExpandedNodes:  32,
			MaxAliasEvents:    8,
			MaxOwnedBytes:     8192,
			MaxScalarBytes:    1024,
			MaxKeyBytes:       512,
			MaxMappingEntries: 16,
			MaxDocuments:      4,
			MaxOutputBytes:    65536,
			MaxWarnings:       16,
		}
	case "yamlprims-core-depth128":
		opts.Limits = yaml.Limits{
			MaxInputBytes:     65536,
			MaxDepth:          128,
			MaxNodes:          512,
			MaxExpandedNodes:  16,
			MaxAliasEvents:    16,
			MaxOwnedBytes:     65536,
			MaxScalarBytes:    1024,
			MaxKeyBytes:       1024,
			MaxMappingEntries: 16,
			MaxDocuments:      4,
			MaxOutputBytes:    65536,
			MaxWarnings:       16,
		}
	case "yamlprims-core-depth255":
		opts.Limits = yaml.Limits{
			MaxInputBytes:     65536,
			MaxDepth:          255,
			MaxNodes:          512,
			MaxExpandedNodes:  16,
			MaxAliasEvents:    16,
			MaxOwnedBytes:     65536,
			MaxScalarBytes:    1024,
			MaxKeyBytes:       1024,
			MaxMappingEntries: 16,
			MaxDocuments:      4,
			MaxOutputBytes:    65536,
			MaxWarnings:       16,
		}
	case "yamlprims-core-entries2":
		opts.Limits = yaml.Limits{
			MaxInputBytes:     4096,
			MaxDepth:          128,
			MaxNodes:          64,
			MaxExpandedNodes:  8,
			MaxAliasEvents:    8,
			MaxOwnedBytes:     8192,
			MaxScalarBytes:    1024,
			MaxKeyBytes:       1024,
			MaxMappingEntries: 2,
			MaxDocuments:      4,
			MaxOutputBytes:    4096,
			MaxWarnings:       16,
		}
	}
	return opts, single
}

func yamlErrorToResponse(req caseRequest, e *yaml.Error, parseNs int64) caseResponse {
	phase := classifyYAMLErrorPhase(e)
	status := "error"
	if phase == "limit" || req.Budgets != nil {
		status = "refused_bounded"
	}
	code := e.Kind.Name()
	return caseResponse{
		Version:        protocolVersion,
		CaseID:         req.CaseID,
		Subject:        "yamlprims-go",
		SubjectVersion: subjectVersion(),
		Status:         status,
		ErrorPhase:     phase,
		ErrorCode:      code,
		ParseNs:        parseNs,
	}
}

func classifyYAMLErrorPhase(e *yaml.Error) string {
	switch {
	case e.Kind == yaml.ErrorKindInvalidUtf8 ||
		(e.Kind == yaml.ErrorKindLimitExceeded && e.Detail.Limit == yaml.LimitMaxInputBytes):
		return "input"
	case e.Kind == yaml.ErrorKindSyntax:
		return "syntax"
	case e.Kind == yaml.ErrorKindLimitExceeded:
		return "limit"
	case e.Kind == yaml.ErrorKindDuplicateKey ||
		e.Kind == yaml.ErrorKindInvalidKey ||
		e.Kind == yaml.ErrorKindUnknownAnchor ||
		e.Kind == yaml.ErrorKindRecursiveAlias ||
		e.Kind == yaml.ErrorKindMergeRejected ||
		e.Kind == yaml.ErrorKindMergeSource ||
		e.Kind == yaml.ErrorKindMultipleDocuments:
		return "structure"
	default:
		return "resolution"
	}
}

func notation(n *yaml.Node) map[string]any {
	v := n.Value()
	switch v.Kind() {
	case yaml.ValueKindNull:
		return map[string]any{"t": "null"}
	case yaml.ValueKindBool:
		b, _ := v.AsBool()
		return map[string]any{"t": "bool", "v": b}
	case yaml.ValueKindInt:
		i, _ := v.AsInt()
		return map[string]any{"t": "int", "v": i.String()}
	case yaml.ValueKindFloat:
		f, _ := v.AsFloat()
		bits := math.Float64bits(f)
		if math.IsNaN(f) {
			bits = 0x7ff8000000000000
		}
		return map[string]any{"t": "float", "bits": fmt.Sprintf("%016x", bits)}
	case yaml.ValueKindString:
		s, _ := v.AsStr()
		return map[string]any{"t": "str", "v": s}
	case yaml.ValueKindSequence:
		seq, _ := v.AsSequence()
		items := make([]any, len(seq))
		for i := range seq {
			items[i] = notation(&seq[i])
		}
		return map[string]any{"t": "seq", "items": items}
	case yaml.ValueKindMapping:
		m, _ := v.AsMapping()
		entries := make([]any, m.Len())
		for i := 0; i < m.Len(); i++ {
			k, val := m.At(i)
			entries[i] = []any{notation(&k), notation(&val)}
		}
		return map[string]any{"t": "map", "entries": entries}
	case yaml.ValueKindTagged:
		tag, _ := v.AsTagged()
		return map[string]any{"t": "tagged", "tag": tag.Tag().URI(), "inner": notation(tag.Value())}
	default:
		return map[string]any{"t": "unknown", "v": strconv.Itoa(int(v.Kind()))}
	}
}

func marshalCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
