// Package system provides host inspection and safe filesystem helpers.
//
// It is the only package that reads host state directly (/etc/os-release,
// /proc/uptime, the filesystem). Isolating that here keeps the rest of the
// codebase testable: callers depend on plain Go values rather than on the
// machine the tests happen to run on.
package system

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// Info describes the host InfraPilot is running on.
//
// It contains no user identifiers, no network addresses and no environment
// variables, because it is rendered by `infrapilot status`, which must not
// disclose sensitive host detail.
type Info struct {
	// OS is the Go runtime operating system, for example "linux".
	OS string

	// Arch is the Go runtime architecture, for example "amd64".
	Arch string

	// Distribution is the human-readable OS name from /etc/os-release, for
	// example "Ubuntu 24.04.1 LTS". It is empty when it cannot be determined.
	Distribution string

	// Kernel is the running kernel release. Empty when unavailable.
	Kernel string

	// Uptime is how long the host has been up. Zero when unavailable.
	Uptime time.Duration

	// NumCPU is the number of logical CPUs usable by the process.
	NumCPU          int
	CPUModel        string
	MemoryTotal     uint64
	MemoryAvailable uint64
	DiskTotal       uint64
	DiskAvailable   uint64
}

// Collect gathers host information.
//
// Collect never fails: fields that cannot be determined are left at their zero
// value. A status command should still render on an unusual host rather than
// refuse to run. Callers that need to know whether a field is available should
// test it for its zero value.
func Collect() Info {
	info := Info{
		OS: runtime.GOOS, Arch: runtime.GOARCH, NumCPU: runtime.NumCPU(),
		Distribution: distribution(), Kernel: kernelRelease(), Uptime: uptime(), CPUModel: cpuModel(),
	}
	info.MemoryTotal, info.MemoryAvailable = memory()
	info.DiskTotal, info.DiskAvailable = disk("/")
	return info
}

var procCPUInfoPath = "/proc/cpuinfo"
var procMemInfoPath = "/proc/meminfo"

func cpuModel() string {
	f, err := os.Open(procCPUInfoPath)
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if ok && (strings.TrimSpace(key) == "model name" || strings.TrimSpace(key) == "Hardware") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func memory() (total, available uint64) {
	f, err := os.Open(procMemInfoPath)
	if err != nil {
		return 0, 0
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = value * 1024
		case "MemAvailable":
			available = value * 1024
		}
	}
	return total, available
}

func disk(path string) (total, available uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	return stat.Blocks * uint64(stat.Bsize), stat.Bavail * uint64(stat.Bsize)
}

// IsSupportedOS reports whether the current OS is supported for full
// operation. InfraPilot manages Linux hosts; other systems can build and run
// the CLI, but Agent functionality is not supported there.
func IsSupportedOS() bool { return runtime.GOOS == "linux" }

// osReleasePath is the file consulted for distribution identity. It is a
// variable so tests can point it at a fixture instead of the real host.
var osReleasePath = "/etc/os-release"

// distribution reads a human-readable OS name from /etc/os-release.
func distribution() string {
	f, err := os.Open(osReleasePath)
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is not actionable

	// PRETTY_NAME is the preferred display string; NAME is the fallback.
	var name, pretty string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "PRETTY_NAME":
			pretty = value
		case "NAME":
			name = value
		}
	}
	if err := scanner.Err(); err != nil {
		return ""
	}

	if pretty != "" {
		return pretty
	}
	return name
}

// procUptimePath is the file consulted for host uptime. It is a variable so
// tests can substitute a fixture.
var procUptimePath = "/proc/uptime"

// uptime reads host uptime from /proc/uptime, whose first field is the number
// of seconds since boot.
func uptime() time.Duration {
	data, err := os.ReadFile(procUptimePath)
	if err != nil {
		return 0
	}

	first, _, _ := strings.Cut(strings.TrimSpace(string(data)), " ")
	seconds, err := strconv.ParseFloat(first, 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

// kernelRelease reads the running kernel version.
//
// It reads /proc/sys/kernel/osrelease rather than shelling out to `uname -r`.
// That avoids spawning a process entirely, which removes any possibility of
// command injection or PATH manipulation affecting the result.
var kernelReleasePath = "/proc/sys/kernel/osrelease"

func kernelRelease() string {
	data, err := os.ReadFile(kernelReleasePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// FormatUptime renders d for human consumption, for example "3d 4h 12m".
// It returns "unknown" for non-positive input.
func FormatUptime(d time.Duration) string {
	if d <= 0 {
		return "unknown"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	switch {
	case days > 0:
		return strconv.Itoa(days) + "d " + strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	case hours > 0:
		return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	default:
		// Show seconds only for very short uptimes, where minutes alone would
		// read as "0m".
		if minutes == 0 {
			return strconv.Itoa(int(d.Seconds())) + "s"
		}
		return strconv.Itoa(minutes) + "m"
	}
}

// Filesystem permissions used across InfraPilot.
//
// These are deliberately restrictive. The data directory will eventually hold
// device public keys, audit records and module state; none of it should be
// readable by other local users.
const (
	// DirMode is the permission for InfraPilot-owned directories:
	// owner read/write/execute, group read/execute, no access for others.
	DirMode os.FileMode = 0o750

	// ConfigMode is the permission for configuration files: owner read/write,
	// group read, no access for others. Group read lets an operator add an
	// administrator to the infrapilot group for inspection.
	ConfigMode os.FileMode = 0o640

	// PrivateFileMode is the permission for files that must never be read by
	// anyone but the owner, such as the database and any future private key.
	PrivateFileMode os.FileMode = 0o600
)

// EnsureDir creates dir (and any missing parents) and enforces mode on dir
// itself.
//
// An explicit Chmod follows creation because os.MkdirAll applies the process
// umask, which commonly turns 0750 into 0700 or 0755. Relying on MkdirAll
// alone would make on-disk permissions depend on the invoking shell.
//
// If dir already exists, its permissions are still corrected. That makes the
// installer idempotent and repairs a directory loosened by hand.
func EnsureDir(dir string, mode os.FileMode) error {
	const op = "system.EnsureDir"

	if dir == "" {
		return errors.New(errors.KindValidation, op, "directory path is empty")
	}
	if !filepath.IsAbs(dir) {
		return errors.Newf(errors.KindValidation, op, "directory path must be absolute, got %q", dir)
	}

	if err := os.MkdirAll(dir, mode); err != nil {
		return errors.Wrapf(errors.KindPermission, op, err, "failed to create directory %s", dir)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return errors.Wrapf(errors.KindPermission, op, err, "failed to inspect directory %s", dir)
	}
	if !info.IsDir() {
		return errors.Newf(errors.KindValidation, op, "path %s exists but is not a directory", dir)
	}

	if info.Mode().Perm() != mode.Perm() {
		if err := os.Chmod(dir, mode); err != nil {
			return errors.Wrapf(errors.KindPermission, op, err,
				"failed to set permissions %#o on directory %s", mode.Perm(), dir)
		}
	}

	return nil
}

// IsWorldAccessible reports whether mode grants any permission to "others".
func IsWorldAccessible(mode os.FileMode) bool { return mode.Perm()&0o007 != 0 }

// IsGroupWritable reports whether mode grants write permission to the group.
func IsGroupWritable(mode os.FileMode) bool { return mode.Perm()&0o020 != 0 }

// CheckFilePermissions reports whether the file at path is safe to hold
// sensitive data: it must not be readable or writable by other users.
//
// It returns a nil error and false when the file does not exist, so callers
// can distinguish "absent" from "present but unsafe".
func CheckFilePermissions(path string) (exists bool, err error) {
	const op = "system.CheckFilePermissions"

	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil
		}
		return false, errors.Wrapf(errors.KindPermission, op, statErr, "failed to inspect %s", path)
	}

	if IsWorldAccessible(info.Mode()) {
		return true, errors.Newf(errors.KindPermission, op,
			"%s is accessible to all users (permissions %#o); tighten it to %#o",
			path, info.Mode().Perm(), PrivateFileMode.Perm())
	}

	return true, nil
}
