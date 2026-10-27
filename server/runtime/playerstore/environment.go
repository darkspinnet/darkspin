// Package playerstore selects and opens durable player storage for server hosts.
// Content storage is independent and always remains local SQLite.
package playerstore

import (
	"errors"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// FromEnvironment requires a PostgreSQL connection for the headless host.
// Missing, partial or invalid settings are errors, never a SQLite fallback.
// Callers opt into this only for the dedicated darkrun host or its admin tools.
func FromEnvironment() (*pgx.ConnConfig, error) {
	connectionURL := strings.TrimSpace(os.Getenv("DARKSPIN_POSTGRES_URL"))
	isRequested := os.Getenv("DARKSPIN_POSTGRES_URL") != ""
	for _, name := range []string{
		"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD",
		"PGSSLMODE", "PGSSLROOTCERT", "PGCONNECT_TIMEOUT",
	} {
		isRequested = isRequested || os.Getenv(name) != ""
	}
	if !isRequested {
		return nil, errors.New("darkrun requires PostgreSQL; set DARKSPIN_POSTGRES_URL or PGHOST, PGDATABASE and PGUSER")
	}
	if connectionURL == "" {
		for _, name := range []string{"PGHOST", "PGDATABASE", "PGUSER"} {
			if strings.TrimSpace(os.Getenv(name)) == "" {
				return nil, errors.New("PostgreSQL requires PGHOST, PGDATABASE and PGUSER, or DARKSPIN_POSTGRES_URL")
			}
		}
	} else {
		address, err := url.Parse(connectionURL)
		if err != nil {
			// Parser errors can include credentials. Never propagate the raw URL.
			return nil, errors.New("invalid DARKSPIN_POSTGRES_URL")
		}
		if (address.Scheme != "postgres" && address.Scheme != "postgresql") ||
			address.Hostname() == "" || address.User == nil ||
			address.User.Username() == "" || strings.Trim(address.Path, "/") == "" {
			return nil, errors.New("DARKSPIN_POSTGRES_URL requires a PostgreSQL URL with host, user and database")
		}
	}
	configuration, err := pgx.ParseConfig(connectionURL)
	if err != nil {
		// pgx parse errors can contain the original connection string.
		return nil, errors.New("invalid PostgreSQL connection settings; check URL, port, TLS and timeout")
	}
	configuration.RuntimeParams["application_name"] = "darkrun"
	return configuration, nil
}
