package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

func TestSKCreateUsesResolvedDefaultDataDirectory(t *testing.T) {
	root := t.TempDir()
	env := Env{
		Config: config.Config{},
		Paths:  system.Paths{DataDir: filepath.Join(root, "data")},
	}
	var out strings.Builder
	if err := runSK(context.Background(), env, []string{"create"}, IO{Out: &out}); err != nil {
		t.Fatalf("runSK create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.Paths.DataDir, "device.key")); err != nil {
		t.Fatalf("default identity key: %v", err)
	}
	if !strings.Contains(out.String(), "Private key: stored securely") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestSKCreateUsesCustomConfiguredDataDirectory(t *testing.T) {
	root := t.TempDir()
	custom := filepath.Join(root, "custom-identity")
	env := Env{
		Config: config.Config{Agent: config.AgentConfig{DataDir: custom}},
		Paths:  system.Paths{DataDir: filepath.Join(root, "default-data")},
	}
	if err := runSK(context.Background(), env, []string{"create"}, IO{Out: &strings.Builder{}}); err != nil {
		t.Fatalf("runSK create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(custom, "device.key")); err != nil {
		t.Fatalf("custom identity key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.Paths.DataDir, "device.key")); !os.IsNotExist(err) {
		t.Fatalf("identity unexpectedly created in default directory: %v", err)
	}
}

func TestSKCreateRejectsRelativeConfiguredDataDirectory(t *testing.T) {
	env := Env{Config: config.Config{Agent: config.AgentConfig{DataDir: "relative"}}, Paths: system.Paths{DataDir: "/absolute/default"}}
	err := runSK(context.Background(), env, []string{"create"}, IO{Out: &strings.Builder{}})
	if !errors.IsKind(err, errors.KindValidation) {
		t.Fatalf("error kind = %v, want validation", errors.KindOf(err))
	}
}
