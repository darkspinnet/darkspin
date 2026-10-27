// Package postgres adapts PostgreSQL's native backup tools to bootstrap snapshots.
package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Backup struct {
	dumpPath    string
	restorePath string
	envs        []string
	identity    string
}

// New uses the server's parsed destination and credentials. The entire database
// belongs to one Darkrun deployment; rollback recreates it through postgres.
func New(ctx context.Context, config *pgx.ConnConfig) (*Backup, error) {
	if config == nil {
		return nil, errors.New("PostgreSQL backup requires connection settings")
	}
	if config.Database == "postgres" || config.Database == "template0" || config.Database == "template1" {
		return nil, errors.New("bootstrap requires a dedicated player database, not postgres or a template database")
	}
	for _, fallback := range config.Fallbacks {
		if fallback.Host != config.Host || fallback.Port != config.Port {
			return nil, errors.New("bootstrap backup requires one PostgreSQL host and port")
		}
	}
	dumpPath, err := exec.LookPath("pg_dump")
	if err != nil {
		return nil, fmt.Errorf("dumpTool: %w", err)
	}
	restorePath, err := exec.LookPath("pg_restore")
	if err != nil {
		return nil, fmt.Errorf("restoreTool: %w", err)
	}
	envs, err := clientEnvs(config)
	if err != nil {
		return nil, fmt.Errorf("clientEnvironment: %w", err)
	}
	err = validateRestoreAccess(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("restoreAccess: %w", err)
	}
	target := config.Host + "\x00" + strconv.Itoa(int(config.Port)) + "\x00" + config.Database
	checksum := sha256.Sum256([]byte(target))
	return &Backup{dumpPath: dumpPath, restorePath: restorePath, envs: envs,
		identity: fmt.Sprintf("postgresql:%x", checksum)}, nil
}

func validateRestoreAccess(ctx context.Context, config *pgx.ConnConfig) error {
	operationContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	maintenanceConfig := config.Copy()
	maintenanceConfig.Database = "postgres"
	connection, err := pgx.ConnectConfig(operationContext, maintenanceConfig)
	if err != nil {
		return fmt.Errorf("maintenanceConnect: %w", err)
	}
	var isAllowed bool
	err = connection.QueryRow(operationContext, `SELECT role.rolsuper OR
		(role.rolcreatedb AND NOT EXISTS (
			SELECT 1 FROM pg_database AS database
			WHERE database.datname = $1 AND database.datdba <> role.oid))
		FROM pg_roles AS role WHERE role.rolname = CURRENT_USER`, config.Database).Scan(&isAllowed)
	closeErr := connection.Close(operationContext)
	if err != nil || closeErr != nil {
		return fmt.Errorf("roleRead: %w", errors.Join(err, closeErr))
	}
	if !isAllowed {
		return errors.New("bootstrap requires database ownership and CREATEDB, or a superuser, for PostgreSQL rollback")
	}
	return nil
}

func (e *Backup) Identity() string {
	return e.identity
}

func (e *Backup) Dump(ctx context.Context, path string) error {
	operationContext, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("dumpCreate: %w", err)
	}
	// Never put the connection URL or password in process arguments or logs.
	command := exec.CommandContext(operationContext, e.dumpPath, "--format=custom", "--no-password")
	command.Env = e.envs
	command.Stdout = w
	err = command.Run()
	syncErr := w.Sync()
	closeErr := w.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("dumpWrite: %w", errors.Join(err, syncErr, closeErr))
	}
	// A readable archive is required before storage marks the snapshot complete.
	command = exec.CommandContext(operationContext, e.restorePath, "--list", path)
	err = command.Run()
	if err != nil {
		return fmt.Errorf("dumpInspect: %w", err)
	}
	return nil
}

func (e *Backup) Restore(ctx context.Context, path string) error {
	operationContext, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	// Recreate the database so objects added by a failed candidate cannot survive.
	// Connect through the maintenance database even if an interrupted restore
	// already dropped the player database. The persisted transaction makes retries
	// repeatable; callers must not start the server until restoration succeeds.
	command := exec.CommandContext(operationContext, e.restorePath,
		"--no-password", "--clean", "--if-exists", "--create", "--exit-on-error",
		"--no-owner", "--no-privileges", "--dbname=postgres", path)
	command.Env = e.envs
	err := command.Run()
	if err != nil {
		return fmt.Errorf("restoreRun: %w", err)
	}
	return nil
}

func clientEnvs(config *pgx.ConnConfig) ([]string, error) {
	settings := map[string]string{
		"PGHOST":            config.Host,
		"PGPORT":            strconv.Itoa(int(config.Port)),
		"PGDATABASE":        config.Database,
		"PGUSER":            config.User,
		"PGPASSWORD":        config.Password,
		"PGAPPNAME":         "darkrun-backup",
		"PGCONNECT_TIMEOUT": "10",
	}
	// pgx and libpq do not share service-file or hostaddr resolution. Refuse
	// implicit redirection instead of risking a dump of a different database.
	for _, name := range []string{"PGSERVICE", "PGSERVICEFILE", "PGHOSTADDR"} {
		if os.Getenv(name) != "" {
			return nil, fmt.Errorf("bootstrap does not support %s", name)
		}
	}
	if config.ConnString() != "" {
		address, err := url.Parse(config.ConnString())
		if err != nil {
			return nil, errors.New("invalid PostgreSQL backup connection URL")
		}
		parameters, err := url.ParseQuery(address.RawQuery)
		if err != nil {
			return nil, errors.New("invalid PostgreSQL backup connection parameters")
		}
		for name, arguments := range parameters {
			if len(arguments) != 1 {
				return nil, errors.New("duplicate PostgreSQL backup connection parameter")
			}
			switch name {
			case "host", "port", "dbname", "user", "password", "application_name":
				// These fields already come from the server's parsed configuration.
			case "sslmode", "sslrootcert", "sslcert", "sslkey", "sslpassword", "connect_timeout", "options", "target_session_attrs":
				settings["PG"+strings.ToUpper(name)] = arguments[0]
			default:
				return nil, errors.New("unsupported PostgreSQL URL parameter for bootstrap backup")
			}
		}
	}
	envs := make([]string, 0, len(os.Environ())+len(settings))
	for _, entry := range os.Environ() {
		name, setting, isFound := strings.Cut(entry, "=")
		if !isFound {
			continue
		}
		replacement, isReplaced := settings[name]
		if isReplaced {
			envs = append(envs, name+"="+replacement)
			delete(settings, name)
			continue
		}
		if name != "DARKSPIN_POSTGRES_URL" {
			envs = append(envs, name+"="+setting)
		}
	}
	for name, setting := range settings {
		envs = append(envs, name+"="+setting)
	}
	return envs, nil
}
