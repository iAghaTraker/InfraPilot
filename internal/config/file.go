package config

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// fileConfig mirrors Config as it appears on disk.
//
// It exists for two reasons:
//
//   - Durations are written as human strings ("15s"), which yaml.v3 cannot
//     decode into time.Duration on its own. Parsing them here reuses exactly
//     the same rules as the environment overrides, so `15s` and `15` mean the
//     same thing wherever they appear.
//   - Every field is a pointer, so "absent" is distinguishable from "set to the
//     zero value". Without that, a file that omits logging.level would reset it
//     rather than inherit the default.
type fileConfig struct {
	Version *int `yaml:"version"`

	Agent *struct {
		DataDir           *string `yaml:"data_dir"`
		ShutdownTimeout   *string `yaml:"shutdown_timeout"`
		HeartbeatInterval *string `yaml:"heartbeat_interval"`
	} `yaml:"agent"`

	Logging *struct {
		Level  *string `yaml:"level"`
		Format *string `yaml:"format"`
	} `yaml:"logging"`

	Storage *struct {
		Path        *string `yaml:"path"`
		BusyTimeout *string `yaml:"busy_timeout"`
	} `yaml:"storage"`
}

// SchemaVersion is the configuration format version.
//
// It is recorded in generated files so that a future release can migrate or
// reject an old format deliberately instead of misreading it.
const SchemaVersion = 1

// decodeFile parses YAML into a fileConfig, rejecting unknown keys.
func decodeFile(path string, data []byte) (fileConfig, error) {
	const op = "config.decodeFile"

	var fc fileConfig

	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	// An unrecognised key is an error: silently ignoring one means an operator
	// believes a setting took effect when it did not. A typo in a security
	// setting would then fail open.
	decoder.KnownFields(true)

	if err := decoder.Decode(&fc); err != nil {
		// An empty document yields io.EOF, which simply means "no overrides".
		if err.Error() == "EOF" {
			return fileConfig{}, nil
		}
		// Newf rather than Wrapf: the cleaned message already states everything
		// the yaml error says that an operator can act on, and wrapping would
		// append the raw form back onto it.
		return fileConfig{}, errors.Newf(errors.KindConfig, op,
			"cannot parse configuration file %s: %s", path, cleanYAMLError(err))
	}

	if fc.Version != nil && *fc.Version != SchemaVersion {
		return fileConfig{}, errors.Newf(errors.KindConfig, op,
			"unsupported configuration version %d in %s: this build understands version %d",
			*fc.Version, path, SchemaVersion)
	}

	return fc, nil
}

// unknownFieldPattern matches yaml.v3's unknown-key message, whose tail is the
// Go type it was decoding into.
// The type is rendered with its fields and tags, so it contains spaces and runs
// to the end of the line.
var unknownFieldPattern = regexp.MustCompile(`field (\S+) not found in type .*`)

// cleanYAMLError turns a yaml.v3 error into something an operator can act on.
//
// yaml.v3 reports an unknown key by printing the Go struct it was decoding
// into, tags and all. That tells the reader nothing about their file, and
// exposes internal types in a message an operator sees. The line number and the
// offending key are the useful parts, so they are kept and the type is dropped.
func cleanYAMLError(err error) string {
	message := strings.TrimPrefix(err.Error(), "yaml: unmarshal errors:\n")

	var cleaned []string
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := unknownFieldPattern.FindStringSubmatch(line); m != nil {
			line = unknownFieldPattern.ReplaceAllString(line,
				"unknown setting "+strconv.Quote(m[1]))
		}
		cleaned = append(cleaned, line)
	}

	if len(cleaned) == 0 {
		return err.Error()
	}
	return strings.Join(cleaned, "; ")
}

// overlay applies the values present in fc onto cfg.
func (fc fileConfig) overlay(cfg *Config, path string) error {
	if fc.Agent != nil {
		if fc.Agent.DataDir != nil {
			cfg.Agent.DataDir = *fc.Agent.DataDir
		}
		if err := setDuration(&cfg.Agent.ShutdownTimeout, fc.Agent.ShutdownTimeout, "agent.shutdown_timeout", path); err != nil {
			return err
		}
		if err := setDuration(&cfg.Agent.HeartbeatInterval, fc.Agent.HeartbeatInterval, "agent.heartbeat_interval", path); err != nil {
			return err
		}
	}

	if fc.Logging != nil {
		if fc.Logging.Level != nil {
			cfg.Logging.Level = *fc.Logging.Level
		}
		if fc.Logging.Format != nil {
			cfg.Logging.Format = *fc.Logging.Format
		}
	}

	if fc.Storage != nil {
		if fc.Storage.Path != nil {
			cfg.Storage.Path = *fc.Storage.Path
		}
		if err := setDuration(&cfg.Storage.BusyTimeout, fc.Storage.BusyTimeout, "storage.busy_timeout", path); err != nil {
			return err
		}
	}

	return nil
}

// setDuration parses raw into target when raw is present.
func setDuration(target *time.Duration, raw *string, field, path string) error {
	if raw == nil {
		return nil
	}

	value := strings.TrimSpace(*raw)
	if value == "" {
		return errors.Newf(errors.KindConfig, "config.setDuration",
			"%s in %s is empty: remove the key to use the default, or give a value such as 30s", field, path)
	}

	parsed, err := parseDuration(field, value)
	if err != nil {
		return errors.Wrapf(errors.KindConfig, "config.setDuration", err,
			"invalid %s in %s", field, path)
	}

	*target = parsed
	return nil
}
