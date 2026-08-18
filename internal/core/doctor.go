package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/logging"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

// CheckStatus is the outcome of one diagnostic check.
type CheckStatus uint8

const (
	// StatusPass means the check found nothing wrong.
	StatusPass CheckStatus = iota

	// StatusWarn means something is not ideal but InfraPilot can still work.
	// A warning must never become a failure without an operator's involvement.
	StatusWarn

	// StatusFail means InfraPilot cannot work correctly until this is fixed.
	StatusFail
)

func (s CheckStatus) String() string {
	switch s {
	case StatusPass:
		return "PASS"
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// CheckResult is what one check reports.
type CheckResult struct {
	// Name identifies the check in output. It is stable, so operators and
	// scripts can refer to it.
	Name string

	// Status is the outcome.
	Status CheckStatus

	// Message explains the outcome. It is required for warnings and failures
	// and optional for a pass.
	Message string
}

// Env is what checks are allowed to look at.
//
// Passing this explicitly rather than letting checks reach for globals is what
// makes them independently testable, and it keeps a check from quietly
// acquiring a dependency nobody declared.
type Env struct {
	// Config is the loaded configuration. When ConfigErr is non-nil this holds
	// defaults, so checks that do not concern configuration still run.
	Config config.Config

	// Paths is the resolved path set.
	Paths system.Paths

	// ConfigErr is the failure that prevented configuration from loading.
	ConfigErr error
}

// Check is one independent diagnostic.
//
// Adding a check means appending to Checks. Each is self-contained: no check
// may depend on another having run, so a failure early in the list never hides
// a problem later in it.
type Check struct {
	Name string
	Run  func(ctx context.Context, env Env) CheckResult
}

// Report is the outcome of a diagnostic run.
type Report struct {
	Results []CheckResult
}

// Worst returns the most severe status in the report.
func (r Report) Worst() CheckStatus {
	worst := StatusPass
	for _, result := range r.Results {
		if result.Status > worst {
			worst = result.Status
		}
	}
	return worst
}

// OK reports whether the report contains no failures. Warnings are acceptable:
// a fresh installation warns about a stopped agent, and that must not make
// "doctor" exit non-zero in a provisioning script.
func (r Report) OK() bool { return r.Worst() < StatusFail }

// ExitCode maps the report to a process exit status.
func (r Report) ExitCode() int {
	if r.OK() {
		return errors.ExitOK
	}
	return errors.ExitFailure
}

// Count returns how many results carry the given status.
func (r Report) Count(status CheckStatus) int {
	n := 0
	for _, result := range r.Results {
		if result.Status == status {
			n++
		}
	}
	return n
}

// checkTimeout bounds a single check so one stuck probe cannot hang the whole
// run. Doctor is what an operator reaches for when things are already wrong,
// so it must always terminate.
const checkTimeout = 5 * time.Second

// Diagnose runs every check in order and collects the results.
//
// Checks are run sequentially rather than concurrently: the output order is
// stable, the total cost is small, and a diagnostic tool that is itself
// concurrent is harder to trust when it reports something strange.
func Diagnose(ctx context.Context, env Env) Report {
	checks := Checks()
	report := Report{Results: make([]CheckResult, 0, len(checks))}

	for _, check := range checks {
		checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		result := check.Run(checkCtx, env)
		cancel()

		// A check that forgets to name itself would produce anonymous output.
		if result.Name == "" {
			result.Name = check.Name
		}
		report.Results = append(report.Results, result)
	}

	return report
}

// Checks is the diagnostic registry.
//
// The order runs from the most fundamental to the most dependent, so the first
// failure an operator sees is the one closest to the root cause.
func Checks() []Check {
	return []Check{
		{Name: "Operating System", Run: checkOS},
		{Name: "Architecture", Run: checkArchitecture},
		{Name: "Configuration", Run: checkConfiguration},
		{Name: "Data Directory", Run: checkDataDirectory},
		{Name: "Database", Run: checkDatabase},
		{Name: "Logging", Run: checkLogging},
		{Name: "Agent", Run: checkAgent},
	}
}

func checkOS(_ context.Context, _ Env) CheckResult {
	info := system.Collect()

	if !system.IsSupportedOS() {
		return CheckResult{
			Status: StatusFail,
			Message: fmt.Sprintf(
				"InfraPilot supports Linux; this host runs %s", info.OS),
		}
	}

	name := info.Distribution
	if name == "" {
		// A missing /etc/os-release is unusual but not fatal: nothing depends
		// on knowing the distribution.
		return CheckResult{
			Status:  StatusWarn,
			Message: "Linux, but the distribution could not be identified",
		}
	}

	return CheckResult{Status: StatusPass, Message: name}
}

func checkArchitecture(_ context.Context, _ Env) CheckResult {
	info := system.Collect()

	// Any architecture the Go toolchain targets will run, so this reports
	// rather than gatekeeps.
	return CheckResult{Status: StatusPass, Message: info.Arch}
}

func checkConfiguration(_ context.Context, env Env) CheckResult {
	if env.ConfigErr != nil {
		return CheckResult{Status: StatusFail, Message: env.ConfigErr.Error()}
	}

	if err := env.Config.Validate(); err != nil {
		return CheckResult{Status: StatusFail, Message: err.Error()}
	}

	exists, err := system.CheckFilePermissions(env.Paths.ConfigFile)
	if err != nil {
		return CheckResult{
			Status:  StatusWarn,
			Message: "using valid settings, but the configuration file could not be inspected: " + err.Error(),
		}
	}
	if !exists {
		return CheckResult{
			Status:  StatusPass,
			Message: "using defaults; no file at " + env.Paths.ConfigFile,
		}
	}

	info, err := os.Stat(env.Paths.ConfigFile)
	if err != nil {
		return CheckResult{Status: StatusWarn, Message: "cannot stat " + env.Paths.ConfigFile}
	}
	// Configuration may name paths and tuning values. It is not a secret store
	// today, but a world-readable file in /etc is worth flagging before one of
	// the fields becomes sensitive.
	if system.IsWorldAccessible(info.Mode()) {
		return CheckResult{
			Status: StatusWarn,
			Message: fmt.Sprintf("%s is accessible to all local users (mode %#o); %#o is recommended",
				env.Paths.ConfigFile, info.Mode().Perm(), system.ConfigMode),
		}
	}

	return CheckResult{Status: StatusPass, Message: "loaded from " + env.Paths.ConfigFile}
}

func checkDataDirectory(_ context.Context, env Env) CheckResult {
	dir := env.Paths.DataDir
	if env.ConfigErr == nil && env.Config.Agent.DataDir != "" {
		dir = env.Config.Agent.DataDir
	}
	if dir == "" {
		return CheckResult{Status: StatusFail, Message: "no data directory is configured"}
	}

	info, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return CheckResult{
			Status:  StatusWarn,
			Message: dir + " does not exist yet; the agent creates it on first start",
		}
	case os.IsPermission(err):
		return CheckResult{
			Status:  StatusFail,
			Message: "cannot inspect " + dir + "; run as " + system.ServiceUser + " or with sudo",
		}
	case err != nil:
		return CheckResult{Status: StatusFail, Message: "cannot inspect " + dir + ": " + err.Error()}
	case !info.IsDir():
		return CheckResult{Status: StatusFail, Message: dir + " is not a directory"}
	}

	// The directory holds the database, so other local accounts must not be
	// able to read it.
	if system.IsWorldAccessible(info.Mode()) {
		return CheckResult{
			Status: StatusWarn,
			Message: fmt.Sprintf("%s is accessible to all local users (mode %#o); %#o is recommended",
				dir, info.Mode().Perm(), system.DirMode),
		}
	}

	if err := writable(dir); err != nil {
		return CheckResult{
			Status:  StatusFail,
			Message: "cannot write to " + dir + "; check that " + system.ServiceUser + " owns it",
		}
	}

	return CheckResult{Status: StatusPass, Message: dir}
}

// writable proves a directory accepts writes.
//
// Comparing the mode bits against the process UID and its groups is fiddly and
// still wrong under ACLs, so this creates and removes a file instead. The
// temporary name is generated by the operating system and cleaned up
// immediately.
func writable(dir string) error {
	file, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return err
	}
	name := file.Name()
	_ = file.Close()
	return os.Remove(name)
}

func checkDatabase(ctx context.Context, env Env) CheckResult {
	if env.ConfigErr != nil {
		return CheckResult{
			Status:  StatusWarn,
			Message: "not checked because configuration could not be loaded",
		}
	}

	path, err := env.Config.DatabasePath()
	if err != nil {
		return CheckResult{Status: StatusFail, Message: err.Error()}
	}

	info, statErr := os.Stat(path)
	if os.IsNotExist(statErr) {
		return CheckResult{
			Status:  StatusWarn,
			Message: "no database at " + path + " yet; the agent creates it on first start",
		}
	}

	// The database is created 0600, but an operator or a restore from a backup
	// can widen it after the fact. It is the one file whose contents another
	// local account must never be able to read, so a regression is reported
	// rather than left to be noticed later.
	if statErr == nil && system.IsWorldAccessible(info.Mode()) {
		return CheckResult{
			Status: StatusWarn,
			Message: fmt.Sprintf("%s is readable by all local users (mode %#o); %#o is recommended",
				path, info.Mode().Perm(), system.PrivateFileMode),
		}
	}

	db, err := storage.Open(ctx, storage.Options{
		Path:        path,
		BusyTimeout: env.Config.Storage.BusyTimeout,
		ReadOnly:    true,
	})
	if err != nil {
		if errors.IsKind(err, errors.KindPermission) {
			return CheckResult{
				Status:  StatusFail,
				Message: "cannot read " + path + "; run as " + system.ServiceUser + " or with sudo",
			}
		}
		return CheckResult{Status: StatusFail, Message: err.Error()}
	}
	defer func() { _ = db.Close() }()

	health := db.Check(ctx)
	if !health.Healthy {
		return CheckResult{Status: StatusFail, Message: health.Detail}
	}

	return CheckResult{
		Status:  StatusPass,
		Message: fmt.Sprintf("healthy, schema version %d", health.SchemaVersion),
	}
}

func checkLogging(_ context.Context, env Env) CheckResult {
	if env.ConfigErr != nil {
		return CheckResult{
			Status:  StatusWarn,
			Message: "not checked because configuration could not be loaded",
		}
	}

	level, err := logging.ParseLevel(env.Config.Logging.Level)
	if err != nil {
		return CheckResult{Status: StatusFail, Message: err.Error()}
	}
	format, err := logging.ParseFormat(env.Config.Logging.Format)
	if err != nil {
		return CheckResult{Status: StatusFail, Message: err.Error()}
	}

	// Building a logger proves the settings work rather than only that they
	// parse.
	if _, err := logging.New(logging.Options{
		Level:  env.Config.Logging.Level,
		Format: format,
		Output: io.Discard,
	}); err != nil {
		return CheckResult{Status: StatusFail, Message: err.Error()}
	}

	return CheckResult{
		Status:  StatusPass,
		Message: fmt.Sprintf("level %s, format %s", level, format),
	}
}

func checkAgent(_ context.Context, env Env) CheckResult {
	pidFile := env.Paths.PIDFile()
	if pidFile == "" {
		return CheckResult{Status: StatusWarn, Message: "the data directory is not known"}
	}

	state, pid := system.CheckProcess(pidFile, AgentBinaryName)
	switch state {
	case system.ProcessRunning:
		return CheckResult{Status: StatusPass, Message: fmt.Sprintf("running, PID %d", pid)}
	case system.ProcessStopped:
		// A stopped agent is a warning, not a failure: doctor is expected to
		// run before the first start and during maintenance.
		return CheckResult{
			Status:  StatusWarn,
			Message: "not running; start it with 'systemctl start infrapilot-agent'",
		}
	default:
		return CheckResult{
			Status:  StatusWarn,
			Message: "state could not be determined; check permissions on " + pidFile,
		}
	}
}
