package cmd_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNoStructdocPackageOrCParserLink(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json", "github.com/3leaps/docpresser/...")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go1.26.10", "CGO_ENABLED=0")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list failed: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Name       string
			CgoFiles   []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode package: %v", err)
		}
		if strings.HasPrefix(pkg.ImportPath, "github.com/3leaps/docpresser") {
			if pkg.Name == "structdoc" || strings.HasSuffix(pkg.ImportPath, "/structdoc") {
				t.Fatalf("forbidden structdoc package detected: %s (name %s)", pkg.ImportPath, pkg.Name)
			}
			if len(pkg.CgoFiles) > 0 {
				t.Fatalf("cgo files are forbidden in coordinator package %s: %v", pkg.ImportPath, pkg.CgoFiles)
			}
		}
	}
}
