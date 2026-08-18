package version

import (
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestVersionIsSemver(t *testing.T) {
	parts := strings.Split(Version, ".")
	if len(parts) != 3 {
		t.Fatalf("Version %q is not MAJOR.MINOR.PATCH", Version)
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			t.Errorf("Version %q has non-numeric component %q", Version, p)
		}
	}
}

func TestCurrentReportsRuntime(t *testing.T) {
	b := Current()

	if b.Version != Version {
		t.Errorf("Version = %q, want %q", b.Version, Version)
	}
	if b.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", b.OS, runtime.GOOS)
	}
	if b.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", b.Arch, runtime.GOARCH)
	}
	if b.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", b.GoVersion, runtime.Version())
	}
}
