// Package sqlstore implements transactional player profiles for SQLite and PostgreSQL.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

const (
	defaultMaxOpenConnections = 8
	defaultMaxIdleConnections = 4
	defaultBusyTimeout        = 5 * time.Second
)

// Options controls the standalone SQLite connection pool.
type Options struct {
	MaxOpenConnections int
	MaxIdleConnections int
	BusyTimeout        time.Duration
}

// Repository stores normalized account aggregates in either SQL backend.
type Repository struct {
	db         *sqlx.DB
	isPostgres bool
}

var _ sporenet.UserRepository = (*Repository)(nil)
var _ sporenet.TutorialCompletionQueueRepository = (*Repository)(nil)

// New opens the database, configures every connection, and initializes its schema.
func New(ctx context.Context, path string, options Options) (*Repository, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if path == "" {
		return nil, errors.New("empty path")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(absolutePath), 0o755)
	if err != nil {
		return nil, fmt.Errorf("pathMkdir: %w", err)
	}
	options = normalizeOptions(options)
	sqlx.BindDriver("sqlite", sqlx.QUESTION)
	db, err := sqlx.Open("sqlite", sqliteDSN(absolutePath, options.BusyTimeout))
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	db.SetMaxOpenConns(options.MaxOpenConnections)
	db.SetMaxIdleConns(options.MaxIdleConnections)
	db.SetConnMaxLifetime(0)
	err = db.PingContext(ctx)
	if err != nil {
		closeErr := db.Close()
		return nil, fmt.Errorf("databasePing: %w", errors.Join(err, closeErr))
	}
	repository := &Repository{db: db}
	err = repository.initializeSchema(ctx)
	if err != nil {
		closeErr := db.Close()
		return nil, fmt.Errorf("schemaInitialize: %w", errors.Join(err, closeErr))
	}
	return repository, nil
}

// Close releases the player connection pool.
func (e *Repository) Close() error {
	if e == nil || e.db == nil {
		return nil
	}
	err := e.db.Close()
	if err != nil {
		return fmt.Errorf("databaseClose: %w", err)
	}
	return nil
}

// Create inserts a complete profile in one transaction.
func (e *Repository) Create(ctx context.Context, record sporenet.UserRecord) (int64, error) {
	if ctx == nil {
		return 0, errors.New("nil context")
	}
	if record.LoginName == "" {
		return 0, fmt.Errorf("createRecord: %w", sporenet.ErrInvalidUser)
	}
	record.CreateDT = time.Now().UTC()
	record.LastConnectionDT = time.Time{}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("createBegin: %w", err)
	}
	defer rollback(tx)
	row := userRowFromRecord(record)
	query, arguments, err := sqlx.Named(insertUserQuery+` ON CONFLICT DO NOTHING RETURNING id`, row)
	if err != nil {
		return 0, fmt.Errorf("createBind: %w", err)
	}
	var id int64
	err = tx.GetContext(ctx, &id, tx.Rebind(query), arguments...)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("createExists: %w", sporenet.ErrUserExists)
	}
	if err != nil {
		return 0, fmt.Errorf("createID: %w", err)
	}
	record, err = sporenet.PrepareCreatedUserRecord(record, id)
	if err != nil {
		return 0, fmt.Errorf("createIdentity: %w", err)
	}
	err = writeDependents(ctx, tx, record)
	if err != nil {
		return 0, fmt.Errorf("createWrite: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return 0, fmt.Errorf("createCommit: %w", err)
	}
	return id, nil
}

// DeleteByLoginName removes one profile. Foreign-key cascades remove every
// aggregate child in the same transaction.
func (e *Repository) DeleteByLoginName(ctx context.Context, loginName string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if loginName == "" {
		return fmt.Errorf("deleteIdentity: %w", sporenet.ErrUserNotFound)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("deleteBegin: %w", err)
	}
	defer rollback(tx)
	result, err := tx.ExecContext(ctx, tx.Rebind("DELETE FROM \"user\" WHERE LOWER(login_name) = LOWER(?)"), loginName)
	if err != nil {
		return fmt.Errorf("deleteUser: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleteAffected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("deleteMissing: %w", sporenet.ErrUserNotFound)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("deleteCommit: %w", err)
	}
	return nil
}

// LoadByLoginName reads a consistent profile snapshot in one transaction.
func (e *Repository) LoadByLoginName(ctx context.Context, loginName string) (sporenet.UserRecord, error) {
	if ctx == nil {
		return sporenet.UserRecord{}, errors.New("nil context")
	}
	tx, err := e.db.BeginTxx(ctx, e.readOptions())
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadBegin: %w", err)
	}
	defer rollback(tx)
	record, err := loadRecord(ctx, tx, loginName)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadRecord: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadCommit: %w", err)
	}
	return record, nil
}

// LoadByID reads a consistent profile snapshot by its public Blaze ID.
func (e *Repository) LoadByID(ctx context.Context, id int64) (sporenet.UserRecord, error) {
	if ctx == nil {
		return sporenet.UserRecord{}, errors.New("nil context")
	}
	if id <= 0 {
		return sporenet.UserRecord{}, fmt.Errorf("loadID: %w", sporenet.ErrUserNotFound)
	}
	tx, err := e.db.BeginTxx(ctx, e.readOptions())
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadIDBegin: %w", err)
	}
	defer rollback(tx)
	var loginName string
	err = tx.GetContext(ctx, &loginName, tx.Rebind(`SELECT login_name FROM "user" WHERE id = ?`), id)
	if errors.Is(err, sql.ErrNoRows) {
		return sporenet.UserRecord{}, fmt.Errorf("loadIDMissing: %w", sporenet.ErrUserNotFound)
	}
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadIDSelect: %w", err)
	}
	record, err := loadRecord(ctx, tx, loginName)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadIDRecord: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadIDCommit: %w", err)
	}
	return record, nil
}

// LoadByDisplayName requires one case-insensitive match so public names cannot
// select an arbitrary account when legacy data contains duplicates.
func (e *Repository) LoadByDisplayName(ctx context.Context, displayName string) (sporenet.UserRecord, error) {
	if ctx == nil {
		return sporenet.UserRecord{}, errors.New("nil context")
	}
	if displayName == "" {
		return sporenet.UserRecord{}, fmt.Errorf("loadDisplay: %w", sporenet.ErrUserNotFound)
	}
	tx, err := e.db.BeginTxx(ctx, e.readOptions())
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadDisplayBegin: %w", err)
	}
	defer rollback(tx)
	loginName := make([]string, 0, 2)
	err = tx.SelectContext(ctx, &loginName, tx.Rebind(`
		SELECT login_name
		FROM "user"
		WHERE LOWER(display_name) = LOWER(?)
		ORDER BY id
		LIMIT 2
	`), displayName)
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadDisplaySelect: %w", err)
	}
	if len(loginName) == 0 {
		return sporenet.UserRecord{}, fmt.Errorf("loadDisplayMissing: %w", sporenet.ErrUserNotFound)
	}
	if len(loginName) != 1 {
		return sporenet.UserRecord{}, fmt.Errorf("loadDisplayDuplicate: %w", sporenet.ErrUserAmbiguous)
	}
	record, err := loadRecord(ctx, tx, loginName[0])
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadDisplayRecord: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return sporenet.UserRecord{}, fmt.Errorf("loadDisplayCommit: %w", err)
	}
	return record, nil
}

// ListIdentities returns known profiles without loading their gameplay aggregates.
func (e *Repository) ListIdentities(ctx context.Context) ([]sporenet.UserIdentity, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows := make([]userIdentityRow, 0)
	err := e.db.SelectContext(ctx, &rows, e.db.Rebind(`
		SELECT login_name, display_name, avatar_id, level, xp, chain_progression,
			create_dt, last_connection_dt, onboarding_progress,
			is_tutorial_completion_pending
		FROM "user"
		ORDER BY display_name, login_name
	`))
	if err != nil {
		return nil, fmt.Errorf("identitySelect: %w", err)
	}
	identities := make([]sporenet.UserIdentity, 0, len(rows))
	for _, row := range rows {
		createDT, parseErr := time.Parse(time.RFC3339Nano, row.CreateDT)
		if parseErr != nil {
			return nil, fmt.Errorf("identityCreateDT: %w", parseErr)
		}
		lastConnectionDT := time.Time{}
		if row.LastConnectionDT != "" {
			lastConnectionDT, parseErr = time.Parse(time.RFC3339Nano, row.LastConnectionDT)
			if parseErr != nil {
				return nil, fmt.Errorf("identityLastConnectionDT: %w", parseErr)
			}
		}
		identities = append(identities, sporenet.UserIdentity{
			LoginName: row.LoginName, DisplayName: row.DisplayName, AvatarID: row.AvatarID,
			CreateDT: createDT, LastConnectionDT: lastConnectionDT,
			Level: row.Level, XP: row.XP, ChainProgression: row.ChainProgression,
			IsTutorialCompleted: sporenet.Account{
				OnboardingProgress: row.OnboardingProgress,
			}.IsTutorialCompleted(),
			IsTutorialCompletionPending: row.IsTutorialCompletionPending != 0,
		})
	}
	return identities, nil
}

// RecordConnection records when a launcher accepted a game launch for a profile.
func (e *Repository) RecordConnection(ctx context.Context, loginName string, connectionDT time.Time) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if strings.TrimSpace(loginName) == "" || connectionDT.IsZero() {
		return fmt.Errorf("connectionRecord: %w", sporenet.ErrInvalidUser)
	}
	result, err := e.db.ExecContext(ctx, e.db.Rebind(`
		UPDATE "user" SET last_connection_dt = ? WHERE LOWER(login_name) = LOWER(?)`),
		connectionDT.UTC().Format(time.RFC3339Nano), loginName)
	if err != nil {
		return fmt.Errorf("connectionUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("connectionAffected: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("connectionMissing: %w", sporenet.ErrUserNotFound)
	}
	return nil
}

// PendingTutorialCompletionLoginNames returns durable deferred tutorial work
// in stable profile order.
func (e *Repository) PendingTutorialCompletionLoginNames(
	ctx context.Context,
) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	loginNames := make([]string, 0)
	err := e.db.SelectContext(ctx, &loginNames, e.db.Rebind(`
		SELECT login_name
		FROM "user"
		WHERE is_tutorial_completion_pending = 1
		ORDER BY id
	`))
	if err != nil {
		return nil, fmt.Errorf("tutorialCompletionSelect: %w", err)
	}
	return loginNames, nil
}

// Save atomically replaces every durable child of an existing profile.
func (e *Repository) Save(ctx context.Context, record sporenet.UserRecord) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if record.LoginName == "" {
		return fmt.Errorf("saveRecord: %w", sporenet.ErrInvalidUser)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("saveBegin: %w", err)
	}
	defer rollback(tx)
	result, err := tx.NamedExecContext(ctx, updateUserQuery, userRowFromRecord(record))
	if err != nil {
		return fmt.Errorf("saveUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("saveAffected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("saveMissing: %w", sporenet.ErrUserNotFound)
	}
	err = clearDependents(ctx, tx, record.Account.ID)
	if err != nil {
		return fmt.Errorf("saveClear: %w", err)
	}
	err = writeDependents(ctx, tx, record)
	if err != nil {
		return fmt.Errorf("saveWrite: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("saveCommit: %w", err)
	}
	return nil
}

func (e *Repository) initializeSchema(ctx context.Context) error {
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("schemaBegin: %w", err)
	}
	defer rollback(tx)
	for index, statement := range schemaStatements {
		_, err = tx.ExecContext(ctx, tx.Rebind(statement))
		if err != nil {
			return fmt.Errorf("schemaStep[%d]: %w", index, err)
		}
	}
	err = ensurePartIdentityColumns(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaPartIdentity: %w", err)
	}
	err = ensureTutorialCompletionColumn(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaTutorialCompletion: %w", err)
	}
	err = ensureUserCreateDTColumn(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaUserCreateDT: %w", err)
	}
	err = ensureUserLastConnectionDTColumn(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaUserLastConnectionDT: %w", err)
	}
	err = ensureOverdriveUnlockColumn(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaOverdriveUnlock: %w", err)
	}
	err = ensureLimitedEditionColumns(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaLimitedEdition: %w", err)
	}
	err = normalizeInventoryCapacity(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaInventoryCapacity: %w", err)
	}
	err = normalizeAccessEntitlement(ctx, tx)
	if err != nil {
		return fmt.Errorf("schemaAccessEntitlement: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("schemaCommit: %w", err)
	}
	return nil
}

func ensureLimitedEditionColumns(ctx context.Context, tx *sqlx.Tx) error {
	columns := []struct {
		name string
	}{
		{name: "limited_edition_miss_count"},
		{name: "limited_edition_used_mask"},
	}
	for index, column := range columns {
		var count int
		err := tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM pragma_table_info('user') WHERE name = ?`), column.name)
		if err != nil {
			return fmt.Errorf("columnCheck[%d]: %w", index, err)
		}
		if count != 0 {
			continue
		}
		_, err = tx.ExecContext(ctx, tx.Rebind(`ALTER TABLE "user" ADD COLUMN `+column.name+
			` INTEGER NOT NULL DEFAULT 0`))
		if err != nil {
			return fmt.Errorf("columnAdd[%d]: %w", index, err)
		}
	}
	return nil
}

func ensureUserLastConnectionDTColumn(ctx context.Context, tx *sqlx.Tx) error {
	var count int
	err := tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM pragma_table_info('user') WHERE name = 'last_connection_dt'`))
	if err != nil {
		return fmt.Errorf("columnCheck: %w", err)
	}
	if count != 0 {
		return nil
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`
		ALTER TABLE "user"
		ADD COLUMN last_connection_dt TEXT NOT NULL DEFAULT ''`))
	if err != nil {
		return fmt.Errorf("columnAdd: %w", err)
	}
	if result == nil {
		return errors.New("columnAdd: missing result")
	}
	return nil
}

func ensureUserCreateDTColumn(ctx context.Context, tx *sqlx.Tx) error {
	var count int
	err := tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM pragma_table_info('user') WHERE name = 'create_dt'`))
	if err != nil {
		return fmt.Errorf("columnCheck: %w", err)
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, tx.Rebind(`
			ALTER TABLE "user"
			ADD COLUMN create_dt TEXT NOT NULL DEFAULT ''`))
		if err != nil {
			return fmt.Errorf("columnAdd: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, tx.Rebind(`
		UPDATE "user"
		SET create_dt = ?
		WHERE create_dt = ''`), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("legacyTimestamp: %w", err)
	}
	return nil
}

func ensureOverdriveUnlockColumn(ctx context.Context, tx *sqlx.Tx) error {
	var count int
	err := tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM pragma_table_info('user') WHERE name = 'is_overdrive_unlocked'`))
	if err != nil {
		return fmt.Errorf("columnCheck: %w", err)
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, tx.Rebind(`
			ALTER TABLE "user"
			ADD COLUMN is_overdrive_unlocked INTEGER NOT NULL DEFAULT 0`))
		if err != nil {
			return fmt.Errorf("columnAdd: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, tx.Rebind(`
		UPDATE "user"
		SET is_overdrive_unlocked = 1
		WHERE chain_progression >= 5 AND is_overdrive_unlocked = 0`))
	if err != nil {
		return fmt.Errorf("legacyUnlock: %w", err)
	}
	return nil
}

func ensureTutorialCompletionColumn(ctx context.Context, tx *sqlx.Tx) error {
	var count int
	err := tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM pragma_table_info('user') WHERE name = 'is_tutorial_completion_pending'`))
	if err != nil {
		return fmt.Errorf("columnCheck: %w", err)
	}
	if count != 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, tx.Rebind(`
		ALTER TABLE "user"
		ADD COLUMN is_tutorial_completion_pending INTEGER NOT NULL DEFAULT 0`))
	if err != nil {
		return fmt.Errorf("columnAdd: %w", err)
	}
	return nil
}

func normalizeAccessEntitlement(ctx context.Context, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, tx.Rebind(`
		UPDATE "user"
		SET is_all_access_granted = 1, is_online_access_granted = 1
		WHERE is_all_access_granted = 0 OR is_online_access_granted = 0`))
	if err != nil {
		return fmt.Errorf("entitlementUpdate: %w", err)
	}
	return nil
}

func normalizeInventoryCapacity(ctx context.Context, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE "user" SET unlock_inventory_identify = 180 WHERE unlock_inventory_identify = 0`))
	if err != nil {
		return fmt.Errorf("capacityUpdate: %w", err)
	}
	return nil
}

func ensurePartIdentityColumns(ctx context.Context, tx *sqlx.Tx) error {
	columns := []struct {
		name       string
		definition string
	}{
		{name: "item_id", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "reference_id", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "buyback_expires_at", definition: "INTEGER NOT NULL DEFAULT 0"},
	}
	for index, column := range columns {
		var count int
		err := tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM pragma_table_info('part') WHERE name = ?`), column.name)
		if err != nil {
			return fmt.Errorf("columnCheck[%d]: %w", index, err)
		}
		if count != 0 {
			continue
		}
		_, err = tx.ExecContext(ctx, tx.Rebind("ALTER TABLE part ADD COLUMN "+column.name+" "+column.definition))
		if err != nil {
			return fmt.Errorf("columnAdd[%d]: %w", index, err)
		}
	}
	return nil
}

func normalizeOptions(options Options) Options {
	if options.MaxOpenConnections <= 0 {
		options.MaxOpenConnections = defaultMaxOpenConnections
	}
	if options.MaxIdleConnections < 0 {
		options.MaxIdleConnections = 0
	}
	if options.MaxIdleConnections == 0 {
		options.MaxIdleConnections = defaultMaxIdleConnections
	}
	if options.MaxIdleConnections > options.MaxOpenConnections {
		options.MaxIdleConnections = options.MaxOpenConnections
	}
	if options.BusyTimeout <= 0 {
		options.BusyTimeout = defaultBusyTimeout
	}
	return options
}

func sqliteDSN(path string, busyTimeout time.Duration) string {
	parameters := url.Values{}
	parameters.Add("_pragma", "busy_timeout("+strconv.FormatInt(busyTimeout.Milliseconds(), 10)+")")
	parameters.Add("_pragma", "foreign_keys(1)")
	parameters.Add("_pragma", "journal_mode(WAL)")
	parameters.Add("_pragma", "synchronous(NORMAL)")
	return "file:" + filepath.ToSlash(path) + "?" + parameters.Encode()
}
