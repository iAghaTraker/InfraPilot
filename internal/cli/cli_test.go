package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

// capture runs the CLI against an isolated installation and returns its
// streams and exit code.
//
// XDG variables plus dev mode point the whole path set at a temporary
// directory, so a test can never read or write a real installation.
func capture(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	root := t.TempDir()
	t.Setenv("INFRAPILOT_DEV", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	var out, errBuf bytes.Buffer
	code = Execute(context.Background(), args, IO{Out: &out, Err: &errBuf})

	return out.String(), errBuf.String(), code
}

func TestVersionCommand(t *testing.T) {
	stdout, stderr, code := capture(t, "version")

	if code != errors.ExitOK {
		t.Errorf("exit = %d, want %d (%s)", code, errors.ExitOK, stderr)
	}
	for _, want := range []string{version.Name, version.Version, "OS:", "Architecture:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not contain %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("version wrote to stderr: %s", stderr)
	}
}

// -v and --version must behave as the subcommand does, since operators reach
// for whichever they know.
func TestVersionFlags(t *testing.T) {
	for _, flag := range []string{"-v", "--version"} {
		t.Run(flag, func(t *testing.T) {
			stdout, _, code := capture(t, flag)

			if code != errors.ExitOK {
				t.Errorf("exit = %d, want %d", code, errors.ExitOK)
			}
			if !strings.Contains(stdout, version.Version) {
				t.Errorf("output does not report the version:\n%s", stdout)
			}
		})
	}
}

// Help on an empty invocation goes to stdout and succeeds: the operator asked
// for it, so it is not an error.
func TestNoArgumentsPrintsHelpAndSucceeds(t *testing.T) {
	stdout, stderr, code := capture(t)

	if code != errors.ExitOK {
		t.Errorf("exit = %d, want %d", code, errors.ExitOK)
	}
	if stderr != "" {
		t.Errorf("help was written to stderr: %s", stderr)
	}
	for _, want := range []string{"Usage:", "version", "status", "doctor"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %q:\n%s", want, stdout)
		}
	}
}

func TestHelpForms(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			stdout, _, code := capture(t, arg)

			if code != errors.ExitOK {
				t.Errorf("exit = %d, want %d", code, errors.ExitOK)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("no usage text:\n%s", stdout)
			}
		})
	}
}

// A misuse must exit 2, so a script can tell "you invoked this wrongly" from
// "the thing you asked about is broken".
func TestUsageErrorsExitTwo(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown command", args: []string{"bogus"}, want: "unknown command"},
		{name: "unknown flag", args: []string{"--nope"}, want: "unknown flag"},
		{name: "unexpected argument", args: []string{"status", "extra"}, want: "takes no arguments"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := capture(t, tt.args...)

			if code != errors.ExitUsage {
				t.Errorf("exit = %d, want %d", code, errors.ExitUsage)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr does not explain the problem (%q):\n%s", tt.want, stderr)
			}
			// A misuse must not look like output a script can parse.
			if stdout != "" {
				t.Errorf("a usage error wrote to stdout:\n%s", stdout)
			}
		})
	}
}

func TestStatusCommand(t *testing.T) {
	stdout, stderr, code := capture(t, "status")

	if code != errors.ExitOK {
		t.Errorf("exit = %d, want %d (%s)", code, errors.ExitOK, stderr)
	}
	for _, want := range []string{"InfraPilot Status", "Agent", "System", "Storage", "Configuration"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status omits the %q section:\n%s", want, stdout)
		}
	}
	// Nothing has been created yet, so the agent must read as stopped rather
	// than as an error.
	if !strings.Contains(stdout, "Status: stopped") {
		t.Errorf("status does not report a stopped agent:\n%s", stdout)
	}
}

// status observes; it must not create the data directory or the database.
func TestStatusCreatesNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("INFRAPILOT_DEV", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	var out, errBuf bytes.Buffer
	if code := Execute(context.Background(), []string{"status"}, IO{Out: &out, Err: &errBuf}); code != errors.ExitOK {
		t.Fatalf("exit = %d: %s", code, errBuf.String())
	}

	dataDir := filepath.Join(root, "data", "infrapilot")
	if entries, err := os.ReadDir(dataDir); err == nil && len(entries) > 0 {
		t.Errorf("status created %d entries in the data directory", len(entries))
	}
}

// A fresh installation must pass doctor: nothing is wrong before the first
// start, so an exit code of 1 would be a false alarm.
func TestDoctorPassesOnFreshInstallation(t *testing.T) {
	stdout, stderr, code := capture(t, "doctor")

	if code != errors.ExitOK {
		t.Errorf("exit = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, errors.ExitOK, stdout, stderr)
	}
	if !strings.Contains(stdout, "InfraPilot Doctor") {
		t.Errorf("no doctor header:\n%s", stdout)
	}
	for _, name := range []string{"Operating System", "Architecture", "Configuration", "Data Directory", "Database", "Logging", "Agent"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("doctor omits the %q check:\n%s", name, stdout)
		}
	}
	if !strings.Contains(stdout, "0 failed") {
		t.Errorf("a fresh installation reports failures:\n%s", stdout)
	}
}

// A failing check must exit 1 while still printing the whole report, so the
// operator sees every result rather than only the first problem.
func TestDoctorFailsOnBrokenInstallation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("INFRAPILOT_DEV", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	// An unparseable configuration file is a real failure an operator can hit
	// by hand-editing YAML.
	configDir := filepath.Join(root, "config", "infrapilot")
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("logging: [this is not a mapping\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var out, errBuf bytes.Buffer
	code := Execute(context.Background(), []string{"doctor"}, IO{Out: &out, Err: &errBuf})

	if code != errors.ExitFailure {
		t.Errorf("exit = %d, want %d\n%s", code, errors.ExitFailure, out.String())
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Errorf("no failing check in the report:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Configuration") {
		t.Errorf("the report does not name the configuration check:\n%s", out.String())
	}
	if errBuf.String() == "" {
		t.Error("a failing doctor run explained nothing on stderr")
	}
}

// status must still report when configuration is broken: that is when it is
// needed most.
func TestStatusSurvivesBrokenConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("INFRAPILOT_DEV", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	configDir := filepath.Join(root, "config", "infrapilot")
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("logging: [broken\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var out, errBuf bytes.Buffer
	code := Execute(context.Background(), []string{"status"}, IO{Out: &out, Err: &errBuf})

	if code != errors.ExitOK {
		t.Errorf("exit = %d, want %d: %s", code, errors.ExitOK, errBuf.String())
	}
	if !strings.Contains(out.String(), "InfraPilot Status") {
		t.Errorf("status produced no report:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Detail:") {
		t.Errorf("status does not explain the configuration failure:\n%s", out.String())
	}
}
