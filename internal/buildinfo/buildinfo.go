// Package buildinfo exposes the version information that is stamped into the
// binary at build time.
package buildinfo

import "runtime/debug"

// Version is the semantic version of the build. It is overridden through
// -ldflags "-X github.com/gopager/pagewire/internal/buildinfo.Version=…".
var Version = "dev"

// commit can be stamped via -ldflags for builds without VCS metadata.
var commit string

// Commit returns the VCS revision recorded by the Go toolchain or stamped at
// build time, or "unknown".
func Commit() string {
	if commit != "" {
		return commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return "unknown"
}

// UserAgent returns the HTTP User-Agent string used towards target servers.
func UserAgent() string {
	return "pagewire/" + Version
}
