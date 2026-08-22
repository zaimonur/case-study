package foodamount

import "errors"

// ErrorKind is a stable amount-resolution failure category.
type ErrorKind string

const (
	ErrorInvalidInput    ErrorKind = "invalid_input"
	ErrorFoodNotFound    ErrorKind = "food_not_found"
	ErrorPortionNotFound ErrorKind = "portion_not_found"
	ErrorDetailFailure   ErrorKind = "detail_failure"
	ErrorCanceled        ErrorKind = "canceled"
	ErrorTimeout         ErrorKind = "timeout"
)

// Error preserves a cause while keeping its public message implementation-safe.
type Error struct {
	Kind ErrorKind
	err  error
}

func (e *Error) Error() string { return string(e.Kind) }

func (e *Error) Unwrap() error { return e.err }

// IsKind reports whether err belongs to the requested amount-resolution category.
func IsKind(err error, kind ErrorKind) bool {
	var amountError *Error
	return errors.As(err, &amountError) && amountError.Kind == kind
}

func newError(kind ErrorKind, cause error) error {
	return &Error{Kind: kind, err: cause}
}
