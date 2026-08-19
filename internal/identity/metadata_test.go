package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataFindsIdentityWithoutPrivateKey(t *testing.T) {
	dir := t.TempDir()
	id, _, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "device.key"), 0); err != nil {
		t.Fatal(err)
	}
	got, err := Metadata(dir)
	if err != nil || got != id.DeviceID {
		t.Fatalf("Metadata()=(%q,%v), want %q", got, err, id.DeviceID)
	}
}
