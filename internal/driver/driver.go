// Package driver implements the out-of-process JSON/NDJSON subject driver client
// backed by the containment supervisor.
package driver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/3leaps/docpresser/internal/containment"
	"github.com/3leaps/docpresser/internal/model"
)

// ProtocolVersion identifies the Wave 1 stdin/stdout driver wire contract.
const ProtocolVersion = "docpresser-driver/v0"

// Request is sent as a single JSON line on stdin to a subject driver.
type Request struct {
	Version     string             `json:"version"`
	CaseID      string             `json:"case_id"`
	Format      string             `json:"format"`
	Operation   string             `json:"operation"`
	Profile     string             `json:"profile,omitempty"`
	InputBase64 string             `json:"input_base64"`
	Budgets     *model.CaseBudgets `json:"budgets,omitempty"`
}

// Response is emitted as a single JSON line on stdout by a subject driver.
type Response struct {
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

// Client invokes an out-of-process subject driver under containment supervision.
type Client struct {
	Name         string
	Role         string
	Version      string
	Command      string
	Args         []string
	DriverSHA256 string
}

// NewClient constructs a subject driver client from a specification string "name=command [args...]"
// and role ("target" or "baseline").
func NewClient(specStr, role string) (*Client, error) {
	if role != "target" && role != "baseline" {
		return nil, fmt.Errorf("invalid subject role %q (want target or baseline)", role)
	}
	parts := strings.SplitN(specStr, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, fmt.Errorf("subject specification %q must have format <name>=<command>", specStr)
	}
	name := strings.TrimSpace(parts[0])
	fields := strings.Fields(parts[1])
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty command in subject specification %q", specStr)
	}
	cmdPath := resolveCommandPath(fields[0])
	args := make([]string, len(fields)-1)
	for i, arg := range fields[1:] {
		args[i] = resolveArgPath(arg)
	}

	driverDigest := computeDriverDigest(cmdPath, args)
	return &Client{
		Name:         name,
		Role:         role,
		Version:      "v0.1.0",
		Command:      cmdPath,
		Args:         args,
		DriverSHA256: driverDigest,
	}, nil
}

// Metadata returns the SubjectMetadata for run-manifest.json.
func (c *Client) Metadata() model.SubjectMetadata {
	ver := c.Version
	if ver == "" {
		ver = "v0.1.0"
	}
	return model.SubjectMetadata{
		Name:         c.Name,
		Role:         c.Role,
		Version:      ver,
		DriverSHA256: c.DriverSHA256,
	}
}

// ExecuteCase runs a single PackCase against the subject driver inside the containment supervisor.
func (c *Client) ExecuteCase(ctx context.Context, pc model.PackCase, inputBytes []byte) (model.Observation, error) {
	req := Request{
		Version:     ProtocolVersion,
		CaseID:      pc.CaseID,
		Format:      pc.Format,
		Operation:   pc.Operation,
		Profile:     pc.Profile,
		InputBase64: base64.StdEncoding.EncodeToString(inputBytes),
		Budgets:     pc.Budgets,
	}
	reqLine, err := json.Marshal(req)
	if err != nil {
		return model.Observation{}, fmt.Errorf("marshal driver request: %w", err)
	}
	reqLine = append(reqLine, '\n')

	timeout := 5 * time.Second
	var maxRSS int64 = 256 * 1024 * 1024
	var maxOut int64 = 8 * 1024 * 1024
	if pc.Budgets != nil {
		if pc.Budgets.MaxWallMs > 0 {
			timeout = time.Duration(pc.Budgets.MaxWallMs) * time.Millisecond
		}
		if pc.Budgets.MaxRSSBytes > 0 {
			maxRSS = pc.Budgets.MaxRSSBytes
		}
		if pc.Budgets.MaxOutputBytes > 0 {
			maxOut = pc.Budgets.MaxOutputBytes
		}
	}

	res, err := containment.Run(ctx, containment.Spec{
		Command:        c.Command,
		Args:           c.Args,
		Stdin:          reqLine,
		Timeout:        timeout,
		MaxRSSBytes:    maxRSS,
		MaxOutputBytes: maxOut,
	})
	if err != nil {
		return model.Observation{}, err
	}

	obs := model.Observation{
		Version:      model.Version,
		CaseID:       pc.CaseID,
		Subject:      c.Name,
		Role:         c.Role,
		Operation:    pc.Operation,
		InputSHA256:  pc.InputSHA256,
		InputBytes:   pc.InputBytes,
		DurationNs:   res.DurationNs,
		PeakRSSBytes: res.PeakRSSBytes,
		ExitCode:     res.ExitCode,
		Signal:       res.Signal,
	}

	switch {
	case res.TimedOut:
		obs.Status = "timeout_exceeded"
		return obs, nil
	case res.OOMKilled:
		obs.Status = "oom_killed"
		return obs, nil
	case res.Crashed:
		obs.Status = "crash_signal"
		if obs.Signal == "" {
			obs.Signal = "exit_nonzero"
		}
		return obs, nil
	case res.OutputExceeded:
		obs.Status = "protocol_error"
		obs.ErrorCode = "output_exceeded"
		return obs, nil
	}

	trimmed := bytes.TrimSpace(res.Stdout)
	if len(trimmed) == 0 {
		obs.Status = "protocol_error"
		obs.ErrorCode = "empty_stdout"
		return obs, nil
	}

	var resp Response
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resp); err != nil {
		obs.Status = "protocol_error"
		obs.ErrorCode = "malformed_json"
		return obs, nil
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		obs.Status = "protocol_error"
		obs.ErrorCode = "trailing_stdout"
		return obs, nil
	}

	if resp.Version != ProtocolVersion || resp.CaseID != pc.CaseID || !validResponseStatus(resp.Status) {
		obs.Status = "protocol_error"
		obs.ErrorCode = "invalid_envelope"
		return obs, nil
	}
	if resp.SubjectVersion != "" {
		c.Version = resp.SubjectVersion
	}

	obs.Status = resp.Status
	obs.CanonicalValue = resp.CanonicalValue
	obs.OutputSHA256 = resp.OutputSHA256
	if obs.OutputSHA256 == "" && resp.CanonicalValue != "" {
		sum := sha256.Sum256([]byte(resp.CanonicalValue))
		obs.OutputSHA256 = hex.EncodeToString(sum[:])
	}
	obs.OutputBytes = resp.OutputBytes
	if obs.OutputBytes == 0 && resp.CanonicalValue != "" {
		obs.OutputBytes = int64(len(resp.CanonicalValue))
	}
	obs.DocumentCount = resp.DocumentCount
	obs.EventCount = resp.EventCount
	obs.ErrorPhase = resp.ErrorPhase
	obs.ErrorCode = resp.ErrorCode
	obs.ParseNs = resp.ParseNs
	obs.EmitNs = resp.EmitNs
	obs.HazardsFound = resp.HazardsFound
	return obs, nil
}

func validResponseStatus(s string) bool {
	switch s {
	case "ok", "error", "refused_bounded", "unsupported":
		return true
	default:
		return false
	}
}

func resolveCommandPath(cmdPath string) string {
	if strings.ContainsRune(cmdPath, filepath.Separator) || strings.HasPrefix(cmdPath, ".") {
		if abs, err := filepath.Abs(cmdPath); err == nil {
			return abs
		}
	}
	return cmdPath
}

func resolveArgPath(arg string) string {
	if strings.HasPrefix(arg, "-") || filepath.IsAbs(arg) {
		return arg
	}
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		if abs, aErr := filepath.Abs(arg); aErr == nil {
			return abs
		}
	}
	return arg
}

func computeDriverDigest(cmdPath string, args []string) string {
	h := sha256.New()
	resolved := cmdPath
	if p, err := exec.LookPath(cmdPath); err == nil {
		resolved = p
	}
	if data, err := os.ReadFile(resolved); err == nil {
		_, _ = h.Write(data)
	} else {
		_, _ = h.Write([]byte(resolved))
	}
	for _, a := range args {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(a))
		if data, err := os.ReadFile(a); err == nil {
			_, _ = h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
