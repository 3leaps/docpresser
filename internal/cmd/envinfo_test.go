package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/fulmenhq/gofulmen/appidentity"
	"github.com/fulmenhq/gofulmen/logging"

	"github.com/3leaps/docpresser/internal/config"
)

func TestEnvinfoCommand_Executes(t *testing.T) {
	t.Setenv("DOCPRESSER_CONFIG_PATH", "/private/secret/docpresser.yaml")
	t.Setenv("DOCPRESSER_RUN_ROOT", "/private/secret/runs")
	identity := &appidentity.Identity{
		BinaryName:  "docpresser",
		Vendor:      "3leaps",
		EnvPrefix:   "DOCPRESSER_",
		ConfigName:  "docpresser",
		Description: "Test tool",
	}
	output := captureStderr(t, func() {
		logger, err := logging.NewCLI(identity.BinaryName)
		if err != nil {
			t.Fatalf("logger init failed: %v", err)
		}
		cfg := &config.Config{}
		Initialize(context.Background(), identity, logger, cfg)
		cmd := newEnvinfoCmd(identity)
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("envinfo command returned error: %v", err)
		}
	})
	if strings.Contains(output, "/private/secret") {
		t.Fatalf("envinfo leaked an environment value: %q", output)
	}
	for _, want := range []string{
		"DOCPRESSER_CONFIG_PATH: set",
		"DOCPRESSER_RUN_ROOT: set",
		"Fulmen Crucible:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("envinfo output does not contain %q: %q", want, output)
		}
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	fn()
	_ = writer.Close()
	os.Stderr = original

	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, reader); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	_ = reader.Close()
	return buffer.String()
}
