// Package evidence records schema-validated run observations and manifests
// inside a confined evidence directory.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/3leaps/docpresser/internal/buildinfo"
	"github.com/3leaps/docpresser/internal/model"
	v0 "github.com/3leaps/docpresser/schemas/docpresser/v0"
)

// Recorder writes observations.ndjson and run-manifest.json inside a confined root.
type Recorder struct {
	mu         sync.Mutex
	root       *os.Root
	obsFile    *os.File
	obsHash    hash.Hash
	obsCount   int
	runID      string
	profile    string
	packSHA256 string
	startedAt  string
	subjects   []model.SubjectMetadata
	closed     bool
}

// NewRecorder initializes a confined evidence bundle directory and opens observations.ndjson.
func NewRecorder(runDir, runID, profile, packSHA256 string, subjects []model.SubjectMetadata) (*Recorder, error) {
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		return nil, fmt.Errorf("create evidence directory: %w", err)
	}
	root, err := os.OpenRoot(runDir)
	if err != nil {
		return nil, fmt.Errorf("open evidence root: %w", err)
	}
	obsFile, err := root.OpenFile("observations.ndjson", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("open observations.ndjson: %w", err)
	}
	if len(subjects) == 0 {
		_ = obsFile.Close()
		_ = root.Close()
		return nil, fmt.Errorf("at least one subject is required")
	}
	return &Recorder{
		root:       root,
		obsFile:    obsFile,
		obsHash:    sha256.New(),
		runID:      runID,
		profile:    profile,
		packSHA256: packSHA256,
		startedAt:  time.Now().UTC().Format(time.RFC3339),
		subjects:   subjects,
	}, nil
}

// Record validates and appends one observation to observations.ndjson.
func (r *Recorder) Record(obs model.Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("evidence recorder is already closed")
	}
	obs.Version = model.Version
	line, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	if err := v0.ValidateObservationBytes(line); err != nil {
		return fmt.Errorf("invalid observation for case %q: %w", obs.CaseID, err)
	}
	line = append(line, '\n')
	if _, err := r.obsFile.Write(line); err != nil {
		return fmt.Errorf("write observation: %w", err)
	}
	_, _ = r.obsHash.Write(line)
	r.obsCount++
	return nil
}

// Complete finalizes observations.ndjson and writes run-manifest.json with the given state ("complete" or "incomplete").
func (r *Recorder) Complete(state string) (*model.RunManifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("evidence recorder is already closed")
	}
	r.closed = true
	defer func() { _ = r.root.Close() }()

	if err := r.obsFile.Sync(); err != nil {
		_ = r.obsFile.Close()
		return nil, fmt.Errorf("sync observations.ndjson: %w", err)
	}
	if err := r.obsFile.Close(); err != nil {
		return nil, fmt.Errorf("close observations.ndjson: %w", err)
	}

	manifest := model.RunManifest{
		Version:            model.Version,
		DocpresserVersion:  buildinfo.Version(),
		State:              state,
		RunID:              r.runID,
		Profile:            r.profile,
		PackSHA256:         r.packSHA256,
		SchemaSHA256:       v0.SchemaSHA256(),
		ObservationsSHA256: hex.EncodeToString(r.obsHash.Sum(nil)),
		ObservationCount:   r.obsCount,
		StartedAt:          r.startedAt,
		CompletedAt:        time.Now().UTC().Format(time.RFC3339),
		Host: model.HostMetadata{
			GOOS:              runtime.GOOS,
			GOARCH:            runtime.GOARCH,
			GoVersion:         runtime.Version(),
			NumCPU:            runtime.NumCPU(),
			CoordinatorSHA256: coordinatorDigest(),
		},
		Subjects: r.subjects,
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal run-manifest.json: %w", err)
	}
	data = append(data, '\n')
	if err := v0.ValidateRunManifestBytes(data); err != nil {
		return nil, fmt.Errorf("validate run-manifest.json: %w", err)
	}
	f, err := r.root.OpenFile("run-manifest.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open run-manifest.json: %w", err)
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return nil, fmt.Errorf("write run-manifest.json: %w", writeErr)
	}
	if syncErr != nil {
		return nil, fmt.Errorf("sync run-manifest.json: %w", syncErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close run-manifest.json: %w", closeErr)
	}
	return &manifest, nil
}

func coordinatorDigest() string {
	exe, err := os.Executable()
	if err == nil {
		if data, readErr := os.ReadFile(exe); readErr == nil {
			sum := sha256.Sum256(data)
			return hex.EncodeToString(sum[:])
		}
	}
	sum := sha256.Sum256([]byte(model.Version))
	return hex.EncodeToString(sum[:])
}
