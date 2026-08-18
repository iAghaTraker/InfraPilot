// Package errors provides InfraPilot's error model.
//
// The model exists to satisfy three requirements that plain fmt.Errorf does
// not cover on its own:
//
//   - Messages must be actionable. "failed to initialize database: permission
//     denied" is useful; "something went wrong" is not.
//   - Callers must be able to react to a *category* of failure without string
//     matching, so that (for example) the CLI can pick an exit code and the
//     Agent can decide whether a failure is fatal.
//   - Internal detail must not leak into user-facing output. The logical
//     operation that failed is recorded separately from the message, so logs
//     can carry it while terminal output stays clean.
//
// Errors wrap their cause, so the standard library's errors.Is and errors.As
// keep working. Use them; this package deliberately does not reimplement them.
package errors

import (
	stderrors "errors"
	"fmt"
	"strings"
)

// Kind classifies a failure so callers can react without inspecting messages.
type Kind uint8

// Error kinds. Keep this list small: a kind earns its place only when some
// caller genuinely behaves differently because of it.
const (
	// KindUnknown is the zero value, used for errors that carry no
	// classification (typically errors from outside InfraPilot).
	KindUnknown Kind = iota

	// KindUsage means the operator invoked something incorrectly, for example
	// an unknown CLI subcommand. It is the only kind that maps to a distinct
	// process exit code.
	KindUsage

	// KindConfig means configuration could not be read, parsed or validated.
	KindConfig

	// KindValidation means supplied input failed a semantic check.
	KindValidation

	// KindPermission means the process lacks the rights to do something, or
	// found permissions on disk that are unsafe to use.
	KindPermission

	// KindNotFound means a required file, directory or record is absent.
	KindNotFound

	// KindStorage means a database operation failed.
	KindStorage

	// KindUnsupported means the host environment cannot support the operation,
	// for example an unsupported operating system.
	KindUnsupported

	// KindInternal means an invariant was violated. It indicates a bug.
	KindInternal
)

// String returns a short stable identifier for the kind, suitable for use as
// structured log context.
func (k Kind) String() string {
	switch k {
	case KindUsage:
		return "usage"
	case KindConfig:
		return "config"
	case KindValidation:
		return "validation"
	case KindPermission:
		return "permission"
	case KindNotFound:
		return "not_found"
	case KindStorage:
		return "storage"
	case KindUnsupported:
		return "unsupported"
	case KindInternal:
		return "internal"
	default:
		return "unknown"
	}
}

// Error is an InfraPilot error.
//
// Msg is user-facing and must never contain a credential. Op is the logical
// operation that failed (for example "storage.Open"); it is intended for logs
// and is deliberately excluded from Error so that internal package layout is
// not printed to operators.
type Error struct {
	Op   string
	Kind Kind
	Msg  string
	Err  error
}

// Error implements the error interface. It renders as "message: cause",
// matching the convention that each layer adds context to the layer below.
func (e *Error) Error() string {
	switch {
	case e.Msg == "" && e.Err == nil:
		// Defensive: an Error with neither is a programming mistake, but
		// returning "" would produce a silent, untraceable failure.
		return "unspecified error in " + fallback(e.Op, "unknown operation")
	case e.Msg == "":
		return e.Err.Error()
	case e.Err == nil:
		return e.Msg
	default:
		return e.Msg + ": " + e.Err.Error()
	}
}

// Unwrap exposes the cause so errors.Is and errors.As traverse the chain.
func (e *Error) Unwrap() error { return e.Err }

// New builds an error with no underlying cause.
func New(kind Kind, op, msg string) *Error {
	return &Error{Op: op, Kind: kind, Msg: msg}
}

// Newf builds an error with no underlying cause, formatting the message.
//
// Take care not to interpolate secrets. Format only values that are safe to
// show an operator, such as paths, counts and names.
func Newf(kind Kind, op, format string, args ...any) *Error {
	return &Error{Op: op, Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// Wrap annotates cause with an operation, a kind and a user-facing message.
//
// Wrap returns nil when cause is nil, so it is safe to use in a
// `return Wrap(...)` tail position without a preceding nil check.
func Wrap(kind Kind, op, msg string, cause error) error {
	if cause == nil {
		return nil
	}
	return &Error{Op: op, Kind: kind, Msg: msg, Err: cause}
}

// Wrapf is Wrap with a formatted message. It returns nil when cause is nil.
func Wrapf(kind Kind, op string, cause error, format string, args ...any) error {
	if cause == nil {
		return nil
	}
	return &Error{Op: op, Kind: kind, Msg: fmt.Sprintf(format, args...), Err: cause}
}

// KindOf reports the kind of the outermost classified error in err's chain.
// It returns KindUnknown for nil and for errors that carry no classification.
func KindOf(err error) Kind {
	var e *Error
	for err != nil {
		if stderrors.As(err, &e) {
			if e.Kind != KindUnknown {
				return e.Kind
			}
			// Classified as unknown; keep looking further down the chain.
			err = e.Unwrap()
			continue
		}
		return KindUnknown
	}
	return KindUnknown
}

// IsKind reports whether err's chain carries the given kind.
func IsKind(err error, kind Kind) bool { return KindOf(err) == kind }

// OpOf returns the operation of the outermost InfraPilot error in err's chain,
// or "" if there is none. Intended for structured logging.
func OpOf(err error) string {
	var e *Error
	if stderrors.As(err, &e) {
		return e.Op
	}
	return ""
}

// Chain returns the operations recorded along err's chain, outermost first.
// It is useful as a single structured log field that shows the failure path
// without printing it to the operator.
func Chain(err error) string {
	var ops []string
	for err != nil {
		var e *Error
		if !stderrors.As(err, &e) {
			break
		}
		if e.Op != "" {
			ops = append(ops, e.Op)
		}
		err = e.Unwrap()
	}
	return strings.Join(ops, " <- ")
}

// Process exit codes. These are the only codes InfraPilot binaries return.
const (
	// ExitOK signals success.
	ExitOK = 0
	// ExitFailure signals that the requested operation did not succeed.
	ExitFailure = 1
	// ExitUsage signals that the invocation itself was wrong.
	ExitUsage = 2
)

// ExitCode maps err to a process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if IsKind(err, KindUsage) {
		return ExitUsage
	}
	return ExitFailure
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
