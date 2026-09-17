package sqlite

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

type localizationText struct {
	Locale        string
	TableID       uint64
	LocaleKey     string
	LocalizedText string
}

func writeLocalizationText(ctx context.Context, transaction *sql.Tx, installPath string) error {
	for _, spec := range buildPackages {
		if !strings.HasPrefix(spec.name, "Text.") {
			continue
		}
		locale := strings.TrimSuffix(strings.TrimPrefix(spec.name, "Text."), ".package")
		packagePath := filepath.Join(installPath, spec.relativePath)
		r, err := os.Open(packagePath)
		if err != nil {
			return fmt.Errorf("packageOpen[%s]: %w", locale, err)
		}
		fi, err := r.Stat()
		if err != nil {
			_ = r.Close()
			return fmt.Errorf("packageStat[%s]: %w", locale, err)
		}
		pkg, err := dbpf.NewReader(r, fi.Size())
		if err != nil {
			_ = r.Close()
			return fmt.Errorf("packageRead[%s]: %w", locale, err)
		}
		localeRows := make([]localizationText, 0, 12*1024)
		for ordinal, entry := range pkg.Entries {
			decoded, openErr := pkg.Open(entry)
			if openErr != nil {
				_ = r.Close()
				return fmt.Errorf("textOpen[%s:%d]: %w", locale, ordinal, openErr)
			}
			rows, parseErr := parseLocalizationText(decoded, locale, entry.Instance)
			if parseErr != nil {
				_ = r.Close()
				return fmt.Errorf("textParse[%s:%d]: %w", locale, ordinal, parseErr)
			}
			localeRows = append(localeRows, rows...)
		}
		err = insertLocalizationRows(ctx, transaction, localeRows)
		if err != nil {
			_ = r.Close()
			return fmt.Errorf("textInsert[%s]: %w", locale, err)
		}
		err = r.Close()
		if err != nil {
			return fmt.Errorf("packageClose[%s]: %w", locale, err)
		}
	}
	return nil
}

const localizationInsertBatchSize = 500

func insertLocalizationRows(ctx context.Context, transaction *sql.Tx, rows []localizationText) error {
	for start := 0; start < len(rows); start += localizationInsertBatchSize {
		end := min(start+localizationInsertBatchSize, len(rows))
		query := strings.Builder{}
		query.WriteString("INSERT INTO localization_text (locale, table_id, locale_key, localized_text) VALUES ")
		arguments := make([]any, 0, (end-start)*4)
		for index, row := range rows[start:end] {
			if index > 0 {
				query.WriteString(", ")
			}
			query.WriteString("(?, ?, ?, ?)")
			arguments = append(arguments, row.Locale, row.TableID, row.LocaleKey, row.LocalizedText)
		}
		_, err := transaction.ExecContext(ctx, query.String(), arguments...)
		if err != nil {
			return fmt.Errorf("batchInsert[%d]: %w", start/localizationInsertBatchSize, err)
		}
	}
	return nil
}

func parseLocalizationText(r io.Reader, locale string, tableID uint64) ([]localizationText, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	rows := make([]localizationText, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		line = strings.TrimPrefix(line, "\ufeff")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) < 10 || !strings.EqualFold(line[:2], "0x") {
			if len(rows) == 0 {
				return nil, fmt.Errorf("lineFormat[%d]: missing locale key in %q", lineNumber, line)
			}
			rows[len(rows)-1].LocalizedText += "\n" + line
			continue
		}
		localeKey := strings.ToLower(line[:10])
		localizedText := strings.TrimLeft(line[10:], " \t")
		rows = append(rows, localizationText{
			Locale: locale, TableID: tableID, LocaleKey: localeKey, LocalizedText: localizedText,
		})
	}
	err := scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("lineScan: %w", err)
	}
	return rows, nil
}
