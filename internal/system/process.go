package system

import (
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// PIDFileName is the file the agent uses to advertise that it is running.
//
// v0.1.0 exposes no management API, so a PID file is how a CLI invocation
// learns whether an agent is alive. It is deliberately the least privileged
// mechanism available: a regular file in the data directory, no listener and
// no IPC surface.
const PIDFileName = "agent.pid"

// maxPIDFileSize caps how much of a PID file is read. A PID is a handful of
// bytes, so anything larger is corrupt or hostile and is not worth reading
// into memory.
const maxPIDFileSize = 64

// ProcessState describes whether a process is running.
type ProcessState uint8

const (
	// ProcessUnknown means liveness could not be determined, usually because
	// the PID file could not be read.
	ProcessUnknown ProcessState = iota

	// ProcessStopped means no PID file exists, or the recorded process is gone.
	ProcessStopped

	// ProcessRunning means the recorded process exists and is reachable.
	ProcessRunning
)

func (s ProcessState) String() string {
	switch s {
	case ProcessStopped:
		return "stopped"
	case ProcessRunning:
		return "running"
	default:
		return "unknown"
	}
}

// WritePIDFile records the current process ID at path.
//
// The file is written and then renamed into place so a reader never observes a
// half-written PID. It is created with restrictive permissions because it
// reveals a process ID belonging to a privileged service.
func WritePIDFile(path string) error {
	const op = "system.WritePIDFile"

	if !filepath.IsAbs(path) {
		return errors.New(errors.KindConfig, op,
			fmt.Sprintf("the PID file path must be absolute, got %q", path))
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".pid-*")
	if err != nil {
		return errors.Wrap(errors.KindPermission, op,
			fmt.Sprintf("cannot create a PID file in %s", filepath.Dir(path)), err)
	}
	tempName := temp.Name()
	// On every failure past this point the temporary file must not be left
	// behind, so cleanup is registered before the first write can fail.
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()

	if err := temp.Chmod(PrivateFileMode); err != nil {
		return errors.Wrap(errors.KindPermission, op, "cannot set PID file permissions", err)
	}
	if _, err := fmt.Fprintf(temp, "%d\n", os.Getpid()); err != nil {
		return errors.Wrap(errors.KindInternal, op, "cannot write the PID file", err)
	}
	// Durability matters here: a PID file that survives a crash as an empty
	// file would make a stopped agent look unknown rather than stopped.
	if err := temp.Sync(); err != nil {
		return errors.Wrap(errors.KindInternal, op, "cannot flush the PID file", err)
	}
	if err := temp.Close(); err != nil {
		return errors.Wrap(errors.KindInternal, op, "cannot close the PID file", err)
	}

	if err := os.Rename(tempName, path); err != nil {
		return errors.Wrap(errors.KindPermission, op,
			fmt.Sprintf("cannot move the PID file into place at %s", path), err)
	}

	return nil
}

// RemovePIDFile deletes the PID file. An already absent file is not an error,
// which keeps shutdown paths simple.
func RemovePIDFile(path string) error {
	const op = "system.RemovePIDFile"

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(errors.KindPermission, op,
			fmt.Sprintf("cannot remove the PID file at %s", path), err)
	}
	return nil
}

// ReadPIDFile returns the process ID recorded at path.
func ReadPIDFile(path string) (int, error) {
	const op = "system.ReadPIDFile"

	file, err := os.Open(path)
	if err != nil {
		switch {
		case os.IsNotExist(err):
			return 0, errors.Wrap(errors.KindNotFound, op, "no PID file", err)
		case os.IsPermission(err):
			return 0, errors.Wrap(errors.KindPermission, op,
				fmt.Sprintf("cannot read the PID file at %s", path), err)
		default:
			return 0, errors.Wrap(errors.KindInternal, op,
				fmt.Sprintf("cannot open the PID file at %s", path), err)
		}
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, maxPIDFileSize)
	n, readErr := file.Read(buf)
	// An empty file reads zero bytes and reports EOF. That is a truncated or
	// half-written PID file, which is a content problem rather than an I/O one.
	if readErr != nil && n == 0 && !stderrors.Is(readErr, io.EOF) {
		return 0, errors.Wrap(errors.KindInternal, op, "cannot read the PID file", readErr)
	}

	text := strings.TrimSpace(string(buf[:n]))
	if text == "" {
		return 0, errors.New(errors.KindValidation, op, "the PID file is empty")
	}

	pid, convErr := strconv.Atoi(text)
	if convErr != nil {
		// The file content is not echoed: it is untrusted input that would end
		// up in logs and terminal output.
		return 0, errors.New(errors.KindValidation, op, "the PID file does not contain a process ID")
	}
	if pid <= 0 {
		return 0, errors.New(errors.KindValidation, op, "the PID file contains an invalid process ID")
	}

	return pid, nil
}

// ProcessAlive reports whether a process with the given ID exists.
//
// Signal 0 performs the kernel's existence and permission checks without
// delivering anything. EPERM counts as alive: the process exists, this account
// simply may not signal it, which is the normal case for an unprivileged CLI
// inspecting a root-owned agent.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		return true
	case stderrors.Is(err, os.ErrPermission):
		return true
	default:
		return false
	}
}

// procCommPath locates a process's command name. It is a variable so tests can
// supply a fixture directory instead of relying on live processes.
var procCommPath = func(pid int) string {
	return filepath.Join("/proc", strconv.Itoa(pid), "comm")
}

// ProcessName returns the command name of a running process, or an empty
// string when it cannot be determined.
//
// The kernel stores this name in a 16-byte field, so names of 16 characters or
// more come back truncated. Callers must compare with that in mind.
func ProcessName(pid int) string {
	if pid <= 0 {
		return ""
	}

	data, err := os.ReadFile(procCommPath(pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// CheckProcess reports the state of the process recorded in a PID file.
//
// expectedName guards against PID reuse: a stale PID file whose number has
// been recycled by an unrelated process would otherwise report the agent as
// running. The comparison is a prefix test because the kernel truncates
// command names to 15 characters, so "infrapilot-agent" is reported as
// "infrapilot-agen".
//
// When the name cannot be read the recorded process is trusted, since an
// unprivileged caller cannot always see another account's process details and
// reporting "stopped" for a healthy agent would be worse than reporting
// "running" for a recycled PID.
func CheckProcess(pidFile, expectedName string) (ProcessState, int) {
	pid, err := ReadPIDFile(pidFile)
	if err != nil {
		if errors.IsKind(err, errors.KindNotFound) {
			return ProcessStopped, 0
		}
		return ProcessUnknown, 0
	}

	if !ProcessAlive(pid) {
		return ProcessStopped, pid
	}

	if expectedName != "" {
		if name := ProcessName(pid); name != "" && !strings.HasPrefix(expectedName, name) {
			// The PID belongs to something else, so the agent is not running
			// even though the file says it is.
			return ProcessStopped, pid
		}
	}

	return ProcessRunning, pid
}
