package foodresolver

import "errors"

// ErrorKind is a stable resolver failure category.
type ErrorKind string

const (
	ErrorInvalidInput  ErrorKind = "invalid_input"
	ErrorSearchFailure ErrorKind = "search_failure"
	ErrorCanceled      ErrorKind = "canceled"
	ErrorTimeout       ErrorKind = "timeout"
)

// Error retains the underlying cause without exposing implementation details
// in its stable message.
type Error struct {
	Kind ErrorKind
	err  error
}

func (e *Error) Error() string { return string(e.Kind) }

func (e *Error) Unwrap() error { return e.err }

// IsKind reports whether err belongs to the requested resolver category.
func IsKind(err error, kind ErrorKind) bool {
	var resolverError *Error
	return errors.As(err, &resolverError) && resolverError.Kind == kind
}

func newError(kind ErrorKind, cause error) error {
	return &Error{Kind: kind, err: cause}
}
