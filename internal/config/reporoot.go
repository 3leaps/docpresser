package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulmenhq/gofulmen/pathfinder"
)

// FindProjectRoot walks up from the current working directory to find the
// repository root.
func FindProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	return FindProjectRootFrom(cwd)
}

// FindProjectRootFrom finds the repository root starting from startPath.
func FindProjectRootFrom(startPath string) (string, error) {
	markers := []string{"go.mod", ".git"}

	cleanStart := filepath.Clean(startPath)
	if resolvedStart, err := filepath.EvalSymlinks(cleanStart); err == nil {
		cleanStart = filepath.Clean(resolvedStart)
	}

	isCI := strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("CI")), "true")
	if isCI {
		boundaryKeys := []string{"FULMEN_WORKSPACE_ROOT", "GITHUB_WORKSPACE", "CI_PROJECT_DIR", "WORKSPACE", "PROJECT_ROOT"}
		var boundaryErr error
		for _, key := range boundaryKeys {
			boundary, ok := resolveCIBoundary(os.Getenv(key), cleanStart)
			if !ok {
				continue
			}

			rootPath, err := pathfinder.FindRepositoryRoot(cleanStart, markers,
				pathfinder.WithBoundary(boundary),
				pathfinder.WithMaxDepth(20),
			)
			if err == nil {
				return rootPath, nil
			}
			boundaryErr = err
		}
		if boundaryErr != nil {
			return "", fmt.Errorf("project root not found within CI workspace boundary: %w", boundaryErr)
		}
	}

	rootPath, err := pathfinder.FindRepositoryRoot(cleanStart, markers, pathfinder.WithMaxDepth(10))
	if err != nil {
		return "", fmt.Errorf("project root not found: %w", err)
	}

	return rootPath, nil
}

func resolveCIBoundary(raw, start string) (string, bool) {
	boundary := filepath.Clean(strings.TrimSpace(raw))
	if boundary == "." || !filepath.IsAbs(boundary) {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(boundary)
	if err != nil {
		return "", false
	}
	resolved = filepath.Clean(resolved)
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return "", false
	}
	defer func() { _ = root.Close() }()
	st, err := root.Stat(".")
	if err != nil || !st.IsDir() {
		return "", false
	}
	startClean := filepath.Clean(start)
	if resolvedStart, err := filepath.EvalSymlinks(startClean); err == nil {
		startClean = filepath.Clean(resolvedStart)
	}
	rel, err := filepath.Rel(resolved, startClean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return resolved, true
}
