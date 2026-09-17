//go:build !windows

package main

import (
	"os"
	"strings"
)

func systemLocaleName() string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		locale := strings.TrimSpace(os.Getenv(name))
		if locale == "" {
			continue
		}
		if separator := strings.IndexAny(locale, ".@"); separator >= 0 {
			locale = locale[:separator]
		}
		return locale
	}
	return ""
}
