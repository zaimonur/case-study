package foodextraction

import (
	"errors"
	"fmt"
)

// ErrorKind is a stable application-level extraction failure category.
type ErrorKind string

const (
	ErrorInvalidInput          ErrorKind = "invalid_input"
	ErrorInvalidProviderOutput ErrorKind = "invalid_provider_output"
	ErrorProviderConfiguration ErrorKind = "provider_configuration"
	ErrorTimeout               ErrorKind = "timeout"
	ErrorCanceled              ErrorKind = "canceled"
	ErrorRateLimit             ErrorKind = "rate_limit"
	ErrorProviderFailure       ErrorKind = "provider_failure"
)

// Error preserves a controlled category while retaining an internal cause.
type Error struct {
	Kind ErrorKind
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return string(e.Kind)
	}
	return fmt.Sprintf("%s: %v", e.Kind, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// IsKind reports whether err belongs to the requested controlled category.
func IsKind(err error, kind ErrorKind) bool {
	var extractionError *Error
	return errors.As(err, &extractionError) && extractionError.Kind == kind
}

// NewError creates a categorized error for extractor adapters.
func NewError(kind ErrorKind, err error) error {
	return &Error{Kind: kind, Err: err}
}
