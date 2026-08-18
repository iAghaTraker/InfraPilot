package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{in: "debug", want: slog.LevelDebug},
		{in: "info", want: slog.LevelInfo},
		{in: "warn", want: slog.LevelWarn},
		{in: "warning", want: slog.LevelWarn},
		{in: "error", want: slog.LevelError},
		{in: "INFO", want: slog.LevelInfo},
		{in: "  info  ", want: slog.LevelInfo},
		{in: "", wantErr: true},
		{in: "trace", wantErr: true},
		{in: "verbose", wantErr: true},
	}

	for _, tt := range tests {
		t.Run("level="+tt.in, func(t *testing.T) {
			got, err := ParseLevel(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) succeeded, want error", tt.in)
				}
				if !errors.IsKind(err, errors.KindValidation) {
					t.Errorf("kind = %v, want validation", errors.KindOf(err))
				}
				// The message must tell the operator what is allowed.
				if !strings.Contains(err.Error(), "debug, info, warn, error") {
					t.Errorf("error %q does not list valid levels", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q) = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{in: "", want: FormatText},
		{in: "text", want: FormatText},
		{in: "json", want: FormatJSON},
		{in: "JSON", want: FormatJSON},
		{in: "xml", wantErr: true},
	}

	for _, tt := range tests {
		t.Run("format="+tt.in, func(t *testing.T) {
			got, err := ParseFormat(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseFormat(%q) succeeded, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFormat(%q) = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseFormat(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewRequiresOutput(t *testing.T) {
	if _, err := New(Options{Level: "info"}); err == nil {
		t.Fatal("New without output succeeded, want error")
	}
}

func TestNewRejectsBadLevel(t *testing.T) {
	var buf bytes.Buffer
	if _, err := New(Options{Level: "loud", Output: &buf}); err == nil {
		t.Fatal("New with bad level succeeded, want error")
	}
}

// A log record must carry timestamp, level, message and structured context.
func TestRecordContainsRequiredFields(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Level: "info", Format: FormatJSON, Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Info("storage ready", slog.String("path", "/var/lib/infrapilot/infrapilot.db"))

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log output is not valid JSON: %v (%q)", err, buf.String())
	}

	for _, field := range []string{slog.TimeKey, slog.LevelKey, slog.MessageKey, "path"} {
		if _, ok := rec[field]; !ok {
			t.Errorf("record is missing %q: %v", field, rec)
		}
	}
	if rec[slog.MessageKey] != "storage ready" {
		t.Errorf("msg = %v, want %q", rec[slog.MessageKey], "storage ready")
	}
	if rec[slog.LevelKey] != "INFO" {
		t.Errorf("level = %v, want INFO", rec[slog.LevelKey])
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Level: "warn", Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Debug("debug message")
	log.Info("info message")
	if buf.Len() != 0 {
		t.Errorf("records below the threshold were emitted: %q", buf.String())
	}

	log.Warn("warn message")
	log.Error("error message")
	out := buf.String()
	if !strings.Contains(out, "warn message") || !strings.Contains(out, "error message") {
		t.Errorf("records at or above the threshold were dropped: %q", out)
	}
}

func TestAllLevelsAreUsable(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Level: "debug", Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")

	if got := strings.Count(strings.TrimSpace(buf.String()), "\n") + 1; got != 4 {
		t.Errorf("emitted %d records, want 4: %q", got, buf.String())
	}
}

func TestComponentTagsRecords(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Level: "info", Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	Component(log, "storage").Info("ready")
	if !strings.Contains(buf.String(), "component=storage") {
		t.Errorf("component tag missing: %q", buf.String())
	}
}

func TestComponentHandlesNilLogger(t *testing.T) {
	// Must not panic: callers should not need a nil check.
	Component(nil, "storage").Info("ready")
}

func TestDiscardWritesNothing(t *testing.T) {
	// Also must not panic at any level.
	log := Discard()
	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")
}

func TestErrorAttrs(t *testing.T) {
	if got := ErrorAttrs(nil); got != nil {
		t.Errorf("ErrorAttrs(nil) = %v, want nil", got)
	}

	var buf bytes.Buffer
	log, err := New(Options{Level: "error", Format: FormatJSON, Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cause := errors.New(errors.KindPermission, "system.EnsureDir", "cannot create directory")
	wrapped := errors.Wrap(errors.KindStorage, "storage.Open", "failed to initialize database", cause)
	log.Error("startup failed", ErrorAttrs(wrapped)...)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if want := "failed to initialize database: cannot create directory"; rec["error"] != want {
		t.Errorf("error = %v, want %q", rec["error"], want)
	}
	if rec["error_kind"] != "storage" {
		t.Errorf("error_kind = %v, want storage", rec["error_kind"])
	}
	if want := "storage.Open <- system.EnsureDir"; rec["error_op"] != want {
		t.Errorf("error_op = %v, want %q", rec["error_op"], want)
	}
}
