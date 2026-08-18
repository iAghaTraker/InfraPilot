package logging

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// theSecret is a distinctive value; any appearance of it in log output is a
// leak, regardless of how it got there.
const theSecret = "s3cr3t-must-never-appear"

func newTestLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log, err := New(Options{Level: "debug", Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return log, &buf
}

func assertNoLeak(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	out := buf.String()
	if strings.Contains(out, theSecret) {
		t.Errorf("secret leaked into log output: %q", out)
	}
	if !strings.Contains(out, Redacted) {
		t.Errorf("output does not contain the redaction placeholder: %q", out)
	}
}

// The Secret type is the explicit contract, and must hold under any key.
func TestSecretTypeIsRedactedUnderAnyKey(t *testing.T) {
	log, buf := newTestLogger(t)
	log.Info("paired", slog.Any("innocuous_field_name", Secret(theSecret)))
	assertNoLeak(t, buf)
}

// Secret must also resist fmt, the most likely accidental leak path (for
// example being interpolated into an error message).
func TestSecretResistsFmt(t *testing.T) {
	s := Secret(theSecret)

	for _, got := range []string{
		fmt.Sprintf("%v", s),
		fmt.Sprintf("%s", s),
		s.String(),
	} {
		if strings.Contains(got, theSecret) {
			t.Errorf("fmt leaked the secret: %q", got)
		}
	}
}

func TestSensitiveKeysAreRedacted(t *testing.T) {
	keys := []string{
		"password", "Password", "PASSWORD", "db_password",
		"passwd", "passphrase",
		"secret", "client_secret",
		"token", "access_token", "refresh_token", "pairing_token",
		"credential", "credentials",
		"apikey", "api_key", "API_KEY",
		"private_key", "privatekey", "signing_key", "session_key",
		"authorization", "auth_header",
	}

	for _, key := range keys {
		t.Run("key="+key, func(t *testing.T) {
			log, buf := newTestLogger(t)
			log.Info("event", slog.String(key, theSecret))
			assertNoLeak(t, buf)
		})
	}
}

// Redaction must survive the two paths that bypass Handle's attribute walk:
// pre-bound attributes (With) and groups.
func TestRedactionAppliesToBoundAttributes(t *testing.T) {
	log, buf := newTestLogger(t)
	log.With(slog.String("api_key", theSecret)).Info("event")
	assertNoLeak(t, buf)
}

func TestRedactionAppliesInsideGroups(t *testing.T) {
	log, buf := newTestLogger(t)
	log.Info("event", slog.Group("database",
		slog.String("host", "localhost"),
		slog.String("password", theSecret),
	))

	assertNoLeak(t, buf)
	// Non-sensitive siblings must survive, otherwise the group is useless.
	if !strings.Contains(buf.String(), "localhost") {
		t.Errorf("non-sensitive value in group was lost: %q", buf.String())
	}
}

func TestRedactionAppliesInNestedGroups(t *testing.T) {
	log, buf := newTestLogger(t)
	log.Info("event", slog.Group("outer",
		slog.Group("inner", slog.String("token", theSecret)),
	))
	assertNoLeak(t, buf)
}

func TestRedactionAppliesWithinWithGroup(t *testing.T) {
	log, buf := newTestLogger(t)
	log.WithGroup("auth").Info("event", slog.String("token", theSecret))
	assertNoLeak(t, buf)
}

func TestNonSensitiveValuesArePreserved(t *testing.T) {
	log, buf := newTestLogger(t)
	log.Info("storage ready",
		slog.String("path", "/var/lib/infrapilot/infrapilot.db"),
		slog.Int("schema_version", 1),
		// These contain "key" but are not credentials; redacting them would
		// make logs useless without improving security.
		slog.String("cache_key", "abc123"),
		slog.Int("key_count", 4),
	)

	out := buf.String()
	for _, want := range []string{"/var/lib/infrapilot/infrapilot.db", "schema_version=1", "abc123", "key_count=4"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output: %q", want, out)
		}
	}
	if strings.Contains(out, Redacted) {
		t.Errorf("harmless attributes were redacted: %q", out)
	}
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"password", true},
		{"db_password", true},
		{"TOKEN", true},
		{"private_key", true},
		{"", false},
		{"path", false},
		{"key", false},
		{"cache_key", false},
		{"level", false},
		{"component", false},
	}

	for _, tt := range tests {
		if got := isSensitiveKey(tt.key); got != tt.want {
			t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

// Redaction must not silently disable level filtering.
func TestRedactHandlerRespectsEnabled(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Level: "error", Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if log.Enabled(nil, slog.LevelInfo) { //nolint:staticcheck // nil context is valid here
		t.Error("info is enabled on an error-level logger")
	}
	if !log.Enabled(nil, slog.LevelError) { //nolint:staticcheck // nil context is valid here
		t.Error("error is not enabled on an error-level logger")
	}
}
