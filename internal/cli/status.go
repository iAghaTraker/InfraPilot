package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/iAghaTraker/InfraPilot/internal/core"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

// runStatus renders a status report.
//
// It returns nil even when parts of the installation are broken. A status
// command reports; it does not judge. Use doctor for a pass or fail verdict,
// which is what its exit code is for.
func runStatus(ctx context.Context, env Env, out IO) error {
	status := core.CollectStatus(ctx, env.Config, env.Paths, env.ConfigErr)
	writeStatus(out.Out, status)
	return nil
}

// writeStatus renders a Status as an aligned plain-text report.
//
// Every value comes from core, and core's Status is documented as carrying
// nothing sensitive. Rendering adds no new sources, so this function cannot
// leak what the report does not contain.
func writeStatus(w io.Writer, s core.Status) {
	fmt.Fprintf(w, "InfraPilot Status\n\n")

	fmt.Fprintf(w, "Agent\n")
	field(w, "Status", s.Agent.State.String())
	field(w, "Version", s.Agent.Version)
	if s.Agent.PID > 0 {
		field(w, "PID", strconv.Itoa(s.Agent.PID))
	}
	optional(w, "Detail", s.Agent.Detail)

	fmt.Fprintf(w, "\nSystem\n")
	field(w, "OS", describeOS(s.System))
	field(w, "Architecture", s.System.Architecture)
	optional(w, "Kernel", s.System.Kernel)
	field(w, "Uptime", system.FormatUptime(s.System.Uptime))
	field(w, "CPUs", strconv.Itoa(s.System.NumCPU))

	fmt.Fprintf(w, "\nStorage\n")
	field(w, "Database", healthLabel(s.Storage.Healthy))
	optional(w, "Path", s.Storage.Path)
	if s.Storage.Healthy {
		field(w, "Schema", "v"+strconv.Itoa(s.Storage.SchemaVersion))
		field(w, "Size", formatBytes(s.Storage.SizeBytes))
	}
	optional(w, "Detail", s.Storage.Detail)

	fmt.Fprintf(w, "\nConfiguration\n")
	field(w, "File", s.Config.File+" ("+presenceLabel(s.Config.FileExists)+")")
	field(w, "Data directory", s.Config.DataDir)
	if s.Config.DevMode {
		field(w, "Mode", "development")
	}
	optional(w, "Detail", s.Config.Detail)
}

// describeOS prefers the distribution name, falling back to the runtime's.
func describeOS(s core.SystemStatus) string {
	if s.Distribution != "" {
		return s.Distribution
	}
	return s.OS
}

func healthLabel(healthy bool) string {
	if healthy {
		return "healthy"
	}
	return "unavailable"
}

// presenceLabel describes a configuration file's presence. A missing file is
// normal, so it is labelled rather than flagged: defaults are complete.
func presenceLabel(exists bool) string {
	if exists {
		return "present"
	}
	return "not present, using defaults"
}

// field writes one indented label and value.
func field(w io.Writer, label, value string) {
	if value == "" {
		value = "unknown"
	}
	fmt.Fprintf(w, "  %s: %s\n", label, value)
}

// optional writes a field only when it has something to say, so a healthy
// report is not padded with empty lines.
func optional(w io.Writer, label, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(w, "  %s: %s\n", label, value)
}

// formatBytes renders a byte count for an operator.
//
// Powers of 1024 with the conventional unit names: a database is measured in
// what the filesystem reports, not in marketing megabytes.
func formatBytes(n int64) string {
	if n < 0 {
		return "unknown"
	}
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}

	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB"}

	for _, unit := range units {
		value /= 1024
		if value < 1024 {
			return strconv.FormatFloat(value, 'f', 1, 64) + " " + unit
		}
	}

	return strconv.FormatFloat(value, 'f', 1, 64) + " PiB"
}
