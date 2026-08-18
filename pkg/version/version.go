// Package version holds the build identity of InfraPilot.
//
// It lives under pkg/ rather than internal/ because release tooling, and
// eventually external clients such as the SDK, need to reference the same
// version constants that the binaries report.
package version

import "runtime"

// Version is the InfraPilot release version, following semantic versioning.
//
// It is a plain constant rather than a linker-injected variable so that a
// plain `go build ./...` always produces a correctly versioned binary. Build
// metadata that genuinely varies per build (commit, date) is read from the
// embedded VCS stamp instead; see Build.
const Version = "0.3.0"

// Name is the product name used in human-facing output.
const Name = "InfraPilot"

// Build describes the environment a binary was compiled for.
type Build struct {
	Version   string
	OS        string
	Arch      string
	GoVersion string
}

// Current returns the build identity of the running binary.
func Current() Build {
	return Build{
		Version:   Version,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
	}
}
