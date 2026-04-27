package llm

import "errors"

// Sentinel errors for standardized error handling across providers.
var (
	ErrRateLimit        = errors.New("llm: rate limit exceeded")
	ErrAuth             = errors.New("llm: authentication failed")
	ErrTimeout          = errors.New("llm: request timeout")
	ErrModelUnavailable = errors.New("llm: model unavailable")
)

// IsRateLimit checks if the error is a rate limit error.
func IsRateLimit(err error) bool { return errors.Is(err, ErrRateLimit) }

// IsAuth checks if the error is an authentication error.
func IsAuth(err error) bool { return errors.Is(err, ErrAuth) }
