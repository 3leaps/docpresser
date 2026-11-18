package v0

import (
	"encoding/json"
	"strings"
	"testing"
)

func validRunManifest() map[string]any {
	return map[string]any{
		"version":             "docpresser/v0",
		"docpresser_version":  "v0.2.0",
		"state":               "complete",
		"run_id":              "run-1",
		"profile":             "smoke",
		"pack_sha256":         strings.Repeat("a", 64),
		"schema_sha256":       strings.Repeat("b", 64),
		"observations_sha256": strings.Repeat("c", 64),
		"observation_count":   1,
		"started_at":          "2026-11-18T00:00:00Z",
		"completed_at":        "2026-11-18T00:00:01Z",
		"host": map[string]any{
			"goos": "linux", "goarch": "amd64", "go_version": "go1.26.10", "num_cpu": 4,
			"coordinator_sha256": strings.Repeat("d", 64),
		},
		"subjects": []any{map[string]any{
			"name": "go-yamlv3", "role": "baseline", "subject_version": "v3.0.1 (go1.26.10)",
			"language": "go", "engine": "gopkg.in/yaml.v3",
			"formats": []any{"yaml"}, "operations": []any{"load_value"},
			"driver_sha256": strings.Repeat("e", 64),
		}},
	}
}

func validateManifest(t *testing.T, m map[string]any) error {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return ValidateRunManifestBytes(data)
}

// The run manifest must record the docpresser build and each subject's
// handshake identity, and must reject the 0.1.0 shape (subjects[].version).
func TestRunManifestRequiresHandshakeIdentity(t *testing.T) {
	if err := validateManifest(t, validRunManifest()); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	m := validRunManifest()
	delete(m, "docpresser_version")
	if validateManifest(t, m) == nil {
		t.Fatal("manifest without docpresser_version accepted")
	}

	for _, field := range []string{"subject_version", "language", "engine", "formats", "operations"} {
		m := validRunManifest()
		delete(m["subjects"].([]any)[0].(map[string]any), field)
		if validateManifest(t, m) == nil {
			t.Fatalf("subject without %s accepted", field)
		}
	}

	for field, bad := range map[string]any{
		"subject_version": "", "language": "", "engine": "",
		"formats": []any{}, "operations": []any{}, "version": "v0.1.0",
	} {
		m := validRunManifest()
		m["subjects"].([]any)[0].(map[string]any)[field] = bad
		if validateManifest(t, m) == nil {
			t.Fatalf("subject with %s=%v accepted", field, bad)
		}
	}
}
