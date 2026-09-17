package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

const defaultDatabaseQueryLimit = 100

var (
	databaseIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	databasePredicatePattern  = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(<=|>=|!=|=|<|>)(.*)$`)
)

type databaseCommandOptions struct {
	configPath       string
	limit            int
	binaryOutputPath string
	binaryDecode     string
	isBinaryForce    bool
}

type databaseTarget struct {
	database *sql.DB
	name     string
}

type databasePredicate struct {
	field    string
	operator string
	value    any
}

type databaseGetResult struct {
	Database string           `json:"database"`
	Table    string           `json:"table"`
	Rows     []map[string]any `json:"rows"`
}

type databaseSetResult struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	Updated  int64  `json:"updated"`
}

type databaseTutorialCompleteResult struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	server.TutorialProfileCompletion
}

type databaseUserCopyResult struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	sporenet.UserProfileCopy
}

func newDatabaseCommand() *cobra.Command {
	options := databaseCommandOptions{configPath: game.DefaultConfigFilename, limit: defaultDatabaseQueryLimit}
	databaseCmd := &cobra.Command{
		Use:   "db <table> <get|bget|set|complete|copy> [params...]",
		Short: "Inspect or update configured database tables",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, arguments []string) error {
			err := runDatabaseCommand(cmd.Context(), cmd.OutOrStdout(), options, arguments)
			if err != nil {
				return fmt.Errorf("databaseRun: %w", err)
			}
			return nil
		},
	}
	databaseCmd.Flags().StringVar(&options.configPath, "config", game.DefaultConfigFilename, "path to darkspin.toml")
	databaseCmd.Flags().IntVar(&options.limit, "limit", defaultDatabaseQueryLimit, "maximum rows returned by get")
	databaseCmd.Flags().StringVar(&options.binaryOutputPath, "output", "", "path for bget binary output")
	databaseCmd.Flags().StringVar(&options.binaryDecode, "decode", "", "optional bget payload codec (zlib)")
	databaseCmd.Flags().BoolVar(&options.isBinaryForce, "force", false, "allow bget to replace an existing output file")
	return databaseCmd
}

func runDatabaseCommand(ctx context.Context, output io.Writer, options databaseCommandOptions, arguments []string) error {
	if options.limit <= 0 || options.limit > 1000 {
		return errors.New("limit must be between 1 and 1000")
	}
	configPath, err := filepath.Abs(options.configPath)
	if err != nil {
		return fmt.Errorf("configPath: %w", err)
	}
	config, _, err := game.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("configLoad: %w", err)
	}
	driver := strings.ToLower(strings.TrimSpace(config.String(game.ConfigStorageDriver)))
	if driver != "" && driver != "sqlite" {
		return fmt.Errorf("driverUnsupported: %s", driver)
	}
	table := strings.ToLower(arguments[0])
	if !databaseIdentifierPattern.MatchString(table) {
		return fmt.Errorf("tableInvalid: %q", arguments[0])
	}
	action := strings.ToLower(arguments[1])
	parameters := arguments[2:]
	if action == "complete" {
		if table != "user" || len(parameters) != 1 || strings.TrimSpace(parameters[0]) == "" {
			return errors.New("complete usage: db user complete <login-name>")
		}
		completion, completeErr := server.CompleteTutorialProfile(ctx, configPath, parameters[0])
		if completeErr != nil {
			return fmt.Errorf("tutorialComplete: %w", completeErr)
		}
		result := databaseTutorialCompleteResult{
			Database: "user", Table: table, TutorialProfileCompletion: completion,
		}
		err = writeDatabaseJSON(output, result)
		if err != nil {
			return fmt.Errorf("resultWrite: %w", err)
		}
		return nil
	}
	if action == "copy" {
		if table != "user" || len(parameters) != 2 ||
			strings.TrimSpace(parameters[0]) == "" ||
			strings.TrimSpace(parameters[1]) == "" {
			return errors.New(
				"copy usage: db user copy <source-login-name> <new-login-name>",
			)
		}
		copyResult, copyErr := server.CopyUserProfile(
			ctx, configPath, parameters[0], parameters[1],
		)
		if copyErr != nil {
			return fmt.Errorf("userCopy: %w", copyErr)
		}
		result := databaseUserCopyResult{
			Database:        "user",
			Table:           table,
			UserProfileCopy: copyResult,
		}
		err = writeDatabaseJSON(output, result)
		if err != nil {
			return fmt.Errorf("resultWrite: %w", err)
		}
		return nil
	}
	runtimePath := filepath.Join(filepath.Dir(configPath), server.DarkspinDirectory)
	target, err := openDatabaseTarget(ctx, runtimePath, table)
	if err != nil {
		return fmt.Errorf("targetOpen: %w", err)
	}
	defer target.database.Close()
	columns, err := databaseColumns(ctx, target.database, table)
	if err != nil {
		return fmt.Errorf("columnsRead: %w", err)
	}
	switch action {
	case "get":
		err = databaseGet(ctx, output, target, table, columns, parameters, options.limit)
		if err != nil {
			return fmt.Errorf("get: %w", err)
		}
	case "bget":
		err = databaseBinaryGet(ctx, output, target, table, columns, parameters, options)
		if err != nil {
			return fmt.Errorf("bget: %w", err)
		}
	case "set":
		err = databaseSet(ctx, output, target, table, columns, parameters)
		if err != nil {
			return fmt.Errorf("set: %w", err)
		}
	default:
		return fmt.Errorf("actionUnsupported: %q; supported actions are get, bget, set, complete, and copy", action)
	}
	return nil
}

func openDatabaseTarget(ctx context.Context, runtimePath, table string) (*databaseTarget, error) {
	files := []struct {
		name string
		path string
	}{
		{name: "user", path: filepath.Join(runtimePath, server.SaveDirectory, server.UserDatabaseFilename)},
		{name: "content", path: filepath.Join(runtimePath, server.CacheDirectory, server.ContentDatabaseFilename)},
	}
	matches := make([]*databaseTarget, 0, 1)
	for _, file := range files {
		_, err := os.Stat(file.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			closeDatabaseTargets(matches)
			return nil, fmt.Errorf("databaseStat[%s]: %w", file.name, err)
		}
		database, err := openExistingSQLite(ctx, file.path)
		if err != nil {
			closeDatabaseTargets(matches)
			return nil, fmt.Errorf("databaseOpen[%s]: %w", file.name, err)
		}
		var count int
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count)
		if err != nil {
			_ = database.Close()
			closeDatabaseTargets(matches)
			return nil, fmt.Errorf("tableLookup[%s]: %w", file.name, err)
		}
		if count == 0 {
			_ = database.Close()
			continue
		}
		matches = append(matches, &databaseTarget{database: database, name: file.name})
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("tableMissing: %q was not found in user.db or content.db", table)
	}
	if len(matches) > 1 {
		closeDatabaseTargets(matches)
		return nil, fmt.Errorf("tableAmbiguous: %q exists in both databases", table)
	}
	return matches[0], nil
}

func closeDatabaseTargets(targets []*databaseTarget) {
	for _, target := range targets {
		if target == nil || target.database == nil {
			continue
		}
		_ = target.database.Close()
	}
}

func openExistingSQLite(ctx context.Context, path string) (*sql.DB, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	parameters := url.Values{}
	parameters.Set("mode", "rw")
	parameters.Add("_pragma", "busy_timeout(5000)")
	parameters.Add("_pragma", "foreign_keys(1)")
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(absolutePath)+"?"+parameters.Encode())
	if err != nil {
		return nil, fmt.Errorf("sqliteOpen: %w", err)
	}
	database.SetMaxOpenConns(1)
	err = database.PingContext(ctx)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("sqlitePing: %w", err)
	}
	return database, nil
}

func databaseColumns(ctx context.Context, database *sql.DB, table string) (map[string]struct{}, error) {
	rows, err := database.QueryContext(ctx, `PRAGMA table_info(`+quoteDatabaseIdentifier(table)+`)`)
	if err != nil {
		return nil, fmt.Errorf("tableInfo: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]struct{})
	for rows.Next() {
		var position, isNotNull, primaryKey int
		var name, valueType string
		var defaultValue sql.NullString
		err = rows.Scan(&position, &name, &valueType, &isNotNull, &defaultValue, &primaryKey)
		if err != nil {
			return nil, fmt.Errorf("columnScan: %w", err)
		}
		columns[strings.ToLower(name)] = struct{}{}
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("columnRows: %w", err)
	}
	if len(columns) == 0 {
		return nil, errors.New("table has no columns")
	}
	return columns, nil
}

func databaseGet(ctx context.Context, output io.Writer, target *databaseTarget, table string, columns map[string]struct{}, parameters []string, limit int) error {
	predicates, err := parseDatabasePredicates(parameters, columns, true)
	if err != nil {
		return fmt.Errorf("predicateParse: %w", err)
	}
	query, values := buildDatabaseWhere(`SELECT * FROM `+quoteDatabaseIdentifier(table), predicates)
	if _, isFound := columns["id"]; isFound {
		query += ` ORDER BY ` + quoteDatabaseIdentifier("id")
	}
	query += ` LIMIT ?`
	values = append(values, limit)
	rows, err := target.database.QueryContext(ctx, query, values...)
	if err != nil {
		return fmt.Errorf("queryExecute: %w", err)
	}
	defer rows.Close()
	resultRows, err := scanDatabaseRows(rows)
	if err != nil {
		return fmt.Errorf("rowRead: %w", err)
	}
	result := databaseGetResult{Database: target.name, Table: table, Rows: resultRows}
	err = writeDatabaseJSON(output, result)
	if err != nil {
		return fmt.Errorf("resultWrite: %w", err)
	}
	return nil
}

func databaseSet(ctx context.Context, output io.Writer, target *databaseTarget, table string, columns map[string]struct{}, parameters []string) error {
	if len(parameters) < 4 {
		return errors.New("usage: db <table> set <key> <value> where <field><operator><value>")
	}
	key := strings.ToLower(parameters[0])
	if _, isFound := columns[key]; !isFound {
		return fmt.Errorf("columnMissing: %q", key)
	}
	if !strings.EqualFold(parameters[2], "where") {
		return errors.New("set requires an explicit where clause")
	}
	predicates, err := parseDatabasePredicates(parameters[3:], columns, false)
	if err != nil {
		return fmt.Errorf("predicateParse: %w", err)
	}
	query := `UPDATE ` + quoteDatabaseIdentifier(table) + ` SET ` + quoteDatabaseIdentifier(key) + ` = ?`
	query, values := buildDatabaseWhere(query, predicates)
	values = append([]any{parseDatabaseValue(parameters[1])}, values...)
	result, err := target.database.ExecContext(ctx, query, values...)
	if err != nil {
		return fmt.Errorf("updateExecute: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("updatedCount: %w", err)
	}
	err = writeDatabaseJSON(output, databaseSetResult{Database: target.name, Table: table, Updated: updated})
	if err != nil {
		return fmt.Errorf("resultWrite: %w", err)
	}
	return nil
}

func parseDatabasePredicates(arguments []string, columns map[string]struct{}, isShorthandAllowed bool) ([]databasePredicate, error) {
	if len(arguments) > 0 && strings.EqualFold(arguments[0], "where") {
		arguments = arguments[1:]
	}
	if len(arguments) == 0 {
		return nil, errors.New("at least one lookup value or predicate is required")
	}
	if isShorthandAllowed && len(arguments) == 1 && databasePredicatePattern.FindStringSubmatch(arguments[0]) == nil {
		return shorthandDatabasePredicate(arguments[0], columns)
	}
	predicates := make([]databasePredicate, 0, len(arguments))
	for index := 0; index < len(arguments); {
		expression := arguments[index]
		matches := databasePredicatePattern.FindStringSubmatch(expression)
		if matches == nil && index+2 < len(arguments) && isDatabaseOperator(arguments[index+1]) {
			expression = arguments[index] + arguments[index+1] + arguments[index+2]
			matches = databasePredicatePattern.FindStringSubmatch(expression)
			index += 3
		} else {
			index++
		}
		if matches == nil {
			return nil, fmt.Errorf("predicateInvalid: %q", expression)
		}
		field := strings.ToLower(matches[1])
		if _, isFound := columns[field]; !isFound {
			return nil, fmt.Errorf("columnMissing: %q", field)
		}
		predicates = append(predicates, databasePredicate{
			field: field, operator: matches[2], value: parseDatabaseValue(matches[3]),
		})
	}
	return predicates, nil
}

func shorthandDatabasePredicate(value string, columns map[string]struct{}) ([]databasePredicate, error) {
	if _, isFound := columns["id"]; isFound {
		id, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return []databasePredicate{{field: "id", operator: "=", value: id}}, nil
		}
	}
	if _, isFound := columns["login_name"]; isFound {
		return []databasePredicate{{field: "login_name", operator: "LIKE", value: "%" + value + "%"}}, nil
	}
	return nil, errors.New("shorthand requires an id or login_name column")
}

func isDatabaseOperator(value string) bool {
	switch value {
	case "=", "!=", "<", ">", "<=", ">=":
		return true
	default:
		return false
	}
}

func parseDatabaseValue(value string) any {
	if strings.EqualFold(value, "null") {
		return nil
	}
	if strings.EqualFold(value, "true") {
		return int64(1)
	}
	if strings.EqualFold(value, "false") {
		return int64(0)
	}
	integer, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		return integer
	}
	decimal, err := strconv.ParseFloat(value, 64)
	if err == nil {
		return decimal
	}
	return value
}

func buildDatabaseWhere(query string, predicates []databasePredicate) (string, []any) {
	values := make([]any, 0, len(predicates))
	if len(predicates) == 0 {
		return query, values
	}
	query += " WHERE "
	for index, predicate := range predicates {
		if index > 0 {
			query += " AND "
		}
		query += quoteDatabaseIdentifier(predicate.field) + " " + predicate.operator + " ?"
		values = append(values, predicate.value)
	}
	return query, values
}

func quoteDatabaseIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func scanDatabaseRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columnsRead: %w", err)
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		err = rows.Scan(destinations...)
		if err != nil {
			return nil, fmt.Errorf("valuesScan: %w", err)
		}
		row := make(map[string]any, len(columns))
		for index, column := range columns {
			value := values[index]
			if contents, isBytes := value.([]byte); isBytes {
				value = string(contents)
			}
			row[column] = value
		}
		result = append(result, row)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("rowsRead: %w", err)
	}
	return result, nil
}

func writeDatabaseJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(value)
	if err != nil {
		return fmt.Errorf("jsonEncode: %w", err)
	}
	return nil
}
