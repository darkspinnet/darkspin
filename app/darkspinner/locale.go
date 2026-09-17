package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const fallbackClientLocale = "en-us"

// ClientLocale is one complete localization installed with Game.
type ClientLocale struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

var clientLocaleLabels = map[string]string{
	"de-de": "Deutsch",
	"en-us": "English (United States)",
	"fr-fr": "Français",
	"pl-pl": "Polski",
	"ru-ru": "Русский",
}

func detectClientLocales(gamePath string) ([]ClientLocale, error) {
	localePath := filepath.Join(gamePath, "Data", "Locale")
	entries, err := os.ReadDir(localePath)
	if err != nil {
		return nil, fmt.Errorf("localeRead: %w", err)
	}
	locales := make([]ClientLocale, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		code := normalizeClientLocale(entry.Name())
		if !isClientLocaleCode(code) {
			continue
		}
		languagePath := filepath.Join(localePath, entry.Name())
		isComplete := true
		for _, filename := range []string{
			"Text.package", "Audio.package", "Movies.package", "version_media_data.txt",
		} {
			fi, statErr := os.Stat(filepath.Join(languagePath, filename))
			if statErr != nil || fi.IsDir() {
				isComplete = false
				break
			}
		}
		if !isComplete {
			continue
		}
		label := clientLocaleLabels[code]
		if label == "" {
			label = code
		}
		locales = append(locales, ClientLocale{Code: code, Label: label})
	}
	sort.Slice(locales, func(left, right int) bool {
		return locales[left].Label < locales[right].Label
	})
	if len(locales) == 0 {
		return nil, errors.New("no complete languages were found under Data/Locale")
	}
	return locales, nil
}

func resolveClientLocale(configuredLocale string, locales []ClientLocale) (string, error) {
	configuredLocale = normalizeClientLocale(configuredLocale)
	if configuredLocale != "" {
		if hasClientLocale(locales, configuredLocale) {
			return configuredLocale, nil
		}
		return "", fmt.Errorf("configured locale %q is not installed", configuredLocale)
	}
	systemLocale := normalizeClientLocale(systemLocaleName())
	if hasClientLocale(locales, systemLocale) {
		return systemLocale, nil
	}
	if len(systemLocale) >= 2 {
		languagePrefix := systemLocale[:2] + "-"
		for _, locale := range locales {
			if strings.HasPrefix(locale.Code, languagePrefix) {
				return locale.Code, nil
			}
		}
	}
	if hasClientLocale(locales, fallbackClientLocale) {
		return fallbackClientLocale, nil
	}
	return locales[0].Code, nil
}

func normalizeClientLocale(locale string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "_", "-"))
}

func isClientLocaleCode(locale string) bool {
	if len(locale) != 5 || locale[2] != '-' {
		return false
	}
	for index, character := range locale {
		if index == 2 {
			continue
		}
		if character < 'a' || character > 'z' {
			return false
		}
	}
	return true
}

func hasClientLocale(locales []ClientLocale, code string) bool {
	for _, locale := range locales {
		if locale.Code == code {
			return true
		}
	}
	return false
}
