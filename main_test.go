package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	const sum = "h1:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG="
	mod := func(v, s string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/aliasproject/notch", Version: v, Sum: s}}
	}
	cases := []struct {
		name    string
		stamped string
		bi      *debug.BuildInfo
		want    string
	}{
		{"goreleaser stamp wins", "0.10.0", mod("v0.9.0", sum), "0.10.0"},
		{"no build info", "dev", nil, "dev"},
		{"go install of a release", "dev", mod("v0.10.0", sum), "v0.10.0"},
		{"go install of a pre-release", "dev", mod("v0.11.0-rc.1", sum), "v0.11.0-rc.1"},
		// Go 1.24+ stamps a checkout built at a tag with that tag, but
		// without a go.sum hash, since nothing was downloaded.
		{"local build at a tag", "dev", mod("v0.10.0", ""), "dev"},
		{"local build at a tag, dirty", "dev", mod("v0.10.0+dirty", ""), "dev"},
		{"local build past a tag", "dev", mod("v0.10.1-0.20261101120000-8e1a4ff10cc0", ""), "dev"},
		{"local build with -buildvcs=false", "dev", mod("(devel)", ""), "dev"},
		{"empty version", "dev", mod("", ""), "dev"},
		// "go install ...@main" downloads a pseudo-version: not a release.
		{"go install of a commit", "dev", mod("v0.10.1-0.20261101120000-8e1a4ff10cc0", sum), "dev"},
		{"go install of a commit with no tags", "dev", mod("v0.0.0-20261101120000-8e1a4ff10cc0", sum), "dev"},
		{"go install of a commit past a pre-release", "dev", mod("v0.11.0-rc.1.0.20261101120000-8e1a4ff10cc0", sum), "dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersion(tc.stamped, tc.bi); got != tc.want {
				t.Errorf("resolveVersion(%q, %+v) = %q, want %q", tc.stamped, tc.bi, got, tc.want)
			}
		})
	}
}
