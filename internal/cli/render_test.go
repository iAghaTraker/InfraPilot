package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/core"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{5 * 1024 * 1024, "5.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{2 * 1024 * 1024 * 1024 * 1024, "2.0 TiB"},
		{-1, "unknown"},
	}

	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// An unset value must read as "unknown" rather than as an empty line, which
// would leave the operator unsure whether the field was checked.
func TestFieldRendersUnknownForEmptyValues(t *testing.T) {
	var b bytes.Buffer
	field(&b, "OS", "")

	if got := b.String(); got != "  OS: unknown\n" {
		t.Errorf("field with an empty value = %q", got)
	}
}

func TestOptionalOmitsEmptyValues(t *testing.T) {
	var b bytes.Buffer
	optional(&b, "Detail", "")
	if b.Len() != 0 {
		t.Errorf("optional wrote %q for an empty value", b.String())
	}

	optional(&b, "Detail", "something happened")
	if !strings.Contains(b.String(), "something happened") {
		t.Errorf("optional dropped a non-empty value: %q", b.String())
	}
}

// The distribution name is what an operator recognises; the runtime name is
// the fallback when /etc/os-release is unreadable.
func TestDescribeOSPrefersDistribution(t *testing.T) {
	if got := describeOS(core.SystemStatus{OS: "linux", Distribution: "Ubuntu 24.04"}); got != "Ubuntu 24.04" {
		t.Errorf("describeOS = %q, want the distribution", got)
	}
	if got := describeOS(core.SystemStatus{OS: "linux"}); got != "linux" {
		t.Errorf("describeOS = %q, want the runtime OS as a fallback", got)
	}
}

// A healthy report must not show a schema version or size for a database that
// could not be read: zeroes there would look like a real measurement.
func TestWriteStatusHidesStorageDetailWhenUnavailable(t *testing.T) {
	var b bytes.Buffer
	writeStatus(&b, core.Status{
		Agent: core.AgentStatus{State: system.ProcessStopped, Version: "0.1.0"},
		Storage: core.StorageStatus{
			Healthy: false,
			Path:    "/var/lib/infrapilot/infrapilot.db",
			Detail:  "the database has not been created yet; start the agent",
		},
	})

	out := b.String()
	if strings.Contains(out, "Schema:") {
		t.Errorf("an unavailable database reports a schema version:\n%s", out)
	}
	if strings.Contains(out, "Size:") {
		t.Errorf("an unavailable database reports a size:\n%s", out)
	}
	if !strings.Contains(out, "unavailable") {
		t.Errorf("the database is not marked unavailable:\n%s", out)
	}
	if !strings.Contains(out, "start the agent") {
		t.Errorf("the report omits the guidance:\n%s", out)
	}
}

func TestWriteStatusRendersAHealthyInstallation(t *testing.T) {
	var b bytes.Buffer
	writeStatus(&b, core.Status{
		Agent: core.AgentStatus{State: system.ProcessRunning, PID: 4242, Version: "0.1.0"},
		System: core.SystemStatus{
			OS: "linux", Distribution: "Ubuntu 24.04", Architecture: "amd64",
			Kernel: "6.8.0", Uptime: 90 * time.Minute, NumCPU: 4,
		},
		Storage: core.StorageStatus{Healthy: true, Path: "/var/lib/infrapilot/infrapilot.db", SchemaVersion: 1, SizeBytes: 32768},
		Config:  core.ConfigStatus{File: "/etc/infrapilot/config.yaml", FileExists: true, DataDir: "/var/lib/infrapilot"},
	})

	out := b.String()
	for _, want := range []string{"running", "4242", "Ubuntu 24.04", "amd64", "1h 30m", "healthy", "v1", "32.0 KiB", "present"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report omits %q:\n%s", want, out)
		}
	}
	// A present file must not be described as missing.
	if strings.Contains(out, "using defaults") {
		t.Errorf("a present configuration file is described as absent:\n%s", out)
	}
}

// A missing configuration file is normal. It must be stated plainly so nobody
// goes looking for a problem that does not exist.
func TestWriteStatusExplainsAMissingConfigurationFile(t *testing.T) {
	var b bytes.Buffer
	writeStatus(&b, core.Status{Config: core.ConfigStatus{File: "/etc/infrapilot/config.yaml"}})

	if !strings.Contains(b.String(), "using defaults") {
		t.Errorf("a missing file is not explained:\n%s", b.String())
	}
}

func TestWriteReportRendersEveryStatusAndASummary(t *testing.T) {
	var b bytes.Buffer
	writeReport(&b, core.Report{Results: []core.CheckResult{
		{Name: "Operating System", Status: core.StatusPass, Message: "Ubuntu 24.04"},
		{Name: "Database", Status: core.StatusWarn, Message: "not created yet"},
		{Name: "Configuration", Status: core.StatusFail, Message: "line 4: unknown key"},
	}})

	out := b.String()
	for _, want := range []string{"PASS", "WARN", "FAIL", "Operating System", "Database", "Configuration", "line 4: unknown key"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report omits %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "1 passed, 1 warnings, 1 failed") {
		t.Errorf("the summary line is wrong:\n%s", out)
	}
	if !strings.Contains(out, "run infrapilot doctor again") {
		t.Errorf("a failing report gives no next step:\n%s", out)
	}
}

// Warnings alone must not read as a broken installation.
func TestWriteReportDoesNotDemandActionForWarningsOnly(t *testing.T) {
	var b bytes.Buffer
	writeReport(&b, core.Report{Results: []core.CheckResult{
		{Name: "Agent", Status: core.StatusWarn, Message: "not running"},
	}})

	out := b.String()
	if strings.Contains(out, "Fix the failures") {
		t.Errorf("a warning-only report demands fixes:\n%s", out)
	}
	if !strings.Contains(out, "No failures") {
		t.Errorf("a warning-only report does not say it passed:\n%s", out)
	}
}

// Rendered output reaches terminals, logs and support tickets, so an
// environment secret must not survive a round trip through any command.
func TestRenderedOutputCarriesNoSecrets(t *testing.T) {
	const secret = "hunter2-must-never-appear"
	t.Setenv("PASSWORD", secret)
	t.Setenv("INFRAPILOT_SECRET_PROBE", secret)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secret)

	for _, cmd := range []string{"version", "status", "doctor"} {
		t.Run(cmd, func(t *testing.T) {
			stdout, stderr, _ := capture(t, cmd)

			if strings.Contains(stdout, secret) {
				t.Errorf("%s leaked a secret to stdout:\n%s", cmd, stdout)
			}
			if strings.Contains(stderr, secret) {
				t.Errorf("%s leaked a secret to stderr:\n%s", cmd, stderr)
			}
		})
	}
}
