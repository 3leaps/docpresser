// Package buildinfo reports the docpresser build version recorded in evidence.
package buildinfo

import (
	"runtime/debug"
	"sync"
)

var (
	mu      sync.RWMutex
	version string
)

// SetVersion records the version injected at link time (main.version).
func SetVersion(v string) {
	mu.Lock()
	defer mu.Unlock()
	version = v
}

// Version returns the docpresser build version: the link-time version when set,
// otherwise the module version Go recorded for this binary (for example from
// `go install github.com/3leaps/docpresser/cmd/docpresser@v0.1.0`), otherwise "dev".
func Version() string {
	mu.RLock()
	v := version
	mu.RUnlock()
	return resolve(v, debug.ReadBuildInfo)
}

func resolve(linked string, read func() (*debug.BuildInfo, bool)) string {
	if linked != "" && linked != "dev" {
		return linked
	}
	if info, ok := read(); ok && info != nil {
		if mv := info.Main.Version; mv != "" && mv != "(devel)" {
			return mv
		}
	}
	return "dev"
}
