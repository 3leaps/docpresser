package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	info := func(mv string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: mv}}, true
		}
	}
	missing := func() (*debug.BuildInfo, bool) { return nil, false }
	cases := []struct {
		name   string
		linked string
		read   func() (*debug.BuildInfo, bool)
		want   string
	}{
		{"link-time version wins", "0.2.0", info("v0.1.0"), "0.2.0"},
		{"go install module version", "dev", info("v0.1.0"), "v0.1.0"},
		{"unset link-time version", "", info("v0.1.0"), "v0.1.0"},
		{"local build", "dev", info("(devel)"), "dev"},
		{"empty module version", "", info(""), "dev"},
		{"no build info", "", missing, "dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.linked, tc.read); got != tc.want {
				t.Fatalf("resolve(%q) = %q, want %q", tc.linked, got, tc.want)
			}
		})
	}
}
