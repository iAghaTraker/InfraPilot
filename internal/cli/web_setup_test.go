package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveBindPreservesConfigurationAndWritesWebSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("agent:\n  shutdown_timeout: 20s\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := saveBind(path, "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "shutdown_timeout: 20s") || !strings.Contains(text, "bind_address: 0.0.0.0") {
		t.Fatalf("unexpected config: %s", text)
	}
}
