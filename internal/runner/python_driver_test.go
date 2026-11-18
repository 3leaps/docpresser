package runner

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The Python driver must run exactly the engine requested and must never credit
// one parser's results to another. A stub `yaml` module on PYTHONPATH stands in
// for PyYAML so the checks do not depend on what the host has installed.
func TestPythonDriverReportsOnlyTheEngineThatRuns(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	driver, err := filepath.Abs("../../drivers/python-yaml/driver.py")
	if err != nil {
		t.Fatal(err)
	}

	stubDir := func(withCSafe bool) string {
		dir := filepath.Join(t.TempDir(), "site")
		if err := os.MkdirAll(filepath.Join(dir, "yaml"), 0o750); err != nil {
			t.Fatal(err)
		}
		src := "__version__ = \"9.9-stub\"\nclass SafeLoader:\n    pass\n"
		if withCSafe {
			src += "class CSafeLoader:\n    pass\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "yaml", "__init__.py"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(empty, 0o750); err != nil {
		t.Fatal(err)
	}

	handshake := func(pythonPath, engine string) (map[string]any, string, error) {
		cmd := exec.Command(python, "-S", driver, "--engine", engine, "--docpresser-handshake")
		cmd.Env = []string{"PYTHONPATH=" + pythonPath, "PYTHONNOUSERSITE=1"}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, runErr := cmd.Output()
		if runErr != nil {
			return nil, stderr.String(), runErr
		}
		var h map[string]any
		if err := json.Unmarshal(out, &h); err != nil {
			t.Fatalf("handshake for %s is not JSON: %v\n%s", engine, err, out)
		}
		return h, stderr.String(), nil
	}

	noCSafe := stubDir(false)
	withCSafe := stubDir(true)

	cases := []struct {
		name        string
		pythonPath  string
		engine      string
		wantVersion string // prefix; empty means the handshake must fail
		wantErr     string
	}{
		{"pyyaml-safe with PyYAML", noCSafe, "pyyaml-safe", "PyYAML 9.9-stub (python ", ""},
		{"pyyaml-csafe without libyaml", noCSafe, "pyyaml-csafe", "", "requires PyYAML built with libyaml"},
		{"pyyaml-csafe with libyaml", withCSafe, "pyyaml-csafe", "PyYAML 9.9-stub with libyaml ", ""},
		{"pyyaml-safe without PyYAML", empty, "pyyaml-safe", "", "requires PyYAML"},
		{"pyyaml-csafe without PyYAML", empty, "pyyaml-csafe", "", "requires PyYAML"},
		{"explicit builtin parser", empty, "builtin-yaml11", "builtin-yaml11 (python ", ""},
		{"unimplemented engine", withCSafe, "yamlprims", "", "unknown engine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, stderr, err := handshake(tc.pythonPath, tc.engine)
			if tc.wantVersion == "" {
				if err == nil {
					t.Fatalf("handshake for %s succeeded (%v); it must fail", tc.engine, h)
				}
				if !strings.Contains(stderr, tc.wantErr) {
					t.Fatalf("handshake for %s failed without %q: %s", tc.engine, tc.wantErr, stderr)
				}
				return
			}
			if err != nil {
				t.Fatalf("handshake for %s failed: %v\n%s", tc.engine, err, stderr)
			}
			if got, _ := h["subject_version"].(string); !strings.HasPrefix(got, tc.wantVersion) {
				t.Fatalf("subject_version = %q, want prefix %q", got, tc.wantVersion)
			}
			if got, _ := h["engine"].(string); got != tc.engine {
				t.Fatalf("engine = %q, want %q", got, tc.engine)
			}
		})
	}
}
