package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulmenhq/gofulmen/appidentity"
	"github.com/fulmenhq/gofulmen/logging"
)

func TestLoadConfig_DefaultsAndEnvOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := &appidentity.Identity{BinaryName: "docpresser", Vendor: "acme", EnvPrefix: "TEST_", ConfigName: "tool"}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	t.Setenv("TEST_RUN_ROOT", "/env/runs")

	cfg, err := LoadConfig(context.Background(), identity, logger)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.RunRoot != "/env/runs" {
		t.Fatalf("expected environment RunRoot, got %q", cfg.RunRoot)
	}
	if cfg.Source != "environment" {
		t.Fatalf("expected environment configuration source, got %q", cfg.Source)
	}
}

func TestLoadConfig_FileAndEnvPriority(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := &appidentity.Identity{BinaryName: "docpresser", Vendor: "vendorx", EnvPrefix: "MYTOOL_", ConfigName: "mytool"}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	configPath, err := userConfigPath(identity)
	if err != nil {
		t.Fatalf("userConfigPath returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("make config directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("run_root: /file/runs\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("MYTOOL_RUN_ROOT", "/env/runs")

	cfg, err := LoadConfig(context.Background(), identity, logger)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.RunRoot != "/env/runs" {
		t.Fatalf("expected environment override, got %q", cfg.RunRoot)
	}
	if cfg.Source != "environment" {
		t.Fatalf("expected environment configuration source, got %q", cfg.Source)
	}
}

func TestLoadConfigDefaultsHaveDefaultSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := &appidentity.Identity{BinaryName: "docpresser", Vendor: "acme", EnvPrefix: "TEST_", ConfigName: "tool"}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	cfg, err := LoadConfig(context.Background(), identity, logger)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.RunRoot != "" || cfg.Source != "default" {
		t.Fatalf("unexpected default config: %#v", cfg)
	}
}

func TestLoadConfig_ExplicitConfigPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := &appidentity.Identity{BinaryName: "docpresser", Vendor: "3leaps", EnvPrefix: "DOCPRESSER_", ConfigName: "docpresser"}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	path := filepath.Join(t.TempDir(), "docpresser.yaml")
	if err := os.WriteFile(path, []byte("run_root: ./evidence\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DOCPRESSER_CONFIG_PATH", path)

	cfg, err := LoadConfig(context.Background(), identity, logger)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.RunRoot != "./evidence" || cfg.Source != "environment" {
		t.Fatalf("unexpected explicit config: %#v", cfg)
	}
	if cfg.Source == path {
		t.Fatal("configuration source retained an operator path")
	}
}

func TestUserConfigPathRejectsEscape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	identity := &appidentity.Identity{Vendor: "..", ConfigName: "secret"}
	if _, err := userConfigPath(identity); err == nil {
		t.Fatal("expected config path outside ~/.config to be rejected")
	}
	if _, err := readUserConfig(filepath.Join(home, "secret.yaml")); err == nil {
		t.Fatal("expected a read outside ~/.config to be rejected")
	}
}

func TestReadUserConfigRejectsSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("make config directory: %v", err)
	}
	outside := filepath.Join(home, "secret.yaml")
	if err := os.WriteFile(outside, []byte("run_root: /nope\n"), 0o644); err != nil {
		t.Fatalf("write outside config: %v", err)
	}
	link := filepath.Join(configDir, "escape.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := readUserConfig(link); err == nil {
		t.Fatal("symlink under ~/.config pointing outside was accepted")
	}
}

func TestReadExplicitConfigRejectsSymlinkEscape(t *testing.T) {
	configDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.yaml")
	if err := os.WriteFile(outside, []byte("run_root: /nope\n"), 0o644); err != nil {
		t.Fatalf("write outside config: %v", err)
	}
	link := filepath.Join(configDir, "docpresser.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := readExplicitConfig(link); err == nil {
		t.Fatal("explicit config symlink escape was accepted")
	}
}

func TestLoadConfigRejectsUnknownFileFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := &appidentity.Identity{BinaryName: "docpresser", Vendor: "3leaps", EnvPrefix: "DOCPRESSER_", ConfigName: "docpresser"}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	path := filepath.Join(t.TempDir(), "docpresser.yaml")
	if err := os.WriteFile(path, []byte("unknown_field: value\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DOCPRESSER_CONFIG_PATH", path)
	if _, err := LoadConfig(context.Background(), identity, logger); err == nil {
		t.Fatal("unknown configuration field was accepted")
	}
}

func TestLoadConfigRejectsOversizedFileWithoutPathLeak(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := &appidentity.Identity{BinaryName: "docpresser", Vendor: "3leaps", EnvPrefix: "DOCPRESSER_", ConfigName: "docpresser"}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sensitive-config.yaml")
	if err := os.WriteFile(path, make([]byte, maxConfigBytes+1), 0o644); err != nil {
		t.Fatalf("write oversized config: %v", err)
	}
	t.Setenv("DOCPRESSER_CONFIG_PATH", path)
	_, err = LoadConfig(context.Background(), identity, logger)
	if err == nil {
		t.Fatal("oversized configuration was accepted")
	}
	if strings.Contains(err.Error(), path) {
		t.Fatalf("configuration path leaked in error: %v", err)
	}
}

func TestParseConfigDoesNotEchoValues(t *testing.T) {
	const marker = "synthetic-private-value"
	for _, input := range []string{
		"run_root: [" + marker + "]\n",
		marker + ": value\n",
		"run_root: okay\n" + marker + ": one\n" + marker + ": two\n",
		"run_root: okay\n---\n[" + marker + "\n",
	} {
		_, err := parseFileLayer([]byte(input))
		if err == nil {
			t.Fatal("invalid configuration was accepted")
		}
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("configuration value leaked: %v", err)
		}
	}
}
