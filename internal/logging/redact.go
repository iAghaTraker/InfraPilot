package logging

import (
	"context"
	"log/slog"
	"strings"
)

// Redacted is the placeholder substituted for a sensitive value.
const Redacted = "[REDACTED]"

// Secret wraps a value that must never reach a log or an error message.
//
// It implements slog.LogValuer, so logging a Secret emits the placeholder no
// matter what attribute key it is given:
//
//	log.Info("authenticated", slog.Any("credential", logging.Secret(token)))
//
// Prefer Secret over relying on key-name matching. Key matching is a safety
// net for values someone forgot to mark; Secret is the explicit contract.
type Secret string

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(Redacted) }

// String keeps a Secret from leaking through fmt's %v and %s verbs, which is
// the most likely accidental path into an error message.
func (Secret) String() string { return Redacted }

// sensitiveKeyFragments are matched case-insensitively as substrings of an
// attribute key. A match redacts the value.
//
// The list intentionally omits the bare word "key", which appears in many
// harmless identifiers such as "cache_key" and "key_count". Compound forms
// that are genuinely sensitive are listed explicitly instead.
var sensitiveKeyFragments = []string{
	"password",
	"passwd",
	"passphrase",
	"secret",
	"token",
	"credential",
	"apikey",
	"api_key",
	"private_key",
	"privatekey",
	"signing_key",
	"session_key",
	"authorization",
	"auth_header",
}

// isSensitiveKey reports whether an attribute key looks like it holds a
// credential.
func isSensitiveKey(key string) bool {
	if key == "" {
		return false
	}
	lower := strings.ToLower(key)
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// redactHandler wraps a slog.Handler and replaces the value of any attribute
// whose key looks sensitive.
//
// This is defence in depth. The primary mechanism is Secret; this catches the
// case where a contributor logs a raw credential under an obvious key. It
// cannot catch a credential logged under an innocuous key, which is why code
// review still matters.
type redactHandler struct {
	inner slog.Handler
}

// NewRedactHandler wraps inner so that sensitive attribute values are replaced
// before they are formatted.
func NewRedactHandler(inner slog.Handler) slog.Handler {
	return &redactHandler{inner: inner}
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	// Rebuild the record with redacted attributes. slog.Record copies are
	// shallow, so a fresh record avoids mutating the caller's attributes.
	clean := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cleaned := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		cleaned[i] = redactAttr(a)
	}
	return &redactHandler{inner: h.inner.WithAttrs(cleaned)}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{inner: h.inner.WithGroup(name)}
}

// redactAttr redacts a single attribute, recursing into groups so that a
// credential nested inside a group is caught too.
func redactAttr(a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}

	// Resolve LogValuer implementations (such as Secret) before inspecting the
	// value, so a group returned by a LogValuer is also walked.
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		group := v.Group()
		cleaned := make([]any, 0, len(group))
		for _, ga := range group {
			cleaned = append(cleaned, redactAttr(ga))
		}
		return slog.Group(a.Key, cleaned...)
	}

	return slog.Attr{Key: a.Key, Value: v}
}
