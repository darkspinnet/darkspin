package serverinfo

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Welcome returns operator-authored text as bounded, plain system-chat lines.
// Identity accepts multiline text and markup punctuation; chat must not interpret
// that punctuation as client presentation instructions.
func (e *Service) Welcome(ctx context.Context) ([]string, error) {
	snapshot, err := e.Identity(ctx)
	if err != nil {
		return nil, fmt.Errorf("welcomeIdentity: %w", err)
	}
	err = ValidateIdentity(snapshot.Identity)
	if err != nil {
		return nil, fmt.Errorf("welcomeValidate: %w", err)
	}
	message := strings.Join(strings.Fields(snapshot.Identity.Welcome), " ")
	message = strings.NewReplacer("<", "‹", ">", "›").Replace(message)
	var lines []string
	for message != "" {
		end := min(len(message), 512)
		for end < len(message) && !utf8.RuneStart(message[end]) {
			end--
		}
		line, normalizeErr := NormalizeAnnouncement(message[:end])
		if normalizeErr != nil {
			return nil, fmt.Errorf("welcomeLine: %w", normalizeErr)
		}
		lines = append(lines, line)
		message = strings.TrimSpace(message[end:])
	}
	return lines, nil
}
