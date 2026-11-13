// Package runner orchestrates pack execution across target and baseline subject
// drivers and records the resulting evidence bundle.
package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/3leaps/docpresser/internal/driver"
	"github.com/3leaps/docpresser/internal/evidence"
	"github.com/3leaps/docpresser/internal/model"
	"github.com/3leaps/docpresser/internal/pack"
)

// Options configures a docpresser run invocation.
type Options struct {
	PackDir       string
	Profile       string
	RunID         string
	SubjectSpecs  []string
	BaselineSpecs []string
	OutDir        string
}

// Run executes all cases in opts.PackDir against the configured subjects and baselines
// and writes observations.ndjson and run-manifest.json into opts.OutDir.
func Run(ctx context.Context, opts Options) (*model.RunManifest, error) {
	if opts.PackDir == "" || opts.OutDir == "" {
		return nil, fmt.Errorf("--pack and --out are required")
	}
	if len(opts.SubjectSpecs) == 0 && len(opts.BaselineSpecs) == 0 {
		return nil, fmt.Errorf("at least one --subject or --baseline is required")
	}

	loaded, err := pack.LoadAndValidatePack(opts.PackDir)
	if err != nil {
		return nil, fmt.Errorf("validate pack: %w", err)
	}

	clients := make([]*driver.Client, 0, len(opts.SubjectSpecs)+len(opts.BaselineSpecs))
	seenNames := make(map[string]struct{})
	for _, spec := range opts.SubjectSpecs {
		c, cErr := driver.NewClient(spec, "target")
		if cErr != nil {
			return nil, cErr
		}
		if _, dup := seenNames[c.Name]; dup {
			return nil, fmt.Errorf("duplicate subject name %q", c.Name)
		}
		seenNames[c.Name] = struct{}{}
		clients = append(clients, c)
	}
	for _, spec := range opts.BaselineSpecs {
		c, cErr := driver.NewClient(spec, "baseline")
		if cErr != nil {
			return nil, cErr
		}
		if _, dup := seenNames[c.Name]; dup {
			return nil, fmt.Errorf("duplicate subject name %q", c.Name)
		}
		seenNames[c.Name] = struct{}{}
		clients = append(clients, c)
	}

	metas := make([]model.SubjectMetadata, 0, len(clients))
	for _, c := range clients {
		metas = append(metas, c.Metadata())
	}

	profile := opts.Profile
	if profile == "" {
		profile = loaded.Pack.Tier
	}
	runID := opts.RunID
	if runID == "" {
		runID = fmt.Sprintf("run-%s-%d", profile, time.Now().UTC().Unix())
	}

	rec, err := evidence.NewRecorder(opts.OutDir, runID, profile, loaded.PackSHA256, metas)
	if err != nil {
		return nil, err
	}

	packRoot, err := os.OpenRoot(opts.PackDir)
	if err != nil {
		_, _ = rec.Complete("incomplete")
		return nil, fmt.Errorf("open pack root: %w", err)
	}
	defer func() { _ = packRoot.Close() }()

	for _, client := range clients {
		for _, pc := range loaded.Cases {
			if err := ctx.Err(); err != nil {
				_, _ = rec.Complete("incomplete")
				return nil, err
			}
			inputBytes, readErr := readFromRoot(packRoot, pc.InputPath)
			if readErr != nil {
				_, _ = rec.Complete("incomplete")
				return nil, fmt.Errorf("read case %q input: %w", pc.CaseID, readErr)
			}
			obs, execErr := client.ExecuteCase(ctx, pc, inputBytes)
			if execErr != nil {
				_, _ = rec.Complete("incomplete")
				return nil, fmt.Errorf("execute case %q on %q: %w", pc.CaseID, client.Name, execErr)
			}
			if recErr := rec.Record(obs); recErr != nil {
				_, _ = rec.Complete("incomplete")
				return nil, recErr
			}
		}
	}

	return rec.Complete("complete")
}

func readFromRoot(root *os.Root, relPath string) ([]byte, error) {
	f, err := root.Open(relPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
