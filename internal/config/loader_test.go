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
	if err := os.WriteFile(configPath, []byte(`{"run_root": "/file/runs"}`), 0o644); err != nil {
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
	path := filepath.Join(t.TempDir(), "docpresser.json")
	if err := os.WriteFile(path, []byte(`{"run_root": "./evidence"}`), 0o644); err != nil {
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
	if _, err := readUserConfig(filepath.Join(home, "secret.json")); err == nil {
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
	outside := filepath.Join(home, "secret.json")
	if err := os.WriteFile(outside, []byte(`{"run_root": "/nope"}`), 0o644); err != nil {
		t.Fatalf("write outside config: %v", err)
	}
	link := filepath.Join(configDir, "escape.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := readUserConfig(link); err == nil {
		t.Fatal("symlink under ~/.config pointing outside was accepted")
	}
}

func TestReadExplicitConfigRejectsSymlinkEscape(t *testing.T) {
	configDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(outside, []byte(`{"run_root": "/nope"}`), 0o644); err != nil {
		t.Fatalf("write outside config: %v", err)
	}
	link := filepath.Join(configDir, "docpresser.json")
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
	path := filepath.Join(t.TempDir(), "docpresser.json")
	if err := os.WriteFile(path, []byte(`{"unknown_field": "value"}`), 0o644); err != nil {
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
	path := filepath.Join(t.TempDir(), "sensitive-config.json")
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
		`{"run_root": ["` + marker + `"]}`,
		`{"` + marker + `": "value"}`,
		`{"run_root": "okay", "` + marker + `": "one"}`,
		`{"run_root": "okay"} {"` + marker + `": 1}`,
		`{"run_root": "` + marker + `", "run_root": "` + marker + `"}`,
		marker,
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

func TestParseFileLayerAcceptsOnlyOneStringRunRoot(t *testing.T) {
	accepted := map[string]map[string]any{
		`{"run_root": "/runs"}`:       {"run_root": "/runs"},
		" \n {\"run_root\": \"\"}\n ": {"run_root": ""},
		`{}`:                          {},
	}
	for input, want := range accepted {
		got, err := parseFileLayer([]byte(input))
		if err != nil {
			t.Fatalf("parseFileLayer(%q) rejected: %v", input, err)
		}
		if len(got) != len(want) || (len(want) == 1 && got["run_root"] != want["run_root"]) {
			t.Fatalf("parseFileLayer(%q) = %v, want %v", input, got, want)
		}
	}
	for _, input := range []string{
		``,
		`null`,
		`[]`,
		`"run_root"`,
		`{"run_root": null}`,
		`{"run_root": 1}`,
		`{"run_root": true}`,
		`{"run_root": {"x": "y"}}`,
		`{"run_root": "a", "run_root": "b"}`,
		`{"run_root": "a"} {}`,
		`{"run_root": "a"} trailing`,
		`{"run_root": "a",}`,
		`{"other": "a"}`,
		`run_root: /runs`,
	} {
		if layer, err := parseFileLayer([]byte(input)); err == nil {
			t.Fatalf("parseFileLayer(%q) accepted: %v", input, layer)
		}
	}
}

func TestLoadConfig_UserFileIsJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	identity := &appidentity.Identity{BinaryName: "docpresser", Vendor: "3leaps", EnvPrefix: "DOCPRESSER_", ConfigName: "docpresser"}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	configPath, err := userConfigPath(identity)
	if err != nil {
		t.Fatalf("userConfigPath returned error: %v", err)
	}
	if filepath.Base(configPath) != "docpresser.json" {
		t.Fatalf("user config file is %q, want docpresser.json", filepath.Base(configPath))
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("make config directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"run_root": "/file/runs"}`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	cfg, err := LoadConfig(context.Background(), identity, logger)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.RunRoot != "/file/runs" || cfg.Source != "file" {
		t.Fatalf("unexpected config from the JSON user file: %#v", cfg)
	}
}

func TestLoadConfig_ReportsLegacyYAMLUserFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	identity := &appidentity.Identity{BinaryName: "docpresser", Vendor: "3leaps", EnvPrefix: "DOCPRESSER_", ConfigName: "docpresser"}
	logger, err := logging.NewCLI(identity.BinaryName)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	dir := filepath.Join(home, ".config", "3leaps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docpresser.yaml"), []byte("run_root: /old\n"), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	_, err = LoadConfig(context.Background(), identity, logger)
	if err == nil || !strings.Contains(err.Error(), "now JSON") {
		t.Fatalf("legacy YAML user config was not reported: %v", err)
	}
	if strings.Contains(err.Error(), home) {
		t.Fatalf("legacy config error leaked the home path: %v", err)
	}
	// Once the JSON file exists, the legacy file no longer matters.
	if err := os.WriteFile(filepath.Join(dir, "docpresser.json"), []byte(`{"run_root": "/new"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(context.Background(), identity, logger)
	if err != nil || cfg.RunRoot != "/new" {
		t.Fatalf("JSON config beside a legacy file: cfg=%#v err=%v", cfg, err)
	}
}
