package sporenet

import "errors"

const MaximumDesktopUsernameLength = 20

var (
	ErrUsernameRequired  = errors.New("Username is required.")
	ErrUsernamePattern   = errors.New("Username can only contain letters and numbers.")
	ErrUsernameTooLong   = errors.New("Username is too long (maximum 20 characters).")
	ErrPasswordRequired  = errors.New("Password is required.")
	ErrPasswordTooLong   = errors.New("Password is too long (maximum 72 UTF-8 bytes).")
	ErrPasswordIncorrect = errors.New("incorrect password")
)

// ValidateDesktopUsername checks names for newly created desktop accounts.
// Existing credentials retain their legacy login-name compatibility.
func ValidateDesktopUsername(username string) error {
	if username == "" {
		return ErrUsernameRequired
	}
	for _, character := range username {
		isLetter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		if !isLetter && !isDigit {
			return ErrUsernamePattern
		}
	}
	if len(username) > MaximumDesktopUsernameLength {
		return ErrUsernameTooLong
	}
	return nil
}

// ValidateDesktopPassword exposes bcrypt's actual storage limit. Loopback-only
// local account creation may omit a password; remote requests must supply one.
func ValidateDesktopPassword(password string, isRequired bool) error {
	if isRequired && password == "" {
		return ErrPasswordRequired
	}
	if len(password) > 72 {
		return ErrPasswordTooLong
	}
	return nil
}
