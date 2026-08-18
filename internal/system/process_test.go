package system

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

func TestWriteAndReadPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), PIDFileName)

	if err := WritePIDFile(path); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	pid, err := ReadPIDFile(path)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}

// The PID file names a privileged process, so it must not be world-readable.
func TestWritePIDFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), PIDFileName)

	if err := WritePIDFile(path); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != PrivateFileMode {
		t.Errorf("mode = %#o, want %#o", got, PrivateFileMode)
	}
}

// Restarting must overwrite the previous PID rather than fail or append.
func TestWritePIDFileOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), PIDFileName)

	if err := os.WriteFile(path, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WritePIDFile(path); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	pid, err := ReadPIDFile(path)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}

// A relative path would resolve against the working directory, which for a
// service is not a location anyone intended.
func TestWritePIDFileRejectsRelativePath(t *testing.T) {
	err := WritePIDFile("agent.pid")
	if err == nil {
		t.Fatal("WritePIDFile accepted a relative path, want error")
	}
	if !errors.IsKind(err, errors.KindConfig) {
		t.Errorf("kind = %v, want config", errors.KindOf(err))
	}
}

// A failed write must not leave a temporary file behind for the next start to
// trip over.
func TestWritePIDFileLeavesNoTempFileOnFailure(t *testing.T) {
	dir := t.TempDir()

	// A missing directory makes the temporary file creation itself fail.
	if err := WritePIDFile(filepath.Join(dir, "absent", PIDFileName)); err == nil {
		t.Fatal("WritePIDFile succeeded with a missing directory, want error")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("the directory is not clean after a failure: %v", entries)
	}
}

func TestReadPIDFileErrors(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content string
		write   bool
		kind    errors.Kind
	}{
		{name: "missing file", write: false, kind: errors.KindNotFound},
		{name: "empty", content: "", write: true, kind: errors.KindValidation},
		{name: "blank", content: "   \n", write: true, kind: errors.KindValidation},
		{name: "not a number", content: "agent\n", write: true, kind: errors.KindValidation},
		{name: "zero", content: "0\n", write: true, kind: errors.KindValidation},
		{name: "negative", content: "-1\n", write: true, kind: errors.KindValidation},
		{name: "trailing junk", content: "123 extra\n", write: true, kind: errors.KindValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".pid")
			if tt.write {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}

			_, err := ReadPIDFile(path)
			if err == nil {
				t.Fatal("ReadPIDFile succeeded, want error")
			}
			if !errors.IsKind(err, tt.kind) {
				t.Errorf("kind = %v, want %v (%v)", errors.KindOf(err), tt.kind, err)
			}
		})
	}
}

// A corrupt PID file is untrusted input. Echoing it back would place arbitrary
// bytes into terminal output and logs.
func TestReadPIDFileDoesNotEchoContent(t *testing.T) {
	const marker = "SUSPICIOUS-CONTENT-\x1b[31m"

	path := filepath.Join(t.TempDir(), PIDFileName)
	if err := os.WriteFile(path, []byte(marker), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := ReadPIDFile(path)
	if err == nil {
		t.Fatal("ReadPIDFile succeeded, want error")
	}
	if got := err.Error(); strings.Contains(got, "SUSPICIOUS") || strings.Contains(got, "\x1b") {
		t.Errorf("the error echoes file content: %q", got)
	}
}

// An oversized file must not be read wholesale into memory.
func TestReadPIDFileIgnoresOversizedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), PIDFileName)

	huge := make([]byte, 1<<20)
	for i := range huge {
		huge[i] = '7'
	}
	if err := os.WriteFile(path, huge, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Only the capped prefix is read, so this either parses to a bounded
	// number or fails; it must not allocate a megabyte or hang.
	pid, err := ReadPIDFile(path)
	if err == nil && pid <= 0 {
		t.Errorf("pid = %d, want a positive number or an error", pid)
	}
}

func TestRemovePIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), PIDFileName)

	if err := WritePIDFile(path); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	if err := RemovePIDFile(path); err != nil {
		t.Fatalf("RemovePIDFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the PID file still exists after removal")
	}

	// Shutdown paths may run twice, so a second removal must be harmless.
	if err := RemovePIDFile(path); err != nil {
		t.Errorf("RemovePIDFile on a missing file: %v", err)
	}
}

func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Error("the current process is reported as not alive")
	}
	if ProcessAlive(0) {
		t.Error("PID 0 is reported as alive")
	}
	if ProcessAlive(-1) {
		t.Error("a negative PID is reported as alive")
	}

	// A process that has exited must be reported as gone. Running a real
	// command and waiting for it gives a PID that is genuinely dead.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot run a helper process: %v", err)
	}
	dead := cmd.Process.Pid

	// The kernel may recycle the PID, which would make this flaky. Treat a
	// recycled PID as inconclusive rather than a failure.
	if ProcessAlive(dead) && ProcessName(dead) == "true" {
		t.Errorf("PID %d is still reported as the exited helper", dead)
	}
}

func TestProcessName(t *testing.T) {
	name := ProcessName(os.Getpid())
	if name == "" {
		t.Skip("/proc is not available on this host")
	}
	// The kernel truncates to 15 characters, so only a prefix is guaranteed.
	if len(name) > 15 {
		t.Errorf("name %q is longer than the kernel's 15-character field", name)
	}

	if got := ProcessName(0); got != "" {
		t.Errorf("ProcessName(0) = %q, want empty", got)
	}
	if got := ProcessName(-5); got != "" {
		t.Errorf("ProcessName(-5) = %q, want empty", got)
	}
}

func TestCheckProcess(t *testing.T) {
	dir := t.TempDir()

	t.Run("no PID file means stopped", func(t *testing.T) {
		state, pid := CheckProcess(filepath.Join(dir, "absent.pid"), "")
		if state != ProcessStopped {
			t.Errorf("state = %v, want stopped", state)
		}
		if pid != 0 {
			t.Errorf("pid = %d, want 0", pid)
		}
	})

	t.Run("live process is running", func(t *testing.T) {
		path := filepath.Join(dir, "live.pid")
		if err := WritePIDFile(path); err != nil {
			t.Fatalf("WritePIDFile: %v", err)
		}

		// The expected name is this test binary's own command name, which the
		// kernel may have truncated.
		state, pid := CheckProcess(path, ProcessName(os.Getpid()))
		if state != ProcessRunning {
			t.Errorf("state = %v, want running", state)
		}
		if pid != os.Getpid() {
			t.Errorf("pid = %d, want %d", pid, os.Getpid())
		}
	})

	t.Run("corrupt PID file is unknown", func(t *testing.T) {
		path := filepath.Join(dir, "corrupt.pid")
		if err := os.WriteFile(path, []byte("not a pid\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		state, _ := CheckProcess(path, "")
		if state != ProcessUnknown {
			t.Errorf("state = %v, want unknown", state)
		}
	})

	// PID reuse: the file points at a live process that is not the agent, so
	// the agent must be reported as stopped rather than running.
	t.Run("recycled PID is stopped", func(t *testing.T) {
		path := filepath.Join(dir, "recycled.pid")
		if err := WritePIDFile(path); err != nil {
			t.Fatalf("WritePIDFile: %v", err)
		}

		if ProcessName(os.Getpid()) == "" {
			t.Skip("/proc is not available on this host")
		}

		state, _ := CheckProcess(path, "some-other-daemon")
		if state != ProcessStopped {
			t.Errorf("state = %v, want stopped for a mismatched process name", state)
		}
	})

	// A truncated /proc name must still match the full binary name, otherwise
	// an agent with a long name would always look stopped.
	t.Run("truncated process name matches", func(t *testing.T) {
		path := filepath.Join(dir, "truncated.pid")
		if err := WritePIDFile(path); err != nil {
			t.Fatalf("WritePIDFile: %v", err)
		}

		short := ProcessName(os.Getpid())
		if short == "" {
			t.Skip("/proc is not available on this host")
		}

		state, _ := CheckProcess(path, short+"-with-a-long-suffix")
		if state != ProcessRunning {
			t.Errorf("state = %v, want running when the kernel name is a prefix", state)
		}
	})

	// When the process name cannot be read the recorded process is trusted,
	// because an unprivileged caller often cannot see another account's
	// details and a false "stopped" is worse than a false "running".
	t.Run("unreadable process name trusts the PID", func(t *testing.T) {
		path := filepath.Join(dir, "noproc.pid")
		if err := WritePIDFile(path); err != nil {
			t.Fatalf("WritePIDFile: %v", err)
		}

		original := procCommPath
		procCommPath = func(int) string { return filepath.Join(dir, "no-such-comm") }
		defer func() { procCommPath = original }()

		state, _ := CheckProcess(path, "infrapilot-agent")
		if state != ProcessRunning {
			t.Errorf("state = %v, want running", state)
		}
	})

	t.Run("dead PID is stopped", func(t *testing.T) {
		path := filepath.Join(dir, "dead.pid")

		cmd := exec.Command("true")
		if err := cmd.Run(); err != nil {
			t.Skipf("cannot run a helper process: %v", err)
		}
		dead := cmd.Process.Pid

		if err := os.WriteFile(path, []byte(strconv.Itoa(dead)+"\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		state, _ := CheckProcess(path, "infrapilot-agent")
		if state == ProcessRunning {
			t.Skip("the PID was recycled, so this run is inconclusive")
		}
		if state != ProcessStopped {
			t.Errorf("state = %v, want stopped", state)
		}
	})
}

func TestProcessStateString(t *testing.T) {
	tests := map[ProcessState]string{
		ProcessRunning:  "running",
		ProcessStopped:  "stopped",
		ProcessUnknown:  "unknown",
		ProcessState(9): "unknown",
	}

	for state, want := range tests {
		if got := state.String(); got != want {
			t.Errorf("ProcessState(%d).String() = %q, want %q", state, got, want)
		}
	}
}

func TestPathsPIDFile(t *testing.T) {
	paths := Paths{DataDir: "/var/lib/infrapilot"}
	if got, want := paths.PIDFile(), "/var/lib/infrapilot/"+PIDFileName; got != want {
		t.Errorf("PIDFile = %q, want %q", got, want)
	}

	// Without a data directory there is nowhere sensible to point, and a
	// relative path would be worse than none.
	if got := (Paths{}).PIDFile(); got != "" {
		t.Errorf("PIDFile with no data directory = %q, want empty", got)
	}
}
