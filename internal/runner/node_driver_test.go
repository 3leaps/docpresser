package runner

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The Node driver must fail its handshake when js-yaml is not installed, and
// otherwise report the js-yaml version it resolves alongside the Node version.
func TestNodeDriverReportsOnlyTheEngineThatRuns(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	src, err := os.ReadFile("../../drivers/node-yaml/driver.mjs")
	if err != nil {
		t.Fatal(err)
	}

	install := func(withJSYaml bool) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "driver.mjs"), src, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if withJSYaml {
			mod := filepath.Join(dir, "node_modules", "js-yaml")
			if err := os.MkdirAll(mod, 0o750); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				"package.json": `{"name":"js-yaml","version":"9.9.9-stub","main":"index.js"}`,
				"index.js":     "module.exports = { loadAll: () => [] };\n",
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(mod, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		return filepath.Join(dir, "driver.mjs")
	}

	run := func(driver string) ([]byte, string, error) {
		cmd := exec.Command(node, driver, "--docpresser-handshake")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, runErr := cmd.Output()
		return out, stderr.String(), runErr
	}

	out, stderr, err := run(install(false))
	if err == nil {
		t.Fatalf("handshake without js-yaml succeeded: %s", out)
	}
	if !strings.Contains(stderr, "js-yaml is not installed") {
		t.Fatalf("handshake without js-yaml failed for another reason: %s", stderr)
	}

	out, stderr, err = run(install(true))
	if err != nil {
		t.Fatalf("handshake with js-yaml failed: %v\n%s", err, stderr)
	}
	var h map[string]any
	if err := json.Unmarshal(out, &h); err != nil {
		t.Fatalf("handshake is not JSON: %v\n%s", err, out)
	}
	if got, _ := h["subject_version"].(string); !strings.HasPrefix(got, "js-yaml 9.9.9-stub (node ") {
		t.Fatalf("subject_version = %q, want the resolved js-yaml version", got)
	}
}
