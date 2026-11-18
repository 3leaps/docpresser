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

// subjectVersion is the version this driver reports in its handshake and in
// every case response.
const subjectVersion = "faultdriver-1.0.0"

// handshake answers --docpresser-handshake. A --handshake=<mode> argument
// selects a faulty reply so the coordinator's handshake checks can be tested.
func handshake(mode string) {
	reply := map[string]any{
		"version":         "docpresser-driver/v0",
		"subject":         "faultdriver",
		"subject_version": subjectVersion,
		"language":        "go",
		"engine":          "faultdriver",
		"formats":         []string{"yaml"},
		"operations":      []string{"load_value"},
	}
	switch mode {
	case "silent":
		return
	case "bad_json":
		_, _ = fmt.Fprintln(os.Stdout, `{"version":`)
		return
	case "wrong_protocol":
		reply["version"] = "docpresser-driver/v9"
	case "empty_version":
		reply["subject_version"] = " "
	case "no_formats":
		reply["formats"] = []string{}
	case "extra_field":
		reply["unexpected"] = true
	case "trailing":
		enc, _ := json.Marshal(reply)
		_, _ = fmt.Fprintf(os.Stdout, "%s\n%s\n", enc, enc)
		return
	case "hang":
		time.Sleep(30 * time.Second)
		return
	case "crash":
		_, _ = fmt.Fprintln(os.Stderr, "faultdriver: engine unavailable")
		os.Exit(3)
	}
	enc, _ := json.Marshal(reply)
	_, _ = fmt.Fprintf(os.Stdout, "%s\n", enc)
}

func main() {
	mode := ""
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--handshake=") {
			mode = strings.TrimPrefix(arg, "--handshake=")
		}
	}
	for _, arg := range os.Args[1:] {
		if arg == "--docpresser-handshake" {
			handshake(mode)
			return
		}
	}

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		os.Exit(2)
	}
	decoded, _ := base64.StdEncoding.DecodeString(req.InputBase64)
	mode = strings.TrimSpace(string(decoded))
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
			"subject_version": subjectVersion,
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
			"subject_version": subjectVersion,
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

	case "version_mismatch":
		out := map[string]any{
			"version":         "docpresser-driver/v0",
			"case_id":         req.CaseID,
			"subject":         "faultdriver",
			"subject_version": "faultdriver-9.9.9",
			"status":          "ok",
		}
		enc, _ := json.Marshal(out)
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", string(enc))

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
