package driver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	v0 "github.com/3leaps/docpresser/schemas/docpresser/v0"
)

// requireDriverToolchains turns a missing toolchain or a failed driver build
// into a test failure instead of a skip. CI sets it where every toolchain is
// installed.
const requireDriverToolchains = "DOCPRESSER_REQUIRE_DRIVER_TOOLCHAINS"

type subjectDriver struct {
	name    string
	command func(t *testing.T, root string) []string
}

func toolOrSkip(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		setupUnavailable(t, "%s not found: %v", name, err)
	}
	return path
}

func setupUnavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv(requireDriverToolchains) == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func buildGoDriver(dir string) func(t *testing.T, root string) []string {
	return func(t *testing.T, root string) []string {
		t.Helper()
		bin := filepath.Join(t.TempDir(), dir)
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = filepath.Join(root, "drivers", dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			setupUnavailable(t, "build %s: %v\n%s", dir, err, out)
		}
		return []string{bin}
	}
}

func subjectDrivers() []subjectDriver {
	return []subjectDriver{
		{name: "go-yamlv3", command: buildGoDriver("go-yamlv3")},
		{name: "yamlprims-go", command: buildGoDriver("yamlprims-go")},
		{name: "yamlprims-rust", command: func(t *testing.T, root string) []string {
			cargo := toolOrSkip(t, "cargo")
			dir := filepath.Join(root, "drivers", "yamlprims-rust")
			cmd := exec.Command(cargo, "build", "--release", "--locked")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				setupUnavailable(t, "build yamlprims-rust: %v\n%s", err, out)
			}
			bin := filepath.Join(dir, "target", "release", "yamlprims-rust")
			if runtime.GOOS == "windows" {
				bin += ".exe"
			}
			return []string{bin}
		}},
		{name: "python-yaml", command: func(t *testing.T, root string) []string {
			return []string{toolOrSkip(t, "python3"), filepath.Join(root, "drivers", "python-yaml", "driver.py"), "--engine", "builtin-yaml11"}
		}},
		{name: "node-yaml", command: func(t *testing.T, root string) []string {
			return []string{toolOrSkip(t, "node"), filepath.Join(root, "drivers", "node-yaml", "driver.mjs")}
		}},
	}
}

func observationErrorPhases(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := v0.Schema("observation.schema.json")
	if err != nil {
		t.Fatalf("load observation schema: %v", err)
	}
	var schema struct {
		Properties struct {
			ErrorPhase struct {
				Enum []string `json:"enum"`
			} `json:"error_phase"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse observation schema: %v", err)
	}
	if len(schema.Properties.ErrorPhase.Enum) == 0 {
		t.Fatal("observation schema has no error_phase enum")
	}
	allowed := map[string]bool{}
	for _, p := range schema.Properties.ErrorPhase.Enum {
		allowed[p] = true
	}
	return allowed
}

// TestSubjectDriversAnswerUnsupportedOperations sends every in-repository
// subject driver an operation it does not implement. Each must answer
// "unsupported", and any error_phase it reports must be one the observation
// schema allows; otherwise docpresser run rejects the observation and stops.
func TestSubjectDriversAnswerUnsupportedOperations(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	allowed := observationErrorPhases(t)
	input := []byte("a: 1\n")
	req, err := json.Marshal(Request{
		Version:     ProtocolVersion,
		CaseID:      "unsupported.extract_text",
		Format:      "yaml",
		Operation:   "extract_text",
		InputBase64: base64.StdEncoding.EncodeToString(input),
	})
	if err != nil {
		t.Fatal(err)
	}
	req = append(req, '\n')

	for _, d := range subjectDrivers() {
		t.Run(d.name, func(t *testing.T) {
			argv := d.command(t, root)
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Stdin = bytes.NewReader(req)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			done := make(chan error, 1)
			if err := cmd.Start(); err != nil {
				t.Fatalf("start %s: %v", d.name, err)
			}
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("%s exited with %v; stderr: %s", d.name, err, stderr.String())
				}
			case <-time.After(60 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("%s did not answer within 60s", d.name)
			}
			var resp map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
				t.Fatalf("%s reply is not one JSON object: %v; stdout: %q", d.name, err, stdout.String())
			}
			if resp["status"] != "unsupported" {
				t.Fatalf("%s status = %v, want unsupported (reply %v)", d.name, resp["status"], resp)
			}
			if resp["error_code"] != "unsupported_operation" {
				t.Fatalf("%s error_code = %v, want unsupported_operation", d.name, resp["error_code"])
			}
			if phase, ok := resp["error_phase"]; ok {
				s, isString := phase.(string)
				if !isString || !allowed[s] {
					t.Fatalf("%s error_phase = %v, not allowed by observation.schema.json", d.name, phase)
				}
			}
		})
	}
}
