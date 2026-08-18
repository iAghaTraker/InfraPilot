package cli

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/identity"
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

func TestSKDeleteRequiresConfirmation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	env := Env{Paths: system.Paths{DataDir: dir}}
	if _, _, err := identity.Create(dir); err != nil {
		t.Fatal(err)
	}
	err := runSK(context.Background(), env, []string{"delete"}, IO{Out: &strings.Builder{}})
	if !errors.IsKind(err, errors.KindUsage) || !strings.Contains(err.Error(), "cannot be recovered") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "device.key")); err != nil {
		t.Fatalf("identity was deleted without confirmation: %v", err)
	}
}

func TestSKDeleteRemovesOnlyLocalIdentityAndAudits(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "data")
	dbPath := filepath.Join(root, "infrapilot.db")
	env := Env{
		Config: config.Config{Storage: config.StorageConfig{Path: dbPath, BusyTimeout: time.Second}},
		Paths:  system.Paths{DataDir: dir},
	}
	i, _, err := identity.Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := openIdentityDB(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec("INSERT INTO device_identities(device_id,public_key,created_at,status) VALUES(?,?,?,?)", "remote-device", make([]byte, 32), time.Now().UTC().Format(time.RFC3339Nano), "active"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var out strings.Builder
	if err := runSK(context.Background(), env, []string{"reset", "--confirm"}, IO{Out: &out}); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Load(dir); !errors.IsKind(err, errors.KindNotFound) {
		t.Fatalf("local identity still exists: %v", err)
	}
	db, err = openIdentityDB(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	if err := db.SQL().QueryRow("SELECT status FROM device_identities WHERE device_id='remote-device'").Scan(&status); err != nil || status != "active" {
		t.Fatalf("remote device changed: status=%q err=%v", status, err)
	}
	var auditedID string
	if err := db.SQL().QueryRow("SELECT device_id FROM security_audit WHERE event_type='local_identity_deleted'").Scan(&auditedID); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if auditedID != i.DeviceID {
		t.Fatalf("audit device = %q, want %q", auditedID, i.DeviceID)
	}
	if !strings.Contains(out.String(), "trusted remote devices were not changed") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestSKHelpListsIdentityCommands(t *testing.T) {
	var out strings.Builder
	if err := runSK(context.Background(), Env{}, []string{"help"}, IO{Out: &out}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"create", "status", "revoke", "delete", "reset", "list"} {
		if !strings.Contains(out.String(), command) {
			t.Errorf("help missing %q: %s", command, out.String())
		}
	}
}
