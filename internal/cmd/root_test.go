package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/3leaps/docpresser/internal/config"
	"github.com/fulmenhq/gofulmen/appidentity"
	"github.com/fulmenhq/gofulmen/foundry"
	"github.com/fulmenhq/gofulmen/logging"
)

func TestInitializeConfiguresDiagnosticCLI(t *testing.T) {
	identity := &appidentity.Identity{
		BinaryName:  "docpresser",
		Vendor:      "3leaps",
		EnvPrefix:   "DOCPRESSER_",
		ConfigName:  "docpresser",
		Description: "Qualification and synthetic-workload harness for structured documents",
	}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "docpresser")
	Initialize(ctx, identity, logger, &config.Config{})

	if rootCmd == nil {
		t.Fatal("Initialize did not create the root command")
	}
	if rootCmd.Context() != ctx {
		t.Fatal("root command did not retain its context")
	}
	if !rootCmd.SilenceUsage || !rootCmd.SilenceErrors {
		t.Fatal("root command must leave command errors to the application boundary")
	}
	for _, name := range []string{"version", "doctor", "envinfo"} {
		if command, _, err := rootCmd.Find([]string{name}); err != nil || command == rootCmd {
			t.Fatalf("missing diagnostic command %q: %v", name, err)
		}
	}
}

func TestExitCodeCatalog(t *testing.T) {
	if got := ExitCode(nil); got != foundry.ExitSuccess {
		t.Fatalf("ExitCode(nil) = %d, want %d", got, foundry.ExitSuccess)
	}
	if got := ExitCode(context.Canceled); got != foundry.ExitTestInterrupted {
		t.Fatalf("ExitCode(context.Canceled) = %d, want %d", got, foundry.ExitTestInterrupted)
	}
	if got := ExitCode(context.DeadlineExceeded); got != foundry.ExitOperationTimeout {
		t.Fatalf("ExitCode(context.DeadlineExceeded) = %d, want %d", got, foundry.ExitOperationTimeout)
	}
	errInvalid := commandFailure(foundry.ExitDataInvalid, errors.New("bad pack"))
	if got := ExitCode(errInvalid); got != foundry.ExitDataInvalid {
		t.Fatalf("ExitCode(commandFailure) = %d, want %d", got, foundry.ExitDataInvalid)
	}
}
