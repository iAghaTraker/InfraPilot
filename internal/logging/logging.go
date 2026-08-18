// Package logging provides InfraPilot's structured logging.
//
// It is a thin layer over log/slog from the standard library. The layer earns
// its place by supplying three things the Agent and CLI both need:
//
//   - a single place where log level and output format are decided, so
//     configuration is not re-parsed at each call site;
//   - automatic redaction of attributes that look like credentials, so a
//     mistake in one package cannot write a secret to disk (see redact.go);
//   - a Discard logger, so tests never emit noise and never need nil checks.
//
// The API is deliberately *slog.Logger rather than a bespoke interface. slog
// is already an abstraction; wrapping it in another one would add indirection
// without adding capability, and would prevent callers from using the standard
// slog helpers.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// Format selects how log records are encoded.
type Format string

const (
	// FormatText is a human-readable key=value encoding. It is the default for
	// interactive use.
	FormatText Format = "text"

	// FormatJSON is a machine-readable encoding, appropriate when logs are
	// shipped to journald and then to a log aggregator.
	FormatJSON Format = "json"
)

// Options configures a logger.
type Options struct {
	// Level is one of debug, info, warn, error. Case-insensitive.
	Level string

	// Format is text or json. Empty means FormatText.
	Format Format

	// Output is where records are written. Required.
	Output io.Writer

	// AddSource includes the source file and line in each record. It is
	// intended for development; it costs a stack lookup per record.
	AddSource bool
}

// ParseLevel converts a configured level name to a slog.Level.
//
// It is exported because configuration validation needs to reject a bad level
// before a logger is ever constructed.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, errors.Newf(errors.KindValidation, "logging.ParseLevel",
			"unsupported log level %q: must be one of debug, info, warn, error", name)
	}
}

// ParseFormat converts a configured format name to a Format.
func ParseFormat(name string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", string(FormatText):
		return FormatText, nil
	case string(FormatJSON):
		return FormatJSON, nil
	default:
		return "", errors.Newf(errors.KindValidation, "logging.ParseFormat",
			"unsupported log format %q: must be one of text, json", name)
	}
}

// New builds a logger from opts.
//
// Every logger returned by New redacts sensitive attributes.
func New(opts Options) (*slog.Logger, error) {
	const op = "logging.New"

	if opts.Output == nil {
		return nil, errors.New(errors.KindInternal, op, "log output writer is required")
	}

	level, err := ParseLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	format, err := ParseFormat(string(opts.Format))
	if err != nil {
		return nil, err
	}

	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.AddSource,
	}

	var handler slog.Handler
	switch format {
	case FormatJSON:
		handler = slog.NewJSONHandler(opts.Output, handlerOpts)
	default:
		handler = slog.NewTextHandler(opts.Output, handlerOpts)
	}

	return slog.New(NewRedactHandler(handler)), nil
}

// Discard returns a logger that writes nothing.
//
// Tests and short-lived CLI commands use it so that code depending on a logger
// never has to guard against nil.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// Component returns a child logger tagged with the subsystem it belongs to,
// so records can be filtered by origin.
func Component(logger *slog.Logger, name string) *slog.Logger {
	if logger == nil {
		return Discard()
	}
	return logger.With(slog.String("component", name))
}

// ErrorAttrs renders err as structured log context.
//
// It records the message, the error kind and the failure path recorded by
// internal/errors. This is the one place that decides how errors appear in
// logs, so the shape stays consistent across the codebase.
func ErrorAttrs(err error) []any {
	if err == nil {
		return nil
	}

	attrs := []any{
		slog.String("error", err.Error()),
		slog.String("error_kind", errors.KindOf(err).String()),
	}
	if chain := errors.Chain(err); chain != "" {
		attrs = append(attrs, slog.String("error_op", chain))
	}
	return attrs
}

// Verify at compile time that Secret cannot silently lose its protection.
var (
	_ slog.LogValuer = Secret("")
	_ fmt.Stringer   = Secret("")
)
