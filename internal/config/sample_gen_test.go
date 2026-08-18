package config

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/system"
)

// update regenerates files this package is the source of truth for, rather
// than asserting they match. It exists so a changed default is a one-command
// fix instead of a manual edit that can be forgotten.
var update = flag.Bool("update", false, "regenerate the shipped sample configuration")

// sampleFile is the annotated configuration the installer ships, relative to
// the repository root.
const sampleFile = "../../installer/config.sample.yaml"

// TestSampleConfigIsCurrent keeps the shipped sample identical to what
// Sample renders from the real defaults.
//
// A sample that drifts from the code teaches operators the wrong values, so it
// is generated rather than maintained by hand. Run "go test ./internal/config
// -update" to regenerate it after changing a default.
func TestSampleConfigIsCurrent(t *testing.T) {
	want, err := Sample(system.SystemDataDir)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if *update {
		if err := os.MkdirAll(filepath.Dir(sampleFile), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(sampleFile, []byte(want), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		t.Logf("regenerated %s", sampleFile)
		return
	}

	got, err := os.ReadFile(sampleFile)
	if err != nil {
		t.Fatalf("read %s: %v (run: go test ./internal/config -update)", sampleFile, err)
	}

	if string(got) != want {
		t.Errorf("%s is out of date; run: go test ./internal/config -update", sampleFile)
	}
}

// The shipped sample must be valid input: an installer that writes a file the
// Agent then refuses to load would break the first start.
func TestSampleConfigLoads(t *testing.T) {
	data, err := os.ReadFile(sampleFile)
	if err != nil {
		t.Skipf("no sample file yet: %v", err)
	}

	root := t.TempDir()
	configFile := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configFile, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The sample names the production data directory, which does not exist
	// here. Loading must still succeed: Load validates values, and creating
	// directories is the Agent's job.
	cfg, err := Load(system.Paths{ConfigFile: configFile, DataDir: root})
	if err != nil {
		t.Fatalf("the shipped sample does not load: %v", err)
	}

	if cfg.Agent.DataDir != system.SystemDataDir {
		t.Errorf("data_dir = %q, want %q", cfg.Agent.DataDir, system.SystemDataDir)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("the shipped sample does not validate: %v", err)
	}
}
