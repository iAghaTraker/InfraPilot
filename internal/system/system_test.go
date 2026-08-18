package system

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

func TestCollectReportsRuntimeValues(t *testing.T) {
	info := Collect()

	if info.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
	if info.NumCPU < 1 {
		t.Errorf("NumCPU = %d, want at least 1", info.NumCPU)
	}
}

// Host-dependent fields are read from files, so they are tested against
// fixtures rather than the developer's machine.
func TestDistributionFromFixture(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "prefers PRETTY_NAME",
			content: "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nVERSION_ID=\"24.04\"\n",
			want:    "Ubuntu 24.04.1 LTS",
		},
		{
			name:    "falls back to NAME",
			content: "NAME=\"Debian GNU/Linux\"\nVERSION_ID=\"12\"\n",
			want:    "Debian GNU/Linux",
		},
		{
			name:    "handles unquoted values",
			content: "PRETTY_NAME=Alpine Linux v3.20\n",
			want:    "Alpine Linux v3.20",
		},
		{
			name:    "ignores malformed lines",
			content: "this line has no equals sign\nPRETTY_NAME=\"Fedora 40\"\n",
			want:    "Fedora 40",
		},
		{
			name:    "empty file yields empty result",
			content: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "os-release")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			restore := osReleasePath
			osReleasePath = path
			defer func() { osReleasePath = restore }()

			if got := distribution(); got != tt.want {
				t.Errorf("distribution() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDistributionMissingFile(t *testing.T) {
	restore := osReleasePath
	osReleasePath = filepath.Join(t.TempDir(), "absent")
	defer func() { osReleasePath = restore }()

	if got := distribution(); got != "" {
		t.Errorf("distribution() = %q, want empty for a missing file", got)
	}
}

func TestUptimeFromFixture(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    time.Duration
	}{
		{"typical", "12345.67 98765.43\n", 12345670 * time.Millisecond},
		{"zero", "0.00 0.00\n", 0},
		{"single field", "500.5", 500500 * time.Millisecond},
		{"garbage", "not-a-number\n", 0},
		{"empty", "", 0},
		{"negative is rejected", "-5.0 0.0\n", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "uptime")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			restore := procUptimePath
			procUptimePath = path
			defer func() { procUptimePath = restore }()

			if got := uptime(); got != tt.want {
				t.Errorf("uptime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKernelReleaseFromFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "osrelease")
	if err := os.WriteFile(path, []byte("6.8.0-31-generic\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	restore := kernelReleasePath
	kernelReleasePath = path
	defer func() { kernelReleasePath = restore }()

	if got, want := kernelRelease(), "6.8.0-31-generic"; got != want {
		t.Errorf("kernelRelease() = %q, want %q", got, want)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "unknown"},
		{-time.Second, "unknown"},
		{45 * time.Second, "45s"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{25 * time.Hour, "1d 1h 0m"},
		{76*time.Hour + 12*time.Minute, "3d 4h 12m"},
	}

	for _, tt := range tests {
		if got := FormatUptime(tt.in); got != tt.want {
			t.Errorf("FormatUptime(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEnsureDirCreatesWithExactMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")

	if err := EnsureDir(dir, DirMode); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("EnsureDir did not create a directory")
	}
	// The umask must not influence the final mode.
	if got := info.Mode().Perm(); got != DirMode.Perm() {
		t.Errorf("mode = %#o, want %#o", got, DirMode.Perm())
	}
}

func TestEnsureDirRepairsLoosePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "loose")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if err := EnsureDir(dir, DirMode); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != DirMode.Perm() {
		t.Errorf("mode = %#o, want %#o; world-accessible directory was not repaired", got, DirMode.Perm())
	}
}

func TestEnsureDirIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repeat")
	for i := range 3 {
		if err := EnsureDir(dir, DirMode); err != nil {
			t.Fatalf("EnsureDir call %d: %v", i+1, err)
		}
	}
}

func TestEnsureDirRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"empty", ""},
		{"relative", "relative/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnsureDir(tt.dir, DirMode)
			if err == nil {
				t.Fatal("EnsureDir succeeded, want error")
			}
			if !errors.IsKind(err, errors.KindValidation) {
				t.Errorf("kind = %v, want validation", errors.KindOf(err))
			}
		})
	}
}

func TestEnsureDirRejectsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := EnsureDir(path, DirMode); err == nil {
		t.Fatal("EnsureDir on a regular file succeeded, want error")
	}
}

func TestPermissionPredicates(t *testing.T) {
	tests := []struct {
		mode        os.FileMode
		world       bool
		groupWrite  bool
		description string
	}{
		{0o600, false, false, "owner only"},
		{0o640, false, false, "owner rw, group r"},
		{0o750, false, false, "standard directory"},
		{0o644, true, false, "world readable"},
		{0o604, true, false, "world readable, no group"},
		{0o660, false, true, "group writable"},
		{0o777, true, true, "wide open"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := IsWorldAccessible(tt.mode); got != tt.world {
				t.Errorf("IsWorldAccessible(%#o) = %v, want %v", tt.mode, got, tt.world)
			}
			if got := IsGroupWritable(tt.mode); got != tt.groupWrite {
				t.Errorf("IsGroupWritable(%#o) = %v, want %v", tt.mode, got, tt.groupWrite)
			}
		})
	}
}

func TestCheckFilePermissions(t *testing.T) {
	dir := t.TempDir()

	t.Run("absent file", func(t *testing.T) {
		exists, err := CheckFilePermissions(filepath.Join(dir, "absent"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists {
			t.Error("exists = true for an absent file")
		}
	})

	t.Run("safe permissions", func(t *testing.T) {
		path := filepath.Join(dir, "safe")
		if err := os.WriteFile(path, []byte("x"), PrivateFileMode); err != nil {
			t.Fatalf("write: %v", err)
		}
		exists, err := CheckFilePermissions(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists {
			t.Error("exists = false for a present file")
		}
	})

	t.Run("world readable is reported", func(t *testing.T) {
		path := filepath.Join(dir, "loose")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod: %v", err)
		}

		exists, err := CheckFilePermissions(path)
		if !exists {
			t.Error("exists = false for a present file")
		}
		if err == nil {
			t.Fatal("world-readable file accepted, want error")
		}
		if !errors.IsKind(err, errors.KindPermission) {
			t.Errorf("kind = %v, want permission", errors.KindOf(err))
		}
	})
}
