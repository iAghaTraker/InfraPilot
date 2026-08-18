package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

// diagEnv builds an Env for a self-contained installation.
func diagEnv(t *testing.T) Env {
	t.Helper()

	cfg, paths := env(t)
	return Env{Config: cfg, Paths: paths}
}

// find returns the named result, failing the test when it is absent.
func find(t *testing.T, report Report, name string) CheckResult {
	t.Helper()

	for _, result := range report.Results {
		if result.Name == name {
			return result
		}
	}
	t.Fatalf("no check named %q in the report", name)
	return CheckResult{}
}

func TestDiagnoseRunsEveryCheck(t *testing.T) {
	report := Diagnose(context.Background(), diagEnv(t))

	if len(report.Results) != len(Checks()) {
		t.Fatalf("got %d results, want %d", len(report.Results), len(Checks()))
	}

	// Every check must identify itself and explain a non-pass outcome, or the
	// output is not actionable.
	for _, result := range report.Results {
		if result.Name == "" {
			t.Error("a check produced an unnamed result")
		}
		if result.Status != StatusPass && result.Message == "" {
			t.Errorf("check %q reports %v with no message", result.Name, result.Status)
		}
	}
}

// The registry must stay stable: these names appear in documentation and in
// operators' scripts.
func TestChecksAreRegistered(t *testing.T) {
	want := []string{
		"Operating System",
		"Architecture",
		"Configuration",
		"Data Directory",
		"Database",
		"Logging",
		"Agent",
	}

	checks := Checks()
	if len(checks) != len(want) {
		t.Fatalf("got %d checks, want %d", len(checks), len(want))
	}
	for i, name := range want {
		if checks[i].Name != name {
			t.Errorf("check %d is %q, want %q", i, checks[i].Name, name)
		}
		if checks[i].Run == nil {
			t.Errorf("check %q has no implementation", name)
		}
	}
}

func TestDiagnoseHealthyInstallation(t *testing.T) {
	e := diagEnv(t)
	seedDatabase(t, e.Config)

	report := Diagnose(context.Background(), e)

	if !report.OK() {
		for _, result := range report.Results {
			if result.Status == StatusFail {
				t.Errorf("FAIL %s: %s", result.Name, result.Message)
			}
		}
		t.Fatal("a healthy installation reports failures")
	}

	for _, name := range []string{"Operating System", "Architecture", "Configuration", "Data Directory", "Database", "Logging"} {
		if got := find(t, report, name).Status; got != StatusPass {
			t.Errorf("%s = %v, want PASS: %s", name, got, find(t, report, name).Message)
		}
	}

	// The agent is not running under test, which is a warning rather than a
	// failure so that doctor can be used before the first start.
	if got := find(t, report, "Agent").Status; got != StatusWarn {
		t.Errorf("Agent = %v, want WARN when the agent is not running", got)
	}
}

// A fresh installation, before the agent has ever run, must not report a
// failure: nothing is wrong, things simply have not been created yet.
func TestDiagnoseFreshInstallationPasses(t *testing.T) {
	report := Diagnose(context.Background(), diagEnv(t))

	if !report.OK() {
		for _, result := range report.Results {
			if result.Status == StatusFail {
				t.Errorf("FAIL %s: %s", result.Name, result.Message)
			}
		}
		t.Fatal("a fresh installation reports failures")
	}
	if report.ExitCode() != errors.ExitOK {
		t.Errorf("ExitCode = %d, want %d", report.ExitCode(), errors.ExitOK)
	}

	database := find(t, report, "Database")
	if database.Status != StatusWarn {
		t.Errorf("Database = %v, want WARN before the first start", database.Status)
	}
	if !strings.Contains(database.Message, "first start") {
		t.Errorf("Database message = %q, want it to explain the agent creates it", database.Message)
	}
}

func TestDiagnoseReportsConfigurationFailure(t *testing.T) {
	e := diagEnv(t)
	e.ConfigErr = errors.New(errors.KindConfig, "config.Load", "line 4: unknown key")

	report := Diagnose(context.Background(), e)

	configuration := find(t, report, "Configuration")
	if configuration.Status != StatusFail {
		t.Errorf("Configuration = %v, want FAIL", configuration.Status)
	}
	if !strings.Contains(configuration.Message, "unknown key") {
		t.Errorf("message = %q, want the underlying reason", configuration.Message)
	}

	if report.OK() {
		t.Error("the report is OK despite a failing check")
	}
	if report.ExitCode() != errors.ExitFailure {
		t.Errorf("ExitCode = %d, want %d", report.ExitCode(), errors.ExitFailure)
	}

	// Checks that depend on configuration must warn rather than report a
	// confident failure about values they never saw.
	for _, name := range []string{"Database", "Logging"} {
		if got := find(t, report, name).Status; got != StatusWarn {
			t.Errorf("%s = %v, want WARN when configuration is unavailable", name, got)
		}
	}

	// Checks that do not depend on configuration must still run.
	if got := find(t, report, "Operating System").Status; got == StatusFail {
		t.Error("the operating system check failed because of a configuration error")
	}
}

func TestDiagnoseRejectsInvalidConfiguration(t *testing.T) {
	e := diagEnv(t)
	e.Config.Logging.Level = "trace"

	result := find(t, Diagnose(context.Background(), e), "Configuration")
	if result.Status != StatusFail {
		t.Errorf("Configuration = %v, want FAIL for an invalid level", result.Status)
	}
	if !strings.Contains(result.Message, "logging.level") {
		t.Errorf("message = %q, want it to name the field", result.Message)
	}
}

func TestDiagnoseDataDirectoryMissing(t *testing.T) {
	e := diagEnv(t)
	e.Config.Agent.DataDir = filepath.Join(t.TempDir(), "never-created")
	e.Paths.DataDir = e.Config.Agent.DataDir

	result := find(t, Diagnose(context.Background(), e), "Data Directory")
	if result.Status != StatusWarn {
		t.Errorf("Data Directory = %v, want WARN for a directory the agent will create", result.Status)
	}
}

func TestDiagnoseDataDirectoryIsAFile(t *testing.T) {
	e := diagEnv(t)

	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	e.Config.Agent.DataDir = notADir
	e.Paths.DataDir = notADir

	result := find(t, Diagnose(context.Background(), e), "Data Directory")
	if result.Status != StatusFail {
		t.Errorf("Data Directory = %v, want FAIL when the path is a file", result.Status)
	}
}

// The data directory holds the database, so world access is worth flagging.
func TestDiagnoseWarnsOnWorldAccessibleDataDirectory(t *testing.T) {
	e := diagEnv(t)

	if err := os.Chmod(e.Config.Agent.DataDir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	result := find(t, Diagnose(context.Background(), e), "Data Directory")
	if result.Status != StatusWarn {
		t.Errorf("Data Directory = %v, want WARN for a world-accessible directory", result.Status)
	}
	if !strings.Contains(result.Message, "all local users") {
		t.Errorf("message = %q, want it to explain the exposure", result.Message)
	}
}

func TestDiagnoseUnwritableDataDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	e := diagEnv(t)
	if err := os.Chmod(e.Config.Agent.DataDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.Config.Agent.DataDir, 0o750) })

	result := find(t, Diagnose(context.Background(), e), "Data Directory")
	if result.Status != StatusFail {
		t.Errorf("Data Directory = %v, want FAIL for an unwritable directory", result.Status)
	}
}

// The writability probe must not leave its test file behind.
func TestDataDirectoryCheckLeavesNoFiles(t *testing.T) {
	e := diagEnv(t)

	Diagnose(context.Background(), e)

	entries, err := os.ReadDir(e.Config.Agent.DataDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".doctor-") {
			t.Errorf("the writability probe left %q behind", entry.Name())
		}
	}
}

func TestDiagnoseConfigurationFilePermissions(t *testing.T) {
	e := diagEnv(t)

	if err := os.WriteFile(e.Paths.ConfigFile, []byte("logging:\n  level: info\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := find(t, Diagnose(context.Background(), e), "Configuration")
	if result.Status != StatusWarn {
		t.Errorf("Configuration = %v, want WARN for a world-readable file", result.Status)
	}

	if err := os.Chmod(e.Paths.ConfigFile, system.ConfigMode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	result = find(t, Diagnose(context.Background(), e), "Configuration")
	if result.Status != StatusPass {
		t.Errorf("Configuration = %v, want PASS after tightening permissions: %s",
			result.Status, result.Message)
	}
}

func TestDiagnoseLogging(t *testing.T) {
	e := diagEnv(t)

	result := find(t, Diagnose(context.Background(), e), "Logging")
	if result.Status != StatusPass {
		t.Errorf("Logging = %v, want PASS: %s", result.Status, result.Message)
	}

	e.Config.Logging.Format = "xml"
	result = find(t, Diagnose(context.Background(), e), "Logging")
	if result.Status != StatusFail {
		t.Errorf("Logging = %v, want FAIL for an unknown format", result.Status)
	}
}

func TestDiagnoseDatabaseCorrupt(t *testing.T) {
	e := diagEnv(t)

	path, err := e.Config.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	if err := os.WriteFile(path, []byte("this is not a database"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := find(t, Diagnose(context.Background(), e), "Database")
	if result.Status != StatusFail {
		t.Errorf("Database = %v, want FAIL for a corrupt file", result.Status)
	}
}

// A database another local account can read defeats the point of storing it
// under a 0750 directory, so widening the mode must be reported.
func TestDiagnoseWarnsOnWorldReadableDatabase(t *testing.T) {
	e := diagEnv(t)

	path, err := e.Config.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}

	db, err := storage.Open(context.Background(), storage.Options{
		Path:        path,
		BusyTimeout: e.Config.Storage.BusyTimeout,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	result := find(t, Diagnose(context.Background(), e), "Database")
	if result.Status != StatusWarn {
		t.Errorf("Database = %v, want WARN for a world-readable database", result.Status)
	}
	if !strings.Contains(result.Message, "readable by all local users") {
		t.Errorf("message does not explain the problem: %q", result.Message)
	}
}

func TestReportWorstAndCounts(t *testing.T) {
	report := Report{Results: []CheckResult{
		{Name: "a", Status: StatusPass},
		{Name: "b", Status: StatusWarn},
		{Name: "c", Status: StatusPass},
	}}

	if got := report.Worst(); got != StatusWarn {
		t.Errorf("Worst = %v, want WARN", got)
	}
	if !report.OK() {
		t.Error("OK is false with only warnings; warnings must not fail a run")
	}
	if got := report.Count(StatusPass); got != 2 {
		t.Errorf("Count(PASS) = %d, want 2", got)
	}
	if got := report.Count(StatusFail); got != 0 {
		t.Errorf("Count(FAIL) = %d, want 0", got)
	}

	report.Results = append(report.Results, CheckResult{Name: "d", Status: StatusFail})
	if got := report.Worst(); got != StatusFail {
		t.Errorf("Worst = %v, want FAIL", got)
	}
	if report.OK() {
		t.Error("OK is true despite a failure")
	}

	empty := Report{}
	if got := empty.Worst(); got != StatusPass {
		t.Errorf("Worst of an empty report = %v, want PASS", got)
	}
	if !empty.OK() {
		t.Error("an empty report is not OK")
	}
}

func TestCheckStatusString(t *testing.T) {
	tests := map[CheckStatus]string{
		StatusPass:      "PASS",
		StatusWarn:      "WARN",
		StatusFail:      "FAIL",
		CheckStatus(42): "UNKNOWN",
	}

	for status, want := range tests {
		if got := status.String(); got != want {
			t.Errorf("CheckStatus(%d).String() = %q, want %q", status, got, want)
		}
	}
}

// Doctor output reaches terminals and support tickets, so no check may echo a
// credential from the environment.
func TestDoctorOutputCarriesNoSecrets(t *testing.T) {
	const secret = "hunter2-must-never-appear"
	t.Setenv("PASSWORD", secret)
	t.Setenv("INFRAPILOT_SECRET_PROBE", secret)

	e := diagEnv(t)
	seedDatabase(t, e.Config)

	report := Diagnose(context.Background(), e)

	for _, result := range report.Results {
		if strings.Contains(result.Message, secret) {
			t.Errorf("check %q leaked an environment secret: %s", result.Name, result.Message)
		}
	}
}

// Doctor is run when things are broken, so it must terminate even if a probe
// is slow. A cancelled context proves the run completes rather than blocks.
func TestDiagnoseCompletesWithCancelledContext(t *testing.T) {
	e := diagEnv(t)
	seedDatabase(t, e.Config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := Diagnose(ctx, e)

	if len(report.Results) != len(Checks()) {
		t.Errorf("got %d results, want %d", len(report.Results), len(Checks()))
	}
}

// Every check must be independently runnable: a check that assumed another had
// already run would make the registry order load-bearing.
func TestChecksAreIndependent(t *testing.T) {
	e := diagEnv(t)

	for _, check := range Checks() {
		t.Run(check.Name, func(t *testing.T) {
			result := check.Run(context.Background(), e)
			if result.Status != StatusPass && result.Message == "" {
				t.Errorf("%s reports %v with no message", check.Name, result.Status)
			}
		})
	}
}

// A default configuration with no data directory must not panic.
func TestDiagnoseWithZeroEnv(t *testing.T) {
	report := Diagnose(context.Background(), Env{Config: config.Default()})

	if len(report.Results) != len(Checks()) {
		t.Errorf("got %d results, want %d", len(report.Results), len(Checks()))
	}
}
