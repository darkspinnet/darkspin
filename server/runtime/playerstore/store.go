package playerstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/sporenet/savebuffer"
	profilesql "github.com/darkspinnet/darkspin/server/sporenet/sqlstore"
	"github.com/darkspinnet/darkspin/server/zone/checkpoint"
	checkpointsql "github.com/darkspinnet/darkspin/server/zone/checkpoint/sqlstore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

type Profile interface {
	savebuffer.Store
	chat.Recorder
	io.Closer
}

type Checkpoint interface {
	checkpoint.Store
	io.Closer
}

type Store struct {
	Profile    Profile
	Checkpoint Checkpoint
	Driver     string
}

// OpenPostgresPool opens a bounded pool without creating or migrating tables.
func OpenPostgresPool(ctx context.Context, configuration *pgx.ConnConfig) (*sqlx.DB, error) {
	if ctx == nil || configuration == nil {
		return nil, errors.New("PostgreSQL connection configuration unavailable")
	}
	database := sqlx.NewDb(stdlib.OpenDB(*configuration), "pgx")
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(4)
	database.SetConnMaxIdleTime(5 * time.Minute)
	err := database.PingContext(ctx)
	if err != nil {
		closeErr := database.Close()
		return nil, fmt.Errorf("postgresPing: %w", errors.Join(err, closeErr))
	}
	return database, nil
}

func OpenProfile(ctx context.Context, path string, configuration *pgx.ConnConfig) (Profile, error) {
	if configuration == nil {
		repository, err := profilesql.New(ctx, path, profilesql.Options{})
		if err != nil {
			return nil, fmt.Errorf("sqliteProfile: %w", err)
		}
		return repository, nil
	}
	database, err := OpenPostgresPool(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("poolOpen: %w", err)
	}
	repository, err := profilesql.NewPostgres(ctx, database)
	if err != nil {
		closeErr := database.Close()
		return nil, fmt.Errorf("postgresProfile: %w", errors.Join(err, closeErr))
	}
	return repository, nil
}

// Open wires every durable player capability to the selected backend.
func Open(ctx context.Context, path string, configuration *pgx.ConnConfig) (*Store, error) {
	if configuration == nil {
		profile, err := OpenProfile(ctx, path, nil)
		if err != nil {
			return nil, fmt.Errorf("profileOpen: %w", err)
		}
		checkpoint, err := checkpointsql.New(ctx, path, checkpointsql.Options{BusyTimeout: 5 * time.Second})
		if err != nil {
			closeErr := profile.Close()
			return nil, fmt.Errorf("checkpointOpen: %w", errors.Join(err, closeErr))
		}
		return &Store{Profile: profile, Checkpoint: checkpoint, Driver: "sqlite"}, nil
	}
	database, err := OpenPostgresPool(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("poolOpen: %w", err)
	}
	profile, err := profilesql.NewPostgres(ctx, database)
	if err != nil {
		closeErr := database.Close()
		return nil, fmt.Errorf("profileOpen: %w", errors.Join(err, closeErr))
	}
	checkpoint, err := checkpointsql.NewPostgres(ctx, database)
	if err != nil {
		closeErr := database.Close()
		return nil, fmt.Errorf("checkpointOpen: %w", errors.Join(err, closeErr))
	}
	return &Store{Profile: profile, Checkpoint: checkpoint, Driver: "postgres"}, nil
}
