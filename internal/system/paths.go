package system

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// Production filesystem locations on Linux.
//
// These follow the Filesystem Hierarchy Standard: configuration in /etc,
// variable state in /var/lib. They are constants rather than configuration
// because the location of the configuration file cannot itself be configured
// in the file it points to.
const (
	// SystemConfigDir holds InfraPilot configuration.
	SystemConfigDir = "/etc/infrapilot"

	// SystemDataDir holds InfraPilot state, including the database.
	SystemDataDir = "/var/lib/infrapilot"

	// ConfigFileName is the configuration file name in either mode.
	ConfigFileName = "config.yaml"

	// ServiceUser is the unprivileged account the Agent runs as. The Agent
	// must not run as root; see installer/systemd/infrapilot-agent.service.
	ServiceUser = "infrapilot"
)

// DevModeEnv, when set to a truthy value, switches path resolution to
// user-local directories so a developer can run InfraPilot without root.
const DevModeEnv = "INFRAPILOT_DEV"

// Environment variables that override individual paths. These exist so tests
// and packagers can relocate state without editing a configuration file.
const (
	ConfigPathEnv = "INFRAPILOT_CONFIG"
	DataDirEnv    = "INFRAPILOT_DATA_DIR"
)

// Paths holds the resolved filesystem locations for one InfraPilot process.
type Paths struct {
	// ConfigFile is the configuration file path.
	ConfigFile string

	// DataDir is the directory holding InfraPilot state.
	DataDir string

	// DevMode reports whether user-local paths are in use.
	DevMode bool
}

// PIDFile is the path at which the agent advertises that it is running.
//
// It lives in the data directory rather than /run so that development mode and
// production share one rule, and so no extra directory needs provisioning.
func (p Paths) PIDFile() string {
	if p.DataDir == "" {
		return ""
	}
	return filepath.Join(p.DataDir, PIDFileName)
}

// DevModeEnabled reports whether development mode is requested.
//
// Any value other than the empty string, "0", "false" and "no" enables it,
// so both INFRAPILOT_DEV=1 and INFRAPILOT_DEV=true work.
func DevModeEnabled() bool {
	return truthy(os.Getenv(DevModeEnv))
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// ResolvePaths determines where configuration and state live.
//
// Resolution order, highest precedence first:
//
//  1. INFRAPILOT_CONFIG and INFRAPILOT_DATA_DIR, applied independently.
//  2. Development mode (INFRAPILOT_DEV), which uses user-local directories.
//  3. The production locations under /etc and /var/lib.
//
// Overrides must be absolute paths. Accepting relative paths would make the
// resolved location depend on the process working directory, which for a
// systemd service is not something the operator controls.
func ResolvePaths() (Paths, error) {
	const op = "system.ResolvePaths"

	dev := DevModeEnabled()

	configDir, dataDir := SystemConfigDir, SystemDataDir
	if dev {
		var err error
		if configDir, dataDir, err = devDirs(); err != nil {
			return Paths{}, err
		}
	}

	paths := Paths{
		ConfigFile: filepath.Join(configDir, ConfigFileName),
		DataDir:    dataDir,
		DevMode:    dev,
	}

	if override := strings.TrimSpace(os.Getenv(ConfigPathEnv)); override != "" {
		if !filepath.IsAbs(override) {
			return Paths{}, errors.Newf(errors.KindValidation, op,
				"%s must be an absolute path, got %q", ConfigPathEnv, override)
		}
		paths.ConfigFile = filepath.Clean(override)
	}

	if override := strings.TrimSpace(os.Getenv(DataDirEnv)); override != "" {
		if !filepath.IsAbs(override) {
			return Paths{}, errors.Newf(errors.KindValidation, op,
				"%s must be an absolute path, got %q", DataDirEnv, override)
		}
		paths.DataDir = filepath.Clean(override)
	}

	return paths, nil
}

// devDirs returns user-local configuration and data directories, following the
// XDG Base Directory specification.
func devDirs() (configDir, dataDir string, err error) {
	const op = "system.devDirs"

	// os.UserConfigDir and os.UserHomeDir honour XDG_CONFIG_HOME and HOME, and
	// fail with a clear error when neither is set.
	userConfig, err := os.UserConfigDir()
	if err != nil {
		return "", "", errors.Wrap(errors.KindConfig, op,
			"cannot determine the user configuration directory for development mode", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", errors.Wrap(errors.KindConfig, op,
			"cannot determine the home directory for development mode", err)
	}

	// XDG_DATA_HOME, defaulting to ~/.local/share. The standard library has no
	// UserDataDir equivalent, so this is resolved here.
	dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataHome == "" || !filepath.IsAbs(dataHome) {
		dataHome = filepath.Join(home, ".local", "share")
	}

	return filepath.Join(userConfig, "infrapilot"), filepath.Join(dataHome, "infrapilot"), nil
}

// ResolveInDir joins name onto dir, guaranteeing the result stays inside dir.
//
// This is the single guard against path traversal for names that come from
// configuration or, in later versions, from a remote client. A name such as
// "../../etc/shadow" is rejected rather than silently escaping the data
// directory.
func ResolveInDir(dir, name string) (string, error) {
	const op = "system.ResolveInDir"

	if dir == "" {
		return "", errors.New(errors.KindValidation, op, "base directory is empty")
	}
	if !filepath.IsAbs(dir) {
		return "", errors.Newf(errors.KindValidation, op, "base directory must be absolute, got %q", dir)
	}
	if name == "" {
		return "", errors.New(errors.KindValidation, op, "file name is empty")
	}
	if filepath.IsAbs(name) {
		return "", errors.Newf(errors.KindValidation, op,
			"file name must be relative to %s, got absolute path %q", dir, name)
	}

	base := filepath.Clean(dir)
	joined := filepath.Join(base, name)

	// Compare against base + separator so that a sibling directory sharing a
	// prefix (/var/lib/infrapilot-evil) is not mistaken for a child.
	if joined != base && !strings.HasPrefix(joined, base+string(os.PathSeparator)) {
		return "", errors.Newf(errors.KindValidation, op,
			"file name %q resolves outside the base directory %s", name, base)
	}

	return joined, nil
}
