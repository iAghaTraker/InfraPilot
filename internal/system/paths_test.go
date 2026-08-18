package system

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTruthy(t *testing.T) {
	falsy := []string{"", "0", "false", "no", "off", "  ", "FALSE", "Off"}
	for _, v := range falsy {
		if truthy(v) {
			t.Errorf("truthy(%q) = true, want false", v)
		}
	}

	for _, v := range []string{"1", "true", "yes", "on", "TRUE", "anything"} {
		if !truthy(v) {
			t.Errorf("truthy(%q) = false, want true", v)
		}
	}
}

func TestResolvePathsProduction(t *testing.T) {
	// t.Setenv restores the previous value and forbids parallel tests, so the
	// environment cannot leak between cases.
	t.Setenv(DevModeEnv, "")
	t.Setenv(ConfigPathEnv, "")
	t.Setenv(DataDirEnv, "")

	paths, err := ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}

	if want := filepath.Join(SystemConfigDir, ConfigFileName); paths.ConfigFile != want {
		t.Errorf("ConfigFile = %q, want %q", paths.ConfigFile, want)
	}
	if paths.DataDir != SystemDataDir {
		t.Errorf("DataDir = %q, want %q", paths.DataDir, SystemDataDir)
	}
	if paths.DevMode {
		t.Error("DevMode = true without the environment variable set")
	}
}

func TestResolvePathsDevMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv(DevModeEnv, "1")
	t.Setenv(ConfigPathEnv, "")
	t.Setenv(DataDirEnv, "")

	paths, err := ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}

	if !paths.DevMode {
		t.Error("DevMode = false with INFRAPILOT_DEV=1")
	}
	// Development mode must not touch system directories, since that is the
	// whole point of running without root.
	if strings.HasPrefix(paths.ConfigFile, SystemConfigDir) {
		t.Errorf("ConfigFile = %q, want a user-local path", paths.ConfigFile)
	}
	if strings.HasPrefix(paths.DataDir, SystemDataDir) {
		t.Errorf("DataDir = %q, want a user-local path", paths.DataDir)
	}
	if !strings.HasPrefix(paths.ConfigFile, home) {
		t.Errorf("ConfigFile = %q, want it under %q", paths.ConfigFile, home)
	}
	if !strings.HasPrefix(paths.DataDir, home) {
		t.Errorf("DataDir = %q, want it under %q", paths.DataDir, home)
	}
}

func TestResolvePathsEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "custom.yaml")
	dataDir := filepath.Join(dir, "state")

	t.Setenv(DevModeEnv, "")
	t.Setenv(ConfigPathEnv, configPath)
	t.Setenv(DataDirEnv, dataDir)

	paths, err := ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}

	if paths.ConfigFile != configPath {
		t.Errorf("ConfigFile = %q, want %q", paths.ConfigFile, configPath)
	}
	if paths.DataDir != dataDir {
		t.Errorf("DataDir = %q, want %q", paths.DataDir, dataDir)
	}
}

// Overrides must be absolute: a relative path would resolve against the
// process working directory, which an operator does not control for a service.
func TestResolvePathsRejectsRelativeOverrides(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		t.Setenv(DevModeEnv, "")
		t.Setenv(ConfigPathEnv, "relative/config.yaml")
		t.Setenv(DataDirEnv, "")

		if _, err := ResolvePaths(); err == nil {
			t.Fatal("relative config override accepted, want error")
		}
	})

	t.Run("data dir", func(t *testing.T) {
		t.Setenv(DevModeEnv, "")
		t.Setenv(ConfigPathEnv, "")
		t.Setenv(DataDirEnv, "relative/state")

		if _, err := ResolvePaths(); err == nil {
			t.Fatal("relative data dir override accepted, want error")
		}
	})
}

func TestResolveInDirAcceptsSafeNames(t *testing.T) {
	base := "/var/lib/infrapilot"

	tests := []struct {
		name string
		want string
	}{
		{"infrapilot.db", "/var/lib/infrapilot/infrapilot.db"},
		{"sub/file.db", "/var/lib/infrapilot/sub/file.db"},
		{"./infrapilot.db", "/var/lib/infrapilot/infrapilot.db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveInDir(base, tt.name)
			if err != nil {
				t.Fatalf("ResolveInDir(%q): %v", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("ResolveInDir(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// Path traversal is the attack this function exists to stop.
func TestResolveInDirRejectsTraversal(t *testing.T) {
	base := "/var/lib/infrapilot"

	names := []string{
		"../etc/shadow",
		"../../etc/shadow",
		"sub/../../outside",
		"..",
		"../",
		"a/b/../../../escape",
		"/etc/shadow",
		"/absolute/path",
		"",
	}

	for _, name := range names {
		t.Run("name="+name, func(t *testing.T) {
			got, err := ResolveInDir(base, name)
			if err == nil {
				t.Fatalf("ResolveInDir(%q) = %q, want error", name, got)
			}
		})
	}
}

// A sibling directory sharing a name prefix must not be treated as a child.
func TestResolveInDirRejectsPrefixSibling(t *testing.T) {
	if got, err := ResolveInDir("/var/lib/infrapilot", "../infrapilot-evil/file"); err == nil {
		t.Fatalf("ResolveInDir returned %q, want error for a prefix sibling", got)
	}
}

func TestResolveInDirRejectsBadBase(t *testing.T) {
	for _, base := range []string{"", "relative/base"} {
		if _, err := ResolveInDir(base, "file.db"); err == nil {
			t.Errorf("ResolveInDir(%q, ...) succeeded, want error", base)
		}
	}
}
