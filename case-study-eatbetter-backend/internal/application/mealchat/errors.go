package mealchat

import (
	"errors"
	"fmt"
)

// ErrorKind is a stable conversational interpretation failure category.
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

// Error retains a controlled category and an internal cause.
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

func IsKind(err error, kind ErrorKind) bool {
	var chatError *Error
	return errors.As(err, &chatError) && chatError.Kind == kind
}

func NewError(kind ErrorKind, err error) error { return &Error{Kind: kind, Err: err} }
