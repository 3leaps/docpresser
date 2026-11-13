// Package main provides a deterministic adversarial fault driver for testing
// the docpresser containment supervisor and driver client.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type request struct {
	Version     string `json:"version"`
	CaseID      string `json:"case_id"`
	Format      string `json:"format"`
	Operation   string `json:"operation"`
	InputBase64 string `json:"input_base64"`
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		os.Exit(2)
	}
	decoded, _ := base64.StdEncoding.DecodeString(req.InputBase64)
	mode := strings.TrimSpace(string(decoded))
	if idx := strings.LastIndex(req.CaseID, "."); idx >= 0 && mode == "" {
		mode = req.CaseID[idx+1:]
	}

	switch mode {
	case "clean_pass":
		canonical := `[{"documents":[{"entries":[[{"t":"str","v":"mode"},{"t":"str","v":"clean_pass"}]],"t":"map"}]}]`
		sum := sha256.Sum256([]byte(canonical))
		out := map[string]any{
			"version":         "docpresser-driver/v0",
			"case_id":         req.CaseID,
			"subject":         "faultdriver",
			"subject_version": "v0.1.0",
			"status":          "ok",
			"canonical_value": canonical,
			"output_sha256":   hex.EncodeToString(sum[:]),
			"output_bytes":    len(canonical),
			"document_count":  1,
		}
		enc, _ := json.Marshal(out)
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", string(enc))

	case "refused_bounded":
		out := map[string]any{
			"version":         "docpresser-driver/v0",
			"case_id":         req.CaseID,
			"subject":         "faultdriver",
			"subject_version": "v0.1.0",
			"status":          "refused_bounded",
			"error_phase":     "limit",
			"error_code":      "max_alias_events",
		}
		enc, _ := json.Marshal(out)
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", string(enc))

	case "crash_sigabrt":
		_, _ = fmt.Fprintln(os.Stderr, "SIGABRT: aborting faultdriver")
		os.Exit(134)

	case "memory_spike":
		buf := make([]byte, 32*1024*1024)
		for i := 0; i < len(buf); i += 4096 {
			buf[i] = byte(i)
		}
		_, _ = fmt.Fprintf(os.Stderr, "fatal error: runtime: out of memory (allocated %d bytes)\n", len(buf))
		os.Exit(2)

	case "infinite_hang":
		time.Sleep(30 * time.Second)

	case "truncated_json":
		_, _ = fmt.Fprint(os.Stdout, `{"version":"docpresser-driver/v0","case_id":"`+req.CaseID)

	default:
		out := map[string]any{
			"version": "docpresser-driver/v0",
			"case_id": req.CaseID,
			"status":  "unsupported",
		}
		enc, _ := json.Marshal(out)
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", string(enc))
	}
}
