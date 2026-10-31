package auth

import (
	"errors"
	"net/http"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Existing accounts may have legacy names; apply the new alphanumeric naming
// rule only at registration, while identifying missing login fields precisely.
func validateDesktopLogin(username, password string, isPasswordRequired bool) error {
	if username == "" {
		return sporenet.ErrUsernameRequired
	}
	if len(username) > 320 {
		return errors.New("Username is too long (maximum 320 UTF-8 bytes).")
	}
	if isPasswordRequired && password == "" {
		return sporenet.ErrPasswordRequired
	}
	return nil
}

func writeDesktopRegistrationError(writer http.ResponseWriter, err error) {
	status := http.StatusConflict
	message := err.Error()
	switch {
	case errors.Is(err, sporenet.ErrUsernameRequired):
		status, message = http.StatusBadRequest, sporenet.ErrUsernameRequired.Error()
	case errors.Is(err, sporenet.ErrUsernamePattern):
		status, message = http.StatusBadRequest, sporenet.ErrUsernamePattern.Error()
	case errors.Is(err, sporenet.ErrUsernameTooLong):
		status, message = http.StatusBadRequest, sporenet.ErrUsernameTooLong.Error()
	case errors.Is(err, sporenet.ErrPasswordRequired):
		status, message = http.StatusBadRequest, sporenet.ErrPasswordRequired.Error()
	case errors.Is(err, sporenet.ErrPasswordTooLong):
		status, message = http.StatusBadRequest, sporenet.ErrPasswordTooLong.Error()
	case errors.Is(err, sporenet.ErrUserExists):
		message = "Username already exists on this server. Check your password or choose another username."
	}
	writeAuthError(writer, status, message)
}

func (e *LocalBroker) writeDesktopAuthenticationError(writer http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	code := "invalid_credentials"
	message := "Username or password is incorrect."
	switch {
	case errors.Is(err, sporenet.ErrPasswordIncorrect):
		code = "invalid_password"
		message = "Username already exists on this server, but the password is incorrect."
	case errors.Is(err, sporenet.ErrUserNotFound):
		code = "username_not_found"
		message = "Username was not found on this server."
	case err != nil:
		status = http.StatusInternalServerError
		code = "authentication_unavailable"
		message = "The server could not verify this account. Please try again."
		if e.logger != nil {
			e.logger.Printf("desktop account verification failed: %v", err)
		}
	}
	writeAuthJSON(writer, status, map[string]string{"error": message, "code": code})
}
