package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/fulmenhq/gofulmen/appidentity"
	"github.com/fulmenhq/gofulmen/logging"

	"github.com/3leaps/docpresser/internal/config"
)

func TestDoctorCommand_Executes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := &appidentity.Identity{
		BinaryName:  "docpresser",
		Vendor:      "3leaps",
		EnvPrefix:   "DOCPRESSER_",
		ConfigName:  "docpresser",
		Description: "Qualification and synthetic-workload harness for structured documents",
	}
	output := captureStderr(t, func() {
		logger, err := logging.NewCLI(identity.BinaryName)
		if err != nil {
			t.Fatalf("logger init failed: %v", err)
		}
		Initialize(context.Background(), identity, logger, &config.Config{})
		cmd := newDoctorCmd(identity)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("doctor command returned error: %v", err)
		}
	})
	if !strings.Contains(output, "=== docpresser Doctor ===") {
		t.Fatalf("expected doctor header in stderr output: %q", output)
	}
	if !strings.Contains(output, "All checks passed") {
		t.Fatalf("expected all checks passed message in stderr output: %q", output)
	}
}
