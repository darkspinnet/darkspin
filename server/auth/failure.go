package auth

import "errors"

// loginFailure preserves the diagnostic cause while exposing only a controlled
// explanation suitable for the launcher. Token contents never enter that text.
type loginFailure struct {
	reason  string
	message string
	cause   error
}

func (e *loginFailure) Error() string { return e.cause.Error() }
func (e *loginFailure) Unwrap() error { return e.cause }

// LoginFailure classifies a rejected launch without exposing parser errors,
// account claims, signature details, or server authentication configuration.
func (e *JWTVerifier) LoginFailure(err error) (string, string) {
	var failure *loginFailure
	if errors.As(err, &failure) {
		return failure.reason, failure.message
	}
	return "launch-token-invalid", "The server rejected the launch token. Sign in again from Darkspinner."
}
