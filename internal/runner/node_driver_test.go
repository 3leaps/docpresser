package runner

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The Node driver's handshake must load js-yaml exactly as its YAML cases do
// and fail on any load problem; when it succeeds it reports the js-yaml version
// it loaded alongside the Node version.
func TestNodeDriverReportsOnlyTheEngineThatRuns(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	src, err := os.ReadFile("../../drivers/node-yaml/driver.mjs")
	if err != nil {
		t.Fatal(err)
	}

	// install copies the driver into a fresh directory with the given
	// node_modules/js-yaml files (none when files is nil).
	install := func(t *testing.T, files map[string]string) string {
		t.Helper()
		dir := t.TempDir()
		// ESM resolution walks parent directories; an ancestor js-yaml would
		// make the absence controls meaningless.
		for d := filepath.Dir(dir); ; d = filepath.Dir(d) {
			if _, statErr := os.Stat(filepath.Join(d, "node_modules", "js-yaml")); statErr == nil {
				t.Skipf("an ancestor of the temp dir provides js-yaml (%s)", d)
			}
			if filepath.Dir(d) == d {
				break
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "driver.mjs"), src, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if files != nil {
			mod := filepath.Join(dir, "node_modules", "js-yaml")
			if err := os.MkdirAll(mod, 0o750); err != nil {
				t.Fatal(err)
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(mod, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		return filepath.Join(dir, "driver.mjs")
	}

	// run executes a handshake with global module folders disabled: HOME is a
	// fresh directory and NODE_PATH is unset.
	run := func(t *testing.T, driver string) ([]byte, string, error) {
		t.Helper()
		cmd := exec.Command(node, driver, "--docpresser-handshake")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, runErr := cmd.Output()
		return out, stderr.String(), runErr
	}

	pkg := `{"name":"js-yaml","version":"9.9.9-stub","main":"index.js"}`
	failing := map[string]map[string]string{
		"not installed": nil,
		"init throws": {
			"package.json": pkg,
			"index.js":     "throw new Error('stub init failure');\n",
		},
		"missing transitive dependency": {
			"package.json": pkg,
			"index.js":     "require('js-yaml-missing-dependency');\nmodule.exports = { loadAll: () => [] };\n",
		},
		"no loadAll export": {
			"package.json": pkg,
			"index.js":     "module.exports = { load: () => null };\n",
		},
	}
	// Initializers that throw falsy or non-Error values must fail just as
	// loudly as ones that throw an Error.
	for _, thrown := range []string{"null", "undefined", "0", "''", "false", "NaN", "0n"} {
		failing["init throws "+thrown] = map[string]string{
			"package.json": pkg,
			"index.js":     "throw " + thrown + ";\n",
		}
	}
	for name, files := range failing {
		t.Run(name, func(t *testing.T) {
			out, stderr, err := run(t, install(t, files))
			if err == nil {
				t.Fatalf("handshake succeeded with a js-yaml that cannot run: %s", out)
			}
			if !strings.Contains(stderr, "js-yaml cannot be loaded") {
				t.Fatalf("handshake failed for another reason: %s", stderr)
			}
		})
	}

	t.Run("loads", func(t *testing.T) {
		out, stderr, err := run(t, install(t, map[string]string{
			"package.json": pkg,
			"index.js":     "module.exports = { loadAll: () => [] };\n",
		}))
		if err != nil {
			t.Fatalf("handshake with a working js-yaml failed: %v\n%s", err, stderr)
		}
		var h map[string]any
		if err := json.Unmarshal(out, &h); err != nil {
			t.Fatalf("handshake is not JSON: %v\n%s", err, out)
		}
		if got, _ := h["subject_version"].(string); !strings.HasPrefix(got, "js-yaml 9.9.9-stub (node ") {
			t.Fatalf("subject_version = %q, want the loaded js-yaml version", got)
		}
	})
}
