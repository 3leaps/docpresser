// Command go-yamlv3 is the standalone baseline subject driver for gopkg.in/yaml.v3
// implementing the docpresser-driver/v0 protocol.
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
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
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

func subjectVersion() string {
	return moduleVersion("gopkg.in/yaml.v3") + " (" + runtime.Version() + ")"
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
			Subject:        "go-yamlv3",
			SubjectVersion: subjectVersion(),
			Language:       "go",
			Engine:         "gopkg.in/yaml.v3",
			Formats:        []string{"yaml", "json"},
			Operations:     []string{"load_value"},
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

	if req.Operation != "" && req.Operation != "load_value" {
		writeResp(caseResponse{
			Version:        protocolVersion,
			CaseID:         req.CaseID,
			Subject:        "go-yamlv3",
			SubjectVersion: subjectVersion(),
			Status:         "unsupported",
			ErrorPhase:     "driver",
			ErrorCode:      "unsupported_operation",
		})
		return
	}

	input, err := base64.StdEncoding.DecodeString(req.InputBase64)
	if err != nil {
		os.Exit(2)
	}

	writeResp(executeLoadValue(req, input))
}

func executeLoadValue(req caseRequest, input []byte) caseResponse {
	start := time.Now()
	if !utf8.Valid(input) {
		return caseResponse{
			Version:        protocolVersion,
			CaseID:         req.CaseID,
			Subject:        "go-yamlv3",
			SubjectVersion: subjectVersion(),
			Status:         "error",
			ErrorPhase:     "input",
			ErrorCode:      "invalid_utf8",
			ParseNs:        time.Since(start).Nanoseconds(),
		}
	}

	dec := yaml.NewDecoder(bytes.NewReader(input))
	var outDocs []any
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return classifyErr(req, err, time.Since(start).Nanoseconds())
		}
		var materialized any
		if mErr := doc.Decode(&materialized); mErr != nil {
			return classifyErr(req, mErr, time.Since(start).Nanoseconds())
		}
		root := &doc
		if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
			root = doc.Content[0]
		}
		conv, cErr := nodeToNotation(root, 0)
		if cErr != nil {
			return classifyErr(req, cErr, time.Since(start).Nanoseconds())
		}
		outDocs = append(outDocs, conv)
	}
	parseNs := time.Since(start).Nanoseconds()

	if len(outDocs) == 0 {
		outDocs = []any{map[string]any{"t": "null"}}
	}

	emitStart := time.Now()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(outDocs); err != nil {
		return classifyErr(req, err, parseNs)
	}
	emitNs := time.Since(emitStart).Nanoseconds()
	canonicalBytes := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	sum := sha256.Sum256(canonicalBytes)
	return caseResponse{
		Version:        protocolVersion,
		CaseID:         req.CaseID,
		Subject:        "go-yamlv3",
		SubjectVersion: subjectVersion(),
		Status:         "ok",
		CanonicalValue: string(canonicalBytes),
		OutputSHA256:   hex.EncodeToString(sum[:]),
		OutputBytes:    int64(len(canonicalBytes)),
		DocumentCount:  len(outDocs),
		ParseNs:        parseNs,
		EmitNs:         emitNs,
	}
}

func nodeToNotation(n *yaml.Node, depth int) (map[string]any, error) {
	if depth > 512 {
		return nil, fmt.Errorf("limit: recursion depth exceeded")
	}
	if n == nil {
		return map[string]any{"t": "null"}, nil
	}
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return nil, fmt.Errorf("unknown anchor %q", n.Value)
		}
		return nodeToNotation(n.Alias, depth+1)
	}

	switch n.Kind {
	case yaml.ScalarNode:
		return scalarToNotation(n)
	case yaml.SequenceNode:
		items := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			c, err := nodeToNotation(child, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, c)
		}
		return map[string]any{"t": "seq", "items": items}, nil
	case yaml.MappingNode:
		entries := make([]any, 0, len(n.Content)/2)
		seenKeys := make(map[string]bool)
		var mergedPairs [][2]*yaml.Node

		for i := 0; i+1 < len(n.Content); i += 2 {
			kNode := n.Content[i]
			vNode := n.Content[i+1]
			if kNode.Tag == "!!merge" || (kNode.Kind == yaml.ScalarNode && kNode.Value == "<<" && kNode.Style == 0) {
				pairs, err := extractMergePairs(vNode, depth+1)
				if err != nil {
					return nil, err
				}
				mergedPairs = append(mergedPairs, pairs...)
				continue
			}
			kNot, err := nodeToNotation(kNode, depth+1)
			if err != nil {
				return nil, err
			}
			vNot, err := nodeToNotation(vNode, depth+1)
			if err != nil {
				return nil, err
			}
			kJSON, _ := json.Marshal(kNot)
			seenKeys[string(kJSON)] = true
			entries = append(entries, []any{kNot, vNot})
		}

		if len(mergedPairs) > 0 {
			finalEntries := make([]any, 0, len(mergedPairs)+len(entries))
			for _, mp := range mergedPairs {
				kNot, err := nodeToNotation(mp[0], depth+1)
				if err != nil {
					return nil, err
				}
				kJSON, _ := json.Marshal(kNot)
				if seenKeys[string(kJSON)] {
					continue
				}
				seenKeys[string(kJSON)] = true
				vNot, err := nodeToNotation(mp[1], depth+1)
				if err != nil {
					return nil, err
				}
				finalEntries = append(finalEntries, []any{kNot, vNot})
			}
			finalEntries = append(finalEntries, entries...)
			entries = finalEntries
		}

		return map[string]any{"t": "map", "entries": entries}, nil
	default:
		return map[string]any{"t": "null"}, nil
	}
}

func extractMergePairs(vNode *yaml.Node, depth int) ([][2]*yaml.Node, error) {
	if depth > 512 {
		return nil, fmt.Errorf("limit: recursion depth exceeded")
	}
	target := vNode
	if target.Kind == yaml.AliasNode && target.Alias != nil {
		target = target.Alias
	}
	switch target.Kind {
	case yaml.MappingNode:
		var out [][2]*yaml.Node
		for i := 0; i+1 < len(target.Content); i += 2 {
			out = append(out, [2]*yaml.Node{target.Content[i], target.Content[i+1]})
		}
		return out, nil
	case yaml.SequenceNode:
		var out [][2]*yaml.Node
		for _, item := range target.Content {
			sub, err := extractMergePairs(item, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("invalid merge source kind")
	}
}

func scalarToNotation(n *yaml.Node) (map[string]any, error) {
	switch n.Tag {
	case "!!null":
		return map[string]any{"t": "null"}, nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return nil, err
		}
		return map[string]any{"t": "bool", "v": b}, nil
	case "!!int":
		var i int64
		if err := n.Decode(&i); err == nil {
			return map[string]any{"t": "int", "v": strconv.FormatInt(i, 10)}, nil
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			return map[string]any{"t": "int", "v": strconv.FormatUint(u, 10)}, nil
		}
		return map[string]any{"t": "int", "v": n.Value}, nil
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return nil, err
		}
		bits := math.Float64bits(f)
		if math.IsNaN(f) {
			bits = 0x7ff8000000000000
		}
		return map[string]any{"t": "float", "bits": fmt.Sprintf("%016x", bits)}, nil
	case "!!str", "":
		return map[string]any{"t": "str", "v": n.Value}, nil
	default:
		return map[string]any{"t": "str", "v": n.Value}, nil
	}
}

func classifyErr(req caseRequest, err error, parseNs int64) caseResponse {
	msg := err.Error()
	phase := "syntax"
	status := "error"
	if strings.Contains(msg, "excessive aliasing") || strings.Contains(msg, "limit:") || strings.Contains(msg, "exceeded max depth") {
		phase = "limit"
		status = "refused_bounded"
	} else if strings.Contains(msg, "already defined") || strings.Contains(msg, "unknown anchor") || strings.Contains(msg, "merge") {
		phase = "structure"
	}
	if req.Budgets != nil && status == "error" {
		status = "refused_bounded"
	}
	return caseResponse{
		Version:        protocolVersion,
		CaseID:         req.CaseID,
		Subject:        "go-yamlv3",
		SubjectVersion: subjectVersion(),
		Status:         status,
		ErrorPhase:     phase,
		ErrorCode:      "yaml_v3_error",
		ParseNs:        parseNs,
	}
}

func writeResp(resp caseResponse) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
}
