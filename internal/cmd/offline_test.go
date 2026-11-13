package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3leaps/docpresser/internal/config"
	"github.com/fulmenhq/gofulmen/appidentity"
	"github.com/fulmenhq/gofulmen/logging"
)

func TestOfflineCLICommands_GenerateValidateVerifySummarize(t *testing.T) {
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
	ctx := context.Background()
	Initialize(ctx, identity, logger, &config.Config{})

	outPack := filepath.Join(t.TempDir(), "pack")
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"generate", "--seed", "19", "--scenario", "k8s-bundle", "--out", outPack})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("generate command failed: %v\n%s", err, buf.String())
	}

	buf.Reset()
	rootCmd.SetArgs([]string{"validate", "--pack", outPack})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("validate command failed: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "Valid pack:") {
		t.Fatalf("unexpected validate output: %s", buf.String())
	}

	validSpecPack := filepath.Join("..", "..", "corpus", "v0", "valid-smoke", "pack")
	validSpecRun := filepath.Join("..", "..", "corpus", "v0", "valid-smoke", "run")

	buf.Reset()
	rootCmd.SetArgs([]string{"verify", "--run", validSpecRun, "--pack", validSpecPack})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("verify command failed on valid-smoke: %v\n%s", err, buf.String())
	}

	buf.Reset()
	rootCmd.SetArgs([]string{"summarize", "--run", validSpecRun, "--pack", validSpecPack, "--format", "markdown"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("summarize command failed on valid-smoke: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "# Docpresser Qualification Report") {
		t.Fatalf("unexpected summarize markdown output: %s", buf.String())
	}
}
