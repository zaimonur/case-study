package mealai

import "errors"

// ErrorKind is a stable meal-interpretation failure category.
type ErrorKind string

const (
	ErrorInvalidInput      ErrorKind = "invalid_input"
	ErrorAIUnavailable     ErrorKind = "ai_unavailable"
	ErrorAIRateLimited     ErrorKind = "ai_rate_limited"
	ErrorAITimeout         ErrorKind = "ai_timeout"
	ErrorAIInvalidResponse ErrorKind = "ai_invalid_response"
	ErrorAIFailure         ErrorKind = "ai_failure"
	ErrorFoodNotFound      ErrorKind = "food_not_found"
	ErrorPortionNotFound   ErrorKind = "portion_not_found"
	ErrorResolutionFailure ErrorKind = "resolution_failure"
	ErrorCanceled          ErrorKind = "canceled"
	ErrorTimeout           ErrorKind = "timeout"
)

// Error preserves a cause without exposing dependency details in its message.
type Error struct {
	Kind ErrorKind
	err  error
}

func (e *Error) Error() string { return string(e.Kind) }

func (e *Error) Unwrap() error { return e.err }

// IsKind reports whether err belongs to the requested interpretation category.
func IsKind(err error, kind ErrorKind) bool {
	var interpretationError *Error
	return errors.As(err, &interpretationError) && interpretationError.Kind == kind
}

func newError(kind ErrorKind, cause error) error {
	return &Error{Kind: kind, err: cause}
}
