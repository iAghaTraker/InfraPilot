// Package version holds the build identity of InfraPilot.
//
// It lives under pkg/ rather than internal/ because release tooling, and
// eventually external clients such as the SDK, need to reference the same
// version constants that the binaries report.
package version

import "runtime"

// Version is the InfraPilot release version, following semantic versioning.
//
// Release builds override this value with -ldflags -X. Plain development
// builds still report the current source version.
var Version = "0.4.1"

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
