package serverinfo

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

func ValidateIdentity(identity Identity) error {
	if !isIdentityText(identity.Name, 80, false) ||
		!isIdentityText(identity.Description, 1024, true) ||
		!isIdentityText(identity.Welcome, 2048, true) ||
		!isIdentityText(identity.Rules, 8192, true) ||
		!isIdentityText(identity.ContactURL, 2048, false) {
		return ErrInvalidIdentity
	}
	if identity.ContactURL == "" {
		return nil
	}
	for _, character := range identity.ContactURL {
		if unicode.IsSpace(character) {
			return fmt.Errorf("contactWhitespace: %w", ErrInvalidIdentity)
		}
	}
	contact, err := url.Parse(identity.ContactURL)
	if err != nil {
		return fmt.Errorf("contactParse: %w", ErrInvalidIdentity)
	}
	if (contact.Scheme != "http" && contact.Scheme != "https") || contact.Hostname() == "" || contact.User != nil {
		return fmt.Errorf("contactScheme: %w", ErrInvalidIdentity)
	}
	return nil
}

func isIdentityText(text string, limit int, isMultiline bool) bool {
	if len(text) > limit || !utf8.ValidString(text) {
		return false
	}
	for _, character := range text {
		if isMultiline && (character == '\n' || character == '\r' || character == '\t') {
			continue
		}
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func NormalizeAnnouncement(message string) (string, error) {
	if len(message) > 512 || !utf8.ValidString(message) {
		return "", ErrInvalidAnnouncement
	}
	for _, character := range message {
		if unicode.IsControl(character) || character == '<' || character == '>' || character == '\u2028' || character == '\u2029' {
			return "", ErrInvalidAnnouncement
		}
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "", ErrInvalidAnnouncement
	}
	return message, nil
}
