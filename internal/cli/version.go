package cli

import (
	"context"
	"fmt"

	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

// runVersion prints version and platform information.
//
// It deliberately ignores Env: version must answer even when configuration is
// unreadable, since "which build is this" is the first question asked when
// something else is broken.
func runVersion(_ context.Context, _ Env, out IO) error {
	build := version.Current()

	fmt.Fprintf(out.Out, "%s v%s\n", version.Name, build.Version)
	fmt.Fprintf(out.Out, "OS: %s\n", build.OS)
	fmt.Fprintf(out.Out, "Architecture: %s\n", build.Arch)
	fmt.Fprintf(out.Out, "Go: %s\n", build.GoVersion)

	return nil
}
