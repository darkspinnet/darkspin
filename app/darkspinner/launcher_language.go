package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/BurntSushi/toml"
)

type LauncherLanguageConfiguration struct {
	Locale  string         `json:"locale"`
	Locales []ClientLocale `json:"locales"`
}

func launcherLocales() []ClientLocale {
	locales := make([]ClientLocale, 0, len(clientLocaleLabels))
	for _, code := range []string{"en-us", "de-de", "fr-fr", "pl-pl", "ru-ru"} {
		locales = append(locales, ClientLocale{Code: code, Label: clientLocaleLabels[code]})
	}
	return locales
}

func (e *App) GetLauncherLanguageConfiguration() (LauncherLanguageConfiguration, error) {
	paths, err := e.spinnerPaths()
	if err != nil {
		return LauncherLanguageConfiguration{}, fmt.Errorf("languagePath: %w", err)
	}
	settings, err := readLauncherSettings(paths.configPath)
	if err != nil {
		return LauncherLanguageConfiguration{}, fmt.Errorf("languageRead: %w", err)
	}
	locales := launcherLocales()
	code := normalizeClientLocale(settings.Locale)
	isMigrationNeeded := code == ""
	if isMigrationNeeded {
		// Preserve the previously shared explicit choice once. Future game
		// language changes must never alter the launcher's independent setting.
		code, err = previousSharedLocale(paths.configPath)
		if err != nil {
			return LauncherLanguageConfiguration{}, fmt.Errorf("languagePrevious: %w", err)
		}
	}
	if code != "" && !hasClientLocale(locales, code) {
		code = fallbackClientLocale
	}
	code, err = resolveClientLocale(code, locales)
	if err != nil {
		return LauncherLanguageConfiguration{}, fmt.Errorf("languageResolve: %w", err)
	}
	if isMigrationNeeded {
		err = e.SetLauncherLocale(code)
		if err != nil {
			return LauncherLanguageConfiguration{}, fmt.Errorf("languageMigrate: %w", err)
		}
	}
	return LauncherLanguageConfiguration{Locale: code, Locales: locales}, nil
}

func previousSharedLocale(configPath string) (string, error) {
	contents, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("previousRead: %w", err)
	}
	var document struct {
		Game struct {
			Locale string `toml:"locale"`
		} `toml:"game"`
	}
	err = toml.Unmarshal(contents, &document)
	if err != nil {
		return "", fmt.Errorf("previousDecode: %w", err)
	}
	return normalizeClientLocale(document.Game.Locale), nil
}

func (e *App) SetLauncherLocale(code string) error {
	code = normalizeClientLocale(code)
	if !hasClientLocale(launcherLocales(), code) {
		return errors.New("Launcher language is not supported.")
	}
	paths, err := e.spinnerPaths()
	if err != nil {
		return fmt.Errorf("languagePath: %w", err)
	}
	err = writeLauncherConfigLine(paths.configPath, "locale", "locale = "+strconv.Quote(code))
	if err != nil {
		return fmt.Errorf("languageSave: %w", err)
	}
	return nil
}
