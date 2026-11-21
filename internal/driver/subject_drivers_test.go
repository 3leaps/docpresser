package driver

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/3leaps/docpresser/internal/model"
	v0 "github.com/3leaps/docpresser/schemas/docpresser/v0"
)

// requireDriverToolchains turns a missing toolchain, engine or failed driver
// build into a test failure instead of a skip. CI sets it where every
// toolchain is installed.
const requireDriverToolchains = "DOCPRESSER_REQUIRE_DRIVER_TOOLCHAINS"

type subjectDriver struct {
	name string
	// spec returns the NewClient specification (<name>=<command> [args]).
	spec func(t *testing.T, root string) string
}

func setupUnavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv(requireDriverToolchains) == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func toolOrSkip(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		setupUnavailable(t, "%s not found: %v", name, err)
	}
	return path
}

func buildGoDriver(dir string) func(t *testing.T, root string) string {
	return func(t *testing.T, root string) string {
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
		return dir + "=" + bin
	}
}

// buildRustDriver builds the yamlprims-rust driver and returns the
// executable Cargo reports for it, wherever the target directory or target
// triple places it.
func buildRustDriver(t *testing.T, root string) string {
	t.Helper()
	cargo := toolOrSkip(t, "cargo")
	cmd := exec.Command(cargo, "build", "--release", "--locked", "--message-format=json-render-diagnostics")
	cmd.Dir = filepath.Join(root, "drivers", "yamlprims-rust")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		setupUnavailable(t, "build yamlprims-rust: %v\n%s", err, stderr.String())
	}
	var bin string
	scanner := bufio.NewScanner(&stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var msg struct {
			Reason     string  `json:"reason"`
			Executable *string `json:"executable"`
			Target     struct {
				Name string `json:"name"`
			} `json:"target"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.Reason == "compiler-artifact" && msg.Target.Name == "yamlprims-rust" && msg.Executable != nil {
			bin = *msg.Executable
		}
	}
	if bin == "" {
		t.Fatalf("cargo reported no yamlprims-rust executable")
	}
	return "yamlprims-rust=" + bin
}

func subjectDrivers() []subjectDriver {
	return []subjectDriver{
		{name: "go-yamlv3", spec: buildGoDriver("go-yamlv3")},
		{name: "yamlprims-go", spec: buildGoDriver("yamlprims-go")},
		{name: "yamlprims-rust", spec: buildRustDriver},
		{name: "python-yaml", spec: func(t *testing.T, root string) string {
			return "python-yaml=" + toolOrSkip(t, "python3") + " " +
				filepath.Join(root, "drivers", "python-yaml", "driver.py") + " --engine builtin-yaml11"
		}},
		{name: "node-yaml", spec: func(t *testing.T, root string) string {
			return "node-yaml=" + toolOrSkip(t, "node") + " " + filepath.Join(root, "drivers", "node-yaml", "driver.mjs")
		}},
	}
}

func unsupportedCase(mode string) (model.PackCase, []byte) {
	raw := []byte(mode + "\n")
	sum := sha256.Sum256(raw)
	return model.PackCase{
		CaseID:      "unsupported." + mode,
		Format:      "yaml",
		Axis:        "correctness",
		Profile:     "yaml-core",
		Operation:   "extract_text",
		InputPath:   "inputs/yaml/unsupported.yaml",
		InputSHA256: hex.EncodeToString(sum[:]),
		InputBytes:  int64(len(raw)),
		Expected:    model.CaseExpectation{Outcome: "unsupported"},
	}, raw
}

// checkUnsupported runs one unsupported operation through the same client
// path as docpresser run and validates the resulting observation against the
// observation schema, as the recorder does.
func checkUnsupported(t *testing.T, spec string, input string) error {
	t.Helper()
	client, err := NewClient(spec, "target")
	if err != nil {
		return fmt.Errorf("NewClient: %w", err)
	}
	if err := client.Handshake(context.Background()); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	pc, raw := unsupportedCase(input)
	obs, err := client.ExecuteCase(context.Background(), pc, raw)
	if err != nil {
		return fmt.Errorf("ExecuteCase: %w", err)
	}
	if obs.Status != "unsupported" || obs.ErrorCode != "unsupported_operation" {
		return fmt.Errorf("status %q code %q, want unsupported/unsupported_operation", obs.Status, obs.ErrorCode)
	}
	data, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	if err := v0.ValidateObservationBytes(data); err != nil {
		return fmt.Errorf("observation rejected: %w", err)
	}
	return nil
}

// TestSubjectDriversAnswerUnsupportedOperations sends every in-repository
// subject driver an operation it does not implement. Each must answer
// "unsupported" with an observation the schema accepts; otherwise docpresser
// run rejects the observation and stops.
func TestSubjectDriversAnswerUnsupportedOperations(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range subjectDrivers() {
		t.Run(d.name, func(t *testing.T) {
			spec := d.spec(t, root)
			if err := checkUnsupported(t, spec, "a: 1"); err != nil {
				if strings.HasPrefix(err.Error(), "handshake:") && strings.Contains(err.Error(), "js-yaml") {
					setupUnavailable(t, "%s: %v", d.name, err)
				}
				t.Fatalf("%s: %v", d.name, err)
			}
		})
	}
}

// TestUnsupportedCheckRejectsInvalidPhases shows the check above fails for
// replies that would stop docpresser run, including an upper-case key.
func TestUnsupportedCheckRejectsInvalidPhases(t *testing.T) {
	spec := "faultdriver=" + buildFaultDriver(t)
	for _, mode := range []string{"unsupported_driver_phase", "unsupported_upper_phase"} {
		t.Run(mode, func(t *testing.T) {
			err := checkUnsupported(t, spec, mode)
			if err == nil || !strings.Contains(err.Error(), "observation rejected") {
				t.Fatalf("checkUnsupported(%s) = %v, want an observation schema rejection", mode, err)
			}
		})
	}
}
