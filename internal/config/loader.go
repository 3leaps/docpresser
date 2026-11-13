package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulmenhq/gofulmen/appidentity"
	fulmenconfig "github.com/fulmenhq/gofulmen/config"
	"github.com/fulmenhq/gofulmen/logging"
	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 64 * 1024

// LoadConfig combines typed defaults, a confined optional file, and explicitly
// declared runtime overrides. The merge and environment parsing use gofulmen's
// configuration primitives; file reads remain locally confined so an operator
// supplied path cannot redirect through a final symlink.
func LoadConfig(ctx context.Context, identity *appidentity.Identity, logger *logging.Logger) (*Config, error) {
	_ = ctx
	_ = logger

	defaults := map[string]any{"run_root": ""}
	filePath, err := userConfigPath(identity)
	if err != nil {
		return nil, err
	}
	readFile := readUserConfig
	source := "default"
	if override := strings.TrimSpace(os.Getenv(identity.EnvPrefix + "CONFIG_PATH")); override != "" {
		filePath, err = filepath.Abs(override)
		if err != nil {
			return nil, fmt.Errorf("resolve config path: %w", err)
		}
		readFile = readExplicitConfig
		source = "environment"
	}

	fileLayer := map[string]any{}
	data, err := readFile(filePath)
	if err == nil {
		fileLayer, err = parseFileLayer(data)
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read configuration: %w", redactPathError(err))
	}

	runtimeLayer, err := fulmenconfig.LoadEnvOverrides([]fulmenconfig.EnvVarSpec{{
		Name: identity.EnvPrefix + "RUN_ROOT",
		Path: []string{"run_root"},
		Type: fulmenconfig.EnvString,
	}})
	if err != nil {
		return nil, fmt.Errorf("load runtime configuration: %w", err)
	}

	merged, err := fulmenconfig.MergeMaps(defaults, fileLayer)
	if err != nil {
		return nil, fmt.Errorf("merge file configuration: %w", err)
	}
	merged, err = fulmenconfig.MergeMaps(merged, runtimeLayer)
	if err != nil {
		return nil, fmt.Errorf("merge runtime configuration: %w", err)
	}

	runRoot, ok := merged["run_root"].(string)
	if !ok {
		return nil, errors.New("invalid run_root configuration")
	}
	if _, present := runtimeLayer["run_root"]; present {
		source = "environment"
	} else if len(fileLayer) > 0 && source == "default" {
		source = "file"
	}
	return &Config{RunRoot: runRoot, Source: source}, nil
}

type fileConfig struct {
	RunRoot *string `yaml:"run_root"`
}

func parseFileLayer(data []byte) (map[string]any, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	var parsed fileConfig
	if err := decoder.Decode(&parsed); err != nil {
		return nil, errors.New("configuration must be valid YAML containing only supported fields")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("configuration contains multiple documents")
		}
		return nil, errors.New("configuration must be valid YAML containing only supported fields")
	}
	layer := map[string]any{}
	if parsed.RunRoot != nil {
		layer["run_root"] = *parsed.RunRoot
	}
	return layer, nil
}

func userConfigPath(identity *appidentity.Identity) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	path := filepath.Join(homeDir, ".config", identity.Vendor, identity.ConfigName+".yaml")
	return cleanUserConfigPath(homeDir, path)
}

func cleanUserConfigPath(homeDir, path string) (string, error) {
	root := filepath.Join(homeDir, ".config")
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("config path is outside the user config directory")
	}
	return clean, nil
}

func readUserConfig(path string) ([]byte, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	clean, err := cleanUserConfigPath(homeDir, path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Join(homeDir, ".config"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	rel, err := filepath.Rel(filepath.Join(homeDir, ".config"), clean)
	if err != nil {
		return nil, err
	}
	return readRegularRootFile(root, rel)
}

func readExplicitConfig(path string) ([]byte, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return nil, errors.New("explicit config path must be absolute")
	}
	root, err := os.OpenRoot(filepath.Dir(clean))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return readRegularRootFile(root, filepath.Base(clean))
}

func readRegularRootFile(root *os.Root, name string) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("configuration file must be a regular file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, errors.New("configuration file changed while opening")
	}
	return readConfigLimit(f)
}

func readConfigLimit(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, errors.New("configuration exceeds the maximum size")
	}
	return data, nil
}

func redactPathError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New("configuration file is unavailable")
}
